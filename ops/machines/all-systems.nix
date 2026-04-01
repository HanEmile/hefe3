{ hefe, ... }:

(with hefe.ops.machines.x86; [
	mail
	medano
	lampadas
])
++
(with hefe.ops.machines.aarch64; [
	caladan
])
