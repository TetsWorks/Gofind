package indexer

import (
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/TetsWorks/gofind/internal/extractor"
)

// PostingList lista de docs que contêm um termo
type PostingList struct {
	DocIDs    []uint64
	Freqs     map[uint64]int
	Positions map[uint64][]int
}

func newPosting() *PostingList {
	return &PostingList{Freqs: make(map[uint64]int), Positions: make(map[uint64][]int)}
}

func (pl *PostingList) add(docID uint64, pos int) {
	if pl.Freqs[docID] == 0 {
		pl.DocIDs = append(pl.DocIDs, docID)
	}
	pl.Freqs[docID]++
	pl.Positions[docID] = append(pl.Positions[docID], pos)
}

// Index é o índice invertido
type Index struct {
	mu       sync.RWMutex
	docs     map[uint64]*Document
	terms    map[string]*PostingList
	docTerms map[uint64]map[string]int
	docCount atomic.Int64
	nextID   atomic.Uint64
	Dirs     []string
}

func New() *Index {
	return &Index{
		docs:     make(map[uint64]*Document),
		terms:    make(map[string]*PostingList),
		docTerms: make(map[uint64]map[string]int),
	}
}

func (idx *Index) AddDocument(doc *Document) {
	idx.mu.Lock()
	defer idx.mu.Unlock()
	id := idx.nextID.Add(1)
	doc.ID = id
	tokens := Tokenize(doc.Content)
	doc.TokenCount = len(tokens)
	doc.WordCount = len(strings.Fields(doc.Content))
	idx.docs[id] = doc
	idx.docTerms[id] = make(map[string]int)
	for pos, token := range tokens {
		if idx.terms[token] == nil {
			idx.terms[token] = newPosting()
		}
		idx.terms[token].add(id, pos)
		idx.docTerms[id][token]++
	}
	// Nome do arquivo tem peso dobrado
	for pos, token := range Tokenize(doc.Name) {
		if idx.terms[token] == nil {
			idx.terms[token] = newPosting()
		}
		idx.terms[token].add(id, pos+1000000)
		idx.terms[token].add(id, pos+1000001)
		idx.docTerms[id][token] += 2
	}
	idx.docCount.Add(1)
}

func (idx *Index) RemoveDocument(path string) {
	idx.mu.Lock()
	defer idx.mu.Unlock()
	var docID uint64
	for id, doc := range idx.docs {
		if doc.Path == path {
			docID = id
			break
		}
	}
	if docID == 0 {
		return
	}
	for term := range idx.docTerms[docID] {
		if pl, ok := idx.terms[term]; ok {
			var newIDs []uint64
			for _, id := range pl.DocIDs {
				if id != docID {
					newIDs = append(newIDs, id)
				}
			}
			pl.DocIDs = newIDs
			delete(pl.Freqs, docID)
			delete(pl.Positions, docID)
		}
	}
	delete(idx.docs, docID)
	delete(idx.docTerms, docID)
	idx.docCount.Add(-1)
}

// Search AND: todos os termos devem estar presentes
func (idx *Index) Search(query string) []*Document {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	tokens := Tokenize(query)
	if len(tokens) == 0 {
		return nil
	}
	var candidates map[uint64]bool
	for _, token := range tokens {
		pl, ok := idx.terms[token]
		if !ok {
			return nil
		}
		if candidates == nil {
			candidates = make(map[uint64]bool)
			for _, id := range pl.DocIDs {
				candidates[id] = true
			}
		} else {
			next := make(map[uint64]bool)
			for _, id := range pl.DocIDs {
				if candidates[id] {
					next[id] = true
				}
			}
			candidates = next
		}
	}
	var result []*Document
	for id := range candidates {
		if doc, ok := idx.docs[id]; ok {
			result = append(result, doc)
		}
	}
	return result
}

// SearchOR: qualquer termo
func (idx *Index) SearchOR(query string) []*Document {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	tokens := Tokenize(query)
	seen := make(map[uint64]bool)
	var result []*Document
	for _, token := range tokens {
		pl, ok := idx.terms[token]
		if !ok {
			continue
		}
		for _, id := range pl.DocIDs {
			if !seen[id] {
				seen[id] = true
				if doc, ok := idx.docs[id]; ok {
					result = append(result, doc)
				}
			}
		}
	}
	return result
}

func (idx *Index) TF(term string, docID uint64) float64 {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	terms := idx.docTerms[docID]
	total := 0
	for _, f := range terms {
		total += f
	}
	if total == 0 {
		return 0
	}
	return float64(terms[term]) / float64(total)
}

func (idx *Index) IDF(term string) float64 {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	pl, ok := idx.terms[term]
	if !ok || len(pl.DocIDs) == 0 {
		return 0
	}
	n := float64(idx.docCount.Load())
	return math.Log(n/float64(len(pl.DocIDs))) + 1
}

func (idx *Index) AllTerms() []string {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	out := make([]string, 0, len(idx.terms))
	for t := range idx.terms {
		out = append(out, t)
	}
	return out
}

