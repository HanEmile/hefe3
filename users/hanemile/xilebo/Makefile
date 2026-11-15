.PHONY: build build-server build-tui build-cli test test-unit test-int test-e2e lint generate migrate clean dev

# Go parameters
GOBIN ?= $(shell pwd)/bin
GOFLAGS ?= -trimpath

# Build
build: build-server build-tui build-cli

build-server:
	go build $(GOFLAGS) -o $(GOBIN)/xilebo-server ./cmd/xilebo-server

build-tui:
	go build $(GOFLAGS) -o $(GOBIN)/xilebo-tui ./cmd/xilebo-tui

build-cli:
	go build $(GOFLAGS) -o $(GOBIN)/xilebo-cli ./cmd/xilebo-cli

# Test
test: test-unit test-int

test-unit:
	go test -race -count=1 ./internal/... ./pkg/...

test-int:
	go test -race -count=1 -tags=integration ./test/integration/...

test-e2e:
	cd test/e2e && go test -race -count=1 -tags=e2e ./...

# Lint
lint:
	golangci-lint run ./...

# Code generation
generate:
	sqlc generate

# Database migrations
migrate:
	@echo "Usage: make migrate-up DB=path/to/db or make migrate-down DB=path/to/db"

migrate-up:
	goose -dir internal/storage/migrations/sqlite sqlite3 "$(DB)" up

migrate-down:
	goose -dir internal/storage/migrations/sqlite sqlite3 "$(DB)" down

# Development — the web frontend is plain JS, no build step needed.
# Just start the server and open http://localhost:8080.
dev: build-server
	$(GOBIN)/xilebo-server -config config.toml

# Clean
clean:
	rm -rf $(GOBIN)
