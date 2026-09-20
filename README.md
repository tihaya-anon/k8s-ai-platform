# k8s-ai-platform

An internal AI model operations platform on Kubernetes: register a dataset version, train a model,
evaluate it against a golden set, and only then let it take production traffic.

It is also a training environment. The point is not the code that exists but the code that does not:
the platform composes **Kueue** for queueing and quota and **KServe** for serving and traffic
splitting, and contributes the part nobody ships for you — the domain objects, their lineage, and
the gate that refuses to serve an unevaluated model.

> **Status: level 0 done.** The cluster script and the `Dataset` CRD exist; the reconcilers do not
> yet. The first version of this platform implemented everything by hand — its own Kubernetes
> client, its own queue, its own traffic split, no CRDs — and is archived at `3cb8302`. Read it as a
> reference for what the frameworks are doing on your behalf.

## Read in this order

| Document                                           | What it is                                                                 |
| -------------------------------------------------- | -------------------------------------------------------------------------- |
| [docs/requirements.md](docs/requirements.md)       | The requirements. User stories and required behaviour. Source of truth.    |
| [docs/architecture.md](docs/architecture.md)       | The design: what is built here, what is adopted, and where the line falls. |
| [docs/training-tasks.md](docs/training-tasks.md)   | The build plan and the curriculum: what is built, and what you answer for. |
| [docs/progress.md](docs/progress.md)               | The maintainer log.                                                        |
| [docs/agent-container.md](docs/agent-container.md) | The sandboxed development container.                                       |

## The shape of it

```mermaid
flowchart TB
    DS[Dataset] --> TJ[TrainingJob]
    TJ --> MV[ModelVersion]
    MV --> EV[Evaluation]
    EV --> RL[Release]

    KUEUE([Kueue<br/>quota, queue,<br/>preemption])
    KUEUE -. admits .-> TJ
    KUEUE -. admits .-> EV

    GATE{{admission<br/>webhook}}
    MV -- gate status --> GATE
    GATE -- refuses an<br/>ungated version --> RL

    RL -. renders .-> KSERVE([KServe<br/>stable / canary])
```

Kueue and KServe are other people's operators, configured well. Everything else in that diagram —
five custom resources, five controllers, one webhook — is defined here.

## Getting started

Requirements: Go 1.25+, Docker, kind, kubectl, kubebuilder.

Build the cluster and install the adopted operators — pinned versions, safe to re-run:

```bash
./hack/set-up-cluster.sh
```

Then install the CRDs and run the tests:

```bash
make install            # Dataset CRD into the cluster
make fmt vet test       # envtest against a real API server
kubectl apply -f config/samples/
kubectl get datasets
```

Work the levels in [docs/training-tasks.md](docs/training-tasks.md) in order. Each one arrives as
code plus a quiz plus a patch job; the code is the least important of the three.

Development container:

```bash
make -f agent.Makefile agent-shell
```

## Why this way

An AI platform on Kubernetes is mostly other people's controllers. Writing your own queue teaches
you what a queue is; running Kueue teaches you what a platform engineer actually does, which is
decide policy and own the abstractions your users see. This repository does the second, having
already done the first — and the first is still in the history, one `git show` away, for when you
want to know what the framework is hiding.
