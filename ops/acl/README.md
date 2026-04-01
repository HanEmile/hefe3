# ACL (Access Control Lists)

This `default.nix` contains information on who has access to what host.

The `hosts` attribute contains attributes representing the individual hosts:

```nix
{
  hosts = {
    abc = withDefault {
        alice = [ hefe.users.alice.keys.all ];
        bob   = [
          hefe.users.bob.keys.all
          # ... more keys for bob
        ];
        # ... more users on "abc"
    };
    # ... more hosts
  }
}
```

The `usersForHosts` function can be passed such an entry, it then generates an attribute set that can be used in the machine config.

```nix
  # ...
  users = 
  let
    aclconf = with acl; (usersForHost host."${hostname}");
  in {
    users = aclconf.users;
    groups = aclconf.groups;
  }
  # ...
```
