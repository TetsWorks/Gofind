package storage

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/TetsWorks/gofind/internal/indexer"
)

const indexFile = "index.json"

// Snapshot é a estrutura persistida em disco
type Snapshot struct {
	Version   string              `json:"version"`
	CreatedAt time.Time           `json:"created_at"`
	UpdatedAt time.Time           `json:"updated_at"`
	Dirs      []string            `json:"dirs"`
	Docs      []*indexer.Document `json:"docs"`
	Terms     map[string][]uint64 `json:"terms"`       // term -> docIDs
	Freqs     map[string]map[uint64]int `json:"freqs"` // term -> docID -> freq
}

// Store persiste e carrega índices do disco
type Store struct {
	dir string
}

func New(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, err
	}
	return &Store{dir: dir}, nil
}

func (s *Store) path() string {
	return filepath.Join(s.dir, indexFile)
}

// Save salva o índice em disco
func (s *Store) Save(idx *indexer.Index) error {
	docs := idx.AllDocs()
	terms := idx.AllTerms()

	termsMap := make(map[string][]uint64)
	freqsMap := make(map[string]map[uint64]int)

	for _, term := range terms {
		var docIDs []uint64
		freqsMap[term] = make(map[uint64]int)
		for _, doc := range docs {
			tf := idx.TF(term, doc.ID)
			if tf > 0 {
				docIDs = append(docIDs, doc.ID)
				freqsMap[term][doc.ID] = int(tf * float64(doc.TokenCount))
			}
		}
		if len(docIDs) > 0 {
			termsMap[term] = docIDs
		}
	}

	snap := &Snapshot{
		Version:   "1.0",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
		Dirs:      idx.Stats().Directories,
		Docs:      docs,
		Terms:     termsMap,
		Freqs:     freqsMap,
	}

	data, err := json.Marshal(snap)
	if err != nil {
		return err
	}
	return os.WriteFile(s.path(), data, 0644)
}

// Load carrega o índice do disco
func (s *Store) Load(idx *indexer.Index) error {
	data, err := os.ReadFile(s.path())
	if err != nil {
		if os.IsNotExist(err) {
			return nil // sem índice ainda, ok
		}
		return err
	}

	var snap Snapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		return err
	}

	// Recria documentos no índice
	for _, doc := range snap.Docs {
		// Recarrega o conteúdo do arquivo para reidexar tokens
		idx.AddDocument(doc)
	}

	idx.Dirs = snap.Dirs
	return nil
}

// Exists verifica se existe índice salvo
func (s *Store) Exists() bool {
	_, err := os.Stat(s.path())
	return err == nil
}

// Clear remove o índice do disco
func (s *Store) Clear() error {
	return os.Remove(s.path())
}

// Info retorna informações sobre o índice salvo
func (s *Store) Info() (time.Time, int64, error) {
	info, err := os.Stat(s.path())
	if err != nil {
		return time.Time{}, 0, err
	}
	return info.ModTime(), info.Size(), nil
}
