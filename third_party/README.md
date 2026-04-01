# npins

```bash
$ npins show
$ npins add ...
$ npins add git https://github.com/oxalica/rust-overlay
```

```nix
let
  sources = import ./npins;
  pkgs = import sources.nixpkgs {};
in
  …
```

or how I like to do it:

```nix
let
  sources = hefe.third_party;
  nixos = sources."nixos-25.05";

  pkgs = import nixos {
    system = "x86_64-linux"; # target architecture
    config = { };
  };

  lib = import (nixos + "/lib");
in
  ...
```
