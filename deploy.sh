#!/usr/bin/env bash
nix-build -A "ops.nixos.$1.deploy" && "./result/bin/deploy"
