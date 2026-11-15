---
title: net-default
type: network
visibility: internal
subnet: 192.168.75.0/24
gateway: 192.168.75.1
bridge: virbr0
---

# net-default

Primary VM network.

## VMs on this network

(table :type "vm" :columns (:title :ip-default :memory :services) :where (has "networks" "default"))
