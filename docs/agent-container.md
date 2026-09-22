# AI agent development container

Use this container when you want an AI coding agent to work on this repository while keeping its
normal commands scoped to the repository checkout.

This is different from the product image in the root `Dockerfile`. The product image is `scratch`
and runs the platform. `agent.Dockerfile` is a developer sandbox with Go, Make, Git, and small CLI
tools for editing and testing. It also installs the `claude`, `pi` and `codex` CLIs, but it does not
bake any model-provider credentials into the image.

## Boundary

The container is for reducing the agent's **marginal operational risk** compared with running it on
the WSL host. Model calls cost the same whether the agent runs on the host or in this container; the
container boundary is about file, process, Docker, and Kubernetes access.

Allowed in the development shell:

- run the agent process inside the Docker container;
- read and write this repository checkout;
- run local build, format, vet, and unit-test commands;
- use named Go cache volumes created for this repo.

Allowed in the review shell:

- read this repository checkout;
- read explicitly mounted AI credentials;
- write only to container-local state and named cache volumes.

Not allowed unless you explicitly mount it:

- host filesystem access outside this checkout, with one exception below;
- host Docker socket access;
- kubeconfig access;
- cloud credentials, SSH keys, or personal secrets.

That means the agent can be given model credentials without also giving it your host filesystem,
Docker daemon, Kubernetes clusters, or cloud accounts. Within those limits the agent runs
unattended, with its own permission prompts turned off -- see
[Why the agents run unattended](#why-the-agents-run-unattended).

The exception is `$GOMODCACHE/cache/download`, mounted read-only. That directory is already laid out
as a module proxy, so `hack/agent.sh` puts it at the front of `GOPROXY` as `file:///hostproxy`
rather than making the container re-download the Kubernetes dependency tree into its own volume:
measured, that is 3.5 seconds instead of fourteen minutes, for three seconds of actual CPU. What it
exposes is public module source, read-only, and nothing else of the host. `GOSUMDB` is passed
through unchanged, so `go.sum` verification is exactly as it is on the host, and the network proxy
stays behind the file one as a fallback for anything missing. `--no-host-mods` turns it off and
restores the strict reading of the rule above.

## Build the agent image

From the repository root in WSL, prepare the usual development dependencies and images:

```bash
hack/agent.sh image
```

This downloads Go modules, builds the agent container image, fixes ownership on the named Go cache
volumes, fills the container Go cache, verifies that the source builds and tests inside the agent
container, and builds the product Docker image. Container Go commands inherit the host
`go env GOPROXY` value by default; override it with `GO_PROXY=...` if your network needs a different
module proxy.

To build only the agent image:

```bash
hack/agent.sh image
```

The build passes your UID/GID into the image so files created by the container belong to your WSL
user. It also passes the host's `go env GOPROXY` and `npm config get registry` through, so the build
uses whatever mirrors the host already trusts; `APT_MIRROR` and `GITHUB_MIRROR` are build args on
the same footing.

Expect around 3GB and a slow first build. `codex` and `claude-code` are thin npm wrappers around
per-platform native binaries pulled in as optional dependencies -- roughly 570MB unpacked between
them -- and that is the bulk of both the image and the build time. Keeping all three agent CLIs is
deliberate: the image is a general-purpose agent sandbox, not a Claude-specific one. The npm cache
is a BuildKit cache mount, so an interrupted build resumes rather than starting the download again.

To fill only the Go module cache used by the container:

```bash
hack/agent.sh deps
```

For host-side development without the container, use:

```bash
go mod download
```

## Run for development

This mode gives the agent write access to this repository only. It is the normal mode for coding
changes after you decide the repo itself is an acceptable write boundary.

```bash
hack/agent.sh shell
```

Inside the container:

```bash
make fmt
make vet
make test
```

Or without opening a shell at all, which is what a hook or a CI step wants — `--` passes the rest
through, and the tty is only allocated when there is one to attach to:

```bash
hack/agent.sh shell -- go test ./internal/...
```

Setup output — the image build and the volume permissions — is captured rather than printed, since
it is noise on every run but the first. `-v` prints it, along with the exact `docker` commands. A
step that fails prints what it captured either way.

If the Go module cache is empty, `make vet` or `make test` may need dependencies that are not yet in
the cache. Fill the cache once with `hack/agent.sh deps`, then return to `hack/agent.sh shell` for
normal work.

Do not mount kubeconfigs, SSH keys, cloud credentials, or broad host directories into the cache-fill
container.

## Formatting and the git hook

The image pins `markdownlint-cli2` and `prettier` alongside the agent CLIs, so `hack/format.sh`
behaves identically inside the container and on the host. Both degrade to a warning when a formatter
is missing, which would otherwise mean the container silently accepted markdown the host rejects.

`core.hooksPath` lives in `.git/config`, which is not tracked, so a fresh clone starts with the
pre-commit hook disarmed. The image's entrypoint sets it on every entry instead of relying on anyone
remembering `git config core.hooksPath hack/git-hooks`. It also adds the workspace to
`safe.directory`, because the mount is owned by the UID that built the image and git refuses a repo
owned by anyone else.

Arming is best effort. The review and build targets mount the checkout `:ro` and cannot write
`.git/config`; the entrypoint says so on stderr and carries on, since failing to arm a hook must not
fail a build.

To format or lint the tree with the container's pinned versions rather than whatever the host has:

```bash
hack/agent.sh fmt
hack/agent.sh lint
```

## The shell inside the container

The container is meant to be lived in, not only shelled into, so it gets the shell the host runs:
bash with ble.sh, starship and the usual git/docker/kubectl aliases, vendored at `hack/agent-home/`,
baked into the image so it works standalone and bind-mounted read-only over that copy by the run
targets so a change on the host takes effect in the next shell without a rebuild.

It does not get an IDE. Neovim is installed as `$EDITOR` — `git commit` without `-m` and
`git rebase -i` need one — but without the LazyVim configuration or any language server. Editing
happens on the host against the same inodes through the bind mount; [editors.md](editors.md) has the
reasoning.

`~/.bash_env` is not vendored and not mounted: it holds real API tokens on the host, and the
`.bashrc` guard skips it when absent. Pass `--mount` if a session genuinely needs one of those
tokens.

## Run for read-only review

This mode is best when you want the agent to inspect or explain the project but not modify the
checkout.

```bash
hack/agent.sh review
```

## Running an AI CLI inside the container

If the AI agent CLI itself must call a model provider, mount only the credential files it needs,
read-only. The agent still runs inside the container; only the model API call leaves the container.

Mount credential files read-only and keep the rest of the boundary unchanged:

- mount only this repository;
- mount only the AI provider token needed by the agent;
- do not mount `~/.kube`, cloud config directories, SSH keys, or `/var/run/docker.sock`;
- prefer a read-only repo mount for review-only sessions.

Claude Code is the exception, and the easier case. The image sets `CLAUDE_CONFIG_DIR` to `~/.claude`
and `hack/agent.sh` backs that directory with a named volume, so running `claude` inside the
container and logging in once there keeps the credential in the volume across `docker run --rm`. The
host credential is never mounted, and revoking the container's access is
`docker volume rm k8s-ai-claude` rather than a hunt for what was bind-mounted where.

Pi stores credentials in `~/.pi/agent/auth.json`. Codex commonly uses files under `~/.codex/`. Start
a writable coding shell by passing only the credential files you want the container to see:

```bash
hack/agent.sh shell \
  --mount type=bind,src=$HOME/.pi/agent/auth.json,dst=/home/agent/.pi/agent/auth.json,readonly \
  --mount type=bind,src=$HOME/.codex/auth.json,dst=/home/agent/.codex/auth.json,readonly
```

For read-only review with model credentials, pass the same flags to `review`:

```bash
hack/agent.sh review \
  --mount type=bind,src=$HOME/.pi/agent/auth.json,dst=/home/agent/.pi/agent/auth.json,readonly \
  --mount type=bind,src=$HOME/.codex/auth.json,dst=/home/agent/.codex/auth.json,readonly
```

Then run the agent inside that shell, for example:

```bash
claude
# or
pi
# or
codex
```

## Why the agents run unattended

`claude` and `codex` in this image are wrapper scripts in `hack/agent-bin/`, copied to
`/usr/local/libexec/agent-bin` and put ahead of npm's `/usr/local/bin` on `PATH`. They exec the real
binary with its interactive gate removed -- `--dangerously-skip-permissions` for Claude Code,
`--dangerously-bypass-approvals-and-sandbox` for Codex -- so an agent works to completion instead of
stopping on every write and every shell command.

That is the point of the container. The boundary is the one described under [Boundary](#boundary):
`--cap-drop ALL`, `--security-opt no-new-privileges`, no Docker socket, no kubeconfig, no host home,
and the only writable host path is this checkout. Everything the agent could reach by answering a
prompt with "yes", it can reach anyway; the prompt is a question whose answer is already fixed, and
the honest place to have refused was `docker run`. Withdrawing the trust is `docker rm` and
`docker volume rm k8s-ai-claude`, not an audit of what got approved at 02:00.

The consequence, stated plainly: **an agent in this container can destroy the checkout.** It is a
bind mount of your working tree, not a copy. Commit or push before a long unattended run, and use
`hack/agent.sh review` -- where the checkout is mounted `:ro` -- for anything that only needs to
read. `pi` is left alone; it has no equivalent global flag here.

It is a launch flag rather than configuration, on both sides, and deliberately:

- Claude Code's `permissions.defaultMode` would live in `settings.json` inside the `k8s-ai-claude`
  volume, which outlives any one container and travels with the credential.
- Codex's `approval_policy` and `sandbox_mode` would live in `~/.codex/config.toml`, the directory
  sessions bind-mount `auth.json` into from the host.

Either would quietly widen something outside the container's lifetime. A flag is scoped to the
process that was launched.

To get the prompts back for one session, set `AGENT_BYPASS=0`:

```bash
AGENT_BYPASS=0 claude
```

The wrappers also stand aside on their own when the invocation already decides the question --
`claude --permission-mode plan`, `codex --sandbox read-only` -- and when the subcommand does not
take the flag at all, such as `claude mcp list` or `codex login`. They are root-owned and `0755`, so
the agent user cannot edit its own launcher.

Model usage is controlled by the credentials/account you mount. The repo/container boundary only
prevents the agent's shell commands from reaching host secrets or cloud credentials you did not
mount.

## Kubernetes and Docker work

For this repository, the cluster targets such as `make up`, `make image`, `make load`,
`make deploy`, and `make down` need Docker, kind, and kubectl access. Giving those to a container
usually requires mounting the host Docker socket, and that is effectively host-level trust.

Use one of these safer patterns instead:

1. Let the agent edit code and run unit tests in the container, then you run cluster commands from
   WSL after reviewing the diff.
2. Create a separate disposable VM/WSL distro for platform experiments, and run the agent there.
3. Only if you accept host-level trust, start a separate high-trust session with the Docker socket
   and a throwaway local kind cluster. Never include real kubeconfigs or cloud credentials.

For the normal rule you gave — operations scoped to this repo with no extra host risk — use
`agent-shell` or `agent-review` without Docker socket or kubeconfig mounts.
