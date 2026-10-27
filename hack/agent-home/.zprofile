# ~/.zprofile: executed by zsh(1) for login shells.
# PATH and exports set here are inherited by every shell and editor started from the session.

# Debian's zsh does not read /etc/profile, so load /etc/profile.d (snap, distro tool paths) here.
for profile_script in /etc/profile.d/*.sh(N); do
    emulate sh -c ". '$profile_script'"
done
unset profile_script

# Homebrew sets PATH, MANPATH and INFOPATH when it is installed.
if [[ -x /home/linuxbrew/.linuxbrew/bin/brew ]]; then
    eval "$(/home/linuxbrew/.linuxbrew/bin/brew shellenv zsh)"
fi
export HOMEBREW_NO_ENV_HINTS=1

# Keep PATH entries unique. (N-/) drops directories that do not exist on this machine.
# Go in ~/.local/go must win over the distro Go and /usr/local/go/bin from /etc/profile.d.
typeset -U path PATH
path=(
    $HOME/.local/go/bin(N-/)
    $HOME/bin(N-/)
    $HOME/.local/bin(N-/)
    $HOME/.terragrunt/bin(N-/)
    $HOME/.cargo/bin(N-/)
    $path
    /opt/nvim-linux-x86_64/bin(N-/)
    $HOME/.pulumi/bin(N-/)
    $HOME/go/bin(N-/)
)

export EDITOR=nvim
export VISUAL=nvim
export UV_CACHE_DIR="${UV_CACHE_DIR:-/tmp/.cache/uv}"

# Private machine-local exports and mirror settings. Loaded once per login, never committed.
# ~/.zsh_env holds real API tokens on the host; it is neither vendored nor mounted, so in the
# agent container this guard is what skips it.
[[ -r "$HOME/.zsh_env" ]] && source "$HOME/.zsh_env"
[[ -r "$HOME/.mirrors" ]] && source "$HOME/.mirrors"
