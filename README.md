# k8s-ai-platform

An internal AI model operations platform on Kubernetes, and the training environment in which it is
being built.

## Description

The platform composes the ecosystem and owns the policy: Kueue does queueing and quota, KServe does
serving and traffic, and this repository contributes the custom resources, controllers, lineage, and
the promotion gate that decides whether a model version may serve traffic.

It is also a curriculum. The repository starts blank on purpose and is rebuilt task by task. Each
task is implemented against a reviewed spec, and the graded work is the review: pick between two
designs, find the seeded defect, narrate a lifecycle, say what breaks when a dependency is down.
Every generated file, script and reconciler is something its maintainer can account for.

## Documentation

[docs/README.md](docs/README.md) is the map. In short:

- [docs/product/requirements.md](docs/product/requirements.md) — what the platform is for.
- [docs/product/architecture.md](docs/product/architecture.md) — the design and the five custom
  resources.
- [docs/product/domain.md](docs/product/domain.md) — every domain term and every field, and the
  decision behind it. Look a word up here.
- [docs/curriculum/training-tasks.md](docs/curriculum/training-tasks.md) — the task list. Each task
  runs in five beats: Learn, Spec, Build, Verify, Review. Read "How a task runs now" first.
- [docs/curriculum/stages.md](docs/curriculum/stages.md) — each beat is a tagged commit on the
  `curriculum/review-format` branch. This has the git commands for standing at one.
- [docs/log/progress.md](docs/log/progress.md) — one entry per task. What was built, the review
  answers, the quiz.

## Getting Started

### Prerequisites

**Use the devcontainer.** Open the repository in VS Code and run **Dev Containers: Reopen in
Container**; `.devcontainer/` is the ordinary human development environment and everything below
assumes it. This is a strong recommendation rather than a convenience:

- Every version this repository holds itself to is baked into the image at a pin -- Go,
  `controller-gen`, `golangci-lint`, `kubectl`, `kind`, `helm`, `kubebuilder`, `k9s`, `shellcheck`,
  `gopls`, Prettier, markdownlint. A host that drifts from one of them disagrees with CI and with
  the pre-commit hook about what passes.
- The pre-commit hook _gates_ on `shellcheck` and markdownlint. A host missing one does not fail the
  commit; it skips that gate and says so in a line that is easy to miss.
- `--network=host` plus the host's Docker socket means `kind`, `kubectl` and `helm` behave exactly
  as they do outside, and one `~/.kube/config` is valid on both sides. Cluster work costs nothing
  for being done in a container.

There is no file-system penalty to pay for that on Linux or on WSL2, as long as the checkout lives
in the Linux file system: the bind mount is the same ext4, and the container sees it at native
speed. A checkout under `/mnt/c` is the case that is genuinely slow, and it is slow for a native
WSL2 shell too -- the fix there is to move the checkout, not to leave the container.

Working on the host anyway means installing these yourself, at the versions
`.devcontainer/Dockerfile` and the `Makefile` pin:

- Go 1.27+ (the module's `go` directive is `1.27.0`)
- Docker 17.03+
- kubectl v1.11.3+, and access to a Kubernetes v1.11.3+ cluster -- `hack/set-up-cluster.sh` creates
  a local one on kind or the host's k3s
- `shellcheck`, for the pre-commit hook's shell gate

`.vscode/settings.json` is tracked, and it is the one owner of editor settings. Everything else
under `.vscode/` is ignored. The reason is reach: settings in `.devcontainer/devcontainer.json`
apply to one window, the one attached to the devcontainer. The same checkout opened over WSL, over
SSH or on the host gets none of them. Workspace settings reach every window, and they outrank the
machine-scope settings a devcontainer contributes. So anything that must agree with a `make` target
lives in the tracked file. The failure this avoids: an editor pointed at a linter path that does not
exist, linting nothing, saying nothing.

Two further pieces of tooling have their own documents:
[docs/tooling/agent-container.md](docs/tooling/agent-container.md) for the AI agent sandbox, which
is a different container from this one, and
[docs/tooling/cluster-providers.md](docs/tooling/cluster-providers.md) for kind versus k3s.

### To Deploy on the cluster

**Build and push your image to the location specified by `IMG`:**

```sh
make docker-build docker-push IMG=<some-registry>/k8s-ai-platform:tag
```

**NOTE:** This image ought to be published in the personal registry you specified. And it is
required to have access to pull the image from the working environment. Make sure you have the
proper permission to the registry if the above commands don’t work.

**Install the CRDs into the cluster:**

```sh
make install
```

**Deploy the Manager to the cluster with the image specified by `IMG`:**

```sh
make deploy IMG=<some-registry>/k8s-ai-platform:tag
```

> **NOTE**: If you encounter RBAC errors, you may need to grant yourself cluster-admin privileges or
> be logged in as admin.

**Create instances of your solution** You can apply the samples (examples) from the config/sample:

```sh
kubectl apply -k config/samples/
```

> **NOTE**: Ensure that the samples has default values to test it out.

### To Uninstall

**Delete the instances (CRs) from the cluster:**

```sh
kubectl delete -k config/samples/
```

**Delete the APIs(CRDs) from the cluster:**

```sh
make uninstall
```

**UnDeploy the controller from the cluster:**

```sh
make undeploy
```

## Project Distribution

Following the options to release and provide this solution to the users.

### By providing a bundle with all YAML files

1. Build the installer for the image built and published in the registry:

```sh
make build-installer IMG=<some-registry>/k8s-ai-platform:tag
```

**NOTE:** The makefile target mentioned above generates an 'install.YAML' file in the dist
directory. This file contains all the resources built with Kustomize, which are necessary to install
this project without its dependencies.

1. Using the installer

Users can just run `kubectl apply -f URL_FOR_YAML_BUNDLE` to install the project, i.e.:

```sh
kubectl apply -f https://raw.githubusercontent.com/<org>/k8s-ai-platform/<tag or branch>/dist/install.yaml
```

### By providing a Helm Chart

1. Build the chart using the optional helm plugin

```sh
kubebuilder edit --plugins=helm/v1-alpha
```

1. See that a chart was generated under 'dist/chart', and users can obtain this solution from there.

**NOTE:** If you change the project, you need to update the Helm Chart using the same command above
to sync the latest changes. Furthermore, if you create webhooks, you need to use the above command
with the '--force' flag and manually ensure that any custom configuration previously added to
'dist/chart/values.YAML' or 'dist/chart/manager/manager.YAML' is manually re-applied afterwards.

## Contributing

// TODO(user): Add detailed information on how you would like others to contribute to this project

**NOTE:** Run `make help` for more information on all potential `make` targets

More information can be found via the
[Kubebuilder Documentation](https://book.kubebuilder.io/introduction.html)

## License

Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file except in
compliance with the License. You may obtain a copy of the License at

```text
http://www.apache.org/licenses/LICENSE-2.0
```

Unless required by applicable law or agreed to in writing, software distributed under the License is
distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or
implied. See the License for the specific language governing permissions and limitations under the
License.
