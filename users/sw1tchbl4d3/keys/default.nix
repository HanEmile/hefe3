{ ... }:

let
  # merges the list of keys below with a list containing a key "all" with
  # the value being a list of all key values
  # src: the tvl monorepo, //users/tazjin/keys/default.nix as of 2025-10-07
  withAll = keys: keys // { all = builtins.attrValues keys; };
in
withAll {
  china = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIFXhzP1GnroI7BiI3lH/8pzjdgBU2IANl9PtkBxGV/L0 sw1tchbl4d3@phic";
}
