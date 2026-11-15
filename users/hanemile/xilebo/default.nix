{ pkgs ? import <nixpkgs> {} }:

let
  # Pin to a specific nixpkgs for reproducibility if desired:
  # pkgs = import (fetchTarball "https://github.com/NixOS/nixpkgs/archive/<rev>.tar.gz") {};
in
pkgs.mkShell {
  name = "xilebo-dev";

  buildInputs = with pkgs; [
    # Go
    go
    gopls
    gotools
    golangci-lint

    # Go tools
    sqlc
    goose

    # Node.js (for web frontend build, optional)
    nodejs_22

    # General
    git
    curl
    jq
    python3  # used by smoke-test.sh for JSON parsing
    sqlite
  ];

  shellHook = ''
    export GOPATH="$HOME/go"
    export PATH="$GOPATH/bin:$PATH"
    echo "xilebo dev shell"
    echo "  go:    $(go version | cut -d' ' -f3)"
    echo "  node:  $(node --version)"
    echo "  sqlc:  $(sqlc version 2>/dev/null || echo 'not found')"
    echo ""
    echo "Quick start:"
    echo "  ./scripts/dev.sh          # start server + frontend"
    echo "  ./scripts/test.sh         # run tests"
    echo "  ./scripts/smoke-test.sh   # API smoke tests"
  '';
}
