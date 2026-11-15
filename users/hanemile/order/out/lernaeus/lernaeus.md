---
title: lernaeus
type: device
visibility: public
location: office
cpu: AMD Ryzen 5 3600
---

# lernaeus

Storage/compute PC. Connected to [lampadas](/nas/) via 10GbE DAC and to [UDM Pro](/udm-pro/) via 1G Ethernet.

## 10G Link

Mellanox ConnectX-3 InfiniBand card, using 10GbE Ethernet mode via QSFP+ DAC to lampadas. Full PCIe 3.0 x4 — no bottleneck on this end.

## Upstream Path to Fritzbox

(trace-path "lernaeus" "fritzbox" :edge "connects-to")

## Path to NAS (10G)

(trace-path "lernaeus" "nas" :edge "connects-to")
