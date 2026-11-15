# vokobe2

## src info

bin/         -> resulting binary (vokobe2) that can be run
bundle/      -> quicklisp bundle, see https://www.quicklisp.org/beta/bundles.html
default.nix  -> nix entrypoint for `nix-build`
deploy.lisp  -> lisp entrypoint for building
Makefile     -> bundle commands
old.lisp     -> deprecated, some tests from the beginning
package.lisp -> the package definition
vokobe2.asd  -> the system definition
vokobe2.fasl -> a build
vokobe2.lisp -> the source
vokobe2.nix  -> the nix derivation
