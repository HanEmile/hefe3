{ hefe, ... }:

(with hefe.ops.machines.x86; [
	mail
	medano
	lampadas
	lernaeus
])
++
(with hefe.ops.machines.aarch64; [
	caladan
	lampadas-bmc
	lernaeus-bmc
	lankiveil-bmc
	parella-bmc
])
