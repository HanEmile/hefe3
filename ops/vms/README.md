# vms

Definitions for the libvirt VMs running on the `medano` hypervisor
(`ops/vms/x86/<name>/`).

Each VM directory has two halves:

- `default.nix` — what runs **inside** the VM. Imports `vm-base.nix`
  (network from IPAM, ssh, tailscale, node-exporter) and expands on it.
- `libvirt.nix` — the VM's **external** domain config as seen from the
  hypervisor. Imports `libvirt-base.nix`, passes nixvirt, and defines
  name/uuid/memory/interfaces/vmdisk. Wired into
  `ops/machines/x86/medano/default.nix` via `(vm "<name>")`.

A minimal `default.nix`:

```nix
{ hefe, pkgs, ... }:
{ config, ... }:

{
  imports = [
    (import ../vm-base.nix { vmhost = "medano"; } { inherit hefe pkgs; })
  ];

  networking.hostName = "vm-name";
  system.stateVersion = "25.05";
}
```

A minimal `libvirt.nix`:

```nix
{ nixvirt, ... }:

import ../libvirt-base.nix { inherit nixvirt; } {
  name = "vm-name";
  uuid = "..."; # uuidgen
  memory = 2; # GB
  interfaces = [ "virbr0" ];
  vmdisk = /keep/pools/vmpool/vm-name.qcow2;
}
```

## Creating a new VM

See `ops/vms/x86/README.md` for the current flow: VMs are bootstrapped
from pre-built qcow2 images (`nix-build -A ops.nixos.<name>.deploy_image`),
not from an installer ISO. The root README's "Bootstrapping a new VM"
section has the step-by-step checklist (IPAM, ACL, readTree exception,
medano wiring, first boot, host key into secrets, tailscale).
