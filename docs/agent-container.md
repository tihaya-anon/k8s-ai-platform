# AI agent development container

Use this container when you want an AI coding agent to work on this repository while keeping its
normal commands scoped to the repository checkout.

This is different from the product image in the root `Dockerfile`. The product image is `scratch`
and runs the platform. `Dockerfile.agent` is a developer sandbox with Go, Make, Git, and small CLI
tools for editing and testing. It also installs the `pi` and `codex` CLIs, but it does not bake any
model-provider credentials into the image.

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
make -f Makefile.agent agent-image
```

This downloads Go modules, builds the agent container image, fixes ownership on the named Go cache
volumes, fills the container Go cache, verifies that the source builds and tests inside the agent
container, and builds the product Docker image. Container Go commands inherit the host
`go env GOPROXY` value by default; override it with `GO_PROXY=...` if your network needs a different
module proxy.

To build only the agent image:

```bash
make -f Makefile.agent agent-image
```

The build passes your UID/GID into the image so files created by the container belong to your WSL
user.

To fill only the Go module cache used by the container:

```bash
make -f Makefile.agent agent-deps
```

For host-side development without the container, use:

```bash
go mod download
```

## Run for development

This mode gives the agent write access to this repository only. It is the normal mode for coding
changes after you decide the repo itself is an acceptable write boundary.

```bash
make -f Makefile.agent agent-shell
```

Inside the container:

```bash
make fmt
make vet
make test
```

If the Go module cache is empty, `make vet` or `make test` may need dependencies that are not yet in
the cache. Fill the cache once with `make -f Makefile.agent agent-deps`, then return to
`make -f Makefile.agent agent-shell` for normal work.

Do not mount kubeconfigs, SSH keys, cloud credentials, or broad host directories into the cache-fill
container.

## Run for read-only review

This mode is best when you want the agent to inspect or explain the project but not modify the
checkout.

```bash
make -f Makefile.agent agent-review
```

## Running an AI CLI inside the container

If the AI agent CLI itself must call a model provider, mount only the credential files it needs,
read-only. The agent still runs inside the container; only the model API call leaves the container.

Mount credential files read-only and keep the rest of the boundary unchanged:

- mount only this repository;
- mount only the AI provider token needed by the agent;
- do not mount `~/.kube`, cloud config directories, SSH keys, or `/var/run/docker.sock`;
- prefer a read-only repo mount for review-only sessions.

Pi stores credentials in `~/.pi/agent/auth.json`. Codex commonly uses files under `~/.codex/`. Start
a writable coding shell by passing only the credential files you want the container to see:

```bash
make -f Makefile.agent agent-shell-ai AGENT_AI_MOUNTS='\
  --mount type=bind,src=$$HOME/.pi/agent/auth.json,dst=/home/agent/.pi/agent/auth.json,readonly \
  --mount type=bind,src=$$HOME/.codex/auth.json,dst=/home/agent/.codex/auth.json,readonly'
```

For read-only review with model credentials, use the same mount variable with `agent-review`:

```bash
make -f Makefile.agent agent-review AGENT_AI_MOUNTS='\
  --mount type=bind,src=$$HOME/.pi/agent/auth.json,dst=/home/agent/.pi/agent/auth.json,readonly \
  --mount type=bind,src=$$HOME/.codex/auth.json,dst=/home/agent/.codex/auth.json,readonly'
```

Then run the agent inside that shell, for example:

```bash
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
