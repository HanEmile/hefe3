{ ... }:
{
  lib,
  stdenv,
  fetchFromGitHub,
  makeWrapper,
  gdb,
  python3,
  bintools-unwrapped,
  file,
  ps,
}:

# bata24's GEF fork (hugsy/gef is more or less unmaintained; this fork tracks
# modern kernels/gdb and is what the local clone at
# ~/src/github.com/bata24/gef mirrors).
#
# Modeled on nixpkgs' hugsy gef package: gef.py goes to $out/share/gef and
# bin/gef wraps gdb so it loads gef on startup. Unlike upstream hugsy gef.py,
# bata24's fork doesn't understand nixpkgs' NIX_PYTHONPATH trampoline, so the
# python deps (used lazily by assemble/rop/unicorn commands) are exposed to
# gdb's embedded python via PYTHONPATH directly.
#
# Local clone for reference: /Users/emile/src/github.com/bata24/gef
# Bump: advance rev/hash (see ops/pkgs/syzkaller for the convention).

let
  pythonPath =
    with python3.pkgs;
    makePythonPath [
      capstone
      keystone-engine
      unicorn
      ropper
    ];
in
stdenv.mkDerivation (finalAttrs: {
  pname = "gef";
  version = "0-unstable-2026-10-13";

  src = fetchFromGitHub {
    owner = "bata24";
    repo = "gef";
    rev = "c2ae20e536982ae1748ebda5fece2c6017845491";
    hash = "sha256-3vEqeJRiXQKdRus9sDOPeUZknJqVGZ7GBNOyWMcrY0c=";
  };

  dontBuild = true;

  nativeBuildInputs = [ makeWrapper ];

  installPhase = ''
    runHook preInstall

    mkdir -p $out/share/gef
    cp gef.py $out/share/gef

    # gdb runs the .py file with __name__ == "__main__", which is exactly how
    # gef.py registers itself (it ends in `if __name__ == "__main__": Gef.main()`).
    makeWrapper ${gdb}/bin/gdb $out/bin/gef \
      --add-flags "-q -x $out/share/gef/gef.py" \
      --prefix PYTHONPATH : ${pythonPath} \
      --prefix PATH : ${
        lib.makeBinPath [
          python3
          bintools-unwrapped # for readelf
          file
          ps
        ]
      }

    runHook postInstall
  '';

  meta = {
    description = "Modern experience for GDB with advanced debugging features for exploit developers & reverse engineers (bata24 fork)";
    mainProgram = "gef";
    homepage = "https://github.com/bata24/gef";
    license = lib.licenses.mit;
    platforms = lib.platforms.unix;
  };
})
