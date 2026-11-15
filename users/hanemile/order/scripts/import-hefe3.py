#!/usr/bin/env python3
"""Import infrastructure data from the hefe3 nix monorepo into order .md files."""

import os
import re
import sys
from pathlib import Path

HEFE3_DIR = Path(sys.argv[1]) if len(sys.argv) > 1 else Path.home() / "src/github.com/hanemile/hefe3"
OUTPUT_DIR = Path(sys.argv[2]) if len(sys.argv) > 2 else Path("example")


def read_file(path):
    try:
        return path.read_text()
    except Exception:
        return ""


def extract_ipam(hefe3):
    """Parse //ops/ipam/default.nix — extract all networks, IPs, MACs, ports."""
    content = read_file(hefe3 / "ops" / "ipam" / "default.nix")

    networks = {}  # network_name -> {host_name -> {v4, mac, ports}}
    current_network = None

    for line in content.split("\n"):
        line = line.strip()
        # Match network block: "default" = {
        m = re.match(r'"(\w+)"\s*=\s*\{', line)
        if m:
            current_network = m.group(1)
            networks[current_network] = {}
            continue

        if current_network:
            # Match host entry: name = {
            m = re.match(r'(\w+)\s*=\s*\{', line)
            if m and not line.startswith("#"):
                host = m.group(1)
                if host not in networks[current_network]:
                    networks[current_network][host] = {"v4": "", "mac": "", "ports": {}}
                continue

            # Match v4
            m = re.search(r'v4\s*=\s*"([^"]+)"', line)
            if m and current_network in networks:
                # Find which host this belongs to (last opened host)
                for h in reversed(list(networks[current_network].keys())):
                    if not networks[current_network][h]["v4"]:
                        networks[current_network][h]["v4"] = m.group(1)
                        break

            # Match mac
            m = re.search(r'mac\s*=\s*"([^"]*)"', line)
            if m and current_network in networks:
                for h in reversed(list(networks[current_network].keys())):
                    if not networks[current_network][h]["mac"]:
                        networks[current_network][h]["mac"] = m.group(1)
                        break

            # Match ports
            m = re.search(r'ports\.(\w+(?:\.\w+)?)\s*=\s*(\d+)', line)
            if m and current_network in networks:
                for h in reversed(list(networks[current_network].keys())):
                    networks[current_network][h]["ports"][m.group(1)] = int(m.group(2))
                    break

    return networks


def find_vms(hefe3):
    vm_dir = hefe3 / "ops" / "vms" / "x86"
    if not vm_dir.exists():
        return []
    return sorted([d.name for d in vm_dir.iterdir() if d.is_dir() and d.name != "__pycache__"])


