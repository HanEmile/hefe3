---
title: net-rou
type: network
visibility: internal
subnet: 192.168.34.0/24
gateway: 192.168.34.1
bridge: virbr2
---

# net-rou

VPN WAN bridge.

## VMs on this network

(table :type "vm" :columns (:title :ip-rou :memory :services) :where (has "networks" "rou"))
