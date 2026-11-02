# Shared interactive zsh configuration, for this WSL host and for the agent container.
#
# The container mounts this file from hack/agent-home/, so there is one copy rather than a host
# version and a container fork drifting apart. Everything host-specific below is already behind an
# existence guard, which is what makes that possible: nvm, sdkman, brew, zoxide and lesspipe simply
# do not fire in the container, and no branch on the environment is needed. The two plugins are
# the exception that has to exist on both sides, so agent.Dockerfile pins them into the same
# ~/.local/share/zsh/plugins path the host installer clones them to.
#
# Environment and PATH live in ~/.zprofile; this file only shapes the interactive session.

# History is shared across sessions and skips duplicate and space-prefixed commands.
HISTFILE="$HOME/.zsh_history"
HISTSIZE=10000
SAVEHIST=10000
setopt append_history share_history hist_ignore_all_dups hist_ignore_space hist_reduce_blanks
setopt interactive_comments no_beep

# Make `less` more useful for non-text files when lesspipe is installed.
[[ -x /usr/bin/lesspipe ]] && eval "$(SHELL=/bin/sh lesspipe)"

# Base aliases are defined here once; personal shortcuts stay in ~/.zsh_aliases.
if [[ -x /usr/bin/dircolors ]]; then
    if [[ -r ~/.dircolors ]]; then
        eval "$(dircolors -b ~/.dircolors)"
    else
        eval "$(dircolors -b)"
    fi
    alias ls='ls --color=auto'
    alias grep='grep --color=auto'
fi

alias ll='ls -alF'
alias la='ls -A'
alias l='ls -CF'

# Send a desktop notification after a long-running command.
alias alert='notify-send --urgency=low -i "$([ $? = 0 ] && echo terminal || echo error)" "$(fc -ln -1 | sed -e '\''s/[;&|]\s*alert$//'\'')"'

[[ -r ~/.zsh_aliases ]] && source ~/.zsh_aliases

# Completions that a tool generates rather than ships. k9s is a Cobra program: `k9s completion
# zsh` prints an fpath function, so nothing is vendored here -- a tracked copy would be a
# generated file that silently disagrees with the pinned binary after a version bump.
#
# Written to a cache directory and regenerated only when it is missing or older than the binary,
# rather than `eval "$(k9s completion zsh)"` at every start: that costs a process per shell, and
# an fpath file lets compinit's own dump do the caching. This is also why it lives here and not
# in .devcontainer/Dockerfile -- the same file is the host's .zshrc, and baking the completion
# into the image alone would leave the host without it. The command guard is the same shape as
# zoxide's and starship's below, so a machine without k9s simply skips it.
#
# Add another Cobra tool -- kubectl, helm, kind -- by naming it in the array.
zsh_completions="${XDG_CACHE_HOME:-$HOME/.cache}/zsh/completions"
mkdir -p "$zsh_completions"
for completion_tool in k9s; do
    (( $+commands[$completion_tool] )) || continue
    if [[ ! -s "$zsh_completions/_$completion_tool" ]] ||
        [[ ${commands[$completion_tool]} -nt "$zsh_completions/_$completion_tool" ]]; then
        $completion_tool completion zsh >| "$zsh_completions/_$completion_tool"
    fi
done
unset completion_tool

# Completion: menu selection, case-insensitive matching, colors from LS_COLORS.
fpath=("$zsh_completions" /home/linuxbrew/.linuxbrew/share/zsh/site-functions(N-/) $fpath)
unset zsh_completions
zmodload zsh/complist
autoload -Uz compinit
mkdir -p "${XDG_CACHE_HOME:-$HOME/.cache}/zsh"
compinit -d "${XDG_CACHE_HOME:-$HOME/.cache}/zsh/zcompdump-$ZSH_VERSION"
zstyle ':completion:*' menu select
zstyle ':completion:*' matcher-list 'm:{a-z}={A-Z}'
zstyle ':completion:*' list-colors "${(s.:.)LS_COLORS}"

# Vi line editing. Enter always runs the command; Alt-Enter inserts a newline.
bindkey -v
KEYTIMEOUT=1
bindkey -M viins '^?' backward-delete-char
bindkey -M viins '^H' backward-delete-char
bindkey -M viins '^W' backward-kill-word
bindkey -M viins '^U' backward-kill-line
bindkey -M viins '^R' history-incremental-search-backward
bindkey -M viins '^[^M' self-insert-unmeta
bindkey -M vicmd '/' history-incremental-search-backward

# Up and Down search history for commands starting with what is already typed.
autoload -Uz up-line-or-beginning-search down-line-or-beginning-search
zle -N up-line-or-beginning-search
zle -N down-line-or-beginning-search
for keymap in viins vicmd; do
    bindkey -M $keymap '^[[A' up-line-or-beginning-search
    bindkey -M $keymap '^[OA' up-line-or-beginning-search
    bindkey -M $keymap '^[[B' down-line-or-beginning-search
    bindkey -M $keymap '^[OB' down-line-or-beginning-search
done
unset keymap

# Vim-like cursor: block in Normal mode, beam in Insert mode.
# zle-line-init also keeps Debian's keypad mode so terminfo key bindings stay valid.
zsh_vi_cursor() {
    case $KEYMAP in
        vicmd) printf '\e[2 q' ;;
        *) printf '\e[5 q' ;;
    esac
}
zle-line-init() {
    (( ${+terminfo[smkx]} )) && printf '%s' "${terminfo[smkx]}"
    zsh_vi_cursor
}
zle-keymap-select() { zsh_vi_cursor }
zle -N zle-line-init
zle -N zle-keymap-select

# Version managers, when installed.
export NVM_DIR="$HOME/.nvm"
[[ -s "$NVM_DIR/nvm.sh" ]] && source "$NVM_DIR/nvm.sh"

export SDKMAN_DIR="$HOME/.sdkman"
[[ -s "$SDKMAN_DIR/bin/sdkman-init.sh" ]] && source "$SDKMAN_DIR/bin/sdkman-init.sh"

# Zoxide replaces repeated `cd` navigation, when installed.
(( $+commands[zoxide] )) && eval "$(zoxide init zsh)"

# Starship owns the prompt and shows the vi mode through its character module.
(( $+commands[starship] )) && eval "$(starship init zsh)"

# Plugins are cloned by the installer. Syntax highlighting must load last.
zsh_plugins="${XDG_DATA_HOME:-$HOME/.local/share}/zsh/plugins"
[[ -r "$zsh_plugins/zsh-autosuggestions/zsh-autosuggestions.zsh" ]] &&
    source "$zsh_plugins/zsh-autosuggestions/zsh-autosuggestions.zsh" &&
    bindkey -M viins '^F' autosuggest-accept
[[ -r "$zsh_plugins/zsh-syntax-highlighting/zsh-syntax-highlighting.zsh" ]] &&
    source "$zsh_plugins/zsh-syntax-highlighting/zsh-syntax-highlighting.zsh"
unset zsh_plugins
