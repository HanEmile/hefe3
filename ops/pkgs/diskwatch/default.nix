{ ... }:
{
  lib,
  buildGoModule,
}:

buildGoModule (finalAttrs: {
  pname = "diskwatch";
  version = "0.0.1";

  src = ./.;

  vendorHash = null;

  modRoot = ".";
  subPackages = [ "." ];

  doCheck = false;

  meta = {
    description = "Posts disk-usage alerts for fleet hosts to an IRC channel";
    longDescription = ''
      Polls each fleet host's node-exporter /metrics for root filesystem
      usage and posts transitions above/below the warn/crit thresholds to an
      IRC channel, staying joined to the channel continuously.
    '';
    homepage = "https://github.com/HanEmile/hefe3/tree/main/ops/pkgs/diskwatch";
    license = lib.licenses.mit;
    maintainers = with lib.maintainers; [ hanemile ];
    mainProgram = "diskwatch";
  };
})
