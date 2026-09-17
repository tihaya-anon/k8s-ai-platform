# AI Training And Inference Platform Project

## Purpose

Build a realistic cloud native AI platform for training, evaluation, batch inference, and online
inference. The goal is to learn how modern AI systems are operated when models, datasets, GPU
capacity, queues, artifacts, traffic, and failures all interact.

This is an exercise, but it should simulate production as much as possible. Start locally with
simulated GPUs and fake topology. After the platform control flow is working, move the same
architecture to rented cloud GPU nodes and replace the simulation pieces with real GPU discovery,
real scheduling pressure, real metrics, and real inference/training workloads.

The core question is:

> How do you run expensive AI workloads on shared infrastructure while keeping resource placement,
> model quality, release safety, and operational visibility under control?

## Product Scenario

Build an internal AI model operations platform.

The platform supports:

- Dataset registration and versioning.
- Fine-tuning or lightweight training jobs.
- Distributed training experiments.
- Batch inference jobs.
- Embedding and retrieval-index rebuild jobs.
- Model evaluation and regression checks.
- Model artifact storage and promotion.
- Online inference serving with canary rollout.
- GPU-aware scheduling and observability.
- Optional NVLink-aware placement for multi-GPU workloads.

The first complete product loop should be:

1. Register dataset version `docs-v12`.
2. Launch training job `ft-support-v7`.
3. Store checkpoints and final model artifact.
4. Run evaluation against a golden dataset.
5. Promote the model if quality, latency, and cost gates pass.
6. Deploy the model to online inference.
7. Send canary traffic to the new model version.
8. Promote or roll back based on metrics and evaluation results.

This should feel like a small production platform, not a demo script. Every important object should
have owner, status, metrics, logs, and a recovery path.

## System Shape

### Control Plane

The control plane owns metadata, desired state, and promotion decisions.

Core components:

- `experiment-api`: creates training jobs, evaluation jobs, and promotion requests.
- `dataset-registry`: stores dataset versions, schemas, ownership, lineage, and storage locations.
- `model-registry`: stores model versions, artifact locations, metrics, evaluation results, and
  promotion status.
- `promotion-controller`: watches evaluation results and creates deployment changes for approved
  models.
- `gpu-topology-controller`: stores GPU topology information and exposes scheduling hints.

### Training Plane

The training plane runs expensive, often bursty GPU workloads.

Core components:

- `training-launcher`: creates Kubernetes Jobs, PyTorchJobs, Ray Jobs, or workflow templates.
- `training-worker`: runs fine-tuning or training code.
- `checkpoint-manager`: records checkpoint paths, latest successful step, and resume metadata.
- `eval-worker`: evaluates trained models before promotion.

### Serving Plane

The serving plane handles low-latency online inference.

Core components:

- `model-server`: serves one or more model versions.
- `model-gateway`: routes requests by model, tenant, canary weight, fallback policy, or cost class.
- `batch-inference-worker`: runs offline inference over files, tables, document sets, or queues.
- `agent-worker`: optional tool-using worker with strict execution boundaries.

### Data Plane

The data plane stores durable state and artifacts.

Core components:

- PostgreSQL for metadata.
- Object storage for datasets, checkpoints, model artifacts, logs, and evaluation reports.
- Vector database for retrieval indexes, if the platform includes RAG.
- Queue for asynchronous training, batch inference, indexing, and evaluation work.

### Observability Plane

The observability plane explains system behavior.

Core signals:

- API request rate, errors, and latency.
- Model latency, token usage, batch size, and error rate.
- Training duration, throughput, checkpoint age, and failure reason.
- GPU utilization, memory, power, temperature, and interconnect metrics.
- Queue depth, job wait time, retry count, and oldest pending job age.
- Evaluation quality, latency, cost, and regression status.
- Canary comparison between old and new model versions.

## Workload Types

### Online Inference

Online inference is latency-sensitive and user-facing.

Characteristics:

- Needs stable replicas.
- Scales with request traffic.
- Often needs model weights resident in GPU memory.
- Uses canary, blue/green, or shadow traffic.
- Should degrade gracefully when model providers or GPU capacity fail.

Platform needs:

- Gateway routing.
- Model server deployment.
- Autoscaling based on request load, queue depth, or GPU metrics.
- Per-model metrics.
- Rollback on failed metrics or evaluation gates.

Possible serving components:

