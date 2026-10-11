{ hefe, ... }:
let
  sources = hefe.third_party;
  nixos = sources."nixos-unstable";
  pkgs = import nixos { };
in pkgs.mkShell {
  packages = with pkgs; [ ollama pi-coding-agent tmux ];
  shellHook = ''
    export ANTHROPIC_AUTH_TOKEN=ollama
    export ANTHROPIC_API_KEY=""
    export ANTHROPIC_BASE_URL=http://localhost:11434
    export SHELL=/bin/bash

    if ! tmux has-session -t ollama 2>/dev/null; then
      tmux new-session -d -s ollama 'ollama serve'
    fi

    # wait for the daemon instead of racing it
    until curl -s http://localhost:11434 >/dev/null 2>&1; do sleep 0.2; done

    export MODEL="qwen3.5:4b"

    printf "run $MODEL (y/n): "
    read input
    if [ "$input" == "y" ]
    then
      ollama launch claude --model "$MODEL" --yes
    fi
  '';
}
