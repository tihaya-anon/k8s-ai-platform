# Cluster dashboards

Three ways to look at the cluster without `kubectl get -o yaml`, each answering a different
question. They are listed in the order you are likely to reach for them.

| Tool         | Where it runs  | Answers                                    | Installed by           |
| ------------ | -------------- | ------------------------------------------ | ---------------------- |
| **k9s**      | your terminal  | what is in the cluster, right now          | the devcontainer image |
| **Headlamp** | in the cluster | the same, in a browser, with relationships | `set-up-cluster.sh`    |
| **Grafana**  | in the cluster | how a number moved over the last hours     | opt-in, see below      |

The split that matters: k9s and Headlamp read the API server, so they show **objects** — the current
spec and status of what exists. Grafana reads Prometheus, so it shows **time series** — how a
counter or a gauge changed. A question like "why did this Pod restart" belongs to the first pair; a
question like "is the reconciler retrying more than it did yesterday" belongs to the second.

## k9s

A terminal UI over the same kubeconfig `kubectl` uses. It installs nothing into the cluster and has
no identity of its own: every request it makes is yours, under your RBAC.

```console
k9s
```

`:` opens the resource prompt and accepts anything the API server serves, custom resources included,
so `:datasets` lists this repository's own kind the same way `:pods` lists Pods. `ctrl-a` lists
every name the prompt will accept, which is `kubectl api-resources` in a pane.

It is baked into `.devcontainer/Dockerfile` at a pin, alongside `kubectl`, `kind` and `helm`. Moving
the version is a `K9S_VERSION` bump and a rebuild — see [agent-container.md](agent-container.md).

### A light terminal

k9s never looks at the terminal's background — no OSC query, no `COLORFGBG`, and `termenv` is only
an indirect dependency through lipgloss — so it opens with its dark stock skin wherever it runs. The
switch is a flag:

```console
k9s --invert
```

That flips the stock skin's colours rather than swapping in a different palette, so the
relationships between them survive.

`invert` is a config key as well as a flag, so `~/.config/k9s/config.yaml` can make it stick:

```yaml
k9s:
  ui:
    invert: true
```

Nothing in this repository writes that file, on purpose — which theme a terminal uses is a property
of the machine, not of the project, and the two sides of a shared dotfile need not agree. Two things
to know before reaching for it: inside the container that path is in the container's own filesystem,
so a Rebuild discards it and the flag is the durable answer there; and k9s writes `config.yaml` only
when it is absent, so one you create is yours to keep — per-context state goes to
`clusters/<cluster>/<context>/config.yaml` beside it either way.

The third route is `ui.skin`, naming a YAML under `skins/` in the same directory. k9s does not ship
those inside the binary, so it means vendoring a skin from its repository — worth it only for a
palette the inversion cannot produce.

### Keys

Taken from the pinned v0.51.0. `?` prints the live list, which is the one to trust after a bump.

| Key            | Does                                                      |
| -------------- | --------------------------------------------------------- |
| `d` / `y`      | describe / YAML of the selected row                       |
| `l` / `p`      | logs / the previous container's logs                      |
| `enter`        | drill in — a Deployment's Pods, a Pod's containers        |
| `shift-j`      | jump to the owner, following one `ownerReference` hop     |
| `shift-f`      | start a port-forward; `f` lists the ones already running  |
| `e` / `ctrl-d` | edit / delete                                             |
| `s`            | shell into a container, or scale a Deployment             |
| `/`            | filter the current view; `0` widens it to every namespace |
| `esc` / `q`    | back                                                      |

`shift-j` is the one worth learning first. **Every child object carries an `ownerReference`** is a
convention this platform holds itself to, and that key checks it without reading any YAML.

### The same question, in each tool

| Asking                 | kubectl                                                         | k9s                        |
| ---------------------- | --------------------------------------------------------------- | -------------------------- |
| what exists            | `kubectl get <resource>`                                        | `:<resource>`              |
| …everywhere            | `kubectl get <resource> -A`                                     | `:<resource>` then `0`     |
| …in one namespace      | `kubectl get <resource> -n <ns>`                                | `:<resource> <ns>`         |
| why is it unhappy      | `kubectl describe <resource> <name>`                            | `:<resource>`, select, `d` |
| the whole object       | `kubectl get <resource> <name> -o yaml`                         | `:<resource>`, select, `y` |
| what did it print      | `kubectl logs -f <pod>`                                         | `:pods`, select, `l`       |
| …before it crashed     | `kubectl logs -p <pod>`                                         | `:pods`, select, `p`       |
| get me a shell         | `kubectl exec -it <pod> -- sh`                                  | `:pods`, select, `s`       |
| reach it from the host | `kubectl port-forward <pod> 8080:8080`                          | `:pods`, select, `shift-f` |
| what just happened     | `kubectl get events --sort-by=...`                              | `:events`                  |
| what kinds are served  | `kubectl api-resources`                                         | `ctrl-a`                   |
| who owns this object   | `kubectl get <r> <n> -o jsonpath='{.metadata.ownerReferences}'` | select, `shift-j`          |

