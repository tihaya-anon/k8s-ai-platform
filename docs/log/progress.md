# Maintainer Log

Your own log for [training-tasks.md](../curriculum/training-tasks.md). Keep it in the repository so
the work is portable: someone reading this file should be able to see what was built, what was
learned, and what is next.

One entry per task. Keep them short; the value is in the evidence and the surprises.

## Entry shape

A task runs in five beats. Two of them are graded. The entry records those two, plus the evidence.

- **Learn — what I saw** — the beat that changes no files. What the live objects showed. The command
  whose output surprised you. The error text worth recognising later. Fill this in first. It is the
  part you cannot reconstruct afterwards.
- **Spec review** — what you accepted, amended or rejected in the proposal under `docs/product/`.
  Say what it left underspecified. Graded.
- **What was built** — one or two lines, plus the commit or the tag. Usually Claude's half. The
  entry keeps it so the log still reads as a history of the platform.
- **Tests I wrote** — the cases the task left to you. Say what each one catches. "Added a test" is
  not evidence. Name what fails if the code regresses.
- **Review answers** then **Review grading** — the exercises from
  [review-exercises.md](../curriculum/review-exercises.md). Your verdict and your reasoning first.
  Claude's grading after. For a seeded defect review, record the score as _n_ of _m_. Keep the ones
  you missed.
- **What surprised me** — one line. The most valuable line in the entry.
- **Quiz answers** then **Quiz review** — your answers first, Claude's grading after. Keep wrong
  answers. They are the record of what to re-ask.

Entries written by Claude say so at the top. That keeps the answers in them from looking like
recall.

## Entries

### Archive before blank rebuild — 2026-10-07

**Spec review:**

**What was built:** preserve the generated scaffold before resetting the curriculum to start from
blank. committed the previous state as `131697f` with message
`Archive generated kubebuilder scaffold before blank rebuild`, then removed the generated
Kubebuilder project, generated manifests, controller/test scaffolds, cluster scripts, local workflow
scripts, editor files, and CI files.

**Tests I wrote:** `git status --short` was clean after the archive commit; the follow-up deletion
leaves this repository as documentation plus project policy.

**Review answers:**

**Review grading:**

**What surprised me:** the generated project is not just `api/` and CRDs. `Makefile`, `PROJECT`,
`go.mod`, `cmd/main.go`, `config/`, RBAC, samples, tests, cluster scripts, and workflow helpers all
carry assumptions. Starting from blank makes those assumptions visible.

**Quiz answers:** none; this was a repository reset, not a training task.

### 0.0 Install the local toolchain — 2026-10-07

**Spec review:**

**What was built:** install tools install go, docker, kubectl, kind, kubebuilder

**Tests I wrote:**

**Review answers:**

**Review grading:**

```shell
go version
kubectl version
kind version
kubebuilder version
docker version
```

```shell
go version go1.25.14 linux/amd64
Client Version: v1.34.8
Kustomize Version: v5.7.1
Server Version: v1.36.1
Warning: version difference between client (1.34) and server (1.36) exceeds the supported minor version skew of +/-1
kind v0.32.0 go1.26.3 linux/amd64
Version: cmd.version{KubeBuilderVersion:"4.9.0", KubernetesVendor:"1.34.0", GitCommit:"5e331e74c7a25c8e8fc0d9d5c33c319b7268f395", BuildDate:"2025-09-22T10:53:21Z", GoOs:"linux", GoArch:"amd64"}
Client: Docker Engine - Community
 Version:           29.8.1
 API version:       1.56
 Go version:        go1.26.8
 Git commit:        4a63305
 Built:             Tue Sep 15 16:26:01 2026
 OS/Arch:           linux/amd64
 Context:           default

Server: Docker Engine - Community
 Engine:
  Version:          29.8.1
  API version:      1.56 (minimum version 1.40)
  Go version:       go1.26.8
  Git commit:       464cd50
  Built:            Tue Sep 15 16:26:01 2026
  OS/Arch:          linux/amd64
  Experimental:     false
 containerd:
  Version:          v2.3.6
  GitCommit:        ee2735368117d2eb259779949d5e75cdafec9761
 runc:
  Version:          1.5.1
  GitCommit:        v1.5.1-0-g8f2685a4
 docker-init:
  Version:          0.19.0
  GitCommit:        de40ad0
```

**What surprised me:** nothing

**Quiz answers:**

1. runtime: go, kubectl, kind, docker; code-generation: kubebuilder
2. to make it reproducible
3. kubebuilder works like spring initializer, controller-gen works like annotation parser

**Quiz review:**

1. The answer incorrectly puts Go in "runtime". Go is the language toolchain used to compile and
   install project binaries; it is not a cluster runtime tool. Docker/containerd is the container
   runtime layer, kind creates a Kubernetes cluster on that runtime, and kubectl talks to the API
   server. Kubebuilder is scaffolding; controller-gen is the generator that later reads markers.
2. "To make it reproducible" is correct but incomplete. Pinning generator versions matters because
   generated CRD schemas, RBAC, deepcopy code, and webhook manifests can change between tool
   releases even when the handwritten Go types did not. A teammate should not get a different diff
   just because their global controller-gen is newer.
3. The Spring Initializr / annotation parser analogy is useful. The missing detail is that
   Kubebuilder also writes and updates project metadata, manager wiring, config layout, and Makefile
   targets. controller-gen is narrower: it consumes Go packages plus markers and emits artifacts
   such as CRDs, RBAC, webhook config, and deepcopy methods.

The installed Kubebuilder is now `v4.9.0`, matching the training task. That is good: scaffolded
output can differ between Kubebuilder releases, so this keeps the exercise aligned with the archived
reference point.

### 0.1 Repository hygiene before Go code — 2026-10-07

**Spec review:**

**What was built:** maintain the repo almost nothing, just commit the repo and push to remote

**Tests I wrote:**

**Review answers:**

**Review grading:**

**What surprised me:**

**Quiz answers:**

1. because kubebuilder init requires an _empty_ dir
2. requirements.md and architecture.md, progress.md and training-tasks.md
3. editor files

**Quiz review:** Better, but still too compressed.

1. Corrected again after trying the real CLI: Kubebuilder v4.9.0 requires a stricter _empty scaffold
   directory_ than expected. It refused existing `cmd/`, and it also refused `docs/`. The safe
   workflow is to commit this repository as the learning baseline, run `kubebuilder init` in a
   temporary empty directory with `--project-name k8s-ai-platform`, then copy the generated scaffold
   back here. The reason to commit first is still diff hygiene: the next commit should show exactly
   what Kubebuilder generated.
2. `requirements.md` and `architecture.md` are product/design documentation. `training-tasks.md` and
   `progress.md` are curriculum/learning records, not product requirements. Workflow files are
   things like `.gitignore`, `.prettierrc`, `.prettierignore`, and `.markdownlint.json`.
3. "editor files" is correct for the pre-scaffold baseline, and the important refinement is timing:
   before generators run, ignore only artifacts already observed locally, such as `.idea` and tool
   caches. Kubebuilder generates its own `.gitignore` during 0.2, so Go/Kubebuilder-specific ignores
   should be merged from that generated file instead of guessed too early.

### 0.2 Initialize the Kubebuilder project — 2026-10-07

**Spec review:**

**What was built:** build the scaffold using kubebuilder cli just move the generated files here

**Tests I wrote:** `go test ./...` passes and `make help` shows the help messages

**Review answers:**

**Review grading:**

**What surprised me:**

**Quiz answers:**

1. only `./cmd/main.go` is the source code
2. to get type definitions so the schema are guaranteed during compiling
3. manifests and generate, Deployment
4. versions and metadata, maybe for compatibility?

**three most-used Makefile targets:**

1. manifests
2. generate
3. build

**Quiz review:** Good scaffold evidence: `go test ./...` passing proves the generated Go module
compiles and the empty test packages are valid, and `make help` proves the generated Makefile
targets are wired.

