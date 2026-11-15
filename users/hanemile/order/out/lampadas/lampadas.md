---
title: lampadas
type: nas
visibility: internal
ip-tailscale: 100.87.209.97
storage: Btrfs + LUKS
services: immich, grafana, restic
location: local
---

# lampadas

## VMs

(children "lampadas" :via "part-of" :show (:title :ip-default :ip-private :memory :services))
