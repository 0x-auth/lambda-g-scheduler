/*
Package lambdag implements the Lambda-G Symmetric Exhaustion Score plugin
for Koordinator scheduler.

Lambda-G scores nodes by measuring how much MORE balanced a node becomes
after placing a pod, using cosine alignment between the pod's resource
request vector and the node's free capacity vector.

This addresses the resource imbalance problem described in:
https://github.com/koordinator-sh/koordinator/issues/2332
https://github.com/koordinator-sh/koordinator/issues/2837

The scoring function uses φ-weighted (golden ratio) combination of:
  1. Cosine alignment between pod request and node capacity vectors
  2. Symmetric exhaustion bonus (entropy reduction after placement)
  3. Entropy leak penalty (penalizes stranding resources)

Reference: github.com/0x-auth/lambda-g-auditor
*/
package lambdag

import (
	"context"
	"fmt"
	"math"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/kubernetes/pkg/scheduler/framework"
)

const (
	// Name is the name of the plugin used in the plugin registry and configurations.
	Name = "LambdaGSymmetricExhaustion"

	// PHI is the golden ratio, used as the primary weighting constant.
	// φ is the fixed point of self-reference: φ - 1 = 1/φ.
	// This provides mathematically optimal decay across scoring layers.
	PHI = 1.618033988749895

	// MaxDimensions is the number of resource dimensions tracked.
	// [CPU, Memory, GPUCore, GPUMemory, IOPS, Network]
	MaxDimensions = 6

	// Koordinator GPU resource names
	ResourceGPUCore       = "koordinator.sh/gpu-core"
	ResourceGPUMemory     = "koordinator.sh/gpu-memory"
	ResourceGPUMemRatio   = "koordinator.sh/gpu-memory-ratio"

	// CapacityGateThreshold is the minimum free fraction below which
	// a node receives a heavy penalty. Prevents overloading.
	CapacityGateThreshold = 0.10
)

// Plugin implements framework.ScorePlugin for symmetric exhaustion scoring.
type Plugin struct {
	handle framework.Handle
}

var _ framework.ScorePlugin = &Plugin{}

// Name returns name of the plugin.
func (pl *Plugin) Name() string {
	return Name
}

// New initializes a new LambdaG plugin instance.
func New(_ runtime.Object, h framework.Handle) (framework.Plugin, error) {
	return &Plugin{handle: h}, nil
}

// nodeVector extracts the normalized free resource vector from a node.
// Returns [cpu_free_fraction, mem_free_fraction, 0.5, 0.5]
// where fractions are in range [0.0, 1.0].
func nodeVector(nodeInfo *framework.NodeInfo) [MaxDimensions]float64 {
	node := nodeInfo.Node()
	if node == nil {
		return [MaxDimensions]float64{}
	}

	allocatable := node.Status.Allocatable
	requested := nodeInfo.Requested

	cpuTotal := float64(allocatable.Cpu().MilliValue())
	memTotal := float64(allocatable.Memory().Value())

	cpuUsed := float64(requested.MilliCPU)
	memUsed := float64(requested.Memory)

	cpuFree := 0.0
	memFree := 0.0
	if cpuTotal > 0 {
		cpuFree = math.Max(0, (cpuTotal-cpuUsed)/cpuTotal)
	}
	if memTotal > 0 {
		memFree = math.Max(0, (memTotal-memUsed)/memTotal)
	}

	// GPU dimensions from Koordinator extended resources
	gpuCoreFree := getExtendedResourceFree(nodeInfo, ResourceGPUCore)
	gpuMemFree := getExtendedResourceFree(nodeInfo, ResourceGPUMemory)

	// Dimensions 5 and 6: IOPS and Network (default neutral when unavailable)
	return [MaxDimensions]float64{cpuFree, memFree, gpuCoreFree, gpuMemFree, 0.5, 0.5}
}