1. `cmd/main.go` is source code, but it is not the only source-like scaffold. `test/`, `go.mod`,
   `go.sum`, and `hack/boilerplate.go.txt` are also code/build inputs. Deployment configuration is
   mainly under `config/`, plus `Dockerfile`/`.dockerignore` for the manager image and `.github/`
   for generated workflow examples. `PROJECT` is Kubebuilder metadata, not runtime deployment YAML.
2. `controller-runtime` is not mainly about compile-time schemas. It is the controller framework:
   manager lifecycle, shared cache/informers, Kubernetes client, scheme registration, leader
   election, probes, metrics, and webhook integration. A handwritten REST client would make the
   project rebuild those operator mechanics by hand.
3. Generator targets are `manifests` and `generate`; targets such as `build`, `run`, `test`, and
   `deploy` call them as dependencies. Live-cluster targets include `install`, `uninstall`,
   `deploy`, `undeploy`, and the e2e kind targets. The target name is `deploy`, while `Deployment`
   is the Kubernetes object rendered under `config/manager/`.
4. `PROJECT` records the Kubebuilder CLI version, plugin/layout, project name, repo/module path, API
   domain, and config version. Kubebuilder uses it so later scaffold commands know how to extend
   this project consistently.

The three most-used targets are reasonable for this point. After the first API exists, expect
`make manifests generate` while changing markers/types, and `make fmt vet test` as the local gate.

`config/` review should stay structural for 0.2. It is enough to know that `config/default/`
composes the deployment, `config/manager/` owns the manager `Deployment`, `config/rbac/` owns
service account and permissions, `config/prometheus/` owns monitoring integration, and
`config/network-policy/` owns metrics access policy. Deep YAML line review belongs later when a task
changes one of those areas.

### 0.3 Create the `Dataset` API and learn the generators — 2026-10-07

**Spec review:**

**What was built:** add a new CRD annotations on generated api Go files

**Tests I wrote:** run generate commands and get the artifacts

**Review answers:**

**Review grading:**

**What surprised me:** the generator is really useful, it only leaves the biz logic for me

**Quiz answers:**

1. config/ belongs to manifests, internal/ belongs to generate
2. in the `openAPIV3Schema` part of the CRD, in `./internal/controller/dataset_controller.go`
3. i dont understand the question
4. because they are dynamic and required to be compared with the spec fields
5. because the k8s api will fail to distinguish each other if not

**Quiz review:** Accepted after the refine pass. `make test` passes, so the generated CRD installs
into envtest and the generated controller scaffold can create a valid sample object. The earlier
blockers were addressed: the sample has required spec fields, the generated controller test creates
a valid Dataset, optional claimed/observed fields use pointers where omission matters, and the
printer column types now match their JSONPaths (`Phase` is `string`, `Age` is `date`).

One small cleanup was made before commit: the generated placeholder comments at the top of
`DatasetSpec` were removed. The remaining field comments now reflect the requirement-side meanings
that were added to `docs/training-tasks.md`.

Validation is intentionally minimal for this first API pass: URI is non-empty with a max length, and
checksum is non-empty with a max length. Stricter URI scheme or checksum-format policy can be added
later when the artifact resolver contract is defined.

Quiz corrections:

1. `make manifests` generates Kubernetes manifests under `config/`, including CRDs and RBAC.
   `make generate` generates Go object code such as `api/v1alpha1/zz_generated.deepcopy.go`; it does
   not generally mean `internal/`.
2. Validation lives in the CRD OpenAPI schema under `openAPIV3Schema`, and the Kubernetes API server
   enforces it when objects are created or updated. It is not enforced by
   `internal/controller/dataset_controller.go`.
3. The status subresource separates writes to `.spec` and `.status`. Normal update/patch calls own
   spec and metadata; status updates go through `/status`. This prevents a controller status update
   from accidentally overwriting user intent and prevents ordinary spec updates from casually
   changing observed state.
4. Correct direction: `observedRows` and `observedChecksum` are status because they are controller
   observations, not user intent. They are compared with spec claims to detect drift or mismatch.
5. The API server can distinguish full resources by group/resource, but short names affect CLI UX
   and discovery. Reusing common aliases like `ds` conflicts with expectations around DaemonSet and
   makes `kubectl get` commands ambiguous or surprising.

The 0.3 acceptance criteria sequencing issue was fixed in `docs/training-tasks.md`: 0.3 now requires
the generated CRD to define the printer columns, while actually observing `kubectl get datasets` is
left until a later task installs the CRD into a live cluster.

### 0.4 Add API-server tests with envtest — 2026-10-09

**Spec review:**

**What was built:** test api server behaviour on our CRD add a new test file `dataset_api_test.go`

**Tests I wrote:** `make test` passes

**Review answers:**

**Review grading:**

**What surprised me:**

**Quiz answers:**

1. Set up the mock api server to test the behaviour
2. This only test the static behaviour, mainly config validation
3. It proves that those validations indeed accepts the legal ones and reject the opposite

**Quiz review:** The answers need correction and more precise terminology.

1. Envtest is not a mock API server. It starts a real Kubernetes API server and etcd, loads the
   generated CRD, and lets the test client exercise API-server behavior without starting the
   controller manager or reconciler.
2. The purpose is not only static configuration validation. Testing the API contract before writing
   reconciliation separates admission/schema failures from controller business-logic failures and
   confirms that invalid resources cannot enter the cluster.
3. The answer has the right direction but is incomplete. Marker-generated validation becomes OpenAPI
   schema in the CRD, and the Kubernetes API server enforces that schema at create/update time. The
   tests prove that the generated schema rejects invalid values and accepts valid ones; they do not
   prove reconciler behavior.

The implementation currently covers valid creation, defaulting, and invalid URI/owner/rows/checksum
inputs. The task also requires tests for status-subresource writes and for normal spec updates
preserving status; those cases are not present in `dataset_api_test.go` yet. The defaulting test
also leaves its resource behind, so cleanup would make the suite more isolated.

### 0.5 Write the cluster setup scripts — 2026-10-10

**Spec review:**

**What was built:** set up the cluster from zero and shutdown it idempotently two scripts,
`hack/set-up-cluster.sh` and `hack/shutdown-cluster.sh`

**Tests I wrote:** ran them

**Review answers:**

**Review grading:**

**What surprised me:** shouldn't rely on script itself to ensure idempotency since these resources
are manged by k8s and helm, should use helm cli instead

**Quiz answers:**

1. install ends when deployments are **CREATED**, not **READY**, while `kubectl wait` can specify
   the status
2. the later one. former one will install several resources at least under the situation that
   current resource's version mismatches the desired one
3. IDK
4. objects are in the cluster, etcd. use `kubectl get <resource>` to inspect

**Quiz review:** Strong on 1 and 4. The "what surprised me" note is the real lesson of this task and
is correct: idempotency belongs to the tool that owns the state, not to a guard clause in the
script.

1. Correct. Two refinements. Without `--wait`, `helm install` returns once the API server has
   _accepted_ the manifests, which only proves they are schema-valid. With `--wait`, Helm blocks on
   readiness but only for resources in its own chart, so cross-chart ordering (KServe's webhooks
   needing cert-manager to actually answer) still needs an explicit `kubectl wait`. An exit code
   reports "manifests accepted", never "controllers working".
2. Right choice, wrong villain. Plain `helm install` does not quietly drift — it fails loudly on the
   second run with "cannot re-use a name". The silent drift came from the `helm status`
   skip-if-exists guard in the first version of this script: it made a changed `--version` or
   `--set` a no-op, so the script stopped being the source of truth without ever failing.
   `helm upgrade --install` converges because it re-renders the chart and diffs against the stored
   release.
3. Helm records ownership on every object it creates: the label `app.kubernetes.io/managed-by=Helm`
   plus annotations `meta.helm.sh/release-name` and `meta.helm.sh/release-namespace`. If adoption
   were allowed, two releases would each believe they own the same object — `helm uninstall` on
   either would delete it out from under the other, and their upgrades would overwrite each other's
   config in whatever order they ran. This is the error hit during this task: a stale
   `kserve-resources` release owned `ServiceAccount/kserve-controller-manager` while the script
   asked for a release named `kserve`. The refusal is correct behavior and should not be bypassed
   with `--force` or by editing the annotations by hand.
