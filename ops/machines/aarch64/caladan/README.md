# caladan

m1 macbook air

nix darwin + home manager


time sudo nix --experimental-features 'nix-command flakes' run https://github.com/LnL7/nix-darwin/archive/master.tar.gz#darwin-rebuild -- switch --flake .#caladan
