{ hefe, ... }:

let
  inherit (import ./utils.nix { inherit hefe; }) withDefault usersForHost;

  host = {
    medano = withDefault {

      # the nas currently syncs backups of photos to the grave on medano via rsync
      lampadas = [ [
        "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIARD1yc+6LpvGg5JcFc2jDFBv3kKRLXeNYqqFnCmAHkZ root@lampadas"
      ] ];

      corrino = [ [
        "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIGHcyg+yw1fwTfpbN1meNluaTXNcl6w1LIL0kz5Kan8I root@corrino"
      ] ];
    };
    naraj = withDefault { };
    arr = withDefault { };
    auth = withDefault { };
    md = withDefault { };
    irc = withDefault { };
    git = withDefault { };
    photo = withDefault { };
    social = withDefault { };

    miki = withDefault { }
      //
        # all users and their keys
        #
        # {
        #   abc = [ [ "..." "..." ] ];
        #   def = [ [ "..." ] ];
        #   ghi = [ [ "..." "..." ] ];
        # }
        builtins.mapAttrs (k: v: [ v.keys.all ]) (

          # have to remove these attributes manually, the rest are users
          builtins.removeAttrs hefe.users [
            "__readTree"
            "__readTreeChildren"
          ]
        );
    data = withDefault { };
    amalthea = withDefault { };
    late = withDefault { };
    dev1 = withDefault { };
    demo01 = withDefault { };
  };
in
{
  inherit host usersForHost;

  # Example usage:
  # users.users = hefe.ops.acl.usersForHost hefe.ops.acl.host.medano;
  #
  # ; cd ~/hefe3
  # nix-repl> :l
  # nix-repl> :p (ops.acl.usersForHost ops.acl.host.medano).users.git
  # {
  #   group = "git";
  #   isNormalUser = true;
  #   openssh = {
  #     authorizedKeys = {
  #       keys = [
  #         ... (all the keys)
  #       ];
  #     };
  #   };
  # }

  # View the machines settings like this:
  # nix-repl> :l
  # nix-repl> ops.nixos.<host>.config.users.users
  # {
  #   acme = { ... };
  #   messagebus = { ... };
  #   nginx = { ... };
  #   nixbld1 = { ... };
  #   nixbld10 = { ... };
  #   nixbld11 = { ... };
  # ...
}
