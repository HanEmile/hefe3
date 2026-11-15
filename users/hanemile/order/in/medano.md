---
title: medano
type: hypervisor
visibility: internal
ip-public: 95.217.35.60
ip-default: 192.168.75.1
ip-private: 192.168.33.1
ssh-port: 2222
storage: ZFS (encrypted)
cpu: Intel
services: libvirt, nginx, NFS, tailscale
location: Hetzner
connects-to: vm-arr, vm-auth, vm-ctf, vm-data, vm-fuzz, vm-git, vm-irc, vm-md, vm-miki, vm-naraj, vm-photo, vm-rou, vm-rss, vm-social, vm-tmp
---

# medano

## VMs

(children "medano" :via "part-of" :show (:title :ip-default :ip-private :memory :services))