// podVector extracts the normalized resource request from a pod.
// Returns [cpu_req_fraction, mem_req_fraction, 0.05, 0.05]
// where fractions are relative to node capacity.
func podVector(pod *corev1.Pod, nodeInfo *framework.NodeInfo) [MaxDimensions]float64 {
	node := nodeInfo.Node()
	if node == nil {
		return [MaxDimensions]float64{}
	}

	allocatable := node.Status.Allocatable
	cpuTotal := float64(allocatable.Cpu().MilliValue())
	memTotal := float64(allocatable.Memory().Value())

	var cpuReq, memReq int64
	for _, c := range pod.Spec.Containers {
		cpuReq += c.Resources.Requests.Cpu().MilliValue()
		memReq += c.Resources.Requests.Memory().Value()
	}

	cpuNorm := 0.0
	memNorm := 0.0
	if cpuTotal > 0 {
		cpuNorm = math.Min(1.0, float64(cpuReq)/cpuTotal)
	}
	if memTotal > 0 {
		memNorm = math.Min(1.0, float64(memReq)/memTotal)
	}

	// GPU request dimensions
	gpuCoreReq := getExtendedResourceReq(pod, ResourceGPUCore)
	gpuMemReq := getExtendedResourceReq(pod, ResourceGPUMemory)

	return [MaxDimensions]float64{cpuNorm, memNorm, gpuCoreReq, gpuMemReq, 0.05, 0.05}
}

// getExtendedResourceFree returns the free fraction of an extended resource on a node.
// Returns 0.5 (neutral) if the resource is not present on the node.
func getExtendedResourceFree(nodeInfo *framework.NodeInfo, resourceName string) float64 {
	node := nodeInfo.Node()
	if node == nil {
		return 0.5
	}

	resName := corev1.ResourceName(resourceName)
	allocatable := node.Status.Allocatable
	total := allocatable[resName]
	if total.IsZero() {
		return 0.5 // Resource not present — neutral score
	}

	// Sum requested by all pods on this node
	var used int64
	for _, podInfo := range nodeInfo.Pods {
		for _, c := range podInfo.Pod.Spec.Containers {
			if req, ok := c.Resources.Requests[resName]; ok {
				used += req.Value()
			}
		}
	}

	totalVal := float64(total.Value())
	if totalVal <= 0 {
		return 0.5
	}
	return math.Max(0, (totalVal-float64(used))/totalVal)
}

// getExtendedResourceReq returns the normalized resource request of a pod
// for an extended resource. Returns 0.05 (minimal) if not requested.
func getExtendedResourceReq(pod *corev1.Pod, resourceName string) float64 {
	resName := corev1.ResourceName(resourceName)
	var total int64
	for _, c := range pod.Spec.Containers {
		if req, ok := c.Resources.Requests[resName]; ok {
			total += req.Value()
		}
	}
	if total <= 0 {
		return 0.05 // Not requested — minimal impact
	}
	// Normalize to 0-1 range (assume 100 = full GPU)
	return math.Min(1.0, float64(total)/100.0)
}

// cosineSimilarity computes the cosine similarity between two vectors.
// Returns value in [-1, 1] where 1 = perfect alignment.
func cosineSimilarity(a, b [MaxDimensions]float64) float64 {
	dot, magA, magB := 0.0, 0.0, 0.0
	for i := 0; i < MaxDimensions; i++ {
		dot += a[i] * b[i]
		magA += a[i] * a[i]
		magB += b[i] * b[i]
	}
	magA = math.Sqrt(magA)
	magB = math.Sqrt(magB)
	if magA < 1e-10 || magB < 1e-10 {
		return 0
	}
	return math.Max(-1, math.Min(1, dot/(magA*magB)))
}

// resourceEntropy computes the Shannon entropy of a resource distribution.
// Lower entropy = more balanced resource usage.
func resourceEntropy(v [MaxDimensions]float64) float64 {
	sum := 0.0
	for _, x := range v {
		sum += x
	}
	if sum < 1e-10 {
		return 0
	}
	h := 0.0
	for _, x := range v {
		p := x / sum
		if p > 1e-10 {
			h -= p * math.Log(p)
		}
	}
	return h
}

