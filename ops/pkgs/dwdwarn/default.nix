{ ... }:
{
  lib,
  buildGoModule,
}:

buildGoModule (finalAttrs: {
  pname = "dwdwarn";
  version = "0.0.1";

  src = ./.;

  vendorHash = null;

  modRoot = ".";
  subPackages = [ "." ];

  doCheck = false;

  meta = {
    description = "Posts Deutscher-Wetterdienst weather warnings for Düsseldorf to an IRC channel";
    longDescription = ''
      Polls the DWD WarnWetter feed for the Düsseldorf warn cells and posts
      newly active warnings (plus an all-clear when they expire) to an IRC
      channel, staying joined to the channel continuously.
    '';
    homepage = "https://github.com/HanEmile/hefe3/tree/main/ops/pkgs/dwdwarn";
    license = lib.licenses.mit;
    maintainers = with lib.maintainers; [ hanemile ];
    mainProgram = "dwdwarn";
  };
})
