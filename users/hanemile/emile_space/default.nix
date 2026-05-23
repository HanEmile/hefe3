# emile.space static site — build with vokobe, deploy to medano nginx.
#
# The site content lives outside the repo (the in/ directory is ~1.2 GB).
# Set EMILESPACE_IN to point at it; defaults to ~/emile.space/in.
#
# Build:
#   nix-build -A users.hanemile.emile_space.build && ./result/bin/build
# Deploy (rsync to medano:/keep/www/emile.space/):
#   nix-build -A users.hanemile.emile_space.deploy && ./result/bin/deploy
{ hefe, pkgs, ... }:

let
  vokobe = hefe.tools.vokobe.package;

  build = pkgs.writeShellScriptBin "build" ''
    set -ue
    IN_DIR="''${EMILESPACE_IN:-$HOME/emile.space/in}"
    OUT_DIR="''${EMILESPACE_OUT:-$(mktemp -d -t emile-space-XXXXXX)}"
    if [ ! -d "$IN_DIR" ]; then
      echo "EMILESPACE_IN points to $IN_DIR which does not exist." >&2
      exit 1
    fi
    echo "[+] building $IN_DIR -> $OUT_DIR"
    ${vokobe}/bin/vokobe -a "$IN_DIR" "$OUT_DIR" emile.space
    chmod -R +r "$OUT_DIR"
    echo "[+] done. Output at: $OUT_DIR"
    echo "$OUT_DIR" > /tmp/.emile_space_last_out
  '';

  deploy = pkgs.writeShellScriptBin "deploy" ''
    set -ue
    IN_DIR="''${EMILESPACE_IN:-$HOME/emile.space/in}"
    TARGET="''${EMILESPACE_TARGET:-root@medano:/keep/www/emile.space/}"
    OUT_DIR=$(mktemp -d -t emile-space-XXXXXX)
    trap 'rm -rf "$OUT_DIR"' EXIT
    echo "[1/2] building -> $OUT_DIR"
    ${vokobe}/bin/vokobe -a "$IN_DIR" "$OUT_DIR" emile.space
    chmod -R +r "$OUT_DIR"
    echo "[2/2] rsync -> $TARGET"
    ${pkgs.rsync}/bin/rsync -avz --links --delete --exclude '.git' "$OUT_DIR/" "$TARGET"
    ${pkgs.openssh}/bin/ssh -o StrictHostKeyChecking=accept-new "''${TARGET%%:*}" "chmod -R +r ''${TARGET#*:}*"
    echo "[+] deployed"
  '';
in
{
  inherit build deploy;
}
