# Lambda-G: V3 Hybrid Scoring Scheduler for Kubernetes

Your K8s cluster is wasting 10-20% of compute budget on stranded resources. Lambda-G fixes this.

## The Problem

Default K8s scheduler scores nodes by "least allocated" — treats 90% CPU / 10% RAM the same as 50% / 50%. This strands resources. You pay for RAM nobody can use.

## The Fix

Lambda-G V3 scores nodes using a **hybrid formula** — combining post-placement variance (balance), cosine alignment (directional match), headroom, and penalty terms for pressure and stranding.

```
score = 0.6 * variance_score + 0.2 * alignment_score + 0.1 * headroom_score
        - pressure_penalty - strand_penalty

variance_score  = max(0, (1.0 - variance(after_used) * 4) * 100)
alignment_score = cosine_similarity(node_free, pod_req) * 100
headroom_score  = mean(node_free) * 100
pressure        = sum over dims: (used > 0.92 -> (v-0.92)*500, >0.85 -> (v-0.85)*50)
strand          = 15 per dim pair where one > 80% used and other < 20% used
```

## Quick Start

### 1. Audit your cluster (free, no install)

```bash
git clone https://github.com/0x-auth/lambda-g-scheduler
cd lambda-g-scheduler
pip install kubernetes colorama
python3 coherence_engine/auditor/auditor.py
```

Shows stranded resources + estimated monthly waste.

### 2. Run benchmark

```bash
python3 benchmark_simulation.py
```

Tests Lambda-G V3 vs LeastAllocated, MostAllocated, BalancedAllocation, and DominantResource across 5 GPU-aware scenarios.

### 3. Deploy

```bash
# Docker
docker pull bitsabhi/lambda-g-controller:latest

# Helm
helm install lambda-g charts/lambda-g
```

### 4. Annotate your pods

```yaml
metadata:
  annotations:
    schedulerName: lambda-g
```

## Benchmark Results

6D scoring across 5 GPU-aware scenarios (30 nodes, mixed CPU/RAM/GPU clusters):

| Scenario | LeastAlloc | BalancedAlloc | Lambda-G V3 | Winner |
|----------|-----------|---------------|-------------|--------|
| Mixed GPU — AI Workload | 65.2 | 78.4 | 82.1 | Lambda-G V3 |
| GPU — Inference Heavy | 61.8 | 76.9 | 80.5 | Lambda-G V3 |
| GPU — Training Heavy | 58.3 | 74.2 | 78.8 | Lambda-G V3 |
| CPU + Few GPUs | 70.1 | 80.6 | 83.2 | Lambda-G V3 |
| Scale (60n x 300p) | 66.7 | 79.1 | 82.9 | Lambda-G V3 |

Lambda-G V3 wins all 5 scenarios. Fewest stranded nodes. Lowest wasted $.

## Architecture

```
K8s API Server -> Lambda-G Controller (Python/kopf) -> Rust Brain (scoring)
                                                        |
                                                  score = 0.6*var + 0.2*align
                                                        + 0.1*headroom
                                                        - pressure - strand
```

- **Rust scoring engine** — sub-microsecond per node, 6D (CPU, RAM, GPU Core, GPU Mem, IOPS, Network)
- **Python controller** — kopf-based, watches for annotated pods
- **Safety valve** — `FailurePolicy: Ignore` -> if Lambda-G dies, K8s default takes over

## Files

```
├── coherence_engine/
│   ├── controller.py          # K8s operator (kopf)
│   ├── brain_bridge.py        # Python -> Rust FFI
│   ├── auditor/auditor.py     # Free cluster scanner
│   └── manifests/test_pod.yaml
├── rust_brain/src/lib.rs      # V3 scoring engine (Rust)
├── koordinator-pr/            # Koordinator scheduler plugin (Go)
├── charts/lambda-g/           # Helm chart
├── Dockerfile                 # Multi-stage (Rust compile + Python)
├── benchmark_simulation.py    # V3 weight search + 5-scenario benchmark
├── benchmark_full.py          # Live K8s benchmark
└── setup_eks.sh               # EKS test cluster setup
```

## How the V3 Scoring Works

```
1. Variance: after_used[i] = 1.0 - (node_free[i] - pod_req[i])
             variance = mean((after_used - mean)^2)
             variance_score = max(0, (1.0 - variance * 4) * 100)

2. Alignment: cosine_similarity(node_free_frac, pod_req_frac) * 100

3. Headroom: mean(node_free_frac) * 100

4. Pressure: if after_used[i] > 0.92: += (v-0.92)*500
             elif > 0.85: += (v-0.85)*50

5. Strand: for pairs (i,j): if used[i]>0.80 && used[j]<0.20: += 15

Final: (0.6*var + 0.2*align + 0.1*headroom - pressure - strand).clamp(0, 100)
```

Combines the best of BalancedAllocation (variance) with directional matching (cosine alignment). Pressure and strand penalties prevent degenerate placements.

## Safety

- **FailurePolicy: Ignore** — K8s continues normally if Lambda-G is down
- **Minimum RBAC** — only needs pod/node read + pod/binding create
- **No data collection** — all computation is local

## Docker Hub

```bash
docker pull bitsabhi/lambda-g-controller:latest
```

## Author

Abhishek Srivastava — [github.com/0x-auth](https://github.com/0x-auth) — ORCID: 0009-0006-7495-5039

V3: variance + alignment > least allocation
