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
	flagWatch   = flag.Bool("watch", false, "Ativa watch mode (re-indexa ao salvar)")
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

	// Inicializa índice e storage
	store, err := storage.New(*flagData)
	if err != nil {
		fatalf("erro ao criar storage: %v", err)
	}

	idx := indexer.New()
	idxr := indexer.NewIndexer(idx, 8)

	// Carrega índice existente do disco
	if store.Exists() {
		if err := store.Load(idx); err != nil {
			fmt.Fprintf(os.Stderr, "aviso: erro ao carregar índice: %v\n", err)
		}
	}

	// ── Comandos ─────────────────────────────────────────────────────────────

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

	// Se há args sem flags, trata como diretório (index) ou query (search)
	if args := flag.Args(); len(args) > 0 {
		// Se parece path, indexa; se não, busca
		arg := args[0]
		if info, err := os.Stat(arg); err == nil && info.IsDir() {
			indexDir(idx, idxr, store, arg)
			return
		}
		// Trata como query de busca sem TUI
		*flagSearch = strings.Join(args, " ")
	}

	if *flagSearch != "" {
		searchCLI(idx, *flagSearch)
		return
	}

	// ── TUI interativa ────────────────────────────────────────────────────────
	if idx.DocCount() == 0 {
		fmt.Println("Índice vazio. Use: gofind <diretório>")
		fmt.Println("                   gofind -index <diretório>")
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

	// Progresso com TUI
	go func() {
		if err := idxr.IndexDir(dir); err != nil {
			fmt.Fprintf(os.Stderr, "erro: %v\n", err)
		}
	}()

	tui.ShowProgress(idxr.Progress(), dir)

	elapsed := time.Since(start)
	stats := idx.Stats()
	fmt.Printf("\n✓ %d arquivos indexados em %s\n", stats.TotalDocs, elapsed.Round(time.Millisecond))
	fmt.Printf("  Tokens: %d  |  Tamanho total: %s\n", stats.TotalTokens, formatSize(stats.TotalSize))

	// Salva em disco
	if err := store.Save(idx); err != nil {
		fmt.Fprintf(os.Stderr, "aviso: erro ao salvar índice: %v\n", err)
	} else {
		fmt.Printf("  Índice salvo em: %s\n", *flagData)
	}
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

	// Output tabular
	fmt.Printf("\n[%d resultados em %s]\n\n", len(results), elapsed.Round(time.Microsecond))
	fmt.Printf("%-6s %-8s %-8s %s\n", "SCORE", "TIPO", "PALAVRAS", "PATH")
	fmt.Println(strings.Repeat("─", 80))

	for _, r := range results {
		score := fmt.Sprintf("%.4f", r.Score)
		fmt.Printf("%-6s %-8s %-8d %s\n",
			score, r.Doc.Type, r.Doc.WordCount, r.Doc.Path)
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
	if len(stats.Directories) > 0 {
		fmt.Printf("  Diretórios:\n")
		for _, d := range stats.Directories {
			fmt.Printf("    %s\n", d)
		}
	}
	if store.Exists() {
		t, size, err := store.Info()
		if err == nil {
			fmt.Printf("  Índice em disco: %s (%s)\n", t.Format("02/01 15:04"), formatSize(size))
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
		Match string  `json:"match_type"`
	}
	var items []item
	for i, r := range results {
		items = append(items, item{
			Rank: i + 1, Score: r.Score, Path: r.Doc.Path,
			Type: string(r.Doc.Type), Words: r.Doc.WordCount,
			Size: r.Doc.Size, Match: string(r.MatchType),
		})
	}
	out := map[string]interface{}{
		"query":       query,
		"total":       len(items),
		"exported_at": time.Now(),
		"results":     items,
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
	case n >= 1<<30: return fmt.Sprintf("%.1fGB", float64(n)/(1<<30))
	case n >= 1<<20: return fmt.Sprintf("%.1fMB", float64(n)/(1<<20))
	case n >= 1<<10: return fmt.Sprintf("%.1fKB", float64(n)/(1<<10))
	default: return fmt.Sprintf("%dB", n)
	}
}

func fatalf(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, "gofind: "+format+"\n", args...)
	os.Exit(1)
}

func usage() {
	fmt.Print(`GoFind v` + Version + ` — Motor de busca local

USO:
  gofind <diretório>              Indexa um diretório e abre a TUI
  gofind -search "query"          Busca pela linha de comando
  gofind -search "query" -fuzzy   Busca fuzzy (Levenshtein)
  gofind -stats                   Mostra estatísticas do índice
  gofind -clear                   Limpa o índice

FLAGS:
  -index  <dir>    Indexa um diretório
  -search <query>  Busca sem abrir a TUI
  -fuzzy           Ativa busca aproximada
  -watch           Re-indexa ao detectar mudanças
  -json            Exporta resultados em JSON
  -n <num>         Máximo de resultados (padrão: 20)
  -data   <dir>    Diretório do índice (padrão: ~/.gofind)
  -clear           Limpa o índice
  -stats           Estatísticas
  -version         Versão

TUI:
  Tab              Alterna exact/fuzzy
  Ctrl+W           Ativa watch mode
  Ctrl+E           Exporta em JSON
  ↓                Move para lista de resultados
  Enter            Abre arquivo
  Esc              Sai

`)
}
