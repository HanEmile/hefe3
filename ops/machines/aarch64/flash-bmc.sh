#!/usr/bin/env bash
# Flash a BMC SD card image from medano to a local disk.
#
# Usage:
#   ./flash-bmc.sh /dev/diskN [lampadas-bmc|lernaeus-bmc|lankiveil-bmc|parella-bmc]
#
# The image is streamed from medano's nix store and written directly to
# the raw disk device. Requires sudo for dd.
set -euo pipefail

DISK="${1:-}"
BMC="${2:-lampadas-bmc}"

if [ -z "$DISK" ]; then
  echo "Usage: $0 /dev/diskN [bmc-name]"
  echo ""
  echo "Available BMCs: lampadas-bmc lernaeus-bmc lankiveil-bmc parella-bmc"
  echo ""
  echo "Find your SD card with: diskutil list"
  exit 1
fi

echo "Building SD image for $BMC on medano..."
STORE=$(ssh root@medano.emile.space "cd /root/hefe3 && nix-build -A ops.nixos.${BMC}.sdImage -j 12 --cores 12 --no-out-link 2>/dev/null")
IMGPATH="${STORE}/sd-image/nixos-image-sd-card-26.05.1183.6b316287bae2-armv6l-linux.img"

echo "Image: $IMGPATH"
echo "Target: $DISK"
echo ""

RAWDISK="${DISK/disk/rdisk}"
echo "Unmounting $DISK..."
diskutil unmountDisk "$DISK"

echo "Flashing to $RAWDISK (this takes a few minutes)..."
ssh root@medano.emile.space "cat $IMGPATH" | sudo dd of="$RAWDISK" bs=64k
sync

echo ""
echo "Done! Eject with: diskutil eject $DISK"
echo "Then insert the SD card into the $BMC Pi and power it on."
