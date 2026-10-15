{ ... }:
{
  lib,
  buildGoModule,
}:

buildGoModule (finalAttrs: {
  pname = "weatherman";
  version = "0.0.1";

  src = ./.;

  vendorHash = null;

  modRoot = ".";
  subPackages = [ "." ];

  doCheck = false;

  meta = {
    description = "Posts a daily Düsseldorf forecast to an IRC channel";
    longDescription = ''
      Fetches a forecast from the Open-Meteo API and posts it to an IRC
      channel once a day at 08:00 Europe/Berlin, staying joined to the
      channel continuously as a long-running daemon.
    '';
    homepage = "https://github.com/HanEmile/hefe3/tree/main/ops/pkgs/weatherman";
    license = lib.licenses.mit;
    maintainers = with lib.maintainers; [ hanemile ];
    mainProgram = "weatherman";
  };
})
