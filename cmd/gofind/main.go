package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/TetsWorks/gofind/internal/fuzzy"
	"github.com/TetsWorks/gofind/internal/indexer"
	"github.com/TetsWorks/gofind/internal/storage"
	"github.com/TetsWorks/gofind/internal/tfidf"
	"github.com/TetsWorks/gofind/internal/tui"
	"github.com/TetsWorks/gofind/internal/watcher"
)

const Version = "0.1.0"

var (
	flagIndex   = flag.String("index", "", "Indexa um diretório")
	flagSearch  = flag.String("search", "", "Busca uma query (sem TUI)")
	flagFuzzy   = flag.Bool("fuzzy", false, "Ativa busca fuzzy")
	flagWatch   = flag.Bool("watch", false, "Ativa watch mode")
	flagExport  = flag.Bool("json", false, "Exporta resultado em JSON")
	flagMax     = flag.Int("n", 20, "Número máximo de resultados")
	flagData    = flag.String("data", defaultDataDir(), "Diretório de dados do índice")
	flagClear   = flag.Bool("clear", false, "Limpa o índice")
	flagStats   = flag.Bool("stats", false, "Mostra estatísticas do índice")
	flagVersion = flag.Bool("version", false, "Versão")
)

func main() {
	flag.Usage = usage
	flag.Parse()

	if *flagVersion {
		fmt.Printf("gofind v%s\n", Version)
		return
	}

	store, err := storage.New(*flagData)
	if err != nil {
		fatalf("erro ao criar storage: %v", err)
	}

	idx := indexer.New()
	idxr := indexer.NewIndexer(idx, 8)

	if store.Exists() {
		if err := store.Load(idx); err != nil {
			fmt.Fprintf(os.Stderr, "aviso: erro ao carregar índice: %v\n", err)
		}
	}

	if *flagClear {
		store.Clear()
		fmt.Println("✓ Índice limpo.")
		return
	}

	if *flagStats {
		printStats(idx, store)
		return
	}

	if *flagIndex != "" {
		indexDir(idx, idxr, store, *flagIndex)
		return
	}

	if args := flag.Args(); len(args) > 0 {
		arg := args[0]
		if info, err := os.Stat(arg); err == nil && info.IsDir() {
			indexDir(idx, idxr, store, arg)
			return
		}
		*flagSearch = strings.Join(args, " ")
	}

	if *flagSearch != "" {
		searchCLI(idx, *flagSearch)
		return
	}

	if idx.DocCount() == 0 {
		fmt.Println("Índice vazio. Use: gofind <diretório>")
		fmt.Println("Exemplo:           gofind ~")
		os.Exit(1)
	}

	var wtch *watcher.Watcher
	if *flagWatch {
		wtch, err = watcher.New(idxr)
		if err == nil {
			for _, dir := range idx.Stats().Directories {
				wtch.Watch(dir)
			}
			wtch.Start()
			defer wtch.Close()
		}
	}

	app := tui.New(idx, idxr, wtch)
	if err := app.Run(); err != nil {
		fatalf("TUI: %v", err)
	}
}

func indexDir(idx *indexer.Index, idxr *indexer.Indexer, store *storage.Store, dir string) {
	dir, err := filepath.Abs(dir)
	if err != nil {
		fatalf("path inválido: %v", err)
	}

	fmt.Printf("⚡ Indexando [%s]...\n", dir)
	start := time.Now()

	// Indexa em goroutine
	done := make(chan struct{})
	go func() {
		defer close(done)
		if err := idxr.IndexDir(dir); err != nil {
			fmt.Fprintf(os.Stderr, "erro: %v\n", err)
		}
	}()

	// Consome progresso no terminal (sem tview)
	for p := range idxr.Progress() {
		if p.Total > 0 {
			pct := p.Done * 100 / p.Total
			filled := pct / 5
			bar := strings.Repeat("█", filled) + strings.Repeat("░", 20-filled)
			name := p.Current
			if len(name) > 28 {
				name = name[:25] + "..."
			}
			fmt.Printf("\r  [%s] %3d%%  %d/%d  %-28s",
				bar, pct, p.Done, p.Total, name)
		}
		if p.Finish {
			break
		}
	}
	<-done

	elapsed := time.Since(start)
	stats := idx.Stats()
	fmt.Printf("\r\033[K") // limpa linha
	fmt.Printf("✓ %d arquivos indexados em %s\n", stats.TotalDocs, elapsed.Round(time.Millisecond))
	fmt.Printf("  Tokens: %d  |  Tamanho total: %s\n", stats.TotalTokens, formatSize(stats.TotalSize))

	fmt.Printf("  Salvando índice...")
	if err := store.Save(idx); err != nil {
		fmt.Fprintf(os.Stderr, "\naviso: erro ao salvar: %v\n", err)
	} else {
		fmt.Printf(" ✓\n")
	}
	fmt.Println("  Pronto! Use 'gofind' para buscar.")
}

