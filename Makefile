# Run inside the builder container:
#   docker compose run --rm builder make <target>
# The targets also work on any machine with Go and Node.js, which is what CI does.

APP := merry-desktop
WINDOWS_ENV := GOOS=windows GOARCH=amd64
WINDOWS_LDFLAGS := -w -s -H windowsgui
GO_DIRS := cmd internal frontend

.PHONY: all build test lint frontend tidy clean

all: lint test build

frontend:
	cd frontend && npm ci && npm run build

# Unit tests run on Linux (inside the builder). The app itself (cmd/) is only
# built for Windows: the Linux build of Wails needs GTK, which the builder lacks.
test: frontend
	go test -race ./internal/...
	$(WINDOWS_ENV) go vet ./...

lint:
	shellcheck --shell=sh scripts/phase0/wsl2/*.sh
	@unformatted="$$(gofmt -l $(GO_DIRS))"; \
	if [ -n "$$unformatted" ]; then echo "gofmt needed:"; echo "$$unformatted"; exit 1; fi

build: frontend
	$(WINDOWS_ENV) go build -tags production -trimpath -ldflags="$(WINDOWS_LDFLAGS)" \
		-o bin/$(APP).exe ./cmd/$(APP)

tidy:
	$(WINDOWS_ENV) go mod tidy

clean:
	rm -rf bin frontend/dist
