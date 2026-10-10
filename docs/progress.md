# Maintainer Log

Your own log for [training-tasks.md](training-tasks.md). Keep it in the repository so the work is
portable: someone reading this file should be able to see what was built, what was learned, and what
is next.

One entry per task. Keep them short; the value is in the evidence and the surprises.

## Entries

### Archive before blank rebuild — 2026-10-07

**Goal:** preserve the generated scaffold before resetting the curriculum to start from blank.

**What I changed:** committed the previous state as `131697f` with message
`Archive generated kubebuilder scaffold before blank rebuild`, then removed the generated
Kubebuilder project, generated manifests, controller/test scaffolds, cluster scripts, local workflow
scripts, editor files, and CI files.

**How I proved it:** `git status --short` was clean after the archive commit; the follow-up deletion
leaves this repository as documentation plus project policy.

**What surprised me:** the generated project is not just `api/` and CRDs. `Makefile`, `PROJECT`,
`go.mod`, `cmd/main.go`, `config/`, RBAC, samples, tests, cluster scripts, and workflow helpers all
carry assumptions. Starting from blank makes those assumptions visible.

**Quiz answers:** none; this was a repository reset, not a training task.

### 0.0 Install the local toolchain — 2026-10-07

**Goal:** install tools

**What I changed:** install go, docker, kubectl, kind, kubebuilder

**How I proved it:**

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

**Goal:** maintain the repo

**What I changed:** almost nothing, just commit the repo and push to remote

**How I proved it:**

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

**Goal:** build the scaffold using kubebuilder cli

**What I changed:** just move the generated files here

**How I proved it:** `go test ./...` passes and `make help` shows the help messages

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

**Goal:** add a new CRD

**What I changed:** annotations on generated api Go files

**How I proved it:** run generate commands and get the artifacts

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

**Goal:** test api server behaviour on our CRD

**What I changed:** add a new test file `dataset_api_test.go`

**How I proved it:** `make test` passes

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

**Goal:** set up the cluster from zero and shutdown it idempotently

**What I changed:** two scripts, `hack/set-up-cluster.sh` and `hack/shutdown-cluster.sh`

**How I proved it:** ran them

**What surprised me:** shouldn't rely on script itself to ensure idempotency since these resources
are manged by k8s and helm, should use helm cli instead

**Quiz answers:**

1. install ends when deployments are **CREATED**, not **READY**, while `kubectl wait` can specify
   the status
2. the later one. former one will install several resources at least under the situation that
   current resource's version mismatches the desired one
3. idk
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

### 0.5a Read the admission webhooks you just installed — 2026-10-10

**Quiz answers:**

1. to prevent malformed actions? but is https safe enough?
2. idk
3. KServe has one, Kueue issues its own. I ran
   `kubectl get -n kserve certificate serving-cert -o yaml` but didn't find failure modes
4. idk

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

**Quiz review:** Answer 3's first half is right; the rest needs the mechanism. "idk" recorded
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

### 0.6 Add development workflow after it hurts — YYYY-MM-DD

**Goal:**

**What I changed:**

**How I proved it:**

**What surprised me:**

**Quiz answers:**

**Quiz review:**

### 1.1 Dataset lifecycle — YYYY-MM-DD

**Goal:**

**What I changed:**

**How I proved it:**

**What surprised me:**

**Quiz answers:**

**Quiz review:**

### 1.2 TrainingJob on Kueue — YYYY-MM-DD

**Goal:**

**What I changed:**

**How I proved it:**

**What surprised me:**

**Quiz answers:**

**Quiz review:**

### 1.3 ModelVersion and the funnel — YYYY-MM-DD

**Goal:**

**What I changed:**

**How I proved it:**

**What surprised me:**

**Quiz answers:**

**Quiz review:**

### 1.4 Evaluation and gates — YYYY-MM-DD

**Goal:**

**What I changed:**

**How I proved it:**

**What surprised me:**

**Quiz answers:**

**Quiz review:**

### 1.5 Release on KServe — YYYY-MM-DD

**Goal:**

**What I changed:**

**How I proved it:**

**What surprised me:**

**Quiz answers:**

**Quiz review:**

### 2.1 The promotion gate as admission control — YYYY-MM-DD

**Goal:**

**What I changed:**

**How I proved it:**

**What surprised me:**

**Quiz answers:**

**Quiz review:**

### 2.2 What CEL can do instead — YYYY-MM-DD

**Goal:**

**What I changed:**

**How I proved it:**

**What surprised me:**

**Quiz answers:**

**Quiz review:**

### 2.3 Quota and priority as configuration — YYYY-MM-DD

**Goal:**

**What I changed:**

**How I proved it:**

**What surprised me:**

**Quiz answers:**

**Quiz review:**

### 3.1 Conditions, events, and metrics — YYYY-MM-DD

**Goal:**

**What I changed:**

**How I proved it:**

**What surprised me:**

**Quiz answers:**

**Quiz review:**

### 3.2 Alerts — YYYY-MM-DD

**Goal:**

**What I changed:**

**How I proved it:**

**What surprised me:**

**Quiz answers:**

**Quiz review:**

### 3.3 Run an incident — YYYY-MM-DD

**Goal:**

**What I changed:**

**How I proved it:**

**What surprised me:**

**Quiz answers:**

**Quiz review:**

### 3.4 Write the runbook — YYYY-MM-DD

**Goal:**

**What I changed:**

**How I proved it:**

**What surprised me:**

**Quiz answers:**

**Quiz review:**

### 4.1 envtest for every reconciler — YYYY-MM-DD

**Goal:**

**What I changed:**

**How I proved it:**

**What surprised me:**

**Quiz answers:**

**Quiz review:**

### 4.2 Idempotency and crash safety — YYYY-MM-DD

**Goal:**

**What I changed:**

**How I proved it:**

**What surprised me:**

**Quiz answers:**

**Quiz review:**

### 4.3 Contract tests for the release path — YYYY-MM-DD

**Goal:**

**What I changed:**

**How I proved it:**

**What surprised me:**

**Quiz answers:**

**Quiz review:**

### 4.4 End-to-end — YYYY-MM-DD

**Goal:**

**What I changed:**

**How I proved it:**

**What surprised me:**

**Quiz answers:**

**Quiz review:**

### 5.1 Make the GPU request real on kind — YYYY-MM-DD

**Goal:**

**What I changed:**

**How I proved it:**

**What surprised me:**

**Quiz answers:**

**Quiz review:**

### 5.2 Swap the training runtime — YYYY-MM-DD

**Goal:**

**What I changed:**

**How I proved it:**

**What surprised me:**

**Quiz answers:**

**Quiz review:**

### 5.3 Identity and ownership — YYYY-MM-DD

**Goal:**

**What I changed:**

**How I proved it:**

**What surprised me:**

**Quiz answers:**

**Quiz review:**

### 5.4 Run it on a cloud cluster — YYYY-MM-DD

**Goal:**

**What I changed:**

**How I proved it:**

**What surprised me:**

**Quiz answers:**

**Quiz review:**
