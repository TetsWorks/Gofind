package tui

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/TetsWorks/gofind/internal/fuzzy"
	"github.com/TetsWorks/gofind/internal/indexer"
	"github.com/TetsWorks/gofind/internal/tfidf"
	"github.com/TetsWorks/gofind/internal/watcher"
)

const maxResults = 200

// App é a TUI do gofind
type App struct {
	app       *tview.Application
	idx       *indexer.Index
	idxr      *indexer.Indexer
	tfidfEng  *tfidf.Engine
	fuzzyEng  *fuzzy.Searcher
	wtch      *watcher.Watcher

	// Widgets
	searchBox  *tview.InputField
	resultList *tview.List
	previewBox *tview.TextView
	statusBar  *tview.TextView
	statsBox   *tview.TextView

	// Estado
	results    []*indexer.SearchResult
	fuzzyMode  bool
	watchMode  bool
}

// New cria a TUI
func New(idx *indexer.Index, idxr *indexer.Indexer, wtch *watcher.Watcher) *App {
	a := &App{
		app:      tview.NewApplication(),
		idx:      idx,
		idxr:     idxr,
		tfidfEng: tfidf.New(idx),
		fuzzyEng: fuzzy.New(idx),
		wtch:     wtch,
	}
	a.build()
	return a
}

func (a *App) build() {
	// ── Search box ──────────────────────────────────────────────────────────
	a.searchBox = tview.NewInputField().
		SetLabel("  🔍 ").
		SetFieldBackgroundColor(tcell.ColorDarkBlue).
		SetFieldTextColor(tcell.ColorWhite).
		SetLabelColor(tcell.ColorAqua).
		SetPlaceholder("Digite para buscar... (Tab=fuzzy, Ctrl+W=watch, Ctrl+E=export)").
		SetPlaceholderTextColor(tcell.ColorGray)

	a.searchBox.SetBorder(true).
		SetTitle(" GoFind ").
		SetTitleColor(tcell.ColorAqua).
		SetBorderColor(tcell.ColorDarkBlue)

	a.searchBox.SetChangedFunc(func(text string) {
		a.search(text)
	})

	a.searchBox.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Key() {
		case tcell.KeyTab:
			a.fuzzyMode = !a.fuzzyMode
			a.updateStatus()
			a.search(a.searchBox.GetText())
			return nil
		case tcell.KeyCtrlW:
			a.toggleWatch()
			return nil
		case tcell.KeyCtrlE:
			a.exportJSON()
			return nil
		case tcell.KeyDown:
			a.app.SetFocus(a.resultList)
			return nil
		case tcell.KeyEscape:
			a.app.Stop()
			return nil
		}
		return event
	})

	// ── Result list ─────────────────────────────────────────────────────────
	a.resultList = tview.NewList().
		ShowSecondaryText(true).
		SetHighlightFullLine(true).
		SetSelectedBackgroundColor(tcell.ColorDarkBlue).
		SetSelectedTextColor(tcell.ColorAqua)

	a.resultList.SetBorder(true).
		SetTitle(" Resultados ").
		SetTitleColor(tcell.ColorAqua).
		SetBorderColor(tcell.ColorDarkBlue)

	a.resultList.SetChangedFunc(func(idx int, main, secondary string, shortcut rune) {
		a.showPreview(idx)
	})

	a.resultList.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Key() {
		case tcell.KeyEscape, tcell.KeyBacktab:
			a.app.SetFocus(a.searchBox)
			return nil
		case tcell.KeyEnter:
			a.openFile()
			return nil
		}
		return event
	})

	// ── Preview ─────────────────────────────────────────────────────────────
	a.previewBox = tview.NewTextView().
		SetDynamicColors(true).
		SetScrollable(true).
		SetWordWrap(true)

	a.previewBox.SetBorder(true).
		SetTitle(" Preview ").
		SetTitleColor(tcell.ColorAqua).
		SetBorderColor(tcell.ColorDarkBlue)

	// ── Stats ────────────────────────────────────────────────────────────────
	a.statsBox = tview.NewTextView().
		SetDynamicColors(true)

	a.statsBox.SetBorder(true).
		SetTitle(" Índice ").
		SetTitleColor(tcell.ColorYellow).
		SetBorderColor(tcell.ColorDarkBlue)

	a.updateStats()

	// ── Status bar ───────────────────────────────────────────────────────────
	a.statusBar = tview.NewTextView().
		SetDynamicColors(true).
		SetText(a.statusText())

	// ── Layout ───────────────────────────────────────────────────────────────
	// Left panel: search + results
	leftPanel := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(a.searchBox, 3, 0, true).
		AddItem(a.resultList, 0, 1, false)

	// Right panel: preview + stats
	rightPanel := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(a.previewBox, 0, 3, false).
		AddItem(a.statsBox, 8, 0, false)

	// Main layout
	main := tview.NewFlex().
		AddItem(leftPanel, 0, 1, true).
		AddItem(rightPanel, 0, 1, false)

	root := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(main, 0, 1, true).
		AddItem(a.statusBar, 1, 0, false)

	a.app.SetRoot(root, true)

	// Watch mode: atualiza stats quando muda
	if a.wtch != nil {
		go func() {
			for range a.wtch.Events() {
				a.app.QueueUpdateDraw(func() {
					a.updateStats()
					a.search(a.searchBox.GetText())
				})
			}
		}()
	}
}