- vLLM
- NVIDIA Triton Inference Server
- KServe
- Seldon Core
- BentoML on Kubernetes
- Ray Serve

### Batch Inference

Batch inference is throughput-sensitive and can tolerate queueing.

Characteristics:

- Runs on files, tables, document sets, or queues.
- Needs retry, checkpoint, and idempotency.
- Produces versioned outputs.
- Often competes with training for GPU capacity.

Platform needs:

- Workflow orchestration.
- Artifact storage.
- Queue metrics.
- Dataset and output versioning.
- Cost and throughput reports.

Possible components:

- Argo Workflows
- Ray Jobs
- Spark on Kubernetes
- Kueue
- Volcano

### Training Or Fine-Tuning

Training and fine-tuning are GPU-heavy and placement-sensitive.

Characteristics:

- Often need multiple GPUs at once.
- May need all workers to start together.
- Write checkpoints.
- Can be preempted only if checkpointing is solid.
- Placement affects performance when GPU interconnect differs.

Platform needs:

- Job queueing.
- Gang scheduling.
- GPU quota.
- Checkpoint storage.
- Distributed job launcher.
- Topology-aware scheduling.
- GPU telemetry.

Possible components:

- Kubeflow Training Operator
- Ray Train
- PyTorchJob
- Volcano
- Kueue
- NVIDIA GPU Operator
- NVIDIA device plugin
- DCGM Exporter

## NVLink-Aware Scheduling

NVLink is a high-bandwidth GPU interconnect. For distributed training or multi-GPU inference, not
all GPU placements are equal.

Default Kubernetes GPU scheduling often treats `nvidia.com/gpu: 4` as a scalar resource. That
answers "how many GPUs?" but not always "which GPUs are close to each other?"

Example:

- A job asks for 4 GPUs.
- A node has 8 GPUs split into two NVLink-connected groups of 4.
- Another node has 8 GPUs with weaker cross-GPU topology.
- Placing the job inside one NVLink group can be much faster than spreading it across weaker PCIe
  paths.

An NVLink-aware plugin teaches a real AI platform problem:

- Device resources are not always interchangeable.
- Scheduling needs topology, not just capacity.
- Performance can depend on placement even when the resource request is satisfied.

## NVLink-Aware Plugin Path

### Version 1: Topology Label Simulator

Use fake or manually defined node labels to represent GPU topology.

Example node labels:

```yaml
gpu.topology/nvlink-groups: "0,1,2,3|4,5,6,7"
gpu.topology/gpu-count: "8"
gpu.topology/nvlink-generation: "4"
```

A training job requests topology preference through annotations:

```yaml
metadata:
  annotations:
    ai.platform/gpu-count: "4"
    ai.platform/prefer-nvlink: "true"
```

A scheduler extension scores nodes higher when the requested GPU count fits inside one NVLink group.

This version works in a local cluster because it does not need real GPUs.

### Version 2: Real Node Discovery Daemon

Run a privileged DaemonSet on GPU nodes that discovers topology and writes labels or custom
resources.

Discovery sources may include:

- `nvidia-smi topo -m`
- NVIDIA Management Library
- DCGM
- GPU Operator exposed metadata

The daemon publishes topology such as:

```yaml
apiVersion: ai.platform.example/v1alpha1
kind: GPUTopology
metadata:
  name: node-a
spec:
  nodeName: node-a
  devices:
    - id: GPU-0
      numaNode: 0
    - id: GPU-1
      numaNode: 0
  links:
    - from: GPU-0
      to: GPU-1
      type: NVLINK
      score: 100
    - from: GPU-0
      to: GPU-4
      type: PCIE
      score: 20
```

The scheduler extension reads these objects and scores nodes.

This version requires real GPU nodes.

### Version 3: Device-Aware Allocation

Go beyond node scoring and choose exact GPU IDs for a Pod.

This is harder because Kubernetes scheduling and device allocation are separate concerns. The
scheduler can choose a node, but the device plugin normally decides which physical GPUs are assigned
on that node.

Possible approaches:

- Use annotations to pass preferred GPU IDs to a custom device plugin.
- Build a custom device plugin that exposes topology-aware extended resources.
- Integrate with Dynamic Resource Allocation if targeting newer Kubernetes features.

This version is advanced. Build it only after node scoring and real topology discovery are working.

## Local-To-Cloud Path

The project should be easy to transfer from local simulation to rented GPU infrastructure.

### Stage 1: Local Control Flow

