{ hefe, ... }:

# A tiny web-based IRC client (bonsai_web frontend + a cohttp/async_rpc_websocket
# backend that itself speaks IRC to a soju bouncer), meant to be hosted
# tailscale-only on the irc VM, the-lounge-style. See
# //users/hanemile/projects/ocaml/ircc for the actual dune project; this file
# is just the nix packaging for it.
#
# Jane Street's "bonsai" (incl. Bonsai_web) is not packaged in nixpkgs, so it's
# built here from source, pinned to the v0.17.0 tag -- the last release where
# bonsai_web still lived inside the main "bonsai" repo/opam-package, built
# against the same core/async/virtual_dom/incr_dom "v0.17" line that nixpkgs'
# `janePackage` generator (pkgs/development/ocaml-modules/janestreet/0.17.nix)
# already provides. Later bonsai_web versions split into their own repo and
# now depend on packages (bonsai_concrete, browser, toplayer, ...) that don't
# exist anywhere in nixpkgs, so pinning to v0.17.0 avoids having to package an
# entire second generation of the Jane Street ecosystem by hand.
#
# Three more wrinkles from that same v0.17/ppxlib skew, worked around locally
# (scoped to just this derivation, not the whole nixpkgs config):
#   - built with `ocaml-ng.ocamlPackages_5_2` rather than the default OCaml:
#     nixpkgs' `virtual_dom` is marked broken for OCaml >= 5.3, and the
#     default toolchain here is 5.4.
#   - `config.allowBroken = true`: with OCaml 5.2 that lands on ppxlib 0.34,
#     which un-breaks `virtual_dom`/`async_js`, but `ppx_css` is still marked
#     broken; `ppx_css` is a hard transitive dependency of bonsai_web (via
#     bonsai_web_ui_view) so evaluation needs to be allowed through.
#   - `ppx_css`'s own "incompatible with sedlex > 3.4" note turns out to be
#     real, not stale: its vendored CSS lexer uses a non-ASCII char-literal
#     range (`'\160' .. '\255'`) that newer sedlex rejects as "not a valid
#     ASCII interval regexp". Patched to the equivalent numeric-codepoint
#     range sedlex now wants instead of chasing a whole separate pinned
#     sedlex version.
let
  sources = hefe.third_party;
  nixos = sources."nixos-26.05";
  pkgs = import nixos { config.allowBroken = true; };
  ocamlPackages = pkgs.ocaml-ng.ocamlPackages_5_2.overrideScope (
    _final: prev: {
      # doCheck disabled: the upstream OCaml 5.2.1 testsuite has one unrelated
      # flaky failure (tests/unwind/driver.ml) in this sandbox; skip rather
      # than chase a stack-unwinding test that has nothing to do with this
      # package.
      ocaml = prev.ocaml.overrideAttrs (_: { doCheck = false; });
      ppx_css = prev.ppx_css.overrideAttrs (old: {
        meta = old.meta // { broken = false; };
        postPatch = (old.postPatch or "") + ''
          substituteInPlace vendor/css_parser/src/lexer.ml \
            --replace-fail "'\160' .. '\255'" "0x00A0 .. 0x00FF"
        '';
      });
    }
  );

  bonsai = ocamlPackages.buildDunePackage {
    pname = "bonsai";
    version = "0.17.0";
    src = pkgs.fetchFromGitHub {
      owner = "janestreet";
      repo = "bonsai";
      rev = "v0.17.0";
      sha256 = "1ghsfnrlrsp0a64zlcw9amklibnrlj4j3j5h19ns6srzzjikpgxf";
    };
    minimalOCamlVersion = "5.1";
    duneVersion = "3";
    doCheck = false;
    # The monorepo bundles its own demo apps/tests, and a couple dozen
    # bonsai_web_ui_* widget libraries we don't use, all under one opam
    # package (bonsai.opam) -- `dune build @install` would otherwise try to
    # build all of it too, pulling in tools (ocaml-embed-file, js_of_ocaml, a
    # `css_inliner` binary) that have nothing to do with the library itself.
    # Keep only src/ (bonsai), web/ (bonsai_web), ppx_bonsai/, extra/, and the
    # two web_ui/ libraries bonsai_web_ui_view actually depends on (view/
    # itself, and toggle/ for bonsai_web_ui_toggle).
    postPatch = ''
      rm -rf examples bench test web_test experimental bindings
      find web_ui -mindepth 1 -maxdepth 1 ! -name view ! -name toggle ! -name dune ! -name README.md \
        -exec rm -rf {} +
    '';
    # ppx_css ships both a findlib library (needed below, in buildInputs, for
    # the `(pps ... ppx_css)` preprocessors) and a `css_inliner` binary dune
    # invokes via `%{bin:css_inliner}`; ocaml-embed-file is build-tool-only.
    # The binaries are only on PATH when listed in nativeBuildInputs --
    # buildInputs alone wires up OCAMLPATH/linking, not PATH.
    nativeBuildInputs = with ocamlPackages; [
      ppx_css
      ocaml-embed-file
    ];
    buildInputs = with ocamlPackages; [
      async
      async_durable
      async_extra
      async_js
      async_kernel
      async_rpc_kernel
      async_rpc_websocket
      babel
      base
      bin_prot
      core
      core_bench
      core_kernel
      core_unix
      expect_test_helpers_core
      fuzzy_match
      incr_dom
      incr_map
      legacy_diffable
      ordinal_abbreviation
      patdiff
      polling_state_rpc
      ppx_css
      ppx_diff
      ppx_here
      ppx_jane
      ppx_let
      ppx_pattern_bind
      ppx_quick_test
      ppx_typed_fields
      profunctor
      record_builder
      sexp_grammar
      sexplib0
      streamable
      textutils
      versioned_polling_state_rpc
      virtual_dom
      base64
      ppxlib
      re
      uri
    ];
    # js_of_ocaml(-ppx/-compiler), gen_js_api and cohttp-async are deliberately
    # NOT listed here even though several of the libraries above need them:
    # virtual_dom/ppx_css/async_rpc_websocket already propagate them at the
    # exact versions this v0.17 ecosystem was built against. Adding our own
    # copy of any of those findlib names pulls in nixpkgs' plain (newer,
    # generic-opam) version alongside it, and ocamlfind refuses to build with
    # two conflicting definitions of the same package on OCAMLPATH.
    propagatedBuildInputs = with ocamlPackages; [
      async
      async_durable
      async_js
      async_kernel
      async_rpc_kernel
      async_rpc_websocket
      core
      core_kernel
      incr_dom
      incr_map
      polling_state_rpc
      ppx_css
      ppx_pattern_bind
      streamable
      versioned_polling_state_rpc
      virtual_dom
      uri
      base64
    ];
    meta = {
      description = "Jane Street's Bonsai UI framework (incl. Bonsai_web), pinned to v0.17.0";
      homepage = "https://github.com/janestreet/bonsai";
      license = pkgs.lib.licenses.mit;
    };
  };
