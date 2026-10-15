{ hefe, pkgs, lib, pkgs-unstable, ... }:
{ config, ... }:

let
  ipam = hefe.ops.ipam.code.code;

  # OpenCode 2; shared with caladan's home config, see ops/pkgs/opencode2
  # (nixpkgs still ships v1). The seeded ~/.config/opencode below is read
  # unchanged by the v2 client; migration notes:
  # https://opencode.ai/v2/docs/migrate-v1/
  opencode = pkgs-unstable.callPackage hefe.ops.pkgs.opencode2 { };

  # opencode config from caladan (~/.config/opencode). Contains provider
  # API keys inline, so it must never enter the git tree: read at eval time
  # (deploys evaluate on caladan, per repo convention). node_modules is
  # dropped: it holds darwin-arm64 native addons; opencode re-fetches
  # provider packages on the VM.
  opencodeConfig = builtins.path {
    name = "opencode-config";
    path = /Users/emile/.config/opencode;
    filter = path: type: baseNameOf path != "node_modules";
  };
in
{
  imports = [
    ../hardware-image.nix
    (import ../vm-base.nix { vmhost = "medano"; primaryBridge = "code"; } { inherit hefe pkgs; })
    ../modules/healthProbes.nix
  ];

  networking.hostName = "code";
  system.stateVersion = "25.05";

  # Sandboxed coding-agent box, attached to the isolated "code" bridge
  # (virbr3 on medano, 192.168.35.0/24). Outbound internet works via NAT;
  # east-west access into virbr0/virbr1/virbr2 is dropped on medano, so an
  # agent gone rogue can't reach the other VMs or medano's services.

  # agents drive nix themselves (nix build/develop in throwaway worktrees)
  nix.settings.experimental-features = [ "nix-command" "flakes" ];

  # mosh server + UDP 60000-61000 in the firewall (reach it via tailscale)
  programs.mosh.enable = true;

  # (opencode comes from the let binding above, not pkgs)
  environment.systemPackages = (with pkgs; [
    gcc
    gnumake
    go
    rustc
    cargo
    python3
    nodejs
    wget
    curl
    gdb
    strace
    jq
    tmux
  ]) ++ [ opencode ];

  # Seed emile's ~/.config/opencode from caladan's copy (re-synced on every
  # switch/boot; edits made inside the VM don't survive a redeploy).
  system.activationScripts.opencode-config = {
    deps = [ "users" ];
    text = ''
      install -d -m 750 -o emile -g emile /home/emile/.config
      ${pkgs.rsync}/bin/rsync -a --delete --chmod=Du+w,Fu+w ${opencodeConfig}/ /home/emile/.config/opencode/
      chown -R emile:emile /home/emile/.config/opencode
    '';
  };

  # agents need root for package/service management inside the sandbox
  security.sudo.extraRules = [{
    users = [ "emile" ];
    commands = [{ command = "ALL"; options = [ "NOPASSWD" ]; }];
  }];

  services.healthProbes.probes = [
    { name = "node-exporter"; url = "http://${ipam.v4}:9100/metrics"; }
  ];
}
