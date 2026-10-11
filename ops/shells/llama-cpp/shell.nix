{ hefe, ... }:
let
  sources = hefe.third_party;
  nixos = sources."nixos-unstable";
  pkgs = import nixos { };
in pkgs.mkShell {
  packages = with pkgs; [
    llama-cpp
    opencode
    tmux
  ];
  shellHook = ''
    if ! tmux has-session -t llama 2>/dev/null; then
      tmux new-session -d -s llama 'llama-server -hf ggml-org/gemma-4-E4B-it-GGUF:Q8_0'
    fi

    printf "run opencode (Y/n): "
    read input
    if [ "$input" != "n" ]
    then
      opencode
    fi
  '';
}