Run locally with kind or another disposable Kubernetes cluster.

Use:

- Fake GPU node labels.
- CPU-only toy training jobs.
- Mock model artifacts.
- Small local object storage.
- Minimal metrics.
- No real GPU dependency.

Goal:

- Prove API, job creation, status tracking, artifact metadata, evaluation, promotion, and inference
  rollout flow.

Exit criteria:

- A training job can be created and tracked.
- A model artifact can be registered.
- Evaluation can approve or reject a model.
- A serving deployment can canary a model version.
- The scheduler plugin can explain a fake topology decision.

### Stage 2: Cloud Kubernetes Without Expensive Training

Move the same manifests and controllers to a managed Kubernetes cluster before renting large GPUs.

Use:

- Managed Kubernetes.
- Object storage from the cloud provider.
- Managed or in-cluster PostgreSQL.
- Small CPU nodes plus optional one cheap GPU node.
- GitOps deployment.
- Real ingress or gateway.

Goal:

- Prove the platform survives real cloud networking, storage, IAM, registry, DNS, TLS, and cluster
  operations.

Exit criteria:

- GitOps can rebuild the environment.
- Artifacts persist outside the cluster.
- Secrets are not stored in plaintext Git.
- Inference can be exposed safely.
- Metrics and logs survive Pod restarts.

### Stage 3: Single-Node GPU Reality Check

Rent one GPU node and run real but small workloads.

Use:

- NVIDIA GPU Operator or device plugin.
- DCGM Exporter.
- vLLM, Triton, or another model server.
- Small model such as a compact embedding model or small instruct model.
- One fine-tuning or batch inference job.

Goal:

- Replace fake GPU assumptions with real device scheduling, image size, model load time, GPU memory
  limits, and telemetry.

Exit criteria:

- Pods can request and receive `nvidia.com/gpu`.
- GPU metrics are visible.
- Model load time and GPU memory usage are measured.
- OOM or insufficient GPU memory has a clear failure signal.
- Batch and online workloads do not silently starve each other.

### Stage 4: Multi-GPU And NVLink Validation

Rent a multi-GPU node only after the previous stages are stable.

Use:

- Node discovery daemon.
- Real topology from `nvidia-smi topo -m`, NVML, or DCGM.
- Scheduler scoring based on discovered topology.
- Training benchmark that is sensitive to GPU placement.

Goal:

- Validate whether topology-aware scheduling changes throughput or stability.

Exit criteria:

- Topology is published automatically.
- Scheduler decisions are explainable.
- Training throughput is compared across placements.
- Bad topology metadata can be detected or overridden.

### Stage 5: Multi-Node Distributed Training

Move to multi-node only when single-node behavior is understood.

Use:

- Kueue or Volcano for queueing and gang scheduling.
- PyTorchJob, Ray Train, or another distributed launcher.
- Checkpoint storage outside the cluster.
- Priority classes and quotas.

Goal:

- Learn fairness, gang scheduling, checkpoint recovery, and expensive failure modes.

Exit criteria:

- A distributed job waits until all required resources are available.
- A killed worker fails or recovers in a controlled way.
- Checkpoint resume works.
- Inference workloads keep priority over experimental training.

## Recommended Implementation Milestones

### Milestone 1: Platform Skeleton

Deliver:

- `experiment-api` can create and list training jobs.
- `model-registry` can register model artifacts and versions.
- `dataset-registry` can register dataset versions.
- PostgreSQL stores metadata.
- Object storage stores artifacts.

Acceptance criteria:

- Dataset, job, model, and evaluation records have stable IDs.
- API responses expose status and failure reason.
- Object storage paths are deterministic and versioned.

### Milestone 2: Fake GPU Scheduling

Deliver:

- Nodes labeled with fake GPU and NVLink topology.
- Annotated training jobs request GPU count and topology preference.
- Scheduler extension scores nodes based on topology fit.
- Decision logs explain why a node was selected.

Acceptance criteria:

- A 4-GPU job prefers a node with a 4-GPU NVLink group.
- A 6-GPU job avoids nodes that only have 4-GPU groups when possible.
- A job without NVLink preference schedules by normal capacity.
- Unit tests cover at least three topology shapes.

### Milestone 3: Training Job Lifecycle

Deliver:

- API or CLI creates training jobs.
- Jobs write checkpoints and final artifacts.
- Failed jobs can resume from checkpoint.
- Job status is visible from the platform API.

