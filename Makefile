# Run inside the builder container:
#   docker compose run --rm builder make <target>
# The targets also work on any machine with Go and Node.js, which is what CI does.

WAILS_SPIKE := spikes/wails
WINDOWS_LDFLAGS := -w -s -H windowsgui

.PHONY: all build test lint spike-wails spike-wails-frontend clean

all: lint test build

build: spike-wails

test: spike-wails-frontend
	cd $(WAILS_SPIKE) && GOOS=windows GOARCH=amd64 go vet ./...

lint:
	shellcheck --shell=sh scripts/phase0/wsl2/*.sh

spike-wails-frontend:
	cd $(WAILS_SPIKE)/frontend && npm ci && npm run build

spike-wails: spike-wails-frontend
	cd $(WAILS_SPIKE) && GOOS=windows GOARCH=amd64 go build -tags production -trimpath \
		-ldflags="$(WINDOWS_LDFLAGS)" -o bin/SpikeWails.exe .

clean:
	rm -rf $(WAILS_SPIKE)/bin $(WAILS_SPIKE)/frontend/dist
