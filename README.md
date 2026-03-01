# GoFind

> Motor de busca local em Go — índice invertido, TF-IDF, fuzzy search, TUI interativa.

## Instalação (Termux)

```bash
pkg install golang git
git clone https://github.com/TetsWorks/gofind
cd gofind
go mod tidy
make termux
```

## Uso

```bash
# Indexa um diretório e abre a TUI
gofind ~/Documents

# Busca pela linha de comando
gofind -search "golang http server"

# Busca fuzzy (tolerante a erros)
gofind -search "golagn" -fuzzy

# Exporta resultado em JSON
gofind -search "readme" -json

# Estatísticas do índice
gofind -stats

# Watch mode (re-indexa ao salvar)
gofind -watch
```

## Features

- **Índice invertido** com posting lists por termo
- **TF-IDF** para ranking por relevância
- **Busca fuzzy** com distância de Levenshtein + Jaro-Winkler
- **Extrator de texto** — TXT, MD, código, PDF, DOCX, CSV, JSON
- **Persistência** — índice salvo em `~/.gofind/` (não some ao fechar)
- **Watch mode** — re-indexa arquivos ao detectar mudanças
- **TUI interativa** — busca em tempo real, preview com highlight, stats
- **Export JSON** — exporta resultados para análise

## Estrutura

```
gofind/
├── cmd/gofind/main.go          CLI + flags
├── internal/
│   ├── indexer/                Índice invertido + tokenizer + stemmer
│   ├── tfidf/                  Motor TF-IDF + ranking
│   ├── fuzzy/                  Levenshtein + Jaro-Winkler + sugestões
│   ├── extractor/              PDF, DOCX, TXT, código
│   ├── storage/                Persistência em JSON
│   ├── watcher/                Watch mode via fsnotify
│   └── tui/                    Interface TUI (tview)
└── Makefile
```

## Licença

MIT
