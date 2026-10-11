{ ... }:
{
  lib,
  buildGoModule,
}:

buildGoModule (finalAttrs: {
  pname = "xkcdbot";
  version = "0.0.1";

  src = ./.;

  vendorHash = null;

  modRoot = ".";
  subPackages = [ "." ];

  doCheck = false;

  meta = {
    description = "Posts new xkcd comics to an IRC channel as they're published";
    longDescription = ''
      Polls xkcd's latest-comic JSON endpoint and posts to an IRC channel via
      a minimal raw-protocol connect/join/privmsg/quit whenever the comic
      number advances past the last one seen (tracked in a state file),
      meant to be run periodically from a systemd timer.
    '';
    homepage = "https://github.com/HanEmile/hefe3/tree/main/ops/pkgs/xkcdbot";
    license = lib.licenses.mit;
    maintainers = with lib.maintainers; [ hanemile ];
    mainProgram = "xkcdbot";
  };
})
