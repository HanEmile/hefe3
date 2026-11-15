{ pkgs, stdenv, ... }:

stdenv.mkDerivation {
  name = "vokobe2";
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
    cp vokobe2 $out/bin/vokobe2
   
    wrapProgram $out/bin/vokobe2 --set LD_LIBRARY_PATH "$LD_LIBRARY_PATH"
  '';

  dontStrip = true;
}
