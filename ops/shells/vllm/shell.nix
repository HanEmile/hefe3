{ hefe, ... }:
let
  sources = hefe.third_party;
  nixos = sources."nixos-unstable";
  pkgs = import nixos { };
in pkgs.mkShell {
  packages = with pkgs; [
    vllm
    tmux
  ];
  shellHook = ''
    # if ! tmux has-session -t llama 2>/dev/null; then
    #   tmux new-session -d -s llama 'llama-cpp serve'
    # fi
  '';
}
