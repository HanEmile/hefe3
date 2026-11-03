{
  lib,
  stdenv,
  fetchFromSourcehut,
  SDL2,
}:

stdenv.mkDerivation (finalAttrs: {
	pname = "uxn2";
  version = "main";

  src = fetchFromSourcehut {
    owner = "~rabbits";
    repo = "uxn2";
    rev = "4ec66b5df3e9e31968f795b6bba17ec42068e6c6";
    hash = "sha256-Ukm+QTaKFc5b2w4S3837AN1bZtorx7zJlDVfXQaH/ow=";
  };

  outputs = [ "out" ];
  nativeBuildInputs = [ SDL2 ];
  buildInputs = [ SDL2 ];

  strictDeps = true;

  postPatch = "";
  buildPhase = ''
    mkdir -p $out
    mkdir -p $out/bin
    mkdir -p $out/rom
    make PREFIX="$out" install

    cat etc/utils/drifblim.rom.txt \
      | $out/bin/uxn2 etc/utils/xh.rom \
      > $out/rom/drifblim.rom

    cat << EOF > "$out/bin/drifblim"
    #!/usr/bin/env bash
    $out/bin/uxn2 $out/rom/drifblim.rom
    EOF
    chmod +x "$out/bin/drifblim"
  '';
  installPhase = ''
    echo ">>> INSTALL <<<"
  '';
  fixupPhase = "";

  meta = {
    homepage = "https://git.sr.ht/~rabbits/uxn2";
    description = "A graphical emulator for the Varvara Computer, written in C99(SDL2).";
    license = lib.licenses.mit;
    maintainers = with lib.maintainers; [ hanemile ];
    mainProgram = "uxn2";
    inherit (SDL2.meta) platforms;
  };
})
