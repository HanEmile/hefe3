{ ... }:
{
  lib,
  buildGoModule,
}:

buildGoModule (finalAttrs: {
  pname = "syswatch";
  version = "0.0.1";

  src = ./.;

  vendorHash = null;

  modRoot = ".";
  subPackages = [ "." ];

  doCheck = false;

  meta = {
    description = "Posts system anomaly alerts for the host it runs on to an IRC channel";
    longDescription = ''
      Runs on the host being watched and posts transitions to an IRC
      channel: failed or stale restic-backups-*.service units, other failed
      systemd units, filesystems with little free space left (cascading
      15/10/5/3/2/1%-remaining thresholds), low MemAvailable and high load.
      Stays joined to the channel continuously as a long-running daemon.
      Unlike the irc-VM bots this one runs on lampadas and connects through
      the public Ergo ingress over TLS (--tls).
    '';
    homepage = "https://github.com/HanEmile/hefe3/tree/main/ops/pkgs/syswatch";
    license = lib.licenses.mit;
    maintainers = with lib.maintainers; [ hanemile ];
    mainProgram = "syswatch";
  };
})
