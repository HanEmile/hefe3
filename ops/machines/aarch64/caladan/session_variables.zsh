function parse_git_branch() {
  git branch 2> /dev/null | sed -n -e 's/^\* \(.*\)/\1/p'
}

setopt prompt_subst

export PROMPT="; "		# minimal prompt

# right side prompt
# - path
# - time of last command executed
#export RPROMPT="%F{green}%/%F{reset}"
export RPROMPT='${SSH_CONNECTION:+%m } $(parse_git_branch) %F{green}%/%F{reset} %D{%K:%M:%S}'

# Refresh the prompt exactly once, when a command is submitted, so the
# time in the rprompt is the time the command was actually executed.
# No timer (TMOUT/TRAPALRM): an async `zle reset-prompt` fires at
# arbitrary moments and clobbers fzf / completion menus / suggestions.
reset-prompt-accept-line() {
  zle reset-prompt
  zle .accept-line
}
zle -N accept-line reset-prompt-accept-line

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

FZF_DEFAULT_COMMAND="fd . $HOME"
FZF_CTRL_T_COMMAND="$FZF_DEFAULT_COMMAND"
FZF_ALT_C_COMMAND="fd -t d . $HOME"

if [ -e '/nix/var/nix/profiles/default/etc/profile.d/nix-daemon.sh' ]; then
  . '/nix/var/nix/profiles/default/etc/profile.d/nix-daemon.sh'
fi
