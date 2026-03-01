BINARY  := gofind
VERSION := 0.1.0
LDFLAGS := -ldflags "-X main.Version=$(VERSION)"

.PHONY: build termux run test clean deps

build:
	@echo "→ Compilando gofind..."
	go build $(LDFLAGS) -o $(BINARY) ./cmd/gofind
	@echo "✓ ./$(BINARY)"

termux: build
	cp $(BINARY) $(PREFIX)/bin/$(BINARY)
	@echo "✓ Instalado"

run: build
	./$(BINARY)

test:
	go test ./...

clean:
	rm -f $(BINARY)

deps:
	go mod tidy && go mod download

help:
	@echo "make build    compila"
	@echo "make termux   instala no Termux"
	@echo "make run      executa TUI"
