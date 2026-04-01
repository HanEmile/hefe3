export PROMPT="; "		# minimal prompt

# right side prompt
# - path
# - time of last command executed
#export RPROMPT="%F{green}%/%F{reset}"
export RPROMPT="%F{green}%/%F{reset} %D{%K:%M:%S}"

# Reset the prompt, so we get the time the command was executed
# in the rprompt. A single second was to much, as it also breaks all the
# prompt suggestions, 5 seconds seems quite sensible, and you can just hit
# `TAB` yet another time if you want to see the suggestions again
TMOUT=60
TRAPALRM() {
  zle reset-prompt
}

export VI_MODE_SET_CURSOR=true

export PROMPT_EOL_MARK="%"  # hide EOL sign ('%')

export EDITOR="hx"
export LC_ALL="en_US.UTF-8"
export LANG="en_US.UTF-8"


export GOPATH=~/go
export GOBIN=$GOPATH/bin

# /usr/local/bin is mac specific and where brew installs stuff. As we are
# making use of brew as fallback so we need to add it
export PATH="$HOME/.local/bin:$HOME/.nix-profile/bin:/usr/local/bin:$PATH"
export PATH="$PATH:~/go/bin"
export PATH="$PATH:~/.emacs.d/bin"
export PATH="$PATH:~/bin"
export PATH="$PATH:/Applications/Racket v8.8/bin"
export PATH="$PATH:$HOME/.cargo/bin"
export PATH="$PATH:/opt/homebrew/bin"
export PATH="$PATH:$HOME/src/chromium.googlesource.com/chromium/tools/depot_tools"

export TERM=xterm-256color

# uxn
export PATH=$PATH:/Users/emile/Documents/projects/uxn/bin

# fzf
export FZF_BASE=$(whereis fzf | awk '{print $2}' | sed "s/fzf$//g")
if [ -n "${commands[fzf-share]}" ]; then
  source "$(fzf-share)/key-bindings.zsh"
  source "$(fzf-share)/completion.zsh"
fi

if [ -e '/nix/var/nix/profiles/default/etc/profile.d/nix-daemon.sh' ]; then
  . '/nix/var/nix/profiles/default/etc/profile.d/nix-daemon.sh'
fi