4. Correct. Worth adding: Helm also stores the rendered manifest _itself_ in the cluster, as a
   Secret of type `helm.sh/release.v1` in the release namespace. That is how `helm list` and
   `helm get manifest` work without the chart. See it with
   `kubectl get secret -n kserve -l owner=helm` and `helm get manifest kserve -n kserve`.

**Evidence to add:** "ran them" is thin for the acceptance criteria. The criteria are (a) from no
kind cluster, (b) re-running is safe, (c) shutdown works. During this task the cluster was deleted
and rebuilt from scratch, which covers (a); record the second consecutive run for (b) and one
`hack/shutdown-cluster.sh` invocation for (c).

**Script failures worth remembering:** the first run exited `141` (SIGPIPE) with no error message,
because `awk '{ print $2; exit }'` closed the pipe while `helm status` was still writing, and
`pipefail` plus `errexit` turned that into a silent death inside a command substitution. Same shape
as `kind get clusters | grep -q`. A pipeline can fail because the _reader_ finished early.

### 0.5a Learn certificates before you read a webhook — 2026-10-13

**Spec review:**

**What was built:** inspect the certificate configs in kind cluster

**Tests I wrote:**

**Review answers:**

**Review grading:**

**What surprised me:**

**Quiz answers:**

1. CN becomes username, O becomes groups
2. error 1: unable to get local issuer certificate, should provide a CA. error 2: hostname mismatch,
   whether add the hostname or correct the hostname

**Quiz review:** Both correct, including the two error strings and both fixes. Nothing to re-answer.

1. Right. Two details worth carrying into task 5.3. A certificate may carry several `O` values and
   each becomes a separate group, so group membership is plural by design; the kubeadm admin
   certificate uses this (`CN=kubernetes-admin`, `O=kubeadm:cluster-admins`, older clusters
   `O=system:masters`). And the mapping is done by the API server's x509 authenticator, not by
   anything in the certificate: x509 has no concept of a Kubernetes group. It also adds
   `system:authenticated` to every successful authentication, which is why `kubectl auth whoami`
   lists a group your certificate does not contain.
2. Right, and the second half is the part most people miss: the two fixes live on different sides.
   Check 1 is fixed on the _client_ — give it the CA it should trust, which in a webhook is
   `clientConfig.caBundle`. Check 2 is fixed on the _server_ — re-issue the certificate with the
   name in `subjectAltName` — or by dialing a name already in the list, which is what `--resolve`
   does in the patch step. A certificate that fails check 2 is still perfectly valid; that is why
   the errors must stay apart.

**Evidence to add:** the quiz is done, but `What I changed`, `How I proved it`, and
`What surprised me` are empty, and the acceptance criteria ask for the step 3 output and the `curl`
error strings alongside the `openssl verify` ones. Record those three `curl` lines when you run the
patch/refine step; the stretch questions stay optional until task 2.1.

### 0.5b Read the admission webhooks you just installed — 2026-10-10

**Quiz answers:**

1. to prevent malformed actions? but is https safe enough?
2. IDK
3. KServe has one, Kueue issues its own. I ran
   `kubectl get -n kserve certificate serving-cert -o yaml` but didn't find failure modes
4. IDK

patch error:

```shell
kubectl get validatingwebhookconfiguration cert-manager-webhook -o yaml > /tmp/test.yml
```

then change the caBundle in `test.yml`

```shell
kubectl apply -f /tmp/test.yml
Warning: resource validatingwebhookconfigurations/cert-manager-webhook is missing the kubectl.kubernetes.io/last-applied-configuration annotation which is required by kubectl apply. kubectl apply should only be used on resources created declaratively by either kubectl create --save-config or kubectl apply. The missing annotation will be patched automatically.
validatingwebhookconfiguration.admissionregistration.k8s.io/cert-manager-webhook configured
```

it seems no error occurred

**Quiz review:** Answer 3's first half is right; the rest needs the mechanism. "IDK" recorded
honestly is the correct move — these are the questions to re-answer after task 2.1.

1. Not about malformed actions — the API server does schema validation itself, before admission. TLS
   is about confidentiality and authentication. The API server ships the _entire object_ to the
   webhook, which may include Secret data, and accepts back a verdict that decides whether the write
   lands. Over plain HTTP, anyone on the network path could read the object or forge an "allow". "Is
   HTTPS safe enough?" is the right instinct: the API server authenticates the _server_ against
   `clientConfig.caBundle`, and a webhook that needs to authenticate the _caller_ configures client
   certificates via the API server's `--admission-control-config-file`. Most webhooks, including the
   ones you will write, rely on the fact that only the API server can reach them in-cluster.
2. Two independent things: **(a)** the TLS chain verifies — the serving certificate is signed by the
   CA in `caBundle`, is valid for the Service's in-cluster DNS name, and has not expired; and
   **(b)** the server is actually answering — the pod is Ready, the Service has endpoints, and it
   returns a well-formed `AdmissionReview` within `timeoutSeconds`. Distinct symptoms: (a) fails
   with `x509: certificate signed by unknown authority`, (b) fails with `connection refused`,
   `no endpoints available for service`, or `context deadline exceeded`. Same user-visible outcome —
   the write is rejected — but completely different fixes.
3. Correct on which is which. The failure modes are operational, not fields in the YAML:
   - **KServe / cert-manager:** a `Certificate` issues a Secret, and cert-manager's `cainjector`
     watches the `cert-manager.io/inject-ca-from` annotation on the webhook configuration and
     patches `caBundle` in. Failure modes: cainjector down or not yet reconciled when the webhook
     configuration appears leaves `caBundle` empty, so every guarded write fails with x509; and a
     renewed certificate needs the controller pod to pick up the new mounted Secret, which is not
     instant. The dependency is external and can be broken by a cert-manager outage.
   - **Kueue:** generates and rotates a self-signed CA in its own controller process and patches the
     webhook configurations itself. Failure modes: certs are regenerated on restart, so there is a
     startup window where the configuration carries a stale `caBundle`; with multiple replicas they
     must agree on one CA. No external dependency, but also nothing your organization's PKI can
     audit or rotate. Verify with `kubectl get certificate -A` (expect entries for
     KServe/cert-manager, none for Kueue) and by diffing the annotations on each
     `ValidatingWebhookConfiguration`.
4. `failurePolicy: Fail` is correct when the webhook enforces an invariant that must never be
   bypassed — the promotion gate in task 2.1 is exactly this: an ungated `ModelVersion` must not
   reach serving, and "the gate was down" is not an acceptable reason to allow it. It becomes a
   self-inflicted outage when the webhook guards high-churn core resources and the cluster needs
   those writes to heal itself. The classic deadlock is a `Fail` webhook on Pods whose own pods
   cannot be scheduled: nothing can start, including the webhook. The standard guards are a
   `namespaceSelector` excluding `kube-system` and the webhook's own namespace, and `Ignore` for
   anything that is advisory rather than a safety invariant.

**On the patch experiment — "it seems no error occurred" is the finding, not a failed test.** Two
reasons nothing broke, both worth knowing:

1. **A webhook is only called for the resources its `rules` match.** `cert-manager-webhook` guards
   cert-manager's own API — `Certificate`, `Issuer`, `ClusterIssuer`, and so on. Corrupting its
   `caBundle` changes nothing until something creates or updates one of _those_. Admission is
   per-resource, not global. Re-run the experiment as: break the `caBundle`, then immediately
   `kubectl apply` a trivial self-signed `Issuer`, and read the x509 error.
2. **cainjector put it back.** That configuration carries `cert-manager.io/inject-ca-from`, so
   cert-manager's cainjector reconciles the field within seconds — the same level-triggered behavior
   this repository's own controllers are required to have. Check with
   `kubectl get validatingwebhookconfiguration cert-manager-webhook -o jsonpath='{...caBundle}'`
   before and a few seconds after the edit.