def extract_vm_info(hefe3, vm_name, ipam):
    """Extract VM info from libvirt.nix and default.nix."""
    vm_dir = hefe3 / "ops" / "vms" / "x86" / vm_name
    info = {"memory": "4", "vcpu": "4", "services": [], "networks": []}

    # Read libvirt.nix for memory/vcpu/interfaces
    libvirt_nix = read_file(vm_dir / "libvirt.nix")
    m = re.search(r'memory\s*=\s*(\d+)', libvirt_nix)
    if m:
        info["memory"] = m.group(1)
    m = re.search(r'vcpu_count\s*=\s*(\d+)', libvirt_nix)
    if m:
        info["vcpu"] = m.group(1)
    # Interfaces -> networks
    ifaces = re.findall(r'"(virbr\d+)"', libvirt_nix)
    bridge_to_net = {"virbr0": "default", "virbr1": "private", "virbr2": "rou"}
    info["networks"] = [bridge_to_net.get(b, b) for b in ifaces]

    # Read default.nix for services
    default_nix = read_file(vm_dir / "default.nix")
    all_nix = default_nix
    # Also read any imported nix files
    for f in vm_dir.glob("*.nix"):
        if f.name not in ("default.nix", "libvirt.nix", "hardware-configuration.nix"):
            all_nix += "\n" + read_file(f)

    service_patterns = {
        "authelia": r"authelia",
        "hedgedoc": r"services\.hedgedoc",
        "soju": r"soju",
        "gotosocial": r"gotosocial|goToSocial",
        "miniflux": r"services\.miniflux",
        "immich": r"services\.immich",
        "sftpgo": r"sftpgo",
        "nginx": r"services\.nginx",
        "wireguard": r"wireguard|wg-quick",
        "openssh": r"openssh\.enable\s*=\s*true",
    }
    for svc_name, pattern in service_patterns.items():
        if re.search(pattern, all_nix, re.IGNORECASE):
            info["services"].append(svc_name)

    # Collect IPs from all networks
    info["ips"] = {}
    for net_name, hosts in ipam.items():
        if vm_name in hosts and hosts[vm_name]["v4"]:
            info["ips"][net_name] = hosts[vm_name]["v4"]

    # MAC from default network
    if vm_name in ipam.get("default", {}):
        info["mac"] = ipam["default"][vm_name].get("mac", "")
    elif vm_name in ipam.get("private", {}):
        info["mac"] = ipam["private"][vm_name].get("mac", "")
    else:
        info["mac"] = ""

    return info


def find_users(hefe3):
    users_dir = hefe3 / "users"
    if not users_dir.exists():
        return []
    return sorted([d.name for d in users_dir.iterdir()
                   if d.is_dir() and not d.name.startswith('.')])


def extract_hosts(hefe3, ipam):
    hosts = {}
    hosts['medano'] = {
        'title': 'medano',
        'type': 'hypervisor',
        'ip-public': '95.217.35.60',
        'ip-default': ipam.get('default', {}).get('medano', {}).get('v4', ''),
        'ip-private': ipam.get('private', {}).get('medano', {}).get('v4', ''),
        'ssh-port': '2222',
        'storage': 'ZFS (encrypted)',
        'cpu': 'Intel',
        'services': 'libvirt, nginx, NFS, tailscale',
        'location': 'Hetzner',
    }
    hosts['lampadas'] = {
        'title': 'lampadas',
        'type': 'nas',
        'ip-tailscale': ipam.get('tailscale', {}).get('lampadas', {}).get('v4', '100.87.209.97'),
        'storage': 'Btrfs + LUKS',
        'services': 'immich, grafana, restic',
        'location': 'local',
    }
    hosts['mail-server'] = {
        'title': 'mail.emile.space',
        'type': 'vps',
        'hostname': 'mail.emile.space',
        'storage': 'ext4',
        'services': 'mailserver, prometheus',
        'domain': 'emile.space',
        'location': 'cloud',
    }
    return hosts


def write_md(path, frontmatter, body=""):
    os.makedirs(path.parent, exist_ok=True)
    lines = ["---"]
    for k, v in frontmatter.items():
        if v:
            lines.append(f"{k}: {v}")
    lines.append("---")
    if body:
        lines.append("")
        lines.append(body)
    path.write_text("\n".join(lines) + "\n")


