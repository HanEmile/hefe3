{ ... }:

let
  # merges the list of keys below with a list containing a key "all" with
  # the value being a list of all key values
  # src: the tvl monorepo, //users/tazjin/keys/default.nix as of 2025-10-07
  withAll = keys: keys // { all = builtins.attrValues keys; };
in
withAll {
  caladan = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIPZi43zHEsoWaQomLGaftPE5k0RqVrZyiTtGqZlpWsew emile@caladan";
  caladan-root = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIHksIESvpl87sNAAxXSwhp6zn9jX4AG1/yY8PtU+C5qf root@caladan.local";
}
