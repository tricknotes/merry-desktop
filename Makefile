# Run inside the builder container:
#   docker compose run --rm builder make <target>
# The targets also work on any machine with Go and Node.js, which is what CI does.

APP := merry-desktop
WINDOWS_ENV := GOOS=windows GOARCH=amd64
WINDOWS_LDFLAGS := -w -s -H windowsgui
GO_DIRS := cmd internal frontend
# The engine-agent runs inside the engine VM and is embedded into the app.
AGENT := internal/vm/wsl/assets/engine-agent

.PHONY: all build test lint frontend agent dev tidy clean

all: lint test build

frontend:
	cd frontend && npm ci && npm run build

# Embedded gzip-compressed; the distro unpacks it with BusyBox gunzip.
agent:
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags="-w -s" -o $(AGENT) ./cmd/engine-agent
	gzip -9 -n -f $(AGENT)

# Unit tests run on Linux (inside the builder). The app itself (cmd/merry-desktop)
# is only built for Windows: the Linux build of Wails needs GTK, which the builder lacks.
test: frontend agent
	go test -race ./internal/... ./cmd/engine-agent/
	go vet ./cmd/merry-dev/
	$(WINDOWS_ENV) go vet ./...

lint:
	shellcheck --shell=sh scripts/phase0/wsl2/*.sh internal/vm/wsl/assets/*.sh
	@unformatted="$$(gofmt -l $(GO_DIRS))"; \
	if [ -n "$$unformatted" ]; then echo "gofmt needed:"; echo "$$unformatted"; exit 1; fi

build: frontend agent dev
	$(WINDOWS_ENV) go build -tags production -trimpath -ldflags="$(WINDOWS_LDFLAGS)" \
		-o bin/$(APP).exe ./cmd/$(APP)

# The development CLI runs inside WSL (Linux) and drives the VM through wsl.exe.
dev: agent frontend
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -o bin/merry-dev ./cmd/merry-dev

tidy: agent
	$(WINDOWS_ENV) go mod tidy

clean:
	rm -rf bin frontend/dist internal/vm/wsl/assets/engine-agent.gz
