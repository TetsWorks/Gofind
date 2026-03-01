package extractor

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"io"
	"os"
	"path/filepath"
	"strings"
)

var supportedExts = map[string]string{
	".txt":"txt",".md":"md",".markdown":"md",
	".go":"code",".py":"code",".js":"code",".ts":"code",
	".java":"code",".c":"code",".cpp":"code",".h":"code",
	".rs":"code",".rb":"code",".php":"code",".sh":"code",
	".yaml":"code",".yml":"code",".toml":"code",".ini":"code",
	".csv":"csv",".json":"json",".xml":"code",".html":"code",
	".pdf":"pdf",".docx":"docx",
}

type Extractor struct{}
func New() *Extractor { return &Extractor{} }

func (e *Extractor) Supported(path string) bool {
	_, ok := supportedExts[strings.ToLower(filepath.Ext(path))]
	return ok
}
func (e *Extractor) Type(path string) string {
	t, ok := supportedExts[strings.ToLower(filepath.Ext(path))]
	if !ok { return "txt" }
	return t
}
func (e *Extractor) Extract(path string) (string, error) {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".pdf": return extractPDF(path)
	case ".docx": return extractDOCX(path)
	default: return extractText(path)
	}
}

func extractText(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil { return "", err }
	sample := data
	if len(sample) > 512 { sample = sample[:512] }
	nonPrint := 0
	for _, b := range sample {
		if b < 32 && b != '\n' && b != '\r' && b != '\t' { nonPrint++ }
	}
	if len(sample) > 0 && nonPrint*100/len(sample) > 30 { return "", nil }
	return string(data), nil
}

func extractPDF(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil { return "", err }
	var sb strings.Builder
	content := string(data)
	i := 0
	for i < len(content) {
		bt := strings.Index(content[i:], "BT")
		if bt < 0 { break }
		bt += i
		et := strings.Index(content[bt:], "ET")
		if et < 0 { break }
		et += bt
		block := content[bt : et+2]
		j := 0
		for j < len(block) {
			op := strings.Index(block[j:], "(")
			if op < 0 { break }
			op += j
			cl := op + 1
			for cl < len(block) {
				if block[cl] == ')' && block[cl-1] != '\\' { break }
				cl++
			}
			if cl < len(block) {
				text := block[op+1 : cl]
				text = strings.ReplaceAll(text, `\n`, "\n")
				sb.WriteString(text); sb.WriteByte(' ')
			}
			j = cl + 1
		}
		i = et + 2
	}
	result := sb.String()
	if len(result) < 10 { return extractPrintable(data), nil }
	return result, nil
}

func extractPrintable(data []byte) string {
	var sb strings.Builder
	for _, b := range data {
		if b >= 32 && b < 127 { sb.WriteByte(b) } else if b == '\n' || b == '\t' { sb.WriteByte(b) }
	}
	return sb.String()
}

func extractDOCX(path string) (string, error) {
	r, err := zip.OpenReader(path)
	if err != nil { return "", err }
	defer r.Close()
	for _, f := range r.File {
		if f.Name != "word/document.xml" { continue }
		rc, err := f.Open()
		if err != nil { return "", err }
		defer rc.Close()
		data, err := io.ReadAll(rc)
		if err != nil { return "", err }
		return parseDocXML(data), nil
	}
	return "", nil
}

func parseDocXML(data []byte) string {
	dec := xml.NewDecoder(bytes.NewReader(data))
	var sb strings.Builder
	inText := false
	for {
		tok, err := dec.Token()
		if err != nil { break }
		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Local == "t" { inText = true } else if t.Name.Local == "p" { sb.WriteByte('\n') }
		case xml.EndElement:
			if t.Name.Local == "t" { inText = false }
		case xml.CharData:
			if inText { sb.Write(t); sb.WriteByte(' ') }
		}
	}
	return sb.String()
}
