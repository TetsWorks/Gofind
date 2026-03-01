package indexer

import "time"

type DocType string

const (
	DocTypeTXT  DocType = "txt"
	DocTypeMD   DocType = "md"
	DocTypePDF  DocType = "pdf"
	DocTypeDOCX DocType = "docx"
	DocTypeCode DocType = "code"
	DocTypeCSV  DocType = "csv"
	DocTypeJSON DocType = "json"
)

type Document struct {
	ID         uint64    `json:"id"`
	Path       string    `json:"path"`
	Name       string    `json:"name"`
	Type       DocType   `json:"type"`
	Size       int64     `json:"size"`
	ModTime    time.Time `json:"mod_time"`
	IndexedAt  time.Time `json:"indexed_at"`
	Content    string    `json:"-"`
	WordCount  int       `json:"word_count"`
	TokenCount int       `json:"token_count"`
}

type SearchResult struct {
	Doc        *Document
	Score      float64
	Highlights []Highlight
	MatchType  MatchType
}

type Highlight struct {
	Line  int
	Text  string
	Term  string
	Start int
	End   int
}

type MatchType string

const (
	MatchExact  MatchType = "exact"
	MatchFuzzy  MatchType = "fuzzy"
	MatchPrefix MatchType = "prefix"
)

type IndexStats struct {
	TotalDocs   int
	TotalTokens int
	TotalSize   int64
	LastIndexed time.Time
	Directories []string
}

type SearchOptions struct {
	Query      string
	MaxResults int
	Fuzzy      bool
	FuzzyDist  int
	MinScore   float64
	FileTypes  []DocType
	Dir        string
	ExportJSON bool
}
