{ pkgs ? import <nixpkgs> { }, ... }:

pkgs.mkShell {
  buildInputs = with pkgs; [

    # this is sbcl with the quicklisp dependencies
    (import ./default.nix { inherit pkgs; }).app.buildInputs

    ripgrep

    # emacs without x foo, i'm mostly on macos for writing the actual code
    emacs-nox
  ] ++ (with emacsPackages; [
    sly
    which-key
    magit
    nix-mode
  ]);
  shellHook = ''
    local dir="$(pwd)/.emacs.d"
    alias emacs="emacs --init-directory=$dir"
  '';
}
