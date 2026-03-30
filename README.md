# Lambda-G: Symmetric Exhaustion Scheduler for Kubernetes

Your K8s cluster is wasting 10-20% of compute budget on stranded resources. Lambda-G fixes this.

## The Problem

Default K8s scheduler scores nodes by "least allocated" — treats 90% CPU / 10% RAM the same as 50% / 50%. This strands resources. You pay for RAM nobody can use.

## The Fix

Lambda-G scores nodes using **vector alignment** — cosine similarity between pod request and node capacity. CPU-heavy pods go to RAM-heavy nodes. Result: symmetric exhaustion.

```
Score = φ × alignment + exhaustion_bonus - entropy_penalty
φ = 1.618033988749895
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

Tests Lambda-G vs default across 5 scenarios. Lambda-G wins all 5.

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

| Scenario | Default | Lambda-G | Winner |
|----------|---------|----------|--------|
| Mixed 20n × 200p | 87.2 | 97.0 | Lambda-G |
| Scale 50n × 500p | 85.9 | 96.7 | Lambda-G |
| CPU-Heavy Skew | 98.2 | 99.1 | Lambda-G |
| RAM-Heavy Skew | 96.2 | 98.2 | Lambda-G |
| Dense Packing | 88.0 | 96.0 | Lambda-G |

Zero stranded nodes in 4/5 scenarios.

## Architecture

```
K8s API Server → Lambda-G Controller (Python/kopf) → Rust Brain (scoring)
                                                      ↓
                                              Score = φ × cos(pod, node)
                                                      + exhaustion_bonus
                                                      - entropy_penalty
```

- **Rust scoring engine** — sub-microsecond per node
- **Python controller** — kopf-based, watches for annotated pods
- **Safety valve** — `FailurePolicy: Ignore` → if Lambda-G dies, K8s default takes over

## Files

```
├── coherence_engine/
│   ├── controller.py          # K8s operator (kopf)
│   ├── brain_bridge.py        # Python → Rust FFI
│   ├── auditor/auditor.py     # Free cluster scanner
│   └── manifests/test_pod.yaml
├── rust_brain/src/lib.rs      # Scoring engine (17 lines of Rust)
├── charts/lambda-g/           # Helm chart
├── Dockerfile                 # Multi-stage (Rust compile + Python)
├── benchmark_simulation.py    # 5-scenario benchmark
├── benchmark_full.py          # Live K8s benchmark
└── setup_eks.sh               # EKS test cluster setup
```

## How the Scoring Works

```rust
fn calculate_score(cpu_free, ram_free, cpu_req, ram_req) -> f64 {
    let phi = 1.618033988749895;
    let initial_entropy = (cpu_free - ram_free).abs();
    let after_cpu = cpu_free - cpu_req;
    let after_ram = ram_free - ram_req;
    let final_entropy = (after_cpu - after_ram).abs();
    let recovery = initial_entropy - final_entropy;
    let exhaustion = 1.0 - (after_cpu + after_ram);
    (recovery * phi * 100.0) + (exhaustion * 10.0)
}
```

17 lines. Measures how much MORE balanced the node becomes after placing the pod. φ weights entropy recovery as the primary signal.

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

φ = 1.618033988749895 · Symmetric exhaustion > least allocation
