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

- host filesystem access outside this checkout;
- host Docker socket access;
- kubeconfig access;
- cloud credentials, SSH keys, or personal secrets.

That means the agent can be given model credentials without also giving it your host filesystem,
Docker daemon, Kubernetes clusters, or cloud accounts.

## Build the agent image

From the repository root in WSL, prepare the usual development dependencies and images:

```bash
make -f agent.Makefile agent-image
```

This downloads Go modules, builds the agent container image, fixes ownership on the named Go cache
volumes, fills the container Go cache, verifies that the source builds and tests inside the agent
container, and builds the product Docker image. Container Go commands inherit the host
`go env GOPROXY` value by default; override it with `GO_PROXY=...` if your network needs a different
module proxy.

To build only the agent image:

```bash
make -f agent.Makefile agent-image
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
make -f agent.Makefile agent-deps
```

For host-side development without the container, use:

```bash
go mod download
```

## Run for development

This mode gives the agent write access to this repository only. It is the normal mode for coding
changes after you decide the repo itself is an acceptable write boundary.

```bash
make -f agent.Makefile agent-shell
```

Inside the container:

```bash
make fmt
make vet
make test
```

If the Go module cache is empty, `make vet` or `make test` may need dependencies that are not yet in
the cache. Fill the cache once with `make -f agent.Makefile agent-deps`, then return to
`make -f agent.Makefile agent-shell` for normal work.

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
make -f agent.Makefile agent-fmt
make -f agent.Makefile agent-lint
```

## The editor inside the container

The container is meant to be lived in, not only shelled into, so it gets the same shell and editor
the host runs: bash with ble.sh, starship and the usual git/docker/kubectl aliases, and Neovim with
the LazyVim configuration. All of it is vendored at `hack/agent-home/`, baked into the image so it
works standalone, and bind-mounted over that copy by the run targets so a change on the host takes
effect in the next shell without a rebuild.

`~/.bash_env` is not vendored and not mounted: it holds real API tokens on the host, and the
`.bashrc` guard skips it when absent. Mount it through `AGENT_AI_MOUNTS` if a session needs one.

Populate the Neovim plugin volumes once before the first session:

```bash
make -f agent.Makefile agent-nvim
```

Configuration is baked into the image; plugins, Treesitter parsers and Mason downloads live in named
volumes and survive `--rm`. The full picture, including the version pins and what differs from the
host config, is in [editors.md](editors.md).

## Run for read-only review

This mode is best when you want the agent to inspect or explain the project but not modify the
checkout.

```bash
make -f agent.Makefile agent-review
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
and `agent.Makefile` backs that directory with a named volume, so running `claude` inside the
container and logging in once there keeps the credential in the volume across `docker run --rm`. The
host credential is never mounted, and revoking the container's access is
`docker volume rm k8s-ai-claude` rather than a hunt for what was bind-mounted where.

Pi stores credentials in `~/.pi/agent/auth.json`. Codex commonly uses files under `~/.codex/`. Start
a writable coding shell by passing only the credential files you want the container to see:

```bash
make -f agent.Makefile agent-shell-ai AGENT_AI_MOUNTS='\
  --mount type=bind,src=$$HOME/.pi/agent/auth.json,dst=/home/agent/.pi/agent/auth.json,readonly \
  --mount type=bind,src=$$HOME/.codex/auth.json,dst=/home/agent/.codex/auth.json,readonly'
```

For read-only review with model credentials, use the same mount variable with `agent-review`:

```bash
make -f agent.Makefile agent-review AGENT_AI_MOUNTS='\
  --mount type=bind,src=$$HOME/.pi/agent/auth.json,dst=/home/agent/.pi/agent/auth.json,readonly \
  --mount type=bind,src=$$HOME/.codex/auth.json,dst=/home/agent/.codex/auth.json,readonly'
```

Then run the agent inside that shell, for example:

```bash
claude
# or
pi
# or
codex
```

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
