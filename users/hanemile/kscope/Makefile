BINARY   := kscope
MODULE   := github.com/hanemile/kscope
CMD      := ./cmd/kscope
PLUGIN_SRC := ./plugins/example/struct_diff.go
PLUGIN_OUT := ./plugins/struct-diff.so
KERNEL_ROOT := ~/src/github.com/torvalds/linux

.PHONY: all build run clean plugin deps

all: deps build

deps:
	go mod tidy

build:
	go build -o $(BINARY) $(CMD)

run: build
	./$(BINARY) -root $(KERNEL_ROOT) -plugins $(PLUGIN_OUT) -addr :8080 -v

plugin:
	go build -buildmode=plugin -o $(PLUGIN_OUT) $(PLUGIN_SRC)

clean:
	rm -f $(BINARY) $(PLUGIN_OUT)

# Quick dev run against current directory
dev: build
	./$(BINARY) -root . -addr :8080 -v
