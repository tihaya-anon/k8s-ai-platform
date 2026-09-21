# Interactive shell for the agent container. Not the host .bashrc: this one assumes the paths the
# image installs to (/usr/share/blesh, /usr/local/bin) and leaves out everything WSL-specific.
#
# Ordering is load-bearing. ble.sh must be sourced early with --noattach so it can wrap the shell's
# line editor before anything else touches PROMPT_COMMAND, and attached last, after starship has
# installed its prompt. Attaching first and then letting starship rewrite the prompt is the usual
# way these two end up fighting over the cursor.

# Non-interactive shells (docker run ... bash -c '...') get nothing from here.
case $- in
*i*) ;;
*) return ;;
esac

[[ -r /usr/share/blesh/ble.sh ]] && source /usr/share/blesh/ble.sh --noattach

HISTCONTROL=ignoreboth
HISTSIZE=10000
HISTFILESIZE=20000
shopt -s histappend checkwinsize

export EDITOR=nvim
export VISUAL=nvim
export PAGER=less
export LESS='-R'
export PATH="$HOME/go/bin:$PATH"

alias ll='ls -alFh --color=auto'
alias ls='ls --color=auto'
alias grep='grep --color=auto'
alias vi=nvim
alias vim=nvim
alias g=git
# The repository keeps its container targets out of the kubebuilder Makefile, so the -f is not
# optional and is easy to forget from inside the container.
alias amake='make -f agent.Makefile'

[[ -r /usr/share/bash-completion/bash_completion ]] && source /usr/share/bash-completion/bash_completion

command -v starship >/dev/null 2>&1 && eval "$(starship init bash)"

[[ ${BLE_VERSION-} ]] && ble-attach
