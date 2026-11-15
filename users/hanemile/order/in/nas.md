---
title: lampadas (NAS)
type: device
visibility: public
location: office
connects-to: lernaeus:dac:10G-Eth
---

# lampadas (NAS)

(toc)

## 10G Link

Connected to [lernaeus](/lernaeus/) via Mellanox ConnectX-3 InfiniBand cards using a QSFP+ DAC cable in 10GbE Ethernet mode (InfiniBand not yet configured).

## Components

(children "nas" :via "part-of" :show (:title :type))

## Upstream Path to Fritzbox

(trace-path "nas" "fritzbox" :edge "connects-to")

## Path to lernaeus (10G)

(trace-path "nas" "lernaeus" :edge "connects-to")
