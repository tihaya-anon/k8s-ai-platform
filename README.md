# k8s-ai-platform

A small but complete AI model operations platform on Kubernetes, built as a training environment.
The product requirements live in [docs/draft.md](docs/draft.md); this repository implements the
first complete product loop from that document:

```text
dataset version -> training job -> model artifact -> evaluation -> promotion gates
                -> canary route -> stable traffic (or rollback)
```

You are the maintainer of this platform. The exercises in
[docs/training-tasks.md](docs/training-tasks.md) are the job: add features, add tests, handle
incidents, and run deployments against a real cluster.

## What is running

| Component         | Command                  | What it does                                                                                                                |
| ----------------- | ------------------------ | --------------------------------------------------------------------------------------------------------------------------- |
| Control plane     | `platform control-plane` | REST API for every platform object, plus the reconcile loop that turns them into Kubernetes Jobs, Deployments, and Services |
| Gateway           | `platform gateway`       | Online inference front door: one logical model name, stable/canary traffic split, per-version metrics                       |
| Model server      | `platform serve`         | Serves one model version, created by the controller, never by hand                                                          |
| Training worker   | `platform train`         | Runs a training attempt, writes checkpoints, resumes after failure                                                          |
| Evaluation worker | `platform eval`          | Scores a model version against a golden dataset and writes the report the gates read                                        |
| CLI               | `platform ctl ...`       | Thin client over the control-plane API                                                                                      |

Everything is one static Go binary and the image has no base layer, so the whole lab builds and runs
offline. The only third-party dependency is Gin, which serves the control-plane API; the rest of the
platform — including the Kubernetes client — is standard library. See [docs/mvp.md](docs/mvp.md) for
the design and its deliberate limits.

## Quickstart

Requirements: Go 1.25+, Docker, kind, kubectl.

```bash
make up      # create the kind cluster, build and load the image, deploy the platform
make demo    # run the full product loop: dataset -> train -> eval -> canary -> promote
make status  # the one page an operator opens first
```

The control-plane API is on `localhost:8080` and the inference gateway on `localhost:8081`.

**Try the API from a browser: <http://localhost:8080/docs>.** It is a self-hosted Swagger UI served
by the control plane itself, with the OpenAPI spec at `/openapi.yaml`. Every endpoint has "Try it
out", so the whole release path — register a dataset, submit a job, evaluate, canary, promote, roll
back — can be driven without the CLI. The assets are vendored into the binary, so the page works
with no internet connection. Datasets, checkpoints, model artifacts, evaluation reports, and the
control-plane state file are all under `.local/artifacts/`, so you can read exactly what the
platform read.

Call the platform like an application developer would:

```bash
curl -s localhost:8081/v1/infer \
  -H 'content-type: application/json' \
  -d '{"model":"support-assistant","input":"how do I reset my device"}' | jq
```

The response carries the request ID, the model version that answered, and which lane it came from.

## Day-to-day commands

```bash
./bin/platform ctl status                                   # everything, one page
./bin/platform ctl train list                               # jobs, phases, failure reasons
./bin/platform ctl model list                               # versions and their gate results
./bin/platform ctl canary -model support-assistant -version v2 -percent 20
./bin/platform ctl infer -model support-assistant -input hi -n 50   # see the traffic split
./bin/platform ctl promote  -model support-assistant
./bin/platform ctl rollback -model support-assistant -reason "latency regression"
./bin/platform ctl events -limit 40                         # the incident timeline
```

Run `make test` for the unit tests and `make help` for every target.

## Documentation

- [docs/draft.md](docs/draft.md) — the product requirements: user stories, roles, and the behaviour
  the platform owes each of them. This is the source of truth.
- [docs/mvp.md](docs/mvp.md) — what is built, how it works, and what is intentionally missing.
- [docs/runbook.md](docs/runbook.md) — operating the platform: deploy, inspect, break, recover.
- [docs/agent-container.md](docs/agent-container.md) — a bounded Docker workspace for AI coding
  agents.
- [docs/training-tasks.md](docs/training-tasks.md) — your exercises, in order, with acceptance
  criteria.
- [docs/progress.md](docs/progress.md) — your own log of what you finished and what you learned.
