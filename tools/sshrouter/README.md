# SSH Router

Routes SSH connections based on username to target hosts.

## readTree Access

- `hefe.tools.sshrouter.package` - the sshrouter binary
- `hefe.tools.sshrouter.module` - NixOS module

## Usage

```nix
{ hefe, ... }:
{ ... }:

{
  imports = [ hefe.tools.sshrouter.module ];

  services.sshrouter = {
    enable = true;
    port = 2222;
    routes = {
      alice = "192.168.75.10:22";
      "guest*" = "192.168.75.20:22";  # wildcard
    };
    default = "192.168.75.2:22";
  };
}
```