Also: `kubectl apply` on a Helm-created object produced the `last-applied-configuration` warning for
a real reason — the object was created by Helm, not by `apply`, and applying to it adds a competing
source of truth. Use `kubectl patch` for a deliberate one-off edit like this.

### 0.6 Add development workflow after it hurts — 2026-10-13

> **Written by Claude, not by the maintainer.** The work below was already in the tree; this entry
> was reconstructed from the repository and the commits named in it, at the maintainer's request.
> The quiz answers are Claude's, so they do not record what the learner knew before reading the
> files — re-answer them from memory if you want that signal.

**Spec review:**

**What was built:** add workflow helpers only where a real mistake had already been repeated, and
keep one owner per formatting concern. three helpers, each from a different commit and for a
different reason.

**Tests I wrote:** every commit since `91ce02f` has gone through the hook, which prints its
markdownlint summary (`Summary: 0 issues in 0 files`) as it runs. `bash hack/format.sh` with no
arguments formats every git-tracked file and exits 0 on a clean tree. `make fmt vet test` is the Go
gate.

**Review answers:**

**Review grading:**

- `hack/format.sh` (`91ce02f`) — one entry point that dispatches by extension: markdownlint `--fix`
  for Markdown structure, then Prettier for wrapping and whitespace, then goimports for Go. The tree
  already had `.markdownlint.json` and `.prettierrc` that nothing ran and that disagreed with each
  other; MD013 is now off because Prettier owns wrapping.
- `hack/git-hooks/pre-commit` (`91ce02f`) — formats staged files, restages them, then gates the
  commit on markdownlint. It also regenerates `CLAUDE.md` from `AGENTS.md` in an `EXIT` trap, so the
  generated copy is never stale and never staged. Opt-in via
  `git config core.hooksPath hack/git-hooks`.
- `agent.Dockerfile` plus `hack/agent-*` (`9ca95a2`) — the development container, added last and
  only after host setup became a real source of drift. Documented in
  [agent-container.md](../tooling/agent-container.md).

`611ea8b` later switched the Go half from `gofmt` to goimports, because `gofmt` leaves imports
untouched: a file looked formatted while still failing `make lint`.

**What surprised me:** the formatter cannot be the gate. Formatting is silent and automatic, but
markdownlint failures — a skipped heading level, a fence with no language — need a human, so they
block. Two different jobs that look like one command. The second surprise is the opposite of
automation: `format.sh` with no arguments uses `git ls-files`, so a brand-new untracked file is
silently skipped until it is staged.

**Quiz answers:**

1. `hack/format.sh`. Not the hook, not the Makefile, not an editor setting — those all call it. The
   point is that the Claude Code hook, the git pre-commit hook, and a human running it by hand apply
   exactly the same rules; a formatter that disagrees with itself depending on who invoked it is
   worse than none. Within that script each concern still has exactly one owner: markdownlint for
   Markdown structure, Prettier for wrapping, goimports for Go.
2. Anything a generator owns, because formatting it produces a diff the next generator run reverts.
   Go files carrying the `// Code generated ... DO NOT EDIT.` marker are skipped by `format.sh` —
   the marker is the Go convention, so a renamed file is still skipped — and `.golangci.yml` matches
   that with `exclusions: generated`. On the YAML side, `.prettierignore` excludes
   `config/crd/bases/` (controller-gen), `dist/`, `bin/`, and `hack/agent-home/`, which lazy.nvim
   writes. `CLAUDE.md` is a third case: generated from `AGENTS.md`, gitignored, and refreshed by the
   hook rather than formatted as a source file.
3. Because a wrapper you cannot run by hand is a wrapper you cannot debug. The evidence rule in the
   task is the same idea from the other end: a helper added before the mistake it prevents has
   actually happened encodes a guess, and each one is a place where the real command can drift out
   of sight. `611ea8b` is the case in point — the `gofmt` fallback was only understood as wrong
   after seeing a formatted file still fail `make lint`.

**Outstanding:** the patch/refine step is not done. No helper prints the command it is about to run;
`format.sh` is silent on success and the pre-commit hook prints only its warnings. The acceptance
criterion "workflow files are documented in README or progress notes" is met for the container by
[agent-container.md](../tooling/agent-container.md) and for the rest by this entry, but README still
does not mention `hack/format.sh` or how to enable the hooks.

**Quiz review:** not applicable — Claude wrote the answers above.

### 1.1 Dataset lifecycle — 2026-10-24

**Learn — what I saw:** typing [1.1-dataset-reconciler.md](../curriculum/1.1-dataset-reconciler.md)
part one. Which step's reason you had not expected, and which of the three Kubebuilder-book patterns
you would have written yourself.

**Spec review:** _(in arrears — `Dataset.status` is already specified in
`docs/product/domain.md#status`. Does it say what happens when the store is unreachable but an
earlier observation exists? Is `Resolved=False` a failure or a fact?)_

**What was built:** drift detection and the immutability webhook. Reconcile logic and
`ValidateUpdate`'s criteria. Built by the maintainer, under the format that came before this one.

**Tests I wrote:** `make test`. Two negatives are the ones worth re-checking: a rewritten artifact
noticed on a later reconcile, and a frozen-field edit rejected. Check they fail for the right
reason.

**Review answers:** _(exercise A — `ValidateUpdate` versus CEL transition rules. Both are built:
`task/1.1/variant-webhook` and `task/1.1/variant-cel`, same six refusal cases on each side. Exercise
D — the `Dataset` an admitted `TrainingJob` referenced is deleted mid-run.)_

**Review grading:**

**What surprised me:** must bear in mind that never trust the status seen last time

**Quiz answers:**

1. IDK, how to consider such situations?
2. what to do next. the condition just records what happened, but doesn't give actions
3. I can only come out of one, change the frozen fields. this will be forbidden in
   dataset_webhook.go 121
4. error will let the queue handle itself, RequeueAfter asks the queue to wait. When nothing went
   wrong but just need to wait, use the latter one, otherwise just return the error. It conflates
   failure and throttle
5. IDK

**Quiz review:** one of six answered correctly, one misunderstood, one half answered, three left
open. Scores below; the corrections are the useful part, so they stay recorded.

1. _Level-triggered line_ — **unanswered** ("how to consider such situations?"). The question is
   asking you to find a line whose correctness depends on nothing being remembered between calls.
   The strongest candidate in the file is that `r.Resolver.Resolve(...)` runs unconditionally: wrap
   it in `if dset.Status.ObservedChecksum == nil` and a rewritten artifact could never be noticed
   again after the first pass, because the one line that could notice it would be skipped. A second
   one is `previousPhase := dset.Status.Phase` — it reads the phase out of the object, not out of a
   field on the reconciler. Cache the last phase in the struct instead and a controller restart or a
   second replica emits the wrong events, because the memory and the cluster disagree.
