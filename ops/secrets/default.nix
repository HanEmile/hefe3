args:
let
  mkSecrets = import ./mkSecrets.nix args;
in
  mkSecrets ./. (import ./secrets.nix) // { inherit mkSecrets; }
