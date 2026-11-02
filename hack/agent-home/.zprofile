# ~/.zprofile: executed by zsh(1) for login shells.
# PATH and exports set here are inherited by every shell and editor started from the session.

# Docker sets no $USER, and starship plus a number of CLIs read it. Derived rather than baked into
# the image, because the container is also entered with `--user root` and a fixed value would be
# wrong there.
export USER="${USER:-$(id -un)}"

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

# The host's proxy settings, bind-mounted read-only by .devcontainer/devcontainer.json. This is
# load-bearing beyond the terminal: Dev Containers' default userEnvProbe is loginInteractiveShell,
# so whatever this login shell exports is applied to the extension host too -- which is how an
# extension that makes its own network calls (the GitHub Actions one, for instance) reaches the
# internet. Reading it here rather than relying on ${localEnv:...} in devcontainer.json is what
# makes it independent of how VS Code was launched; see hack/proxy-env.example for the shape.
#
# -f as well as -r: on a host that never had the file the mount can leave an empty directory
# behind, and the agent container has no mount for it at all. Both cases must be a silent no-op.
[[ -r "$HOME/.proxy_env" && -f "$HOME/.proxy_env" ]] && source "$HOME/.proxy_env"
