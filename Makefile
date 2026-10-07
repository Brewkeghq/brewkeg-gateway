BINARY := brewkeg
ICON   := assets/appicon.png

.PHONY: test build desktop desktop-dev clean

test: ## vet + test every package (engine, CLI, desktop bindings)
	go vet ./...
	go test ./...

build: ## CLI binary for this machine
	CGO_ENABLED=0 go build -ldflags "-s -w" -o $(BINARY) .

desktop: ## native desktop app -> desktop/build/bin
	mkdir -p desktop/build && cp $(ICON) desktop/build/appicon.png
	cd desktop && wails build -clean

desktop-dev: ## desktop app with live frontend reload
	mkdir -p desktop/build && cp $(ICON) desktop/build/appicon.png
	cd desktop && wails dev

clean:
	rm -f $(BINARY)
	rm -rf desktop/build