func (idx *Index) GetDoc(id uint64) (*Document, bool) {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	d, ok := idx.docs[id]
	return d, ok
}

func (idx *Index) AllDocs() []*Document {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	out := make([]*Document, 0, len(idx.docs))
	for _, d := range idx.docs {
		out = append(out, d)
	}
	return out
}

func (idx *Index) DocCount() int { return int(idx.docCount.Load()) }

func (idx *Index) Stats() IndexStats {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	var totalSize int64
	var totalTokens int
	var last time.Time
	for _, d := range idx.docs {
		totalSize += d.Size
		totalTokens += d.TokenCount
		if d.IndexedAt.After(last) {
			last = d.IndexedAt
		}
	}
	return IndexStats{TotalDocs: len(idx.docs), TotalTokens: totalTokens,
		TotalSize: totalSize, LastIndexed: last, Directories: idx.Dirs}
}

// ─── Indexer de arquivos ──────────────────────────────────────────────────────

type Progress struct {
	Total   int
	Done    int
	Current string
	Errors  int
	Finish  bool
}

type Indexer struct {
	Idx      *Index
	ext      *extractor.Extractor
	progress chan Progress
	workers  int
}

func NewIndexer(idx *Index, workers int) *Indexer {
	if workers <= 0 {
		workers = 4
	}
	return &Indexer{Idx: idx, ext: extractor.New(), progress: make(chan Progress, 200), workers: workers}
}

func (ix *Indexer) Progress() <-chan Progress { return ix.progress }

func (ix *Indexer) IndexDir(dir string) error {
	dir, err := filepath.Abs(dir)
	if err != nil {
		return err
	}
	var files []string
	filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			name := info.Name()
			if strings.HasPrefix(name, ".") || name == "node_modules" || name == "vendor" || name == "__pycache__" {
				return filepath.SkipDir
			}
			return nil
		}
		if ix.ext.Supported(path) {
			files = append(files, path)
		}
		return nil
	})
	total := len(files)
	ix.progress <- Progress{Total: total}
	jobs := make(chan string, len(files))
	for _, f := range files {
		jobs <- f
	}
	close(jobs)
	var wg sync.WaitGroup
	var done, errs atomic.Int64
	for i := 0; i < ix.workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for path := range jobs {
				if err := ix.IndexFile(path); err != nil {
					errs.Add(1)
				}
				n := int(done.Add(1))
				ix.progress <- Progress{Total: total, Done: n, Current: filepath.Base(path), Errors: int(errs.Load())}
			}
		}()
	}
	wg.Wait()
	ix.progress <- Progress{Total: total, Done: total, Finish: true}
	ix.Idx.mu.Lock()
	ix.Idx.Dirs = append(ix.Idx.Dirs, dir)
	ix.Idx.mu.Unlock()
	return nil
}

func (ix *Indexer) IndexFile(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	content, err := ix.ext.Extract(path)
	if err != nil || content == "" {
		return err
	}
	doc := &Document{
		Path:      path,
		Name:      filepath.Base(path),
		Type:      DocType(ix.ext.Type(path)),
		Size:      info.Size(),
		ModTime:   info.ModTime(),
		IndexedAt: time.Now(),
		Content:   content,
	}
	ix.Idx.AddDocument(doc)
	return nil
}

func (ix *Indexer) ReindexFile(path string) error {
	ix.Idx.RemoveDocument(path)
	return ix.IndexFile(path)
}

// ─── Tokenizer ────────────────────────────────────────────────────────────────

var reNonAlpha = regexp.MustCompile(`[^a-zA-Z0-9áéíóúàèìòùâêîôûãõçñÁÉÍÓÚÀÈÌÒÙÂÊÎÔÛÃÕÇÑ]+`)

var stopWords = map[string]bool{
	"o": true, "a": true, "os": true, "as": true, "um": true, "uma": true,
	"de": true, "do": true, "da": true, "dos": true, "das": true,
	"em": true, "no": true, "na": true, "nos": true, "nas": true,
	"por": true, "para": true, "com": true, "que": true, "se": true,
	"the": true, "an": true, "and": true, "or": true, "but": true,
	"in": true, "on": true, "at": true, "to": true, "for": true,
	"of": true, "with": true, "by": true, "is": true, "it": true,
	"be": true, "was": true, "are": true, "this": true, "not": true,
}

func Tokenize(text string) []string {
	text = strings.ToLower(text)
	parts := reNonAlpha.Split(text, -1)
	var tokens []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if len(p) < 2 || stopWords[p] {
			continue
		}
		tokens = append(tokens, Stem(p))
	}
	return tokens
}

func Stem(word string) string {
	suffixes := []string{"ando", "endo", "indo", "ção", "ções", "mente",
		"idos", "idas", "ados", "adas", "ing", "tion", "tions", "ness",
		"ment", "ments", "ed", "er", "ers", "es", "ly"}
	for _, suf := range suffixes {
		if strings.HasSuffix(word, suf) && len(word)-len(suf) >= 3 {
			return word[:len(word)-len(suf)]
		}
	}
	return word
}