Acceptance criteria:

- A killed training job resumes from its latest checkpoint.
- A failed job records failure reason and logs.
- Concurrent jobs respect namespace quota.

### Milestone 4: Evaluation And Promotion

Deliver:

- Evaluation workflow runs after training.
- Results are stored with model metadata.
- Promotion requires passing configured gates.
- Serving deployment uses promoted model artifact.

Acceptance criteria:

- A bad model cannot be promoted automatically.
- A promoted model is deployed as a canary.
- A failed canary rolls back to the previous version.

### Milestone 5: Online And Batch Inference

Deliver:

- Model server exposes online inference.
- Model gateway routes traffic by version.
- Batch inference worker processes queued or file-based jobs.
- Metrics compare online and batch GPU usage.

Acceptance criteria:

- Online inference remains available during a batch job.
- Batch jobs are retried or resumed safely.
- Canary routing can send traffic to a new model version.
- Rollback does not delete model artifacts.

### Milestone 6: Real GPU Observability

Deliver:

- GPU metrics dashboard.
- Job queue dashboard.
- Training throughput dashboard.
- Inference GPU headroom alert.

Acceptance criteria:

- GPU memory exhaustion is visible.
- Training job wait time is visible.
- Inference latency can be correlated with GPU saturation.

### Milestone 7: Real Topology Discovery

Deliver:

- Node daemon discovers GPU topology.
- Topology is published as labels or custom resources.
- Scheduler extension consumes discovered topology.
- Fake topology mode remains available for local tests.

Acceptance criteria:

- The platform can run in local simulated mode.
- On GPU nodes, topology data comes from actual node discovery.
- Scheduler behavior can be tested without production GPUs.

## Failure Drills

Run these deliberately:

- Submit more training jobs than available GPUs.
- Submit one high-priority inference workload during training saturation.
- Kill a training worker mid-epoch.
- Delete a checkpoint object.
- Promote a model with worse evaluation score.
- Saturate GPU memory on a model server.
- Break object storage during checkpoint write.
- Force a canary model to return high latency.
- Change topology labels incorrectly and observe bad scheduling.
- Make topology discovery daemon fail on one GPU node.
- Fill the queue with batch inference jobs.
- Make a model image pull extremely slow.

For each drill, answer:

- Did the platform queue, reject, preempt, or degrade?
- Was the user-facing impact visible?
- Did the system preserve model artifacts and metadata consistency?
- Did the scheduler decision make sense?
- Was rollback possible without manual cluster surgery?
- Which signal should have existed earlier?

## Suggested Stack

Start simple and add components only when a milestone needs them.

Local stage:

- Kubernetes: kind
- API: Go, Java, or Python
- Database: PostgreSQL
- Object storage: MinIO
- Queue: Redis, NATS, or RabbitMQ
- Metrics: Prometheus and Grafana
- Delivery: Kustomize and Argo CD
- Workflows: Argo Workflows
- Fake GPU topology: node labels and test fixtures

GPU stage:

- NVIDIA GPU Operator or NVIDIA device plugin
- DCGM Exporter
- vLLM or NVIDIA Triton Inference Server
- Kueue or Volcano
- Kubeflow Training Operator, Ray Train, or PyTorchJob
- Real object storage from the cloud provider

Later hardening:

- OpenTelemetry Collector
- Loki
- Tempo
- Kyverno or OPA Gatekeeper
- External Secrets Operator or SOPS
- Cilium for networking and policy
- Argo Rollouts for metric-aware progressive delivery

## What This Should Teach

By the end, you should be able to explain:

- How training, batch inference, and online inference differ operationally.
- How Kubernetes controllers turn desired state into running AI workloads.
- Why GPU scheduling needs more than scalar resource counts.
- Why NVLink-aware placement can matter for multi-GPU jobs.
- How queues, quotas, priority, and gang scheduling protect shared GPU platforms.
- How checkpoints and artifacts make expensive jobs recoverable.
- How evaluation gates connect model quality to deployment safety.
- How GitOps changes model release and rollback behavior.
- How observability must include quality, cost, GPU, queue, and workflow signals.
- How to transfer a local simulation into real cloud GPU infrastructure without redesigning the
  platform.

The core cloud native AI taste is this: a model is not the product by itself. The product is the
controlled lifecycle around data, training, evaluation, artifacts, serving, scheduling, traffic, and
failure recovery.
