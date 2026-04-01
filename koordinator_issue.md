# Issue Title:
[Proposal] Lambda-G Scoring Function for Resource Imbalance Detection and Balanced Rescheduling (#2332)

# Issue Body:

## Context

Following up on #2332 (Resource Imbalance Rescheduling) — I've built a scoring algorithm called **Lambda-G** that directly addresses the node resource imbalance detection and balanced pod placement problem.

## The Problem (same as #2332)

Default Kubernetes scoring treats these two nodes equivalently:
- Node A: 90% CPU, 10% RAM → score ~50
- Node B: 50% CPU, 50% RAM → score ~50

Node A has stranded RAM — nobody can use it because CPU is full. Lambda-G detects this and scores node placement to **reduce imbalance**, not just spread load.

## Lambda-G Scoring Algorithm

The core scoring function measures how much MORE balanced a node becomes after placing a pod:

```go
func lambdaGScore(nodeVec, podVec [4]float64) float64 {
    // 1. Feasibility check
    for i := 0; i < 4; i++ {
        if nodeVec[i] < podVec[i] { return 0 }
    }

    // 2. Cosine alignment (directional fit)
    alignment := cosineSimilarity(podVec, nodeVec)

    // 3. Symmetric exhaustion bonus
    exhaustionBonus := symmetricExhaustionScore(nodeVec, podVec)

    // 4. Entropy leak penalty
    entropyPenalty := entropyLeakPenalty(nodeVec, podVec)

    // 5. Combine with φ-weighted formula
    PHI := 1.618033988749895
    raw := PHI*alignment + exhaustionBonus - entropyPenalty
    return clamp(raw*30+50, 0, 100)
}
```

Where:
- `nodeVec = [cpu_free, ram_free, iops_free, net_free]` (normalized 0-1)
- `podVec = [cpu_req, ram_req, iops_req, net_req]` (normalized against node capacity)
- `cosineSimilarity` measures directional alignment between pod request and node capacity
- `symmetricExhaustionScore` rewards placements that make the node MORE balanced
- `entropyLeakPenalty` penalizes placements that would strand resources

## Benchmark Results

Tested across 5 scenarios (20-50 simulated nodes, 200-500 pods):

| Scenario | Default K8s | Lambda-G | Improvement |
|----------|------------|----------|-------------|
| Mixed Workload (20n × 200p) | 87.2 | 97.0 | +11% |
| Scale Test (50n × 500p) | 85.9 | 96.7 | +13% |
| CPU-Heavy Skew | 98.2 | 99.1 | +1% |
| RAM-Heavy Skew | 96.2 | 98.2 | +2% |
| Dense Packing | 88.0 | 96.0 | +9% |

Zero stranded nodes in 4/5 scenarios.

## Proposed Integration with Koordinator

### 1. Detection (koord-descheduler)

I've open-sourced an **auditor tool** that scans clusters for resource imbalance:
→ https://github.com/0x-auth/lambda-g-auditor

This could serve as the detection module for the rescheduling plugin proposed in #2332. It identifies nodes where one resource dimension is >90% while another is <60%.

### 2. Scoring (koord-scheduler)

The Lambda-G scoring function can be integrated as a Score plugin in koord-scheduler. I have a Go implementation ready that implements `framework.ScorePlugin`:

```go
type LambdaGScheduler struct {
    handle framework.Handle
}

func (lg *LambdaGScheduler) Score(ctx context.Context, state *framework.CycleState,
    pod *v1.Pod, nodeName string) (int64, *framework.Status) {

    nodeInfo, _ := lg.handle.SnapshotSharedLister().NodeInfos().Get(nodeName)
    nVec := nodeVector(nodeInfo)
    pVec := podVector(pod, nodeInfo)
    score := lambdaGScore(nVec, pVec)
    return int64(score), nil
}
```

### 3. Compatibility with ReservationFirst

Lambda-G scoring is stateless and per-node — it works naturally with Koordinator's `ReservationFirst` eviction mode. The score function doesn't maintain state between calls, so evicted pods can be rescored against all available nodes.

## What I'm Offering

1. **Lambda-G auditor** (open source, MIT) — detection module
2. **Lambda-G Go Score plugin** — ready to adapt to Koordinator's plugin interface
3. **Benchmark suite** — simulation tool to validate against different workload profiles
4. Willing to contribute PRs and attend community meetings

## Next Steps

Happy to discuss at the next bi-weekly meeting. I can also open a draft PR with the Score plugin adapted to Koordinator's interface if there's interest.

**References:**
- Auditor: https://github.com/0x-auth/lambda-g-auditor
- Benchmark: included in auditor repo (`benchmark.py`)
- Full architecture write-up available on request

cc @songtao98 @JBinin
