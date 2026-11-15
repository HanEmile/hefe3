{ pkgs, stdenv, ... }:

stdenv.mkDerivation {
  name = "vokobe3";
  src = pkgs.nix-gitignore.gitignoreSource [] ./.;
  buildInputs = with pkgs; [ sbcl openssl.out makeWrapper ]; 

  unpackPhase = ''
    cp -r $src/* .
  '';
  buildPhase = ''
    export HOME=$(pwd)
    export LD_LIBRARY_PATH="${pkgs.openssl.out}/lib/"
    make
  '';

  installPhase = ''
    export LD_LIBRARY_PATH="${pkgs.openssl.out}/lib/"

    mkdir -p $out/bin/
    cp vokobe3 $out/bin/vokobe3
   
    wrapProgram $out/bin/vokobe3 --set LD_LIBRARY_PATH "$LD_LIBRARY_PATH"
  '';

  dontStrip = true;
}