func (a *App) search(query string) {
	a.resultList.Clear()
	a.previewBox.Clear()
	a.results = nil

	query = strings.TrimSpace(query)
	if len(query) < 2 {
		a.updateStatus()
		return
	}

	start := time.Now()
	var results []*indexer.SearchResult

	if a.fuzzyMode {
		results = a.fuzzyEng.Search(query, 2)
	} else {
		// Exact: AND search + TF-IDF ranking
		docs := a.idx.Search(query)
		if len(docs) == 0 {
			// fallback OR
			docs = a.idx.SearchOR(query)
		}
		results = a.tfidfEng.Rank(query, docs)
	}

	elapsed := time.Since(start)

	if len(results) > maxResults {
		results = results[:maxResults]
	}
	a.results = results

	for _, r := range results {
		name := filepath.Base(r.Doc.Path)
		dir := filepath.Dir(r.Doc.Path)
		score := fmt.Sprintf("%.4f", r.Score)
		mtype := string(r.MatchType)
		label := fmt.Sprintf("[aqua]%s[white]  [gray]%s", name, score)
		secondary := fmt.Sprintf("[darkgray]%s  [yellow]%s", dir, mtype)
		a.resultList.AddItem(label, secondary, 0, nil)
	}

	status := fmt.Sprintf("  [aqua]%d[white] resultados em [yellow]%s[white]", len(results), elapsed.Round(time.Microsecond))
	if a.fuzzyMode {
		status += "  [yellow][FUZZY][white]"
	}
	if a.watchMode {
		status += "  [green][WATCH][white]"
	}
	a.statusBar.SetText(status)

	if len(results) > 0 {
		a.showPreview(0)
	}
}

func (a *App) showPreview(idx int) {
	if idx < 0 || idx >= len(a.results) {
		a.previewBox.Clear()
		return
	}
	r := a.results[idx]
	doc := r.Doc

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("[aqua]%s[white]\n", doc.Path))
	sb.WriteString(fmt.Sprintf("[gray]Tipo: [yellow]%s[gray]  Tamanho: [yellow]%s[gray]  Palavras: [yellow]%d[white]\n",
		doc.Type, formatSize(doc.Size), doc.WordCount))
	sb.WriteString(fmt.Sprintf("[gray]Score TF-IDF: [green]%.6f[gray]  Match: [yellow]%s[white]\n\n", r.Score, r.MatchType))

	// Mostra trecho do conteúdo com highlight do termo
	query := a.searchBox.GetText()
	preview := getPreviewWithHighlight(doc.Content, query, 20)
	sb.WriteString(preview)

	a.previewBox.SetText(sb.String())
	a.previewBox.ScrollToBeginning()
}

func getPreviewWithHighlight(content, query string, maxLines int) string {
	if content == "" {
		return "[gray](sem conteúdo)[white]"
	}
	lines := strings.Split(content, "\n")
	queryLower := strings.ToLower(query)
	queryTerms := strings.Fields(queryLower)

	var matchLines []string
	var otherLines []string

	for _, line := range lines {
		lineLower := strings.ToLower(line)
		hasMatch := false
		for _, term := range queryTerms {
			if strings.Contains(lineLower, term) {
				hasMatch = true
				break
			}
		}
		if hasMatch {
			// Highlight
			highlighted := line
			for _, term := range queryTerms {
				idx := strings.Index(strings.ToLower(highlighted), term)
				if idx >= 0 {
					highlighted = highlighted[:idx] + "[yellow]" + highlighted[idx:idx+len(term)] + "[white]" + highlighted[idx+len(term):]
				}
			}
			matchLines = append(matchLines, "  "+highlighted)
		} else if len(line) > 0 && len(otherLines) < 3 {
			otherLines = append(otherLines, "[darkgray]  "+line+"[white]")
		}
	}

	var result []string
	if len(matchLines) > 0 {
		result = append(result, "[gray]── Ocorrências ──[white]")
		limit := maxLines
		if len(matchLines) < limit { limit = len(matchLines) }
		result = append(result, matchLines[:limit]...)
	} else {
		result = append(result, "[gray]── Início do arquivo ──[white]")
		limit := maxLines
		if len(lines) < limit { limit = len(lines) }
		for _, l := range lines[:limit] {
			result = append(result, "[darkgray]  "+l+"[white]")
		}
	}
	return strings.Join(result, "\n")
}

func (a *App) openFile() {
	idx := a.resultList.GetCurrentItem()
	if idx < 0 || idx >= len(a.results) {
		return
	}
	path := a.results[idx].Doc.Path
	// Tenta abrir com xdg-open / open
	a.statusBar.SetText(fmt.Sprintf("  [green]Abrindo: %s[white]", path))
}