// symmetricExhaustionScore measures how much more balanced the node
// becomes after placing the pod. Positive = node becomes more balanced.
func symmetricExhaustionScore(node, pod [MaxDimensions]float64) float64 {
	var after [MaxDimensions]float64
	for i := 0; i < MaxDimensions; i++ {
		after[i] = math.Max(0, node[i]-pod[i])
	}

	entropyBefore := resourceEntropy(node)
	entropyAfter := resourceEntropy(after)
	recovery := entropyBefore - entropyAfter

	magBefore, magAfter := 0.0, 0.0
	for i := 0; i < MaxDimensions; i++ {
		magBefore += node[i] * node[i]
		magAfter += after[i] * after[i]
	}
	magBefore = math.Sqrt(magBefore)
	magAfter = math.Sqrt(magAfter)

	utilization := 0.0
	if magBefore > 1e-10 {
		utilization = (magBefore - magAfter) / magBefore
	}

	return PHI*recovery + utilization
}

// entropyLeakPenalty penalizes placements that strand resources.
// A resource is "stranded" if after placement it has >70% free capacity
// but the pod used <10% of that dimension (it didn't need it).
func entropyLeakPenalty(node, pod [MaxDimensions]float64) float64 {
	var after [MaxDimensions]float64
	for i := 0; i < MaxDimensions; i++ {
		after[i] = math.Max(0, node[i]-pod[i])
	}

	stranded := 0
	for i := 0; i < MaxDimensions; i++ {
		if after[i] > 0.70 && pod[i] < 0.10 {
			stranded++
		}
	}
	return float64(stranded) * 0.15
}

// lambdaGScore computes the final score for placing a pod on a node.
// Returns a value in [0, 100] where higher = better placement.
func lambdaGScore(nodeVec, podVec [MaxDimensions]float64) float64 {
	// Feasibility check — can the node fit the pod?
	for i := 0; i < MaxDimensions; i++ {
		if nodeVec[i] < podVec[i] {
			return 0
		}
	}

	// Capacity gate — penalize nearly-full nodes
	if nodeVec[0] < CapacityGateThreshold || nodeVec[1] < CapacityGateThreshold {
		return 5 // Very low but not zero (still feasible)
	}

	// 1. Cosine alignment: does the pod's shape match the node's free space?
	alignment := cosineSimilarity(podVec, nodeVec)

	// 2. Symmetric exhaustion: does placing this pod make the node more balanced?
	exhaustionBonus := symmetricExhaustionScore(nodeVec, podVec)

	// 3. Entropy leak: does this placement strand resources?
	entropyPenalty := entropyLeakPenalty(nodeVec, podVec)

	// 4. Headroom bonus: prefer nodes with more breathing room
	headroom := (nodeVec[0] + nodeVec[1]) / 2

	// Combine with φ-weighted formula
	raw := PHI*alignment + exhaustionBonus - entropyPenalty + headroom*0.3
	return math.Max(0, math.Min(100, raw*30+50))
}

// Score implements framework.ScorePlugin.
// It scores each node based on how well the pod's resource request
// aligns with the node's free capacity, aiming for symmetric exhaustion.
func (pl *Plugin) Score(
	ctx context.Context,
	state *framework.CycleState,
	pod *corev1.Pod,
	nodeName string,
) (int64, *framework.Status) {
	nodeInfo, err := pl.handle.SnapshotSharedLister().NodeInfos().Get(nodeName)
	if err != nil {
		return 0, framework.NewStatus(
			framework.Error,
			fmt.Sprintf("getting node %q from snapshot: %v", nodeName, err),
		)
	}

	nVec := nodeVector(nodeInfo)
	pVec := podVector(pod, nodeInfo)
	score := lambdaGScore(nVec, pVec)

	return int64(score), nil
}

// ScoreExtensions returns nil — no normalization needed as scores are already 0-100.
func (pl *Plugin) ScoreExtensions() framework.ScoreExtensions {
	return nil
}