func searchCLI(idx *indexer.Index, query string) {
	start := time.Now()
	var results []*indexer.SearchResult

	if *flagFuzzy {
		fz := fuzzy.New(idx)
		results = fz.Search(query, 2)
	} else {
		engine := tfidf.New(idx)
		docs := idx.Search(query)
		if len(docs) == 0 {
			docs = idx.SearchOR(query)
		}
		results = engine.Rank(query, docs)
	}

	elapsed := time.Since(start)

	if len(results) > *flagMax {
		results = results[:*flagMax]
	}

	if len(results) == 0 {
		fmt.Printf("Nenhum resultado para %q\n", query)
		if !*flagFuzzy {
			fmt.Println("Tente com -fuzzy para busca aproximada")
		}
		return
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})

	if *flagExport {
		exportResults(query, results)
		return
	}

	fmt.Printf("\n[%d resultados em %s]\n\n", len(results), elapsed.Round(time.Microsecond))
	fmt.Printf("%-8s %-8s %-8s %s\n", "SCORE", "TIPO", "PALAVRAS", "PATH")
	fmt.Println(strings.Repeat("─", 80))
	for _, r := range results {
		fmt.Printf("%-8.4f %-8s %-8d %s\n",
			r.Score, r.Doc.Type, r.Doc.WordCount, r.Doc.Path)
	}
}

func printStats(idx *indexer.Index, store *storage.Store) {
	stats := idx.Stats()
	fmt.Printf("\n📊 Estatísticas do índice\n")
	fmt.Printf("  Documentos:  %d\n", stats.TotalDocs)
	fmt.Printf("  Tokens:      %d\n", stats.TotalTokens)
	fmt.Printf("  Tamanho:     %s\n", formatSize(stats.TotalSize))
	if !stats.LastIndexed.IsZero() {
		fmt.Printf("  Indexado em: %s\n", stats.LastIndexed.Format("02/01/2006 15:04:05"))
	}
	for _, d := range stats.Directories {
		fmt.Printf("  Dir: %s\n", d)
	}
	if store.Exists() {
		t, size, err := store.Info()
		if err == nil {
			fmt.Printf("  Arquivo:     %s (%s)\n", t.Format("02/01 15:04"), formatSize(size))
		}
	}
}

func exportResults(query string, results []*indexer.SearchResult) {
	type item struct {
		Rank  int     `json:"rank"`
		Score float64 `json:"score"`
		Path  string  `json:"path"`
		Type  string  `json:"type"`
		Words int     `json:"words"`
		Size  int64   `json:"size"`
	}
	var items []item
	for i, r := range results {
		items = append(items, item{i + 1, r.Score, r.Doc.Path,
			string(r.Doc.Type), r.Doc.WordCount, r.Doc.Size})
	}
	out := map[string]interface{}{
		"query": query, "total": len(items),
		"exported_at": time.Now(), "results": items,
	}
	data, _ := json.MarshalIndent(out, "", "  ")
	fname := fmt.Sprintf("gofind_%d.json", time.Now().Unix())
	os.WriteFile(fname, data, 0644)
	fmt.Printf("✓ Exportado: %s (%d resultados)\n", fname, len(items))
}

func defaultDataDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ".gofind"
	}
	return filepath.Join(home, ".gofind")
}

func formatSize(n int64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.1fGB", float64(n)/(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.1fMB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1fKB", float64(n)/(1<<10))
	default:
		return fmt.Sprintf("%dB", n)
	}
}

func fatalf(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, "gofind: "+format+"\n", args...)
	os.Exit(1)
}

func usage() {
	fmt.Print(`GoFind v` + Version + ` — Motor de busca local

USO:
  gofind <diretório>              Indexa e abre TUI
  gofind -search "query"          Busca pela linha de comando
  gofind -search "query" -fuzzy   Busca fuzzy
  gofind -stats                   Estatísticas do índice
  gofind -clear                   Limpa o índice

FLAGS:
  -index  <dir>    Indexa um diretório
  -search <query>  Busca sem TUI
  -fuzzy           Busca aproximada (Levenshtein)
  -watch           Re-indexa ao detectar mudanças
  -json            Exporta resultados em JSON
  -n <num>         Máx de resultados (padrão: 20)
  -data   <dir>    Dir do índice (padrão: ~/.gofind)
  -clear           Limpa o índice
  -stats           Estatísticas
  -version         Versão

`)
}