func (a *App) toggleWatch() {
	a.watchMode = !a.watchMode
	a.updateStatus()
}

func (a *App) exportJSON() {
	query := a.searchBox.GetText()
	if len(a.results) == 0 {
		a.statusBar.SetText("  [red]Nenhum resultado para exportar[white]")
		return
	}
	type exportItem struct {
		Path      string  `json:"path"`
		Score     float64 `json:"score"`
		MatchType string  `json:"match_type"`
		Type      string  `json:"type"`
		Size      int64   `json:"size"`
		Words     int     `json:"words"`
	}
	var items []exportItem
	for _, r := range a.results {
		items = append(items, exportItem{
			Path: r.Doc.Path, Score: r.Score,
			MatchType: string(r.MatchType), Type: string(r.Doc.Type),
			Size: r.Doc.Size, Words: r.Doc.WordCount,
		})
	}
	export := map[string]interface{}{
		"query":      query,
		"total":      len(items),
		"exported_at": time.Now(),
		"results":    items,
	}
	fname := fmt.Sprintf("gofind_results_%d.json", time.Now().Unix())
	data, _ := json.MarshalIndent(export, "", "  ")
	os.WriteFile(fname, data, 0644)
	a.statusBar.SetText(fmt.Sprintf("  [green]Exportado: %s[white]", fname))
}

func (a *App) updateStats() {
	stats := a.idx.Stats()
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf(" [aqua]Docs:[white] %d   [aqua]Tokens:[white] %d   [aqua]Tamanho:[white] %s\n",
		stats.TotalDocs, stats.TotalTokens, formatSize(stats.TotalSize)))
	if !stats.LastIndexed.IsZero() {
		sb.WriteString(fmt.Sprintf(" [gray]Último índice: %s[white]\n", stats.LastIndexed.Format("02/01 15:04:05")))
	}
	if len(stats.Directories) > 0 {
		sb.WriteString(" [gray]Dirs:[white]\n")
		shown := stats.Directories
		if len(shown) > 3 { shown = shown[:3] }
		for _, d := range shown {
			if len(d) > 40 { d = "..." + d[len(d)-37:] }
			sb.WriteString(fmt.Sprintf("  [yellow]%s[white]\n", d))
		}
	}
	a.statsBox.SetText(sb.String())
}

func (a *App) updateStatus() {
	a.statusBar.SetText(a.statusText())
}

func (a *App) statusText() string {
	mode := "[gray]EXACT[white]"
	if a.fuzzyMode { mode = "[yellow]FUZZY[white]" }
	watch := ""
	if a.watchMode { watch = "  [green]WATCH[white]" }
	return fmt.Sprintf("  %s%s  [gray]Tab=modo  ↓=lista  Enter=abrir  Ctrl+E=export  Esc=sair[white]", mode, watch)
}

// Run inicia a TUI
func (a *App) Run() error {
	return a.app.Run()
}

// ─── Progress TUI ─────────────────────────────────────────────────────────────

// ShowProgress exibe uma barra de progresso simples no terminal
func ShowProgress(progress <-chan indexer.Progress, dir string) {
	app := tview.NewApplication()
	bar := tview.NewTextView().SetDynamicColors(true)
	bar.SetBorder(true).SetTitle(fmt.Sprintf(" Indexando %s ", dir)).SetBorderColor(tcell.ColorDarkBlue)

	app.SetRoot(bar, true)

	go func() {
		for p := range progress {
			p := p
			app.QueueUpdateDraw(func() {
				pct := 0
				if p.Total > 0 {
					pct = p.Done * 100 / p.Total
				}
				filled := pct / 5
				empty := 20 - filled
				barStr := strings.Repeat("█", filled) + strings.Repeat("░", empty)
				text := fmt.Sprintf("\n  [aqua]%s[white]  %d%%\n\n  [gray]%d / %d arquivos[white]\n\n  [yellow]%s[white]",
					barStr, pct, p.Done, p.Total, p.Current)
				if p.Errors > 0 {
					text += fmt.Sprintf("\n\n  [red]Erros: %d[white]", p.Errors)
				}
				bar.SetText(text)
			})
			if p.Finish {
				time.Sleep(500 * time.Millisecond)
				app.Stop()
				return
			}
		}
		app.Stop()
	}()

	app.Run()
}

// SortResults ordena resultados
func SortResults(results []*indexer.SearchResult) {
	sort.Slice(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})
}

func formatSize(n int64) string {
	switch {
	case n >= 1<<30: return fmt.Sprintf("%.1fGB", float64(n)/(1<<30))
	case n >= 1<<20: return fmt.Sprintf("%.1fMB", float64(n)/(1<<20))
	case n >= 1<<10: return fmt.Sprintf("%.1fKB", float64(n)/(1<<10))
	default: return fmt.Sprintf("%dB", n)
	}
}
