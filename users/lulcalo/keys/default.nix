{ ... }:

let
  # merges the list of keys below with a list containing a key "all" with
  # the value being a list of all key values
  # src: the tvl monorepo, //users/tazjin/keys/default.nix as of 2025-10-07
  withAll = keys: keys // { all = builtins.attrValues keys; };
in
withAll {
  # fetched from github on `2025-11-26 12:22 UTC+8`
  a = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIAwd+c9yV/PlppstxGyELiX/TVYqLj8SLYD2folqhtoJ";
}
