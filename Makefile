# HomePanel build tooling. Requires Go and Node (npm).

BINARY   := homepanel
MODULE   := github.com/C7PEK/cypek.dev-panel
VERSION  ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS  := -s -w -X main.version=$(VERSION)
GOFLAGS  := -trimpath
# Builds are always pure Go (modernc sqlite, no cgo). The race detector needs
# cgo + a C compiler, so tests enable it only when gcc is available.
export CGO_ENABLED := 0
RACE     := $(shell command -v gcc >/dev/null 2>&1 && echo -race)

.PHONY: all dev dev-backend dev-frontend build build-frontend build-backend sync-systemfiles test test-go test-web lint fmt clean release deploy

## deploy: build and install on this machine (root), restarting only the panel
deploy: build
	install -m 755 bin/$(BINARY) /usr/local/bin/$(BINARY)
	/usr/local/bin/$(BINARY) install-files
	systemctl daemon-reload
	systemctl restart homepanel

all: build

## dev: run Go backend (--dev) and Vite together; Ctrl+C stops both
dev: web/node_modules
	@trap 'kill 0' INT TERM; \
	( cd web && npm run dev ) & \
	go run ./cmd/homepanel serve --dev --log-level debug; \
	wait

dev-backend:
	go run ./cmd/homepanel serve --dev --log-level debug

dev-frontend: web/node_modules
	cd web && npm run dev

web/node_modules: web/package.json web/package-lock.json
	cd web && npm ci
	@touch web/node_modules

## build: frontend + backend into bin/homepanel
build: build-frontend build-backend

build-frontend: web/node_modules
	cd web && npm run build
	@touch web/dist/.gitkeep

## sync-systemfiles: copy packaging/ units + polkit rule into the binary's embed dir
sync-systemfiles:
	cp packaging/systemd/homepanel.service packaging/systemd/homepanel-server@.service packaging/systemd/homepanel-database-install.service packaging/polkit/50-homepanel.rules cmd/homepanel/systemfiles/

build-backend: sync-systemfiles
	go build $(GOFLAGS) -ldflags '$(LDFLAGS)' -o bin/$(BINARY) ./cmd/homepanel

## release: cross-compile linux/amd64 and linux/arm64 (frontend must be built)
release: build-frontend sync-systemfiles
	GOOS=linux GOARCH=amd64 go build $(GOFLAGS) -ldflags '$(LDFLAGS)' -o dist/$(BINARY)-linux-amd64 ./cmd/homepanel
	GOOS=linux GOARCH=arm64 go build $(GOFLAGS) -ldflags '$(LDFLAGS)' -o dist/$(BINARY)-linux-arm64 ./cmd/homepanel
	cd dist && sha256sum $(BINARY)-linux-* > SHA256SUMS

## test: Go tests (race) + frontend typecheck/lint
test: test-go test-web

test-go:
	go vet ./...
	CGO_ENABLED=$(if $(RACE),1,0) go test $(RACE) ./...

test-web: web/node_modules
	cd web && npx tsc -b && npm run lint

lint:
	gofmt -l . | grep -v '^web/' | tee /dev/stderr | test -z "$$(cat)"
	go vet ./...

fmt:
	gofmt -w ./cmd ./internal ./web/embed.go

clean:
	rm -rf bin dist web/dist/* && touch web/dist/.gitkeep
