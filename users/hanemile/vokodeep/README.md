# vokodeep

## src info

bin/         -> resulting binary (vokodeep) that can be run
default.nix  -> nix entrypoint for `nix-build`
deploy.lisp  -> lisp entrypoint for building
old.lisp     -> deprecated, some tests from the beginning
package.lisp -> the package definition
vokodeep.asd  -> the system definition
vokodeep.fasl -> a build
vokodeep.lisp -> the source
vokodeep.nix  -> the nix derivation
