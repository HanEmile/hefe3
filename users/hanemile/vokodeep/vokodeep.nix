{ pkgs, stdenv, ... }:

stdenv.mkDerivation {
  name = "vokodeep";
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
    cp vokodeep $out/bin/vokodeep
   
    wrapProgram $out/bin/vokodeep --set LD_LIBRARY_PATH "$LD_LIBRARY_PATH"
  '';

  dontStrip = true;
}
