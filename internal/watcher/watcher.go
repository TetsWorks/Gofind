package watcher

import (
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/TetsWorks/gofind/internal/extractor"
	"github.com/TetsWorks/gofind/internal/indexer"
)

type Event struct {
	Path string
	Op   string
}

type Watcher struct {
	fsw      *fsnotify.Watcher
	idxr     *indexer.Indexer
	ext      *extractor.Extractor
	events   chan Event
	debounce map[string]time.Time
}

func New(idxr *indexer.Indexer) (*Watcher, error) {
	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	return &Watcher{
		fsw:      fsw,
		idxr:     idxr,
		ext:      extractor.New(),
		events:   make(chan Event, 100),
		debounce: make(map[string]time.Time),
	}, nil
}

func (w *Watcher) Watch(dir string) error {
	return filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil { return nil }
		if info.IsDir() {
			name := info.Name()
			if strings.HasPrefix(name, ".") || name == "node_modules" { return filepath.SkipDir }
			return w.fsw.Add(path)
		}
		return nil
	})
}

func (w *Watcher) Start() { go w.loop() }

func (w *Watcher) loop() {
	ticker := time.NewTicker(300 * time.Millisecond)
	defer ticker.Stop()
	pending := make(map[string]fsnotify.Op)
	for {
		select {
		case event, ok := <-w.fsw.Events:
			if !ok { return }
			if !w.ext.Supported(event.Name) { continue }
			pending[event.Name] = event.Op
			w.debounce[event.Name] = time.Now()
		case err, ok := <-w.fsw.Errors:
			if !ok { return }
			log.Printf("watcher: %v", err)
		case <-ticker.C:
			now := time.Now()
			for path, t := range w.debounce {
				if now.Sub(t) < 500*time.Millisecond { continue }
				op := pending[path]
				delete(w.debounce, path)
				delete(pending, path)
				w.handle(path, op)
			}
		}
	}
}

func (w *Watcher) handle(path string, op fsnotify.Op) {
	opName := "write"
	switch {
	case op&fsnotify.Create != 0:
		opName = "create"
		w.idxr.IndexFile(path)
	case op&fsnotify.Write != 0:
		opName = "write"
		w.idxr.ReindexFile(path)
	case op&fsnotify.Remove != 0:
		opName = "remove"
		w.idxr.Idx.RemoveDocument(path)
	case op&fsnotify.Rename != 0:
		opName = "rename"
		w.idxr.Idx.RemoveDocument(path)
	}
	select {
	case w.events <- Event{Path: path, Op: opName}:
	default:
	}
}

func (w *Watcher) Events() <-chan Event { return w.events }
func (w *Watcher) Close()               { w.fsw.Close() }
