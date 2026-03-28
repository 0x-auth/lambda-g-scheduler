/*
lambda_g_plugin.go
==================
Kubernetes Scheduler Plugin — Lambda-G Score Phase
Implements the framework.ScorePlugin interface.

This Go plugin calls the Rust scoring engine via CGO.
Drop this into your scheduler-plugins repo and register it.

Build:
  go build -o lambda-g-scheduler .

Deploy:
  kubectl apply -f manifests/
*/

package lambdagscheduler

import (
	"context"
	"fmt"
	"math"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/kubernetes/pkg/scheduler/framework"
)

const (
	Name    = "LambdaGScheduler"
	PHI     = 1.618033988749895
	MaxDims = 4
)

// LambdaGScheduler implements framework.ScorePlugin
type LambdaGScheduler struct {
	handle framework.Handle
}

var _ framework.ScorePlugin = &LambdaGScheduler{}

// New creates a new LambdaGScheduler plugin instance
func New(_ runtime.Object, h framework.Handle) (framework.Plugin, error) {
	return &LambdaGScheduler{handle: h}, nil
}

func (lg *LambdaGScheduler) Name() string { return Name }

// NodeVector extracts normalized resource vector from a node
// Returns [cpu_free, ram_free, iops_free, net_free] all in range [0,1]
func nodeVector(nodeInfo *framework.NodeInfo) [MaxDims]float64 {
	node := nodeInfo.Node()
	if node == nil {
		return [MaxDims]float64{}
	}

	allocatable := node.Status.Allocatable
	requested   := nodeInfo.Requested

	cpuTotal  := float64(allocatable.Cpu().MilliValue())
	ramTotal  := float64(allocatable.Memory().Value())

	cpuUsed   := float64(requested.MilliCPU)
	ramUsed   := float64(requested.Memory)

	cpuFree  := 0.0
	ramFree  := 0.0

	if cpuTotal > 0 { cpuFree  = math.Max(0, (cpuTotal-cpuUsed)/cpuTotal) }
	if ramTotal > 0 { ramFree  = math.Max(0, (ramTotal-ramUsed)/ramTotal) }

	// IOPS and Network: use extended resources if available, else default 0.5
	iopsFree := extendedResourceFree(nodeInfo, "requests.storage-iops")
	netFree  := extendedResourceFree(nodeInfo, "requests.network-bandwidth")

	return [MaxDims]float64{cpuFree, ramFree, iopsFree, netFree}
}

// PodVector extracts normalized resource request from a pod
func podVector(pod *v1.Pod, nodeInfo *framework.NodeInfo) [MaxDims]float64 {
	node := nodeInfo.Node()
	if node == nil {
		return [MaxDims]float64{}
	}

	allocatable := node.Status.Allocatable
	cpuTotal    := float64(allocatable.Cpu().MilliValue())
	ramTotal    := float64(allocatable.Memory().Value())

	var cpuReq, ramReq int64
	for _, c := range pod.Spec.Containers {
		cpuReq += c.Resources.Requests.Cpu().MilliValue()
		ramReq += c.Resources.Requests.Memory().Value()
	}

	cpuNorm := 0.0
	ramNorm := 0.0
	if cpuTotal > 0 { cpuNorm = math.Min(1.0, float64(cpuReq)/cpuTotal) }
	if ramTotal > 0 { ramNorm = math.Min(1.0, float64(ramReq)/ramTotal) }

	return [MaxDims]float64{cpuNorm, ramNorm, 0.05, 0.05}
}

func extendedResourceFree(nodeInfo *framework.NodeInfo, resourceName string) float64 {
	// Stub — real impl reads extended resources from node annotations
	// Defaults to 0.5 (neutral) when not available
	return 0.5
}

// ── PURE GO SCORING (no CGO dependency for portability) ──────────────────────

func cosineSimilarity(a, b [MaxDims]float64) float64 {
	dot, magA, magB := 0.0, 0.0, 0.0
	for i := 0; i < MaxDims; i++ {
		dot  += a[i] * b[i]
		magA += a[i] * a[i]
		magB += b[i] * b[i]
	}
	magA = math.Sqrt(magA)
	magB = math.Sqrt(magB)
	if magA < 1e-10 || magB < 1e-10 { return 0 }
	return math.Max(-1, math.Min(1, dot/(magA*magB)))
}

func resourceEntropy(v [MaxDims]float64) float64 {
	sum := 0.0
	for _, x := range v { sum += x }
	if sum < 1e-10 { return 0 }
	h := 0.0
	for _, x := range v {
		p := x / sum
		if p > 1e-10 { h -= p * math.Log(p) }
	}
	return h
}

func symmetricExhaustionScore(node, pod [MaxDims]float64) float64 {
	var after [MaxDims]float64
	for i := 0; i < MaxDims; i++ {
		after[i] = math.Max(0, node[i]-pod[i])
	}
	entropyBefore := resourceEntropy(node)
	entropyAfter  := resourceEntropy(after)
	reward        := entropyBefore - entropyAfter

	magBefore, magAfter := 0.0, 0.0
	for i := 0; i < MaxDims; i++ {
		magBefore += node[i] * node[i]
		magAfter  += after[i] * after[i]
	}
	magBefore = math.Sqrt(magBefore)
	magAfter  = math.Sqrt(magAfter)

	utilization := 0.0
	if magBefore > 1e-10 {
		utilization = (magBefore - magAfter) / magBefore
	}
	return PHI*reward + utilization
}

func entropyLeakPenalty(node, pod [MaxDims]float64) float64 {
	var after [MaxDims]float64
	for i := 0; i < MaxDims; i++ {
		after[i] = math.Max(0, node[i]-pod[i])
	}
	stranded := 0
	for i := 0; i < MaxDims; i++ {
		if after[i] > 0.70 && pod[i] < 0.10 { stranded++ }
	}
	return float64(stranded) * 0.15
}

func lambdaGScore(nodeVec, podVec [MaxDims]float64) float64 {
	// Feasibility
	for i := 0; i < MaxDims; i++ {
		if nodeVec[i] < podVec[i] { return 0 }
	}
	alignment       := cosineSimilarity(podVec, nodeVec)
	exhaustionBonus := symmetricExhaustionScore(nodeVec, podVec)
	entropyPenalty  := entropyLeakPenalty(nodeVec, podVec)

	raw := PHI*alignment + exhaustionBonus - entropyPenalty
	return math.Max(0, math.Min(100, raw*30+50))
}

// Score implements framework.ScorePlugin
func (lg *LambdaGScheduler) Score(
	ctx      context.Context,
	state    *framework.CycleState,
	pod      *v1.Pod,
	nodeName string,
) (int64, *framework.Status) {

	nodeInfo, err := lg.handle.SnapshotSharedLister().NodeInfos().Get(nodeName)
	if err != nil {
		return 0, framework.NewStatus(
			framework.Error,
			fmt.Sprintf("getting node %q: %v", nodeName, err),
		)
	}

	nVec := nodeVector(nodeInfo)
	pVec := podVector(pod, nodeInfo)
	score := lambdaGScore(nVec, pVec)

	return int64(score), nil
}

// ScoreExtensions returns nil (no normalization needed, already 0-100)
func (lg *LambdaGScheduler) ScoreExtensions() framework.ScoreExtensions {
	return nil
}
