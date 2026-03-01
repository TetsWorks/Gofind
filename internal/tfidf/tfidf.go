package tfidf

import (
	"sort"

	"github.com/TetsWorks/gofind/internal/indexer"
)

// Engine calcula scores TF-IDF e ranqueia resultados
type Engine struct {
	idx *indexer.Index
}

func New(idx *indexer.Index) *Engine {
	return &Engine{idx: idx}
}

// Score calcula o TF-IDF de uma query para um documento
func (e *Engine) Score(query string, doc *indexer.Document) float64 {
	tokens := indexer.Tokenize(query)
	if len(tokens) == 0 {
		return 0
	}
	var score float64
	for _, token := range tokens {
		tf := e.idx.TF(token, doc.ID)
		idf := e.idx.IDF(token)
		score += tf * idf
	}
	// Bonus por match no nome do arquivo
	nameTokens := indexer.Tokenize(doc.Name)
	querySet := make(map[string]bool)
	for _, t := range tokens {
		querySet[t] = true
	}
	for _, nt := range nameTokens {
		if querySet[nt] {
			score *= 1.5
			break
		}
	}
	return score
}

// Rank ordena documentos por score TF-IDF
func (e *Engine) Rank(query string, docs []*indexer.Document) []*indexer.SearchResult {
	results := make([]*indexer.SearchResult, 0, len(docs))
	for _, doc := range docs {
		score := e.Score(query, doc)
		if score > 0 {
			results = append(results, &indexer.SearchResult{
				Doc:       doc,
				Score:     score,
				MatchType: indexer.MatchExact,
			})
		}
	}
	sort.Slice(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})
	return results
}
