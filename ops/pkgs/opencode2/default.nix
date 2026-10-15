{ ... }:
{
  lib,
  stdenv,
  fetchFromGitHub,
  callPackage,
  runCommand,
  makeWrapper,
}:

# OpenCode 2, packaged ahead of nixpkgs (26.05 ships 1.15.10, unstable
# 1.18.28). Used by caladan's home config and the code VM.
#
# The release tarball ships upstream's own nix build (nix/opencode.nix,
# nix/node_modules.nix), which this package reuses. Bump:
#
#   1. set `version` (latest tag: https://github.com/anomalyco/opencode/releases)
#   2. set the src hash to lib.fakeHash, build once, paste the real hash
#      from the error
#   3. check whether the ghostty-web rewrites below still apply
#      (substituteInPlace fails loudly if not)
#   4. refresh nodeModulesHashes (see its comment; a darwin host can harvest
#      all three platforms)
#
# Deviations from upstream's build:
#
# - bun comes from the nixpkgs rev the release pins in its own flake.lock;
#   our pinned channels have bun 1.3.x, which resolves github: lockfile deps
#   against a github.com/repos/... URL that 404s.
#
# - api.github.com is unreachable from here, and bun resolves github: deps
#   through the GitHub API. patchedSrc rewrites the ghostty-web github: dep
#   to the equivalent codeload tarball URL (lockfile format verified against
#   bun 1.4.2: spec id is the full URL, integrity is the codeload tarball's
#   sha512, patchedDependencies keys track the new spec id). Since
#   upstream's nix/node_modules.nix builds its own src, a patched variant is
#   vendored in ./node_modules.nix; nix/opencode.nix itself is still taken
#   from the tarball. On version bumps, check whether the ghostty-web
#   rewrites below still apply (substituteInPlace fails loudly if not).
#
# - bin/opencode is rewrapped with OPENCODE_DISABLE_AUTOUPDATE=true
#   (nix-managed install; the v1 nixpkgs wrapper did the same).
#
# V1 -> V2 migration notes (config, .opencode/ files): the CLI reads V1
# configuration unchanged and auto-migrates it; manual field renames are
# documented at https://opencode.ai/v2/docs/migrate-v1/
let
  version = "2.0.29";

  src = fetchFromGitHub {
    owner = "anomalyco";
    repo = "opencode";
    tag = "v${version}";
    hash = "sha256-qfT8gQCyGahr5v7v95wNArXrBOAnfQZja1WyJkaCpJI=";
  };

  ghosttyWebRev = "83c0a07b8628b748aed073b232cb4b52a6ca11c1";
  ghosttyWebUrl = "https://codeload.github.com/anomalyco/ghostty-web/tar.gz/${ghosttyWebRev}";

  # Release tarball with the ghostty-web dep rewritten from github:
  # shorthand (resolves via api.github.com, unreachable here) to the
  # codeload tarball URL. The three bun.lock edits cover: workspace dep
  # specs (full rev), the packages-index entry incl. integrity of the
  # codeload tarball, and the patchedDependencies key.
  patchedSrc = runCommand "opencode-${version}-src" { } ''
    cp -R ${src} $out
    chmod -R u+w $out

    substituteInPlace \
      $out/bun.lock \
      $out/packages/app/package.json \
      $out/packages/gui-extensions/package.json \
      --replace-fail \
        "github:anomalyco/ghostty-web#${ghosttyWebRev}" \
        "${ghosttyWebUrl}"

    substituteInPlace $out/bun.lock \
      --replace-fail \
        '["ghostty-web@github:anomalyco/ghostty-web#83c0a07", {}, "anomalyco-ghostty-web-83c0a07", "sha512-Lf2v1agHkVUpMpHBWWuCZrhOEmcwwin5/Hboc9rZwQ7/CKkIh5rU1r1CvfLlhkMoFv+ed8z52RZ8hkzGZZj3MQ=="]' \
        '["ghostty-web@${ghosttyWebUrl}", {}, "sha512-yjn1vfQRVFt6k2/yyIyKyxPY+LKaB7Y33cTzhPv+1wpDMdqPdJE6uoCXORb6VIXgrPb4R8vi71M2G6/aN3g/CQ=="]' \
      --replace-fail \
        '"ghostty-web@github:anomalyco/ghostty-web#83c0a07":' \
        '"ghostty-web@${ghosttyWebUrl}":'
  '';

  # nixpkgs rev (and thereby bun) pinned by the release itself.
  upstreamNixpkgs =
    let
      locked =
        (builtins.fromJSON (builtins.readFile (src + "/flake.lock"))).nodes.nixpkgs.locked;
    in
    import (builtins.fetchTarball {
      url = "https://github.com/${locked.owner}/${locked.repo}/archive/${locked.rev}.tar.gz";
      sha256 = locked.narHash;
    }) {
      inherit (stdenv.hostPlatform) system;
      config = { };
    };

  bun = upstreamNixpkgs.bun;

  # Per-platform node_modules FOD hashes for THIS build (patched ghostty-web
  # dep + bun-1.4.2 resolution), not the values from nix/hashes.json: those
  # don't reproduce in this environment. Hashes for the other platforms can
  # be harvested from any darwin host via passthru.nodeModulesFor, e.g.:
  #
  #   nix-build -E 'let h = import ./default.nix { };
  #                     p = import (import ./third_party { })."nixos-unstable"
  #                       { system = "aarch64-darwin"; config = { }; };
  #                 in (p.callPackage h.ops.pkgs.opencode2 { }).passthru.nodeModulesFor {
  #                      bunCpu = "x64"; bunOs = "linux"; }'
  #
  # and pasting the hash from the fixed-output mismatch error in here.
  nodeModulesHashes = {
    "aarch64-darwin" = "sha256-m/8AABYJ0Tc02OJsrMD7TgnqwRWdsxgwTw4hdLmcrL0=";
    "aarch64-linux" = "sha256-l21E8qLDScqFH1n9etzg9sbqn8hawFIW9PuGck6MrSo=";
    "x86_64-linux" = "sha256-amnWnuX53EvQ+y858Fq/QVtpW4MXdS7hqDdXJsn+pX4=";
  };

  mkNodeModules =
    args:
    callPackage ./node_modules.nix (
      {
        inherit bun;
        # Baked into OPENCODE_VERSION (upstream uses the git rev or "dirty",
        # which a tarball can't provide); "release" yields 2.0.29+release.
        rev = "release";
        srcFull = patchedSrc;
        scriptsDir = src + "/nix/scripts";
      }
      // args
    );

  node_modules = mkNodeModules {
    hash =
      nodeModulesHashes.${stdenv.hostPlatform.system}
        or (throw "opencode2: no node_modules hash for ${stdenv.hostPlatform.system}");
  };

  cli = callPackage (src + "/nix/opencode.nix") {
    inherit bun node_modules;
  };
in
# Nix-managed install: disable the built-in updater without touching
# upstream's build. bin/opencode is rewrapped with
# OPENCODE_DISABLE_AUTOUPDATE=true; everything else is symlinked.
(runCommand "opencode-${cli.version}"
  {
    nativeBuildInputs = [ makeWrapper ];
  }
  ''
    mkdir -p $out/bin $out/share
    ln -s ${cli}/share/* $out/share/
    makeWrapper ${cli}/bin/opencode $out/bin/opencode \
      --set OPENCODE_DISABLE_AUTOUPDATE true
    ln -s ${cli}/bin/opencode2 $out/bin/opencode2
  ''
)
// {
  inherit (cli) meta;

  passthru = {
    # Build the node_modules FOD for another bun --cpu/--os target; used to
    # harvest the nodeModulesHashes entries above from a darwin host:
    # nodeModulesFor { bunOs = "linux"; bunCpu = "arm64"; }
    nodeModulesFor =
      { bunCpu, bunOs, hash ? lib.fakeHash }:
      mkNodeModules { inherit bunCpu bunOs hash; };

    inherit node_modules;
  };
}
