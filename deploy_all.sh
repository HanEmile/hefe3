for host in arr auth ctf data git md miki naraj rou;
do
  nix-build -A ops.nixos.$host.deploy && ./result/bin/deploy
done