def main():
    os.makedirs(OUTPUT_DIR, exist_ok=True)
    hefe3 = Path(HEFE3_DIR)

    if not hefe3.exists():
        print(f"Error: hefe3 not found at {hefe3}")
        sys.exit(1)

    print(f"Importing from {hefe3} -> {OUTPUT_DIR}")

    ipam = extract_ipam(hefe3)
    vms = find_vms(hefe3)

    # ---- Physical hosts ----
    hosts = extract_hosts(hefe3, ipam)
    vm_connections = ', '.join(f"vm-{vm}" for vm in vms if vm != 'win1')
    hosts['medano']['connects-to'] = vm_connections

    for host_id, info in hosts.items():
        fm = {'title': info['title'], 'type': info['type'], 'visibility': 'internal'}
        fm.update({k: v for k, v in info.items() if k not in ('title', 'type')})
        body = f"# {info['title']}\n\n## VMs\n\n"
        body += f'(children "{host_id}" :via "part-of" :show (:title :ip-default :ip-private :memory :services))'
        write_md(OUTPUT_DIR / f"{host_id}.md", fm, body)
    print(f"  {len(hosts)} hosts")

    # ---- VMs ----
    for vm_name in vms:
        info = extract_vm_info(hefe3, vm_name, ipam)

        # Build IP strings
        ip_parts = {}
        for net_name, ip in info["ips"].items():
            ip_parts[f"ip-{net_name}"] = ip

        fm = {
            'title': vm_name,
            'type': 'vm',
            'visibility': 'internal',
            'part-of': 'medano',
            'memory': f"{info['memory']} GB",
            'vcpu': info['vcpu'],
            'networks': ', '.join(info['networks']) if info['networks'] else 'default',
        }
        fm.update(ip_parts)
        if info['mac']:
            fm['mac'] = info['mac']
        if info['services']:
            fm['services'] = ', '.join(info['services'])

        body = f"# {vm_name}\n\nVM on [medano](/medano/)."
        write_md(OUTPUT_DIR / f"vm-{vm_name}.md", fm, body)
    print(f"  {len(vms)} VMs")

    # ---- Networks ----
    net_info = {
        'default': {'subnet': '192.168.75.0/24', 'gateway': '192.168.75.1', 'bridge': 'virbr0', 'purpose': 'Primary VM network'},
        'private': {'subnet': '192.168.33.0/24', 'gateway': '192.168.33.1', 'bridge': 'virbr1', 'purpose': 'VPN gateway network'},
        'rou':     {'subnet': '192.168.34.0/24', 'gateway': '192.168.34.1', 'bridge': 'virbr2', 'purpose': 'VPN WAN bridge'},
    }
    for net_name, info in net_info.items():
        fm = {
            'title': f"net-{net_name}",
            'type': 'network',
            'visibility': 'internal',
            'subnet': info['subnet'],
            'gateway': info['gateway'],
            'bridge': info['bridge'],
        }
        body = f"# net-{net_name}\n\n{info['purpose']}.\n\n## VMs on this network\n\n"
        body += f'(table :type "vm" :columns (:title :ip-{net_name} :memory :services) :where (has "networks" "{net_name}"))'
        write_md(OUTPUT_DIR / f"net-{net_name}.md", fm, body)
    print(f"  {len(net_info)} networks")

    # ---- Users ----
    users = find_users(hefe3)
    for user in users:
        user_dir = hefe3 / "users" / user
        key_count = len([f for f in user_dir.glob("*.nix") if f.name != "default.nix"])
        fm = {
            'title': user,
            'type': 'user',
            'visibility': 'internal',
            'ssh-keys': str(key_count),
        }
        body = f"# {user}\n\nInfrastructure user."
        write_md(OUTPUT_DIR / f"user-{user}.md", fm, body)
    print(f"  {len(users)} users")

    # ---- Overview page ----
    fm = {'title': 'Infrastructure', 'type': 'page', 'visibility': 'internal'}
    body = """# Infrastructure

(toc)

## Hosts

(table :where (lambda (o) (member (object-type o) '("hypervisor" "nas" "vps") :test #'string=)) :columns (:title :type :location :services))

## Virtual Machines

(table :type "vm" :columns (:title :ip-default :ip-private :memory :vcpu :networks :services))

## Networks

(table :type "network" :columns (:title :subnet :gateway :bridge))

## Users

(table :type "user" :columns (:title :ssh-keys))

## Topology

(graph "medano" :edge "connects-to")
"""
    write_md(OUTPUT_DIR / "infra.md", fm, body)
    print("  1 overview page")
    print("Done.")


if __name__ == "__main__":
    main()
