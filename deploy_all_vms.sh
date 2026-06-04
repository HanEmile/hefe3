eza -1D ./ops/vms/x86 \
  | xargs \
    -I {} \
    -P 4 \
    bash -c "time nix-build -A ops.nixos.{}.deploy && ./result/bin/deploy 2>&1 > out_{}.log"
