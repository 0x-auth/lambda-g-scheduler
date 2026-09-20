/*
Package lambdag implements the Lambda-G V3 Hybrid Score plugin
for Koordinator scheduler.

Lambda-G V3 scores nodes using a weighted combination of:
  1. Variance score — how balanced the node will be after placement
  2. Alignment score — cosine similarity between pod request and node free capacity
  3. Headroom score — average free capacity across dimensions
  4. Pressure penalty — hard penalty near exhaustion (>85% or >92%)
  5. Strand penalty — penalizes dimension pairs with extreme imbalance

  score = 0.6*variance + 0.2*alignment + 0.1*headroom - pressure - strand

This addresses the resource imbalance problem described in:
https://github.com/koordinator-sh/koordinator/issues/2332
https://github.com/koordinator-sh/koordinator/issues/2837

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
	Name = "LambdaGHybridScore"

	// PHI is the golden ratio, used for parameter free weighting.
	PHI = 1.618033988749895

	// MaxDimensions is the number of resource dimensions tracked.
	// [CPU, Memory, GPUCore, GPUMemory, IOPS, Network]
	MaxDimensions = 6

	// V3 scoring weights
	wVar  = 0.6
	wAlign = 0.2
	wHead  = 0.1

	// Koordinator GPU resource names
	ResourceGPUCore     = "koordinator.sh/gpu-core"
	ResourceGPUMemory   = "koordinator.sh/gpu-memory"
	ResourceGPUMemRatio = "koordinator.sh/gpu-memory-ratio"

	// CapacityGateThreshold is the minimum free fraction below which
	// a node receives a heavy penalty. Prevents overloading.
	CapacityGateThreshold = 0.10
)

// Plugin implements framework.ScorePlugin for V3 hybrid scoring.
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
// Returns [cpu_free_fraction, mem_free_fraction, gpu_core_free, gpu_mem_free, 0.5, 0.5]
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
// Returns [cpu_req_fraction, mem_req_fraction, gpu_core_req, gpu_mem_req, 0.05, 0.05]
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

	// GPU request dimensions (normalized against node allocatable)
	gpuCoreReq := getExtendedResourceReq(pod, nodeInfo, ResourceGPUCore)
	gpuMemReq := getExtendedResourceReq(pod, nodeInfo, ResourceGPUMemory)

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
// for an extended resource, normalized against the node's allocatable capacity.
// Returns 0.05 (minimal) if not requested.
func getExtendedResourceReq(pod *corev1.Pod, nodeInfo *framework.NodeInfo, resourceName string) float64 {
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

	// Normalize against actual node allocatable capacity
	node := nodeInfo.Node()
	if node != nil {
		if nodeTotal := node.Status.Allocatable[resName]; !nodeTotal.IsZero() {
			return math.Min(1.0, float64(total)/float64(nodeTotal.Value()))
		}
	}
	// Fallback: node has no allocatable for this resource
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

// lambdaGScore computes the V3 hybrid score for placing a pod on a node.
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

	// Compute after-placement used fraction
	var afterUsed [MaxDimensions]float64
	for i := 0; i < MaxDimensions; i++ {
		afterUsed[i] = 1.0 - (nodeVec[i] - podVec[i])
	}

	// 1. Variance score (post-placement balance)
	mean := 0.0
	for i := 0; i < MaxDimensions; i++ {
		mean += afterUsed[i]
	}
	mean /= float64(MaxDimensions)
	variance := 0.0
	for i := 0; i < MaxDimensions; i++ {
		d := afterUsed[i] - mean
		variance += d * d
	}
	variance /= float64(MaxDimensions)
	varianceScore := math.Max(0, (1.0-variance*4)*100)

	// 2. Alignment score (cosine similarity)
	alignment := cosineSimilarity(nodeVec, podVec)
	alignmentScore := alignment * 100

	// 3. Headroom score
	headroom := 0.0
	for i := 0; i < MaxDimensions; i++ {
		headroom += nodeVec[i]
	}
	headroom /= float64(MaxDimensions)
	headroomScore := headroom * 100

	// 4. Pressure penalty (near exhaustion)
	pressure := 0.0
	for i := 0; i < MaxDimensions; i++ {
		v := afterUsed[i]
		if v > 0.92 {
			pressure += (v - 0.92) * 500
		} else if v > 0.85 {
			pressure += (v - 0.85) * 50
		}
	}

	// 5. Strand penalty (imbalanced dimension pairs)
	strandPenalty := 0.0
	for i := 0; i < MaxDimensions; i++ {
		for j := i + 1; j < MaxDimensions; j++ {
			if (afterUsed[i] > 0.80 && afterUsed[j] < 0.20) ||
				(afterUsed[j] > 0.80 && afterUsed[i] < 0.20) {
				strandPenalty += 15
			}
		}
	}

	raw := wVar*varianceScore + wAlign*alignmentScore + wHead*headroomScore - pressure - strandPenalty
	return math.Max(0, math.Min(100, raw))
}

// Score implements framework.ScorePlugin.
// It scores each node using the V3 hybrid formula combining variance,
// alignment, headroom, pressure, and strand penalties.
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
