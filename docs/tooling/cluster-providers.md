# Cluster providers: kind and k3s

The cluster scripts run against either of two local clusters. Everything above the cluster -- Helm
releases, readiness gates, the platform's own manifests -- is the same on both; what differs is how
the cluster itself is created, reached, stopped, and given images. That difference lives in one
file, `hack/lib/cluster.sh`, and the scripts call its functions instead of asking which provider
they are on: the same rule as "no `if kind` in Go", applied to the shell.

| Provider | What the cluster is                                   | Typical host                       |
| -------- | ----------------------------------------------------- | ---------------------------------- |
| `kind`   | Docker containers this repository creates and deletes | WSL, macOS, CI                     |
| `k3s`    | the host's own Kubernetes, a systemd service it runs  | a Linux machine with k3s installed |

## Choosing one

`CLUSTER_PROVIDER=kind` or `CLUSTER_PROVIDER=k3s` selects explicitly. When it is unset, a running
`k3s` service selects k3s and anything else selects kind, so a machine that never had k3s behaves
exactly as before. Every script prints the provider it resolved as its first line.

```sh
hack/set-up-cluster.sh                       # whichever the host has
CLUSTER_PROVIDER=kind hack/set-up-cluster.sh # kind even on a k3s host
```

## What differs

| Concern                      | kind                                          | k3s                                                       |
| ---------------------------- | --------------------------------------------- | --------------------------------------------------------- |
| kubeconfig context           | `kind-k8s-ai-platform`                        | `default`, as the k3s installer names it (`K3S_CONTEXT`)  |
| creating the cluster         | `kind create cluster`, pinned node image      | refused: install k3s yourself; a script should not        |
| version pin                  | exact (`v1.36.1`)                             | minor only (`v1.36.x+k3s1`); k3s upgrades itself          |
| chart images                 | pulled on the host and imported into the node | pulled by k3s's containerd through its own proxy settings |
| loading a built image        | `docker save` into the node's containerd      | `docker save` into `sudo k3s ctr`                         |
| `shutdown-cluster.sh stop`   | `docker stop` on the node containers          | `sudo k3s-killall.sh`; it still starts at boot            |
| `shutdown-cluster.sh delete` | `kind delete cluster`                         | refused: that would be uninstalling k3s from the host     |
| e2e cluster                  | a throwaway cluster, deleted after the run    | the same k3s, left running after the run                  |

Two of those rows are worth the reasoning:

- **Why k3s skips preloading.** kind's node is a container that may inherit a proxy pointing at
  `host.docker.internal`, which a Linux kind node cannot resolve, so the host pulls and imports.
  k3s's containerd is on the host and reads its proxy from `/etc/systemd/system/k3s.service.env`, so
  it pulls directly; preloading would only copy every image into Docker's store as well.
- **Why `stop` is `k3s-killall.sh`, not `systemctl stop k3s`.** Stopping the service leaves the
  pods' containers running on purpose, so that upgrading k3s does not restart workloads. Stopping
  the cluster means stopping those too, which is what the killall script does.

## e2e on k3s

`make test-e2e` builds the manager image, loads it with `hack/load-image.sh`, and deploys into
`k8s-ai-platform-system`. On kind that happens in a throwaway cluster. On k3s it happens in the
cluster you develop in, so do not run the e2e suite while a `make deploy` of your own is running
there: both use the same namespace, and the suite's `AfterAll` undeploys it. The image import asks
for sudo, because k3s's containerd socket belongs to root.

## Not covered here

Differences in the platform's own manifests -- storage class (`standard` on kind, `local-path` on
k3s), ingress, GPU node flavors -- belong in kustomize overlays under `config/overlays/`, as
[product/architecture.md](../product/architecture.md) describes. Those overlays are curriculum work,
not tooling.
