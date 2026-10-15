{ ... }:
{
  lib,
  buildGoModule,
}:

buildGoModule (finalAttrs: {
  pname = "tagesschau";
  version = "0.0.1";

  src = ./.;

  vendorHash = null;

  modRoot = ".";
  subPackages = [ "." ];

  doCheck = false;

  meta = {
    description = "Posts the top three tagesschau.de articles to an IRC channel";
    longDescription = ''
      Scrapes the top three headlines off the ARD Text teletext front page
      (https://www.ard-text.de/mobil/101) and posts them to an IRC channel
      at 08:00, 16:00 and 00:00 Europe/Berlin, staying joined to the channel
      continuously.
    '';
    homepage = "https://github.com/HanEmile/hefe3/tree/main/ops/pkgs/tagesschau";
    license = lib.licenses.mit;
    maintainers = with lib.maintainers; [ hanemile ];
    mainProgram = "tagesschau";
  };
})
