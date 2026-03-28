# AWS Marketplace Listing — Lambda-G

## Product Title
Lambda-G: Symmetric Exhaustion Scheduler for Kubernetes

## Short Description
Drop-in K8s scheduler plugin that eliminates stranded resources through φ-weighted vector alignment. Saves 10-20% on compute costs.

## Full Description

### The Problem
Kubernetes default scheduler uses "Least Allocated" scoring — it spreads pods evenly across nodes. This sounds fair but creates **stranded resources**: nodes with 90% CPU used but 60% RAM free. You pay for that RAM every month. Nobody can use it.

At scale (50+ nodes), stranded resources waste 10-20% of your compute budget. That's $5K-50K/month for a mid-size cluster.

### The Solution
Lambda-G replaces the default Score phase with **vector alignment scoring**:

- Each node is a vector: `[cpu_free, ram_free, iops_free, network_free]`
- Each pod is a vector: `[cpu_req, ram_req, iops_req, network_req]`
- Score = directional alignment + symmetric exhaustion bonus - entropy leak penalty
- CPU-heavy pod → steers toward RAM-heavy node → **symmetric exhaustion**

### Results (Simulation Benchmark)
| Scenario | Default K8s | Lambda-G | Improvement |
|----------|------------|----------|-------------|
| Mixed Workload (20 nodes) | 87.2 balance | 97.0 balance | +11% |
| Scale Test (50 nodes) | 85.9 balance | 96.7 balance | +13% |
| CPU-Heavy Skew | 98.2 balance | 99.1 balance | +1% |
| RAM-Heavy Skew | 96.2 balance | 98.2 balance | +2% |
| Dense Packing | 88.0 balance | 96.0 balance | +9% |

**Zero stranded nodes** in 4/5 scenarios (vs 1-10 with default scheduler).

### How It Works
1. `helm install lambda-g` — deploys as a sidecar controller
2. Watches for pods with `schedulerName: lambda-g` annotation
3. Scores all nodes using Rust-powered φ-weighted vector math
4. Binds pod to the optimal node
5. If Lambda-G is down, K8s falls back to default (FailurePolicy: Ignore)

### Safety
- **Zero-risk deployment**: FailurePolicy: Ignore means your cluster keeps running if Lambda-G stops
- **Minimum permissions**: Only needs pod/node read + pod/binding create
- **No data collection**: All computation is local to your cluster

### Architecture
- **Rust scoring engine**: Sub-microsecond per-node scoring
- **Python controller**: kopf-based K8s operator
- **Helm chart**: One-command install with configurable values
- **Docker image**: Multi-stage build, <100MB

## Pricing
- **Free Tier**: Up to 10 nodes, audit reports
- **Pro**: $29/month per cluster, unlimited nodes
- **Enterprise**: Custom pricing, SLA, support

## Categories
- Containers
- Kubernetes
- Cost Optimization
- Infrastructure

## Support
- GitHub Issues: https://github.com/0x-auth/lambda-g-scheduler
- Email: bitsabhi@gmail.com

## Author
Abhishek Srivastava
Independent Researcher & Engineer
ORCID: 0009-0006-7495-5039
