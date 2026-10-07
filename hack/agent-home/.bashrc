# Shared interactive bash configuration, for this WSL host and for the agent container.
#
# The container mounts this file from hack/agent-home/, so there is one copy rather than a host
# version and a container fork drifting apart. Everything host-specific below is already behind an
# existence guard, which is what makes that possible: nvm, sdkman, cargo, brew, zoxide and
# lesspipe simply do not fire in the container, and no branch on the environment is needed.
#
# Deliberately not vendored alongside it: ~/.bash_env, which holds real API tokens and must never
# reach a git repository. The guard below skips it when it is absent, which is the container case.


# Only configure interactive shells.
case $- in
    *i*) ;;
    *) return ;;

esac

# Docker sets no $USER, and ble.sh treats an empty one as an insane environment and patches it
# loudly on every start. Derived rather than exported by the image, because the container is also
# entered with `--user root` and a baked-in value would be wrong there.
export USER="${USER:-$(id -un)}"

# On the host these come from ~/.bash_env, which is sourced further down and is not vendored; the
# `:-` keeps that value and only supplies a default where the file is absent.
export EDITOR="${EDITOR:-nvim}"
export VISUAL="${VISUAL:-nvim}"

# Ignore duplicate and space-prefixed history entries, and retain it across shells.
HISTCONTROL=ignoreboth
shopt -s histappend
HISTSIZE=1000
HISTFILESIZE=2000
shopt -s checkwinsize

path_prepend() {
    [ -d "$1" ] || return
    case ":$PATH:" in
        *":$1:"*) ;;
        *) PATH="$1:$PATH" ;;
    esac
}

path_append() {
    [ -d "$1" ] || return
    case ":$PATH:" in

        *":$1:"*) ;;
        *) PATH="$PATH:$1" ;;
    esac
}

# Make `less` more useful for non-text files when lesspipe is installed.
[ -x /usr/bin/lesspipe ] && eval "$(SHELL=/bin/sh lesspipe)"

# Used by the fallback prompt on Debian systems.

if [ -z "${debian_chroot:-}" ] && [ -r /etc/debian_chroot ]; then
    debian_chroot=$(< /etc/debian_chroot)
fi

# Use a colored fallback prompt when the terminal supports it.
case "$TERM" in
    xterm-color|*-256color) color_prompt=yes ;;
esac

if [ -n "$force_color_prompt" ]; then
    if [ -x /usr/bin/tput ] && tput setaf 1 >&/dev/null; then
        color_prompt=yes
    else
        color_prompt=
    fi
fi

if [ "$color_prompt" = yes ]; then
    PS1='${debian_chroot:+($debian_chroot)}\[\033[01;32m\]\u@\h\[\033[00m\]:\[\033[01;34m\]\w\[\033[00m\]\$ '

else
    PS1='${debian_chroot:+($debian_chroot)}\u@\h:\w\$ '
fi
unset color_prompt force_color_prompt

# Set the terminal title in xterm-compatible terminals.
case "$TERM" in
xterm*|rxvt*)
    PS1="\[\e]0;${debian_chroot:+($debian_chroot)}\u@\h: \w\a\]$PS1"
    ;;
esac

# Base aliases are defined here once; personal shortcuts stay in .bash_aliases.
if [ -x /usr/bin/dircolors ]; then

    test -r ~/.dircolors && eval "$(dircolors -b ~/.dircolors)" || eval "$(dircolors -b)"
    alias ls='ls --color=auto'
    alias grep='grep --color=auto'
    alias fgrep='fgrep --color=auto'
    alias egrep='egrep --color=auto'
fi

# Listing views are also base shell behavior.

alias ll='ls -alF'
alias la='ls -A'
alias l='ls -CF'

# Send a desktop notification after a long-running command.
alias alert='notify-send --urgency=low -i "$([ $? = 0 ] && echo terminal || echo error)" "$(history|tail -n1|sed -e '\''s/^\s*[0-9]\+\s*//;s/[;&|]\s*alert$//'\'')"'

# Personal aliases and shell functions.
if [ -f ~/.bash_aliases ]; then
    . ~/.bash_aliases

fi

# Private machine-local exports and mirror settings.
[ -r "$HOME/.bash_env" ] && source "$HOME/.bash_env"
[ -r "$HOME/.mirrors" ] && source "$HOME/.mirrors"

# Programmable completion, if the distribution has installed it.
if ! shopt -oq posix; then
    if [ -f /usr/share/bash-completion/bash_completion ]; then
        . /usr/share/bash-completion/bash_completion
    elif [ -f /etc/bash_completion ]; then
        . /etc/bash_completion
    fi

fi

# Optional tools. Their guards keep shell startup quiet on a new machine.
# ble.sh: ~/.local/share on the host, /usr/share in the container image. --noattach, with the
# attach at the very bottom of this file, because ble.sh has to wrap the line editor before
# anything else touches PROMPT_COMMAND but must not own the prompt until starship has installed
# its own. Attaching here and letting starship rewrite the prompt afterwards is the usual way
# these two end up fighting over the cursor.
for _ble_sh in "$HOME/.local/share/blesh/ble.sh" /usr/share/blesh/ble.sh; do
    if [ -r "$_ble_sh" ]; then
        source "$_ble_sh" --noattach
        break
    fi
done
unset _ble_sh
[ -r "$HOME/.local/bin/env" ] && source "$HOME/.local/bin/env"

export UV_CACHE_DIR="${UV_CACHE_DIR:-/tmp/.cache/uv}"


path_prepend "$HOME/.local/bin"
path_append "/opt/nvim-linux-x86_64/bin"
path_prepend "$HOME/.terragrunt/bin"
path_append "$HOME/.pulumi/bin"

[ -r "$HOME/.cargo/env" ] && source "$HOME/.cargo/env"

export NVM_DIR="$HOME/.nvm"
[ -s "$NVM_DIR/nvm.sh" ] && . "$NVM_DIR/nvm.sh"
[ -s "$NVM_DIR/bash_completion" ] && . "$NVM_DIR/bash_completion"

export SDKMAN_DIR="$HOME/.sdkman"
[ -s "$SDKMAN_DIR/bin/sdkman-init.sh" ] && source "$SDKMAN_DIR/bin/sdkman-init.sh"

# Homebrew adjusts PATH and related variables.
if [ -x /home/linuxbrew/.linuxbrew/bin/brew ]; then
    eval "$(/home/linuxbrew/.linuxbrew/bin/brew shellenv bash)"
fi
export HOMEBREW_NO_ENV_HINTS=1

# Zoxide replaces repeated `cd` navigation, when installed.
command -v zoxide >/dev/null 2>&1 && eval "$(zoxide init bash)"

# Starship owns the final prompt, when installed.
command -v starship >/dev/null 2>&1 && eval "$(starship init bash)"


unset -f path_prepend path_append

# Go 1.25 is installed under ~/.local/go and must win over the distro's 1.22 in /usr/bin and the
# /usr/local/go/bin entry added by /etc/profile.d/ai-infra-lab-go.sh.
export PATH="$HOME/.local/go/bin:$PATH"
# Go tools installed with `go install` live here (gopls, dlv, ...).
export PATH="$PATH:$HOME/go/bin"

# Last line on purpose: see the ble.sh note above.
[ -n "${BLE_VERSION-}" ] && ble-attach
