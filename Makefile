# TokenControlPlane — build helpers
# CGO stays OFF (pure-Go sqlite via modernc.org/sqlite).

export CGO_ENABLED ?= 0

VERSION_FILE := VERSION
VERSION ?= $(shell cat $(VERSION_FILE) 2>/dev/null || echo dev)
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
BUILT   ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS := -X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.built=$(BUILT)

# Dev loop: gateway on :8088 (matches vite.config proxy default), isolated dev DB.
DEV_PORT ?= 8088
DEV_DB   ?= ./dev.db
DEMO_DB  ?= ./demo.db

.PHONY: build build-go test dev clean-dev demo-db demo frontend sync-dist help release

# Full release build: npm dashboard + Go binary.
build: frontend sync-dist build-go

frontend:
	cd frontend && npm ci && npm run build

# Copy Vite output into the embed package (go:embed cannot use .. paths).
sync-dist:
	mkdir -p pkg/web/dist
	rm -rf pkg/web/dist/*
	cp -a frontend/dist/. pkg/web/dist/

# Go-only build (requires pkg/web/dist already present — committed placeholder or prior sync).
build-go:
	go build -ldflags "$(LDFLAGS)" -o bin/tokencontrolplane ./cmd/gateway
	go build -o bin/tokencontrolplane-license ./cmd/tokencontrolplane-license

test:
	go test ./...
	cd frontend && npm test -- --run

# Local dev loop: gateway on :$(DEV_PORT) (dev DB, isolated from the real one)
# + Vite dev server with HMR on :5173. Ctrl-C stops both.
dev:
	@echo "-> gateway   http://localhost:$(DEV_PORT)  (db: $(DEV_DB))"
	@echo "-> dashboard http://localhost:5173         (HMR, /api -> :$(DEV_PORT))"
	@echo "-> Ctrl-C stops both"
	@JWT_SECRET=$${JWT_SECRET:-dev-insecure-jwt-secret-change-me} \
	  go run ./cmd/gateway --addr :$(DEV_PORT) --db $(DEV_DB) & GW=$$!; \
	cd frontend && VITE_PROXY_TARGET=http://localhost:$(DEV_PORT) npm run dev & FE=$$!; \
	wait $$FE 2>/dev/null; kill $$GW 2>/dev/null; wait $$GW 2>/dev/null

# Drop the dev database (keeps the real gateway.db untouched).
clean-dev:
	rm -f $(DEV_DB)
	@echo "removed $(DEV_DB)"

# Screenshot-ready SQLite with Team plan, usage charts, keys, providers, activity.
demo-db:
	go run ./cmd/seed-demo -o $(DEMO_DB)

# Seed demo.db and run gateway + Vite against it (does not touch $(DEV_DB)).
demo: demo-db
	@echo "-> gateway   http://localhost:$(DEV_PORT)  (db: $(DEMO_DB))"
	@echo "-> dashboard http://localhost:5173         (login: demo@acme.dev / demo-demo-demo)"
	@echo "-> Ctrl-C stops both"
	@JWT_SECRET=$${JWT_SECRET:-dev-insecure-jwt-secret-change-me} \
	  go run ./cmd/gateway --addr :$(DEV_PORT) --db $(DEMO_DB) & GW=$$!; \
	cd frontend && VITE_PROXY_TARGET=http://localhost:$(DEV_PORT) npm run dev & FE=$$!; \
	wait $$FE 2>/dev/null; kill $$GW 2>/dev/null; wait $$GW 2>/dev/null

# Cross-compile release artifacts: make release TAG=v1.0.0
release:
	@test -n "$(TAG)" || (echo "usage: make release TAG=v1.0.0" && exit 1)
	@mkdir -p dist
	$(eval RELVER := $(patsubst v%,%,$(TAG)))
	@for pair in darwin/arm64 darwin/amd64 linux/amd64 linux/arm64; do \
	  os=$${pair%/*}; arch=$${pair#*/}; \
	  out=dist/tokencontrolplane_$${os}_$${arch}_$(TAG); \
	  echo "building $$out"; \
	  GOOS=$$os GOARCH=$$arch CGO_ENABLED=0 go build -trimpath \
	    -ldflags "-X main.version=$(RELVER) -X main.commit=$(COMMIT) -X main.built=$(BUILT)" \
	    -o $$out ./cmd/gateway; \
	done
	cd dist && shasum -a 256 tokencontrolplane_*_$(TAG) > SHA256SUMS
	@echo "artifacts in dist/ — tag images as ghcr.io/devthinker-ai/tokencontrolplane:$(TAG) and :latest"

help:
	@echo "make build              — npm ci + build + sync-dist + go build"
	@echo "make build-go           — go build only (needs pkg/web/dist)"
	@echo "make test               — go test + frontend vitest"
	@echo "make dev                — gateway :8088 (dev.db) + Vite HMR :5173, Ctrl-C stops both"
	@echo "make clean-dev          — remove $(DEV_DB)"
	@echo "make demo-db            — seed screenshot DB at $(DEMO_DB)"
	@echo "make demo               — seed + gateway/Vite on $(DEMO_DB)"
	@echo "make release TAG=vX.Y.Z — cross-compile + SHA256SUMS"
	@go run ./cmd/gateway -h 2>&1 | head -40 || true
