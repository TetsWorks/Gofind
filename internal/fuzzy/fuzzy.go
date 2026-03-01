package fuzzy

import (
	"sort"
	"strings"

	"github.com/TetsWorks/gofind/internal/indexer"
)

// Levenshtein calcula a distância de edição entre duas strings
func Levenshtein(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	la, lb := len(ra), len(rb)
	if la == 0 { return lb }
	if lb == 0 { return la }
	// Otimização de espaço: apenas duas linhas
	prev := make([]int, lb+1)
	curr := make([]int, lb+1)
	for j := 0; j <= lb; j++ { prev[j] = j }
	for i := 1; i <= la; i++ {
		curr[0] = i
		for j := 1; j <= lb; j++ {
			cost := 1
			if ra[i-1] == rb[j-1] { cost = 0 }
			curr[j] = min3(curr[j-1]+1, prev[j]+1, prev[j-1]+cost)
		}
		prev, curr = curr, prev
	}
	return prev[lb]
}

func min3(a, b, c int) int {
	if a < b { if a < c { return a }; return c }
	if b < c { return b }
	return c
}

// Similar retorna true se a distância for <= maxDist
func Similar(a, b string, maxDist int) bool {
	return Levenshtein(strings.ToLower(a), strings.ToLower(b)) <= maxDist
}

// JaroWinkler calcula similaridade Jaro-Winkler (0.0 a 1.0)
func JaroWinkler(s1, s2 string) float64 {
	s1, s2 = strings.ToLower(s1), strings.ToLower(s2)
	if s1 == s2 { return 1.0 }
	l1, l2 := len(s1), len(s2)
	if l1 == 0 || l2 == 0 { return 0.0 }
	matchDist := max2(l1, l2)/2 - 1
	if matchDist < 0 { matchDist = 0 }
	s1Matches := make([]bool, l1)
	s2Matches := make([]bool, l2)
	matches := 0
	transpositions := 0
	for i := 0; i < l1; i++ {
		start := max2(0, i-matchDist)
		end := min2(i+matchDist+1, l2)
		for j := start; j < end; j++ {
			if s2Matches[j] || s1[i] != s2[j] { continue }
			s1Matches[i] = true; s2Matches[j] = true; matches++; break
		}
	}
	if matches == 0 { return 0.0 }
	k := 0
	for i := 0; i < l1; i++ {
		if !s1Matches[i] { continue }
		for k < l2 && !s2Matches[k] { k++ }
		if s1[i] != s2[k] { transpositions++ }
		k++
	}
	jaro := (float64(matches)/float64(l1) + float64(matches)/float64(l2) + float64(matches-transpositions/2)/float64(matches)) / 3.0
	prefix := 0
	for i := 0; i < min2(min2(l1, l2), 4); i++ {
		if s1[i] != s2[i] { break }
		prefix++
	}
	return jaro + float64(prefix)*0.1*(1-jaro)
}

func max2(a, b int) int { if a > b { return a }; return b }
func min2(a, b int) int { if a < b { return a }; return b }

// Match representa um match fuzzy
type Match struct {
	Term     string
	Query    string
	Distance int
	Score    float64
}

// Searcher realiza busca fuzzy no índice
type Searcher struct {
	idx *indexer.Index
}

func New(idx *indexer.Index) *Searcher {
	return &Searcher{idx: idx}
}

// Search busca termos no índice com tolerância a erros
func (s *Searcher) Search(query string, maxDist int) []*indexer.SearchResult {
	queryTokens := indexer.Tokenize(query)
	if len(queryTokens) == 0 {
		return nil
	}
	allTerms := s.idx.AllTerms()
	// Para cada token da query, encontra termos similares
	matchedDocs := make(map[uint64]float64)
	for _, qt := range queryTokens {
		for _, term := range allTerms {
			dist := Levenshtein(qt, term)
			if dist > maxDist {
				continue
			}
			jw := JaroWinkler(qt, term)
			score := jw * float64(maxDist-dist+1) / float64(maxDist+1)
			// Busca docs com esse termo
			docs := s.idx.SearchOR(term)
			for _, doc := range docs {
				tf := s.idx.TF(term, doc.ID)
				idf := s.idx.IDF(term)
				matchedDocs[doc.ID] += tf * idf * score
			}
		}
	}
	var results []*indexer.SearchResult
	for id, score := range matchedDocs {
		doc, ok := s.idx.GetDoc(id)
		if !ok { continue }
		results = append(results, &indexer.SearchResult{
			Doc: doc, Score: score, MatchType: indexer.MatchFuzzy,
		})
	}
	sort.Slice(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})
	return results
}

// Suggest sugere termos similares para autocompletar
func (s *Searcher) Suggest(prefix string, max int) []string {
	prefix = strings.ToLower(indexer.Stem(prefix))
	allTerms := s.idx.AllTerms()
	type scored struct{ term string; score float64 }
	var matches []scored
	for _, term := range allTerms {
		if strings.HasPrefix(term, prefix) {
			matches = append(matches, scored{term, float64(len(prefix)) / float64(len(term))})
			continue
		}
		jw := JaroWinkler(prefix, term)
		if jw > 0.75 {
			matches = append(matches, scored{term, jw})
		}
	}
	sort.Slice(matches, func(i, j int) bool { return matches[i].score > matches[j].score })
	result := make([]string, 0, max)
	for i, m := range matches {
		if i >= max { break }
		result = append(result, m.term)
	}
	return result
}