2. _`observedGeneration` on a condition_ — **misunderstood**. The answer given ("the condition
   records what happened but does not give actions") is about conditions in general. The question is
   narrower: `metadata.generation` increments on every `spec` write and never on a status write, so
   comparing it to the condition's `observedGeneration` answers _has the controller caught up with
   the edit I just made?_ Without it, `Ready: True` is ambiguous right after an edit — it could
   describe the spec you just submitted or the one before it. It is the field that makes "wait for
   my change to take effect" possible.
3. _Two ways to change a frozen field_ — **half answered**. The way named (editing the fields, which
   `ValidateUpdate` rejects) is right. The second is **delete and recreate under the same name**:
   that is a `DELETE` followed by a `CREATE`, so `ValidateUpdate` never runs, `ValidateCreate` sees
   no old object, and it is admitted — a new `uid`, same name, lineage quietly broken. More
   generally the webhook catches exactly what arrives as an update to `datasets` and nothing else:
   not writes to another resource or subresource, not a future `v1beta1` (the marker lists
   `versions=v1alpha1`), and not anything that skips admission, such as deleting the
   `ValidatingWebhookConfiguration` or writing to `etcd` directly.
4. _Error versus `RequeueAfter`_ — **correct**. Worth sharpening with the mechanics: an error is
   logged at error level, increments `controller_runtime_reconcile_errors_total`, and re-adds the
   key rate-limited (exponential, 5ms doubling to 1000s, per item, kept until a clean reconcile
   calls `Forget`); `RequeueAfter` calls `Forget` first and schedules a plain delay. The two things
   `Result.Requeue` conflated are precisely the ones named: _retry because something failed_, which
   is what a rate limiter is for, and _come back soon although nothing failed_, which is a schedule.
5. _Webhook versus marker_ — **unanswered**. What the webhook can see that a plain marker cannot:
   the previous version of the object, the requesting user, the dry-run flag, and other objects in
   the cluster. Which part of the rule could have been a marker: all three field comparisons, as CEL
   transition rules — `+kubebuilder:validation:XValidation:rule="self == oldSelf"` — with the caveat
   that `rows` and `checksum` are optional, and Kubernetes skips a transition rule when the old
   value is absent, so each needs a `has()` companion rule on the parent struct. That leaves the
   webhook earning its place only for rules needing another object or an identity.
   [reference/validation-and-immutability.md](../reference/validation-and-immutability.md) has the
   long version.
6. _The `artifact.Resolver` seam_ — **not answered**. Two things it makes testable: the failure path
   (`FakeResolver{Err: ...}` turns "the store is down" into one line, where a real client would need
   a network envtest does not have), and drift itself (the fake can return different metadata on the
   second call, which is the only way to simulate an artifact rewritten overnight). The cost is a
   second implementation that can disagree with the real one: `FileResolver` returns `Rows: 0` for
   an empty file and the fake never does, which is exactly how review item 5 below stayed invisible.

Revisit 2, 3 and 6 before 1.2 — 2 is a misunderstanding rather than a gap, and 3 and 6 each left a
required part unanswered. 1 and 5 are recorded as misses rather than re-asked.

**Build notes (written by Claude, 2026-10-22):** notes on the drift work as it stands in
`internal/controller/dataset_controller.go`, kept here so the device change does not lose them.
Mechanisms and symptoms only; the fixes are still the build half.

_On the question that prompted this:_ a `ConditionFalse` is data, not a failure. The API server
validates the conditions list structurally — the `listMapKey=type` uniqueness, the
`True`/`False`/`Unknown` enum, the `reason` regex, the message length — and has no idea that `False`
means something went wrong. `Status().Update` errors only for transport reasons: `409` on a stale
`resourceVersion`, `403` on RBAC, `422` when the object violates the schema (for instance
`observedRows: 0` against the `Minimum=1` marker). "Report badness" and "the write failed" are
independent axes, which is why step 4 computes the whole status in memory and step 5 writes once.

_Observations:_

- `detectDrift` dereferences `status.observedRows` and `status.observedChecksum` unconditionally,
  but the resolve-failure branch never sets them. First failing reconcile is a nil dereference in a
  worker goroutine — a restart, not a test failure. The semantic question behind it: when the store
  is unreachable, is comparing the claim against a stale earlier observation meaningful at all?
- Drift currently overwrites the `Resolved` condition with `Reason: DriftDetected`, but `Resolved`
  is still factually true — the artifact was reached and read. A consumer filtering on
  `Resolved=False` cannot separate "the store is down" (retry) from "your checksum is wrong" (do not
  retry). Part one says part two adds the second condition type; _errors name the layer at fault_ is
  the test. Either answer is defensible, but it has to be a decision.
- The return contract changed: `RequeueAfter` now fires only on a phase transition. In the steady
  state a `Ready` Dataset whose artifact is rewritten overnight gets no watch event and is never
  re-resolved, so acceptance criterion one cannot pass.
- The `condition` value is still built at the top and still feeds the event message, but the real
  conditions are now written inline in each branch, so `condition.Message` is empty and the event
  text stops after `Dataset Failed:` with nothing behind the colon. This is the trap step 4's second
  bullet describes, arrived at from the other direction.
- Smaller: `strings.Compare(a, b) != 0` is `a != b` for Go strings; `fmt.Sprintf("%v", []string)`
  renders Go syntax into a message a human reads in `kubectl describe`; and "expect/get" names the
  spec as the truth when the spec is the _claim_ and the artifact is the truth.

_On sharing the check with the webhook:_ they are not the same check. `ValidateUpdate` compares
`oldObj.Spec` to `newObj.Spec` and asks whether the user changed an immutable field; it never reads
`status` and cannot reach the artifact store, since it runs inside the API request path under a
timeout. The reconciler compares the claim in `spec` to what it just observed. What is genuinely
shared is the pointer comparison and the vocabulary, not the rule. `ptr.Equal` from `k8s.io/utils`
(already in the module graph) collapses the both-nil / one-nil / equal-value cases that `*a != *b`
gets wrong. For structure, the Kubernetes idiom is a pure function returning `field.ErrorList` from
`k8s.io/apimachinery/pkg/util/validation/field`: the webhook renders it with `apierrors.NewInvalid`
and gets a structured 422, the reconciler renders `errs.ToAggregate()` into a condition message. It
has to live in a package both can import downward — methods on the type in `api/v1alpha1`, or a
small `internal/` rules package — with no `client.Client` and no `context` in it. The test that the
boundary is right: that package is testable with table-driven unit tests and no envtest.

**Part two review — round two (written by Claude, 2026-10-23):** second attempt is `1b4c1ff`; both
sides now build `field.ErrorList` and compare optional fields with `ptr.Equal`, which is the right
direction. The patterns it is reaching for, what upstream does instead, and both attempts read
against each other are in
[reference/validation-and-immutability.md](../reference/validation-and-immutability.md), written
from a web search of how Kueue, Cluster API, Knative and the CEL transition rules handle the same
problem. The short version, so the log stands alone:

- `apimachinery` already ships `ValidateImmutableField`, which is the hand-rolled comparison plus
  `Semantic.DeepEqual` on the pointees plus the standard `field is immutable` message.
- `ValidateUpdate` returns `apierrors.NewInvalid` unconditionally, so every update is rejected, not
  only the ones that changed a frozen field. An empty `ErrorList.ToAggregate()` is already `nil`.
- `field.ErrorTypeInternal` means the server broke; a user editing a frozen field is
  `field.Invalid`. Field paths belong in `field.NewPath("spec", "rows")`, not bare strings.
- Switching to `ptr.Equal` in `detectDrift` dropped the "only if the user claimed it" guard, so a
  Dataset claiming nothing now drifts against its own observation.
- The drift error returns from `Reconcile` before `Status().Update`, so the condition it computed is
  never persisted, and a permanent mismatch retries forever with backoff.
- `make test` is still red on the same store-unreachable spec: the checksum branch dereferences
  `*dset.Spec.Rows`, a copy-paste that the duplicated per-field block makes easy in both files.
- The modern answer to a pure field-equality immutability rule is a CEL transition rule
  (`self == oldSelf`) in the CRD, with no webhook at all — with a caveat for optional fields. The
  webhook is still the right exercise here, because the admission path is what 1.1 teaches.

**Mechanical pass applied by Claude, 2026-10-23, at the owner's request:** the items above that were
wrong rather than undecided — the `*dset.Spec.Rows` copy-paste in the checksum branch, the
`errorField := "rows"` twin in the webhook, bare string field names, `field.ErrorTypeInternal` on a
user error, the discarded `oldObj` type assertion, the unconditional `NewInvalid` with an empty
`GroupKind`, the dead `details` slices, the odd logr arguments, and `spec.uri` going unchecked. The
webhook now calls `apivalidation.ValidateImmutableField` for the three frozen fields. Three
decisions were deliberately left open, because they are the lesson:

1. whether drift should fire when the user claimed nothing — `ptr.Equal` currently says a `nil`
   claim differs from an observation;
2. which condition type carries drift, `Resolved` or a second one;
3. whether the drift path returns an error from `Reconcile` at all, and where the status write sits
   relative to that return.

`make test` is still red, and now for a readable reason rather than a panic: the store-unreachable
spec fails with `spec.rows: Invalid value: "unset": artifact resolved to unset rows, spec claims 1`.
Nothing was observed on that branch, so decisions 1 and 3 are exactly what the test is asking for.
Controller coverage went from 71.7% to 86.4% in passing.

**Part two review — finished implementation (written by Claude, 2026-10-23):** `make fmt vet test`
passes, 13 of 13 specs, controller coverage 71.1%. `make manifests generate` leaves `config/`
unchanged, so the generated files match the markers.

Against the checklist at the end of
[training-tasks.md](../curriculum/training-tasks.md#reviewing-the-work): ecosystem behavior is
adopted rather than rebuilt (`apivalidation.ValidateImmutableField`, `field.ErrorList`, `ptr.Equal`,
`apimeta` conditions); the API carries lineage rather than translating YAML; errors name the layer
at fault; generated files are in sync. Owner references do not apply — the `Dataset` controller
creates no children.

What the implementation got right: `detectDrift` is gated on `resolveErr == nil`, so an unreachable
store is reported as `ArtifactUnreachable` and never as a mismatch; the nil checks are semantic
(`Spec.Rows != nil` means "the registrant claimed nothing"), not defensive, and nothing dereferences
a pointer it has not proved; the webhook freezes `spec.uri`, `spec.rows` and `spec.checksum`, guards
the empty error list, and returns `NewInvalid` with a real `GroupKind` and object name, so a
rejection arrives as a 422 with one cause per field. Recording `observedRows` and `observedChecksum`
even when they mismatch is a deliberate choice in favor of forensics; the opposite choice is equally
defensible and the quiz should ask for the defense.

Open, in order of impact:

1. **No steady-state resync.** `RequeueAfter` is returned inside the phase-transition branch, so a
   `Ready` object that stays `Ready` ends with `ctrl.Result{}, nil`. The artifact store has no
   watch, so nothing calls the reconciler again, and the first acceptance criterion — a rewritten
   artifact moving a `Ready` Dataset to `Failed` with no user edit — cannot happen in a cluster even
   though the drift logic itself is right. The requeue answers "when do I look again"; the event
   answers "what changed". They are one branch today.
2. **Events carry an empty message.** The `condition` value is declared with only `Type` and
   `ObservedGeneration` and never filled, while the real conditions are built inline, so
   `kubectl describe` prints `Dataset Failed:` with nothing after the colon.
3. **Neither feature has a test.** The webhook suite is still the scaffold's commented-out examples
   and no spec exercises drift, which is why coverage fell from 86.4% to 71.1% while the code grew.
   The suite as it stands cannot catch items 1 or 2.
4. **The rejection does not tell the user what to do.** `field is immutable` is the standard
   phrasing and worth keeping for `rows` and `checksum`; the criterion asks for guidance to create a
   new `Dataset`, which argues for a hand-built `field.Invalid` on `spec.uri`.
5. **An empty artifact fails forever.** `FileResolver` returns `Rows: 0` for a zero-row file,
   `observedRows` carries `+kubebuilder:validation:Minimum=1`, so the status write is rejected with
   a 422 and the object retries with backoff indefinitely. Either zero is a legal observation or it
   must not be recorded.
6. Minor: `detectDrift` takes a `context.Context` it discards.

**Review items 1, 2 and 6 applied by Claude, 2026-10-23:** the resync now returns unconditionally
and a phase transition only decides whether to emit an Event; the reconciler builds one `Condition`
value across the resolve and drift branches and calls `SetStatusCondition` once, so the Event
message is no longer empty; `detectDrift` lost the context it discarded. `make fmt vet test` passes,
controller coverage 72.5%.

Left open on purpose: the tests for drift and for the webhook rejection, and the patch/refine step
(the immutability message telling the user to create a new `Dataset`, plus its assertion) — that
step is the exercise, so it stays unwritten. Review item 5, whether an artifact with zero rows is a
legal observation or an unusable artifact, is a decision rather than a fix.

**The duplicate-URI warning — owner's own work, then Claude's, 2026-10-26:**

Owner-written: the immutability message now appends "Do not modify the immutable fields, create a
new Dataset instead" to the standard `field is immutable` detail rather than replacing it, so the
conventional phrase and the guidance both reach the user, and `IsInvalid` plus the per-field causes
are untouched. That closes the patch/refine step and the third acceptance criterion.

Claude-written at the owner's request, after the owner decided not to implement it: everything
below. It is not part of 1.1's acceptance criteria — it is the "uniqueness is `metadata.name`"
design note, reached by way of a name-collision `Get` in `ValidateCreate` that could not have
helped.

- The `Get` by name/namespace in `ValidateCreate` was deleted. The storage layer already refuses a
  duplicate key with `AlreadyExists` after admission, so the check could only duplicate a guarantee
  it cannot match: it reads a cache that may lag, costs an informer and RBAC, and sees an empty
  `metadata.name` for a `generateName` create, because the name is assigned in the storage layer
  after admission runs. A comment in the file says so, since the absence is the decision.
- `DatasetCustomValidator` now holds a _named_ `Client` field, wired from `mgr.GetClient()` in
  `SetupDatasetWebhookWithManager`. Embedding `client.Client` promoted `Create`/`Update`/`Delete`
  onto a type whose whole job is to be read-only.
- `warnOnDuplicateURI` lists Datasets in the namespace and returns `admission.Warnings` — never an
  error — when another one registers the same `spec.uri`. Two messages: same `spec.checksum` means
  lineage cannot distinguish the two registrations; a different `spec.checksum` means one of the two
  is wrong about the same artifact. A List failure is logged and swallowed; an advisory warning is
  not worth failing a valid create over.
- Five specs in `dataset_webhook_test.go` cover it, each asserting the create is still admitted: no
  duplicate, agreeing checksum, disagreeing checksum, a sibling in another namespace, and the object
  not warning about itself. They use `fake.NewClientBuilder()` rather than envtest, because the rule
  is a pure function of what the List returned.
- The reject spec became a `DescribeTable` over six entries: each frozen field changed, `nil` -> set
  on `rows`, set -> `nil` on `checksum`, and all three at once. Every entry asserts `IsInvalid`, one
  `FieldValueInvalid` cause per changed field matched with `ConsistOf`, and both message substrings.
- No RBAC marker was added: the controller's existing `datasets` `get;list;watch` grant covers the
  webhook's List, since both run under the manager's ServiceAccount. If the webhook ever moves out
  of this manager, that grant has to move with it.

`go test ./...` passes with `KUBEBUILDER_ASSETS` pointed at `bin/k8s/1.34.1-linux-amd64`;
`make fmt vet test` was not run end to end here.

**Review item 5 decided by the owner, 2026-10-26 — zero rows is an unusable artifact**, implemented
by Claude. A Dataset nothing can be trained or evaluated on is a failure of the artifact, so it is
never recorded as a successful observation:

- `Reconcile` now branches three ways instead of two. `ArtifactUnreachable` still means the store
  did not answer; the new `ArtifactEmpty` means it answered and the answer is unusable. Keeping them
  apart is the "errors name the layer at fault" convention — one is retryable infrastructure, the
  other is a fact about the artifact the registrant has to fix.
- The empty branch records `observedChecksum` and `lastResolvedTime`, because the resolution did
  happen, and sets `observedRows` back to `nil`. A row count left over from an earlier pass would
  assert an observation this pass contradicts.
- Drift detection is now gated on `usable` rather than on `resolveErr == nil`. Reporting drift for
  an empty artifact would blame the registrant's claimed row count for an artifact-side problem.
- The `+kubebuilder:validation:Minimum=1` marker on `observedRows` stays, and now expresses the same
  policy at the API layer: a zero-row observation is not representable. It was the symptom that
  raised the question (a 422 on the status write, retrying forever); it is the enforcement now that
  the controller never attempts that write.
- Two specs in `dataset_controller_test.go`: an artifact that resolves to zero rows goes `Failed`
  with `ArtifactEmpty` and no `observedRows`, and a Dataset that was `Ready` at one row has its
  `observedRows` cleared when the artifact is later emptied.

`go test ./...` passes; `make manifests generate` updated only the `observedRows` description in
`config/crd/bases/ml.aiplatform.io_datasets.yaml`.

**Drift tests — owner's own work, finished by Claude at the owner's request, 2026-10-27:** this
closes open item 3 above for the reconciler half; the webhook half closed on 2026-10-26.
`make fmt vet test` passes, 20 of 20 specs, controller coverage 72.5% -> 91.7%, committed as
`f0e01ef`.

Owner-written: the `DescribeTable` driven by resolved `artifact.Metadata` rather than by a mutated
spec, its three entries, the `DriftDetected` and `observedGeneration` assertions, and moving
`validSpec` out of `dataset_api_test.go` into `suite_test.go` as `ValidSpec` so the controller specs
share the fixture the API specs already used.

Claude-written at the owner's request, after the owner decided to stop there: everything below.

- **A `DescribeTable` with no `Entry` registers no specs and still passes.** The first draft had a
  table body and no entries, so `make test` was green over a feature with zero coverage. A table
  generates one spec per `Entry`; with none, Ginkgo has nothing to run and nothing to report. The
  spec count in `-ginkgo.v` output is the only thing that shows it.
- **The table was named for the webhook's job** — "should go Failed when frozen fields are modified"
  — and its body mutated a local `DatasetSpec` copy after `Create`, which reaches no API server. The
  reconciler can never observe that case: `ValidateUpdate` rejects a frozen-field edit at admission,
  so the edit never lands and no reconcile follows. The controller-side story is the opposite one —
  the spec stays frozen and the _artifact_ moves — which is what the table now varies.
- **The acceptance criterion now has a spec.**
  `should fail a Ready Dataset whose artifact was rewritten under it` reconciles twice against
  different metadata and asserts `Generation` is identical across both passes. That is the part that
  makes it the stated criterion rather than a repeat of the table: `generation` increments on a spec
  write and never on a status write, so an unchanged generation is the proof that the world moved
  and the user did not.
- **Transition events are asserted, including the absence of one.** `record.NewFakeRecorder(10)`
  appeared in every spec and was never read, so step 6 was untested. The recorder is now a parameter
  of the shared `reconcileWith` helper so it survives several passes, and a third identical pass
  asserts `recorder.Events` receives nothing — one event per transition, not per resync.
- **An unclaimed field cannot drift.** Both comparisons in `detectDrift` are guarded by `!= nil`,
  and neither guard was exercised, because `ValidSpec` sets both. A Dataset registering no `rows`
  and no `checksum` against an artifact resolving 50 rows must come back `Ready`. This is decision 1
  from the 2026-10-23 mechanical pass, now pinned by a test rather than by a comment.
- **Drift messages are asserted as whole clauses**, not by field path.
  `ContainSubstring("spec.rows")` still matches a message that printed the observed and the claimed
  value the wrong way round, which is the one defect in this code a reader would act on at 3am.

_Verified by mutation rather than by coverage._ Four single-line breaks to `dataset_controller.go`,
each reverted, each failing a named spec:

| Mutation                                      | Caught by                                           |
| --------------------------------------------- | --------------------------------------------------- |
| `if usable` -> `if true`                      | the `ArtifactUnreachable` and `ArtifactEmpty` specs |
| drop the `dset.Spec.Rows != nil` guard        | the unclaimed-field spec                            |
| `!= previousPhase` -> `\|\| true`             | the rewritten-artifact spec                         |
| swap observed and claimed in the rows message | `rows drifted` and `both drifted`                   |

Worth recording from that exercise: the first attempt at the event mutation was `if true {`, which
does not compile, because `previousPhase` goes unused. A mutation that fails to build proves nothing
about the suite, and reads exactly like a mutation the suite failed to catch.

### 1.2 TrainingJob on Kueue — 2026-11-01

**Learn — what I saw:** driving Kueue by hand per
[1.2-Kueue-and-jobs.md](../curriculum/1.2-kueue-and-jobs.md). Record the `Workload` conditions in
the order they appeared, and the exact refusal message when the `ClusterQueue` ran out — that string
is what your controller has to translate.

**Spec review:** the proposal is at `task/1.2/spec` in `docs/product/domain.md#trainingjob-status`.
Read it before the build. It merges nine Kueue reasons into three platform reasons. Each merge and
each split is argued there. A rejected merge changes the implementation, so this comes first.

**What was built:** `task/1.2/build`. Status conditions `Rendered`, `Accepted` and `Finished`, a
phase derived from them, a `jobRef`, one status write per pass, an Event on a phase change, a
`Watches` on the Workload, and three `WorkloadFinder` implementations behind one seam.

**Tests I wrote:** _(yours)_ one translation case per reason the Spec beat kept distinct. The list
is at the foot of `internal/controller/trainingjob_status_test.go`. Claude wrote the rendering cases
in envtest, the finder contract against a fake client, and one worked translation case.

**Review answers:** _(exercise A — which `WorkloadFinder`, and why. Exercise D — the Kueue mutating
webhook is unreachable when the controller creates the Job.)_

**Review grading:**

**What surprised me:**

**Quiz answers:**

**Quiz review:**

**Build notes (written by Claude, 2026-11-09):** what the build turned up, and the mistakes in the
maintainer's first pass that it corrected. Kept here because the misses are the point.

_On the question that prompted this — "which function creates the `Workload`?":_ there is a
function, and it runs in Kueue's process rather than ours. In `sigs.k8s.io/kueue@v0.20.4` the chain
is `jobframework.(*JobReconciler).Reconcile` → `handleJobWithNoWorkload` → `constructWorkload` →
`jobframework.ConstructWorkload` → `NewWorkload`, then a plain `r.client.Create(ctx, wl)`;
`ConstructWorkload` calls `ctrl.SetControllerReference(object, wl, c.Scheme())` on the way, and the
reconciler emits a `CreatedWorkload` event on the Job afterwards. Nothing exotic — the same `Create`
any controller makes, from a reconciler watching Jobs instead of `TrainingJob`s. "Kueue creates it
for you" means that process is already running in `kueue-system` and already watching, which is why
part one produced a `Workload` with no controller of ours alive at all.

Three things follow from reading it. The `Workload` name is derived, not random:
`GetWorkloadNameForOwnerWithGVK(name, uid, gvk)` in `workload_names.go` hashes the owner's UID and
GVK into a five-character suffix, which is what keeps a Job and a JobSet of the same name apart. The
pod shape quota is reserved for comes from `JobPodSets(ctx, job, c)` reading the Job itself, so the
Job's own resource requests are the only input — a field that never reaches the Job cannot be
charged against a `ClusterQueue`. And `GenericJob` — `PodSets`, `Suspend`, `RunWithPodSetsInfo`,
`Finished` — is the interface the tutorial was really describing: implement it, register the
integration, and Kueue manages `TrainingJob` directly with no Job in between. That is a real
alternative design, and not this task. Here the chain is `TrainingJob` → `Job` → (Kueue) →
`Workload`, and writing a `Workload` by hand would put two admission requests behind one Job and
break "never mutate another operator's resources".

_Observations, in rough order of how early they bite:_

- Neither of the two "does not compile" claims this review opened with was true, and both were wrong
  the same way: Go 1.27 is newer than the habits they came from. `new(true)` — `new` over a value
  rather than a type — is 1.27, and `suite_test.go` was already using it.
  `batchv1.Job{Name: ..., Namespace: ...}` — promoted fields of an embedded struct named directly in
  a composite literal — is also 1.27; before it those keys had to go through
  `ObjectMeta: metav1.ObjectMeta{...}`. Both forms stay as originally written. The lesson is about
  the review rather than the code: on a toolchain this far ahead, "that is not valid Go" is the
  claim to check first, not the one to assert.
- `tj.Name + "Job"` is a legal DNS-1123 name only while `tj.Name` has no capitals to clash with —
  but the deeper issue is that the derived name is not deterministic across a rename and carries no
  separator. Kueue names its own child the same way and is worth copying.
- No `ownerReference` on the Job. The repo convention requires one on every child, and part one's
  step 5 is what it buys: delete the `TrainingJob`, the Job goes, and the `Workload` goes with it
  because Kueue made the Job _its_ owner. Without the link the Job outlives its parent.
- `job.Spec.Selector` is set by hand. The Job controller generates a selector with a
  `batch.kubernetes.io/controller-uid` key and only honours a handwritten one under
  `spec.manualSelector: true`; the `TrainingJob`/`Owner` labels wanted here belong on the pod
  template's `metadata.labels`, not in the selector.
- The mutate function assigns `job.Spec` wholesale on every reconcile. Most of `JobSpec` is
  immutable once created, so the second pass fails the update — and `suspend` is precisely the field
  Kueue writes, so rewriting the spec would re-suspend an admitted Job and undo the admission being
  waited for. A mutate function for an existing child can only touch fields the controller owns.
- No Event on the transition, and the one error log says "Deployment reconcile failed" — a leftover
  string naming the wrong kind. Errors are meant to name the layer at fault.
- Nothing yet reads back what Kueue decided. The refusal string recorded in the Learn half —
  `QuotaReserved: False` naming the exhausted resource — is what the `TrainingJob` status has to
  translate, and `kueuev1beta2` is imported but unused until that lands.
- Lowering `Resources.Accelerator` onto the container fails admission outright: a container resource
  name must be standard or fully qualified, and `AcceleratorClassPlaceholder` is neither. envtest
  returns a 422 naming both `resources.requests` and `resources.limits`. Mapping a class to a real
  extended resource name such as `nvidia.com/gpu` is task 5.1, so the field stays unlowered and a
  TrainingJob asking for an accelerator is admitted on cpu and memory alone.

_All fixed at `task/1.2/build`, except the accelerator._ `make fmt vet test lint` is green there.

Two things the build turned up that no amount of reading would have:

- `jobframework` panics on a Job with a nil `Parallelism`. `podsCount` dereferences it on the
  comment "parallelism is always set as it is otherwise defaulted by k8s to 1". True of a Job read
  back from the API server. False of one held in memory. Production is safe. Unit tests with a fake
  client are not.
- `FindMatchingWorkloads` returns nil for a drifted Job. It answers "is there an equivalent
  Workload", not "which Workload belongs to this Job". A list by owner or by job-uid still finds it.

Both are now characterisation tests. They fail if upstream changes its mind.

### 1.3 ModelVersion and the funnel — YYYY-MM-DD

**Learn — what I saw:**

**Spec review:**

**What was built:**

**Tests I wrote:**

**Review answers:**

**Review grading:**

**What surprised me:**

**Quiz answers:**

**Quiz review:**

### 1.4 Evaluation and gates — YYYY-MM-DD

**Learn — what I saw:**

**Spec review:**

**What was built:**

**Tests I wrote:**

**Review answers:**

**Review grading:**

**What surprised me:**

**Quiz answers:**

**Quiz review:**

### 1.5 Release on KServe — YYYY-MM-DD

**Learn — what I saw:**

**Spec review:**

**What was built:**

**Tests I wrote:**

**Review answers:**

**Review grading:**

**What surprised me:**

**Quiz answers:**

**Quiz review:**

### 2.1 The promotion gate as admission control — YYYY-MM-DD

**Learn — what I saw:**

**Spec review:**

**What was built:**

**Tests I wrote:**

**Review answers:**

**Review grading:**

**What surprised me:**

**Quiz answers:**

**Quiz review:**

### 2.2 What CEL can do instead — YYYY-MM-DD

**Learn — what I saw:**

**Spec review:**

**What was built:**

**Tests I wrote:**

**Review answers:**

**Review grading:**

**What surprised me:**

**Quiz answers:**

**Quiz review:**

### 2.3 Quota and priority as configuration — YYYY-MM-DD

**Learn — what I saw:**

**Spec review:**

**What was built:**

**Tests I wrote:**

**Review answers:**

**Review grading:**

**What surprised me:**

**Quiz answers:**

**Quiz review:**

### 3.1 Conditions, events, and metrics — YYYY-MM-DD

**Learn — what I saw:**

**Spec review:**

**What was built:**

**Tests I wrote:**

**Review answers:**

**Review grading:**

**What surprised me:**

**Quiz answers:**

**Quiz review:**

### 3.2 Alerts — YYYY-MM-DD

**Learn — what I saw:**

**Spec review:**

**What was built:**

**Tests I wrote:**

**Review answers:**

**Review grading:**

**What surprised me:**

**Quiz answers:**

**Quiz review:**

### 3.3 Run an incident — YYYY-MM-DD

This entry is an incident note, not a build entry.

**What I broke:**

**Symptom a user would have reported:**

**Where I looked, in order:** own CRD, conditions, events, upstream object, logs — and where the
truth actually was.

**Which layer I blamed first, and whether that was right:**

**What would have told me sooner:** the condition, event, or alert that does not exist yet.

**Fix applied:**

### 3.4 Write the runbook — YYYY-MM-DD

**Learn — what I saw:** re-reading the incident note above and the error strings recorded in 0.5a
and 0.5b. Which symptoms were unambiguous, and which needed a second command to tell apart.

**Spec review:**

**What was built:**

**Tests I wrote:**

**Review answers:**

**Review grading:**

**What surprised me:**

**Quiz answers:**

**Quiz review:**

### 4.1 envtest for every reconciler — YYYY-MM-DD

**Learn — what I saw:**

**Spec review:**

**What was built:**

**Tests I wrote:**

**Review answers:**

**Review grading:**

**What surprised me:**

**Quiz answers:**

**Quiz review:**

### 4.2 Idempotency and crash safety — YYYY-MM-DD

**Learn — what I saw:**

**Spec review:**

**What was built:**

**Tests I wrote:**

**Review answers:**

**Review grading:**

**What surprised me:**

**Quiz answers:**

**Quiz review:**

### 4.3 Contract tests for the release path — YYYY-MM-DD

**Learn — what I saw:**

**Spec review:**

**What was built:**

**Tests I wrote:**

**Review answers:**

**Review grading:**

**What surprised me:**

**Quiz answers:**

**Quiz review:**

### 4.4 End-to-end — YYYY-MM-DD

**Learn — what I saw:**

**Spec review:**

**What was built:**

**Tests I wrote:**

**Review answers:**

**Review grading:**

**What surprised me:**

**Quiz answers:**

**Quiz review:**

### 5.1 Make the GPU request real on kind — YYYY-MM-DD

**Learn — what I saw:**

**Spec review:**

**What was built:**

**Tests I wrote:**

**Review answers:**

**Review grading:**

**What surprised me:**

**Quiz answers:**

**Quiz review:**

### 5.2 Swap the training runtime — YYYY-MM-DD

**Learn — what I saw:**

**Spec review:**

**What was built:**

**Tests I wrote:**

**Review answers:**

**Review grading:**

**What surprised me:**

**Quiz answers:**

**Quiz review:**

### 5.3 Identity and ownership — YYYY-MM-DD

**Learn — what I saw:**

**Spec review:**

**What was built:**

**Tests I wrote:**

**Review answers:**

**Review grading:**

**What surprised me:**

**Quiz answers:**

**Quiz review:**

### 5.4 Run it on a cloud cluster — YYYY-MM-DD

**Learn — what I saw:**

**Spec review:**

**What was built:**

**Tests I wrote:**

**Review answers:**

**Review grading:**

**What surprised me:**

**Quiz answers:**

**Quiz review:**