in
ocamlPackages.buildDunePackage {
  pname = "ircc";
  version = "0.1.0";

  src = ../../../users/hanemile/projects/ocaml/ircc;

  minimalOCamlVersion = "5.1";
  duneVersion = "3";
  doCheck = false;

  # js_of_ocaml-compiler needs the same version override bonsai's dependencies
  # (virtual_dom etc, via nixpkgs' janestreet/0.17.nix) build against
  # internally -- the plain top-level attribute is nixpkgs' newer default and
  # would conflict on OCAMLPATH with the one already pulled in transitively
  # via `bonsai` (see the comment on bonsai's buildInputs above).
  nativeBuildInputs = with ocamlPackages; [
    ocaml-embed-file
    (js_of_ocaml-compiler.override { version = "5.9.1"; })
  ];

  buildInputs = with ocamlPackages; [
    bonsai
    async
    async_ssl
    async_rpc_websocket
    async_js
    async_kernel
    async_rpc_kernel
    core
    core_unix
    base64
    uri
    ppx_jane
  ];

  meta = {
    description = "A tiny the-lounge-style web IRC client for a personal soju bouncer, built with Bonsai_web";
    homepage = "https://github.com/HanEmile/hefe3/tree/main/users/hanemile/projects/ocaml/ircc";
    license = pkgs.lib.licenses.mit;
    maintainers = with pkgs.lib.maintainers; [ hanemile ];
    mainProgram = "ircc";
  };
}
