{ hefe }:

let
  withDefault = let
    default.root = [
      (
        hefe.users.hanemile.keys.all
        ++ [
          "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIGHcyg+yw1fwTfpbN1meNluaTXNcl6w1LIL0kz5Kan8I root@corrino"
        ]
      )
    ];
  in x: default // x;

  flattenKeys = lists: builtins.concatLists lists;

  mkUserEntry =
    user: keyLists:
    {
      openssh.authorizedKeys.keys = flattenKeys keyLists;
    }
    // (
      if user != "root" then
        {
          isNormalUser = true;
          group = "${user}";
          extraGroups = [ "users" ];
          home = "/home/${user}";
          createHome = true;
          shell = "/bin/sh";
        }
      else
        { }
    );

  # As the first argument, provide a `ops.acl.<host>`
  # This function generates a config ready to import in a vm/machine
  usersForHost = host: {
    users = builtins.mapAttrs mkUserEntry host;

    # make sure groups for all users exist
    groups = builtins.mapAttrs (_: _: { }) host;
  };
in
{
  inherit withDefault flattenKeys mkUserEntry usersForHost;
}
