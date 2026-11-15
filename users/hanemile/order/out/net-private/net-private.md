---
title: net-private
type: network
visibility: internal
subnet: 192.168.33.0/24
gateway: 192.168.33.1
bridge: virbr1
---

# net-private

VPN gateway network.

## VMs on this network

(table :type "vm" :columns (:title :ip-private :memory :services) :where (has "networks" "private"))
