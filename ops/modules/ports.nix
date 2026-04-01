{ lib, ... }:

# Allows the definition of an arbitrary attribute set with keys being services
# and their values being their ports
# This is helpful for defining services and referring to this, but in order to
# do so, this service must exist
with lib;
{
  options.local.ports = mkOption { type = types.anything; };
}

