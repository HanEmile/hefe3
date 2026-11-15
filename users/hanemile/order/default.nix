{ pkgs ? import <nixpkgs> {} }:

let
  order = pkgs.stdenv.mkDerivation {
    pname = "order";
    version = "0.1.0";
    src = ./.;

    buildInputs = [ pkgs.sbcl ];

    dontStrip = true;

    buildPhase = ''
      export HOME=$TMPDIR
      sbcl --non-interactive \
        --eval '(require :asdf)' \
        --eval '(push (truename ".") asdf:*central-registry*)' \
        --eval '(asdf:load-system :order)' \
        --eval '(sb-ext:save-lisp-and-die "order" :toplevel (function order:main) :executable t :compression t)'
    '';

    installPhase = ''
      mkdir -p $out/bin
      cp order $out/bin/
    '';
  };

  build-site = pkgs.writeShellScriptBin "order-build" ''
    set -euo pipefail

    INPUT_DIR=''${1:?Usage: order-build <input-dir> <output-dir> <site-name> [visibility] [hefe3-path]}
    OUTPUT_DIR=''${2:?Usage: order-build <input-dir> <output-dir> <site-name> [visibility] [hefe3-path]}
    SITE_NAME=''${3:?Usage: order-build <input-dir> <output-dir> <site-name> [visibility] [hefe3-path]}
    VISIBILITY=''${4:-private}
    HEFE3_PATH=''${5:-}

    echo "order-build: $SITE_NAME"
    echo "  input:      $INPUT_DIR"
    echo "  output:     $OUTPUT_DIR"
    echo "  visibility: $VISIBILITY"

    if [ -n "$HEFE3_PATH" ] && [ -d "$HEFE3_PATH" ]; then
      echo "  hefe3:      $HEFE3_PATH"
      ${pkgs.python3}/bin/python3 ${./scripts/import-hefe3.py} "$HEFE3_PATH" "$INPUT_DIR"
    fi
    echo ""

    mkdir -p "$OUTPUT_DIR"
    ${order}/bin/order "$INPUT_DIR" "$OUTPUT_DIR" "$SITE_NAME" "$VISIBILITY"
  '';

  test-visibility = pkgs.writeShellScriptBin "order-test-visibility" ''
    set -euo pipefail
    exec ${pkgs.bash}/bin/bash ${./scripts/test-visibility.sh} "''${1:-}"
  '';

  test-bench = pkgs.writeShellScriptBin "order-test-bench" ''
    set -euo pipefail
    TMPDIR=$(mktemp -d)
    trap "rm -rf $TMPDIR" EXIT
    ${pkgs.python3}/bin/python3 ${./scripts/generate-test-data.py} "$TMPDIR/input"
    echo "=== Full build ==="
    time ${order}/bin/order "$TMPDIR/input" "$TMPDIR/output" bench.test private
    echo ""
    echo "=== Incremental (no changes) ==="
    time ${order}/bin/order "$TMPDIR/input" "$TMPDIR/output" bench.test private
    echo ""
    echo "=== Incremental (1 file changed) ==="
    echo "" >> "$TMPDIR/input/page-00042.md"
    time ${order}/bin/order "$TMPDIR/input" "$TMPDIR/output" bench.test private
  '';

in {
  inherit order build-site test-visibility test-bench;
}
