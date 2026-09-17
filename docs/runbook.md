# Runbook

Operating the platform: how to deploy it, how to see what it is doing, how to break it safely, and
what to do when it breaks by itself.

## Deploy

```bash
make up        # create the kind cluster, build and load the image, apply the manifests
make deploy    # rebuild, reload, and roll out after a code change
make down      # delete the cluster
```

Ports: control-plane API on `localhost:8080`, inference gateway on `localhost:8081`, both via
NodePorts mapped by the kind config.

The API documents itself at <http://localhost:8080/docs> (spec at `/openapi.yaml`). Use it to fire
requests by hand during a drill — including the ones that are supposed to fail, such as canarying a
version that has not passed its gates.

### Host prerequisite: inotify limits

kind nodes run systemd, which needs inotify instances. If node creation fails with

```text
Failed to create control group inotify object: Too many open files
ERROR: failed to create cluster: could not find a log line that matches "Reached target .*Multi-User System.*"
```

raise the limits on the host (they are consumed by every running container):

```bash
sudo sysctl -w fs.inotify.max_user_instances=512
sudo sysctl -w fs.inotify.max_user_watches=1048576
```

Make it permanent in `/etc/sysctl.d/99-inotify.conf`. On WSL2, use `/etc/wsl.conf` or re-apply after
a restart.

## See what is happening

```bash
./bin/platform ctl status              # datasets, jobs, models, routes, servers, recent events
./bin/platform ctl events -limit 50    # the audit trail, oldest to newest
./bin/platform ctl events -name ft-support-1   # one object's history

kubectl -n ai-platform get jobs,deployments,services -l app.kubernetes.io/managed-by=ai-platform
kubectl -n ai-platform logs -f deployment/control-plane
kubectl -n ai-platform logs job/train-ft-support-1

curl -s localhost:8080/metrics   # control-plane counters and job phase gauges
curl -s localhost:8081/metrics   # per-version request, error, and latency counters
```

Everything the platform read or wrote is on disk under `.local/artifacts/`. The files are written by
containers running as root, so read them freely but delete them with `make reset`:

```text
.local/artifacts/
  state/state.json                       control-plane state, the whole database
  datasets/<name>-<version>.jsonl        registered dataset rows
  models/<model>/<version>/model.json    the model card an evaluation and a server read
  models/<model>/<version>/checkpoints/  what a resumed attempt starts from
  evals/<eval>/result.json               the report the promotion gates applied
```

## Release actions

```bash
./bin/platform ctl canary   -model support-assistant -version v2 -percent 10
./bin/platform ctl infer    -model support-assistant -input "hi" -n 100   # confirm the split
./bin/platform ctl promote  -model support-assistant
./bin/platform ctl rollback -model support-assistant -reason "error rate above budget"
./bin/platform ctl scale    -model support-assistant -replicas 3
```

Rollback takes effect as soon as the gateway refreshes its route table, within about two seconds. No
application changes, no redeploy.

## Failure drills

Run these on purpose; they are the situations the platform exists to handle.

| Drill                          | How                                                             | What should happen                                                             |
| ------------------------------ | --------------------------------------------------------------- | ------------------------------------------------------------------------------ |
| Training crash                 | `kubectl -n ai-platform delete pod -l platform.ai/job=<job>`    | The Job retries; the attempt resumes from the last checkpoint, not from step 0 |
| Unrecoverable training failure | Add `FAIL_AT_STEP` to the Job's container env                   | The job reaches `Failed` with the container's reason and exit code             |
| Failed evaluation gate         | `eval run -min-accuracy 0.99`                                   | The model goes to `PromotionBlocked`; a canary attempt returns 409             |
| Bad canary                     | `canary` a version, then `rollback`                             | Traffic returns to stable; the version is blocked with the rollback reason     |
| Model server loss              | `kubectl -n ai-platform delete deployment ms-<model>-<version>` | The controller recreates it within one reconcile interval                      |
| Route to nothing               | Delete a model server and watch the gateway                     | Requests fail with `errorFrom: model-server`, not a generic 500                |

## Troubleshooting

**Training job stuck in `Pending`.** The controller could not create the Kubernetes Job. Check
control-plane logs for an RBAC or image error. `Queued` instead means the Job exists but no pod is
running yet: `kubectl -n ai-platform describe job train-<name>`.

**Job fails with `ErrImageNeverPull` or `ImagePullBackOff`.** The image is not in the kind node. Run
`make load`.

**Model version stays `Registered`.** No evaluation has been run for it, so it cannot be promoted.
That is the gate working.

**Canary returns 409.** The version is not `PromotionReady`. Look at its `reason`: a failed gate or
a previous rollback.

**Gateway returns 404 with `errorFrom: gateway`.** No route exists for that logical model. Start a
canary to create one.

**Gateway serves the old version after a change.** The route table refreshes every two seconds. If
it persists, check that the gateway can reach `control-plane.ai-platform.svc:8080`.

**Everything looks stale after a restart.** The control plane reads its state from
`.local/artifacts/state/state.json`. If that file is gone, the platform has forgotten every object
while the Kubernetes workloads keep running. Delete the orphans with `make clean-workloads`.

**Two control-plane replicas.** Do not. The state file has one writer; the Deployment uses the
`Recreate` strategy for this reason.

## Reset

```bash
make clean-workloads   # delete platform-created Jobs, Deployments, Services; keep the platform
make reset             # wipe state and artifacts, then restart the control plane
make down              # delete the cluster entirely
```
