BINARY := brewkeg

.PHONY: test build desktop desktop-dev clean

test: ## vet + test every package (engine, CLI, desktop bindings)
	go vet ./...
	go test ./...

build: ## CLI binary for this machine
	CGO_ENABLED=0 go build -ldflags "-s -w" -o $(BINARY) .

desktop: ## native desktop app -> desktop/build/bin
	cd desktop && wails build -clean

desktop-dev: ## desktop app with live frontend reload
	cd desktop && wails dev

clean:
	rm -f $(BINARY)
	rm -rf desktop/build