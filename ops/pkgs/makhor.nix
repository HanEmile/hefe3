{ pkgs, ... }:

let
  inherit (pkgs) buildGoModule lib;
in
buildGoModule {
  pname = "makhor";
  version = "0.1.0";

  src = ../../tools/makhor;

  doCheck = false; # disable running tests, otherwise if test fails package fails

  vendorHash = "sha256-OwoO6lHKSqfy+7nDUU6RhLUqn7ccIUOnLipBv1dhwmo=";

  # Copy templates alongside the binary
  postInstall = ''
    mkdir -p $out/share/makhor
    cp -r templates $out/share/makhor/
  '';

  meta = with lib; {
    description = "A minimal link aggregator inspired by Lobste.rs and Hacker News";
    homepage = "https://github.com/emile/makhor";
    license = licenses.mit;
    maintainers = [ ];
    mainProgram = "makhor";
  };
}
