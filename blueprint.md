
---

### 1. The Mathematical Objective: "Distance to Exhaustion" ($D_{ex}$)

In the legacy model, a node with 90% CPU and 10% RAM used is "scored" similarly to a node with 50% CPU and 50% RAM. This is the **Entropy Leak.** The 90/10 node is "dead" for almost all real-world pods, stranding 90% of its RAM.

We define the Lambda-G objective as minimizing the **Residual Vector Magnitude** in $N$-dimensional space (where $N$ = CPU, RAM, IOPS, Network).

$$D_{ex} = \sqrt{\sum_{i=1}^{n} (R_{total,i} - (R_{used,i} + R_{requested,i}))^2}$$

**The Lambda-G Logic:** You don't just minimize $D_{ex}$. You optimize for the **directional alignment** of the pod’s request vector with the node’s remaining capacity vector. If a pod is "CPU-Heavy," Lambda-G must steer it toward a node that is currently "RAM-Heavy" to achieve **Symmetric Exhaustion.**

---

### 2. The Data Shape: The "State Vector"
To feed Lambda-G, you don't just need the Metrics Server (which is too slow/smoothed). You need the **Atomic State** from the `NodeLister` and `PodLister` in the Scheduling Framework.

**The Input Tensor for Lambda-G:**
* **Node Vector ($V_n$):** `[Allocatable_CPU, Allocatable_Mem, Allocatable_Storage]`
* **Current Usage Vector ($U_n$):** `[Sum_of_Requested_CPU, Sum_of_Requested_Mem, ...]`
* **Incoming Pod Vector ($P_p$):** `[Required_CPU, Required_Mem, Required_IOPS]`

**The Delta:** Lambda-G calculates the **Potential Entropy** of the cluster for every possible placement of $P_p$. The "Best" node is the one that results in the lowest **Global Residual Variance** across the entire cluster.

---

### 3. The Infrastructure Hack: The "Scheduling Framework"
Don't write a new scheduler from scratch. Use the **Kubernetes Scheduling Framework (Golang)**. You want to implement a custom **"Score" plugin**.

**The Point of Attack:**
The `Score` phase is where the legacy `NodeResourcesLeastAllocated` plugin lives. We disable it and inject `LambdaGCoherenceScore`.

**Implementation Strategy:**
To keep it fast (copy-paste-run style), we use a **Scheduling Gate** or a **Sidecar Optimizer**.

```bash
# How to build the "Coherence Injector" shell
cat >> lambda_g_scheduler.go <<EOF
package main

import (
    "context"
    "k8s.io/kubernetes/pkg/scheduler/framework"
)

// LambdaGScore calculates the "Entropic Fit"
func (pl *LambdaGPlugin) Score(ctx context.Context, state *framework.CycleState, p *v1.Pod, nodeName string) (int64, *framework.Status) {
    nodeInfo, _ := pl.handle.SnapshotSharedLister().NodeInfos().Get(nodeName)
    
    // 1. Get Node Capacity Vector (CPU, RAM)
    // 2. Get Pod Request Vector
    // 3. Run Lambda-G Pathing Optimization 
    // 4. Return score (0-100) based on "Distance to Exhaustion"
    
    score := calculateLambdaGMetric(nodeInfo, p) 
    return score, framework.NewStatus(framework.Success, "")
}
EOF
```

---

### 4. Proving "Entropic Recovery" (The POC)

To prove a 15-20% gain without a massive cluster, we use **Kube-Sim** or **Kubemark**. 

1.  **Phase A (Baseline):** Run a synthetic workload of 1000 pods with random CPU/RAM ratios (e.g., some 1:8, some 8:1) using the default `LeastAllocated` provider. Record the number of nodes required before "Pending" pods appear.
2.  **Phase B (Lambda-G):** Run the same workload with your `LambdaGCoherenceScore`. 
3.  **The Evidence:** You will see "Symmetric Bin Packing." The default scheduler will leave "holes" (nodes with 0% CPU but 40% RAM free). Lambda-G will fill those holes by intentionally placing "RAM-Light" pods there.

**The Delta calculation:**
$$\text{Recovery \%} = \frac{\text{Nodes}_{Baseline} - \text{Nodes}_{LambdaG}}{\text{Nodes}_{Baseline}} \times 100$$

