{ hefe, pkgs, ... }:
{ config, ... }:

let
  ipam = hefe.ops.ipam.default.r2wars;
in
{
  imports = [
    ../hardware-image.nix
    (import ../vm-base.nix { vmhost = "medano"; } { inherit hefe pkgs; })
    ../modules/healthProbes.nix
    (import ../modules/backups.nix { inherit hefe; })
    ../../../modules/r2wars-web.nix
  ];

  networking.hostName = "r2wars";
  system.stateVersion = "25.05";

  # r2wars used to be just a radare2 project workspace; it now also runs the
  # r2wars-web service itself. State lives in the VM's own qcow2 disk per
  # design - not on /grave - so it's self-contained. /var/lib/r2wars predates
  # the service (source + sqlite DBs landed there after migration from
  # corrino) - kept as-is rather than renamed, so nothing has to move.
  networking.firewall.interfaces."enp1s0".allowedTCPPorts = [ ipam.ports.web ];

  age.secrets.r2wars_web_environment_file = {
    file = hefe.ops.secrets."r2wars_web_environment_file.age";
    owner = "r2wars-web";
    group = "r2wars-web";
  };

  # Reused as-is from the mail@emile.space SMTP account already used by
  # makhor/immich/etc (see ops/secrets.nix) - same secret, read-only, no
  # content change, so nothing else that depends on it is affected.
  age.secrets.mail_emile_space_password = {
    file = hefe.ops.secrets."mail_emile_space_password.age";
    owner = "r2wars-web";
    group = "r2wars-web";
  };

  services.r2wars-web = {
    enable = true;
    package = hefe.users.hanemile.projects.go.r2wars-web;
    address = ipam.v4;
    port = ipam.ports.web;
    dataDir = "/var/lib/r2wars";
    environmentFile = config.age.secrets.r2wars_web_environment_file.path;

    baseUrl = "https://r2wa.rs";
    smtp = {
      host = "mail.emile.space";
      username = "mail@emile.space";
      # No display name here: ExecStart is a plain whitespace-split command
      # line (not a shell), so "r2wars <mail@emile.space>" would be split
      # into multiple argv entries instead of staying one address.
      from = "mail@emile.space";
      passwordFile = config.age.secrets.mail_emile_space_password.path;
    };
  };

  # Useful tooling for actually doing radare2 work after ssh'ing in.
  #
  # go and rizin dropped for now (2026-10-04): the VM's root disk is only
  # 5.6G and didn't have room for them alongside the new r2wars-web service
  # closure (agenix, health probes, the Go toolchain needed to build the
  # service itself, ...). radare2 is still here for interactive use; the
  # service itself gets its own radare2 via the systemd unit's `path`, so
  # this isn't load-bearing for it. Grow the disk and re-add if wanted.
  environment.systemPackages = with pkgs; [
    radare2
    git
    tmux
  ];

  vmBackups.paths = [
    "/var/lib/r2wars"
  ];

  services.healthProbes.probes = [
    { name = "self"; url = "http://${ipam.v4}:${toString ipam.ports.web}/"; }
    { name = "node-exporter"; url = "http://${ipam.v4}:9100/metrics"; }
  ];
}
