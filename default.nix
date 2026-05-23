{
  localSystem ? builtins.currentSystem,
  crossSystem ? null,
  ...
}@args:

let
  readTree = import ./nix/readTree { };

  usersFilter = readTree.restrictFolder {
    folder = "users";
    reason = ''
 			Code under //users is not considered stable or dependable in the
 			wider hefe context. If a project under //users is required by
 			something else, please move it to a different path.
		'';
    exceptions = [
      # The access control lists access the ssh keys in the users folders
      [ "ops" "acl" ]

      # machines is allowed to access //users for several reasons:
      #
      # 1. User SSH keys are set in //users
      # 2. Some personal websites or demo projects are served from there
      [ "ops" "machines" "x86" "arr" ]
      [ "ops" "machines" "x86" "caladan" ]
      [ "ops" "machines" "x86" "lampadas" ]
      [ "ops" "machines" "x86" "naraj" ]
      [ "ops" "machines" "x86" "medano" ]
        [ "ops" "vms" "x86" "md" ]
        [ "ops" "vms" "x86" "arr" ]
        [ "ops" "vms" "x86" "auth" ]
        [ "ops" "vms" "x86" "naraj" ]
        [ "ops" "vms" "x86" "rou" ]
        [ "ops" "vms" "x86" "git" ]
        [ "ops" "vms" "x86" "data" ]
        [ "ops" "vms" "x86" "miki" ]
        [ "ops" "vms" "x86" "photo" ]
        [ "ops" "vms" "x86" "rss" ]
        [ "ops" "vms" "x86" "social" ]
        [ "ops" "vms" "x86" "tmp" ]
        [ "ops" "vms" "x86" "amalthea" ]
        [ "ops" "vms" "x86" "late" ]
        [ "ops" "vms" "x86" "demo01" ]
    ];
  };

  readHefe =
    args:
    readTree {
      inherit args;
      path = ./.;
      filter = parts: args: (usersFilter parts args);
      scopedArgs = {
        __find_file = _: _: throw "No not import from NIX_PATH within hefe";
        builtins = builtins // {
          currentSystem = throw "Use localSystem from the readTree args instead of builtins.currentSystem!";
        };
      };
    };

  sources = import ./third_party {};

  # nixos
  nixos = sources."nixos-25.11";
  pkgs = import nixos { system = localSystem; config = {}; };
  lib = import (nixos + "/lib");

  # nixos-unstable
  nixos-unstable = sources."nixos-unstable";
  pkgs-unstable = import nixos-unstable { system = localSystem; config = {}; };
in
readTree.fix (
  self:
  (readHefe {
    inherit localSystem crossSystem;
    hefe = self; # this is the "hefe" that can be accessed from all files in this repo

    system = localSystem;

    inherit lib pkgs pkgs-unstable;
    sources = self.third_party;  # Add this!
    externalArgs = args;
  })
  // {
    # Make the path to the hefe available for things that might need it
    # (e.g. NixOS module inclusions)
    # path = self.third_party.nixpkgs.lib.cleanSourceWith {
    path = lib.cleanSourceWith {
      name = "hefe";
      src = ./.;
      filter = lib.cleanSourceFilter;
    };
  }
)