### What it will not do

k9s reads and edits live objects. It has no answer for the commands that ask the API server
something other than "what do you have", and those keep their `kubectl` form throughout the
curriculum:

- **Applying a manifest.** `e` edits an object that already exists; `kubectl apply -f` creates one
  from a file. Nothing in k9s replaces it.
- **Extracting a field.** There is no `-o jsonpath` or `-o custom-columns`; `y` gives you the whole
  object and you read it.
- **The wire.** `-v=8`, `--raw` and `kubectl explain` exist to show the HTTP request, the raw
  response and the schema. k9s is the thing that hides all three.
- **Asking about yourself.** `kubectl auth whoami` and `auth can-i` have no equivalent.
- **Blocking in a script.** `kubectl wait` is what `hack/set-up-cluster.sh` uses; a TUI cannot be a
  step in a script.

## Headlamp

A web console that runs in the cluster, installed by `hack/set-up-cluster.sh` into the `headlamp`
namespace. It is here rather than the Kubernetes Dashboard for one reason: it renders custom
resources from their CRD schemas, so the `Dataset` and `TrainingJob` kinds this repository defines
get real views — including the owner-reference tree that links a parent object to the children its
controller created — without writing a plugin.

```console
hack/dashboard.sh
```

That script does two things. It mints a bearer token with `kubectl create token`, and it forwards a
local port to Headlamp's Service.

Both are worth understanding rather than copying:

- **The token.** Headlamp has no accounts. It takes the token pasted at its login prompt and calls
  the API server _as that identity_, so what the console can see and change is decided by RBAC, not
  by the console. The token is minted through the TokenRequest API — since Kubernetes 1.24 a
  ServiceAccount has no permanent token Secret, and a requested token is time-limited and leaves
  nothing behind when it expires.
- **The port-forward.** A kind node only has the host ports that were mapped when the cluster was
  created, so a NodePort would mean recreating the cluster; and a console on a fixed host port stays
  reachable for as long as the cluster runs. The forward lives and dies with the script.

The chart's default `clusterRoleBinding` binds Headlamp's ServiceAccount to `cluster-admin`. That is
accepted on this cluster and should not be copied to a shared one: it is a local single-node
training cluster, and a curated read-only role would hide exactly the RBAC errors a platform under
development should be surfacing.

## Grafana, opt-in

`kube-prometheus-stack` — Prometheus, Grafana, Alertmanager, node-exporter and kube-state-metrics —
is pinned in `hack/set-up-cluster.sh` but not installed by default. It costs roughly a gigabyte of
memory on a single kind node, and it only starts paying once there is a controller worth watching
over time.

```console
WITH_OBSERVABILITY=1 hack/set-up-cluster.sh
hack/dashboard.sh grafana
```

The admin password is generated by the chart into a Secret; `hack/dashboard.sh grafana` reads it
back and prints it rather than this repository pinning one.

Three things on this cluster are worth a dashboard once it is running:

- **This repository's manager.** `controller-runtime` serves `controller_runtime_reconcile_total`,
  `controller_runtime_reconcile_errors_total`, `controller_runtime_reconcile_time_seconds` and
  `workqueue_depth` on the metrics endpoint for free. Scraping them needs a `ServiceMonitor` — a CR
  the Prometheus operator watches, which is the same compose-the-ecosystem move the platform makes
  everywhere else — and Kubebuilder already generates one under `config/prometheus/`.
- **Kueue.** `kueue_pending_workloads` and `kueue_cluster_queue_resource_usage` are the honest
  answer to "is the job queued or is it stuck".
- **KServe.** Request rate and latency per `InferenceService`, which is what a promotion gate will
  eventually have to read.

Removing the stack again is `helm uninstall kube-prometheus-stack -n observability`;
`hack/set-up-cluster.sh` converges what is asked for and never removes what an earlier run
installed.
