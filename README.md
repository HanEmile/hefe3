# hefe3

```
  *----*
 /|   /|
*----* |
| *--|-*
|/   |/
*----*
```

Welcome to `hefe3`, a monorepo for managing personal projects and infrastructure, built on the principles of the TVL monorepo and powered by Nix.

## Repository Structure

This monorepo is organized into several key directories:

-   `//ops`: Contains NixOS configurations for physical machines and virtual machines.
-   `//tools`: Houses shared tools and utilities, like `sshrouter`.
-   `//users`: A place for personal projects and experiments.
-   `//data`: For storing data and documentation.
-   `//third_party`: Manages external dependencies using `npins`.

The infrastructure is defined hierarchically:

-   **Machines (Hypervisors):** `//ops/machines/<machinename>/default.nix`
-   **Virtual Machines (libvirt):** `//ops/vms/<vmname>/libvirt.nix` (for libvirt definitions and IP configuration)
-   **Virtual Machines (config):** `//ops/vms/<vmname>/default.nix` (for VM-specific configuration)

## Development

### Running Applications

To run an application, you can build it with `nix-build` and then execute the binary from the `result` directory.

**Example: Running the `wok` application**

```bash
# Build and run the native version
nix-build -A users.hanemile.wok.native && ./result/bin/wok

# Build and serve the web version
nix-build -A users.hanemile.wok.web && python3 -m http.server -d result -b ::1 8081
```

### Using the Nix REPL

The Nix REPL is a great way to interactively explore the Nix configurations in this repository.

```nix
# Start the REPL
nix repl

# Load the monorepo's Nix expressions
nix-repl> :l .
Added 7 variables.
__readTree, __readTreeChildren, nix, ops, path, third_party, users

# You can then inspect the configurations, for example, to get access to the `lib` from nixpkgs:
nix-repl> lib = import (third_party."nixos-25.11" + "/lib")
```

## Deployment

Deploying changes to the infrastructure is done using `nix-build` to build the desired configuration and then running the deploy script.

The main deployment configurations are located in `//ops/nixos.nix`. You can see the available deployment targets by running `nix-build -A ops.nixos.` and pressing `<TAB>`.

**Example: Deploying the `medano` hypervisor**

```bash
nix-build -A ops.nixos.medanoDeploy && timeout 600 time ./result/bin/deploy
```

**Example: Deploying a VM**

```bash
# Deploy the 'arr' VM
nix-build -A ops.nixos.arr.deploy && timeout 600 time ./result/bin/deploy

# Deploy the 'rou' VM
nix-build -A ops.nixos.rou.deploy && timeout 600 time ./result/bin/deploy
```

## System Administration

This section contains common administrative tasks.

### Accessing Services

**SFTP:** You can connect to the SFTP service using the following command:

```bash
sftp emile@data.pinto-pike.ts.net
```

### Unlocking Encrypted Drives

To unlock the ZFS encrypted drives on a machine, you can use the following commands after connecting to the unlock shell:

```bash
# Connect to the unlock shell on medano
ssh medano-unlock -p 2222

# From the unlock shell, import the zpool and load the keys
~ # zpool import -a; zfs load-key -a && killall zfs
```

### Accessing VM Consoles

To access the console of a virtual machine, you need to use `virsh console` and specify the console number.

```bash
# Get the list of running VMs
virsh list

# Connect to the 'auth' VM on its text console (console1)
virsh console auth console1
```

**Note:** Connecting without specifying the console number (e.g., `virsh console auth`) will connect you to the serial console 0, which might not be what you want.

### NFS Mounts

To share a ZFS dataset via NFS, you can use `zfs set sharenfs`:

```bash
# Example: Share the 'grave/media' dataset with read-write access for a specific IP
zfs set sharenfs='rw=@192.168.33.3,rw' grave/media
```

## Networking

The networking for the virtual machines is managed by `libvirt`.

### DHCP Leases

You can check the DHCP leases for the different network bridges:

```bash
# Check leases for the 'default' bridge
virsh net-dhcp-leases default

# Check leases for all bridges
echo default; virsh net-dhcp-leases default; echo private; virsh net-dhcp-leases private; echo rou; virsh net-dhcp-leases rou
```

### Manual IP Allocation

Manual IP address assignments are tracked in `//ops/ipam/default.nix`. This file is used to configure static IPs for the VMs.

### Network Bridges

There are several network bridges configured for the VMs:

#### `default` (virbr0)

A default bridge for VMs to communicate with each other.

-   **Subnet:** `192.168.75.0/24`
-   **Gateway:** `192.168.75.1` (`medano`)
-   **VMs:**
    -   `192.168.75.2`: `naraj` (main reverse proxy)
    -   `192.168.75.3`: `auth`
    -   `192.168.75.4`: `md`
    -   `192.168.75.5`: `irc`
    -   ...and so on.

#### `private` (virbr1)

A private bridge for VMs to route their traffic through a VPN gateway (`rou`).

-   **Subnet:** `192.168.33.0/24`
-   **Gateway:** `192.168.33.1` (`medano`)
-   **VMs:**
    -   `192.168.33.2`: `rou` (VPN gateway)
    -   `192.168.33.3`: `arr` (routes its traffic through `rou`)

#### `rou` (virbr2)

A dedicated bridge for the `rou` VM to connect to the internet without interacting with other VMs.

-   **Subnet:** `192.168.34.0/24`
-   **Gateway:** `192.168.34.1` (`medano`)
-   **VMs:**
    -   `192.168.34.2`: `rou`

## Disaster Recovery

In case of a system failure, you might need to boot into a recovery environment and reinstall the system using Nix.

Here are the steps to do so:

1.  **Install Nix:**
    ```bash
    bash <(curl -L https://nixos.org/nix/install) --daemon
    ```

2.  **Set up the Nix environment:**
    ```bash
    set +u +x # sourcing this may refer to unset variables that we have no control over
    . $HOME/.nix-profile/etc/profile.d/nix.sh
    set -u -x
    ```

3.  **Configure the Nix channel:**
    ```bash
    # Keep this in sync with the system.stateVersion in your configuration
    nix-channel --add https://nixos.org/channels/nixos-25.05 nixpkgs
    nix-channel --update
    ```

4.  **Install NixOS tools:**
    ```bash
    nix-env -iE "_: with import <nixpkgs/nixos> { configuration = {}; }; with config.system.build; [ nixos-generate-config nixos-install nixos-enter ]"
    ```

5.  **Generate a new configuration and install:**
    ```bash
    # This will generate a new hardware configuration at /mnt/etc/nixos/hardware-configuration.nix
    nixos-generate-config --root /mnt

    # You can then edit the configuration at /mnt/etc/nixos/configuration.nix and
    # reinstall the system using 'nixos-install'.
    ```

## Shell

```
; nix-shell -A ops.shells.ida-pro-with-mcp

```
