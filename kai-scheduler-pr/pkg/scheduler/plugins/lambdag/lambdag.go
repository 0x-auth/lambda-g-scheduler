/*
Copyright 2026 The KAI Scheduler Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Package lambdag implements the Lambda-G Symmetric Exhaustion node scoring
// plugin for KAI Scheduler. It scores nodes by measuring how much more
// balanced a node becomes after placing a task, using cosine alignment between
// the task's resource request vector and the node's free capacity vector.
//
// This addresses cross-dimensional GPU fragmentation: VRAM full but compute
// idle, or compute maxed but VRAM unused.
package lambdag

import (
	"math"

	"k8s.io/klog/v2"

	"github.com/NVIDIA/KAI-Scheduler/pkg/scheduler/api/node_info"
	"github.com/NVIDIA/KAI-Scheduler/pkg/scheduler/api/pod_info"
	"github.com/NVIDIA/KAI-Scheduler/pkg/scheduler/framework"
	"github.com/NVIDIA/KAI-Scheduler/pkg/scheduler/plugins/scores"
)

const (
	PluginName = "lambdag"

	// PHI is the golden ratio — parameter-free weighting constant.
	PHI = 1.618033988749895

	// CapacityGateThreshold: if any primary resource is below this
	// fraction free, heavily penalize the node.
	CapacityGateThreshold = 0.10
)

type lambdaGPlugin struct{}

func New() framework.Plugin {
	return &lambdaGPlugin{}
}

func (pp *lambdaGPlugin) Name() string {
	return PluginName
}

func (pp *lambdaGPlugin) OnSessionOpen(ssn *framework.Session) {
	ssn.AddNodeOrderFn(pp.Name(), pp.nodeOrderFn)
}

func (pp *lambdaGPlugin) OnSessionClose() {}

// nodeOrderFn scores a node for a given task based on cross-dimensional
// resource balance. Higher score = better placement.
func (pp *lambdaGPlugin) nodeOrderFn(task *pod_info.PodInfo, node *node_info.NodeInfo) (float64, error) {
	if task == nil || node == nil {
		return 0, nil
	}

	nodeVec, podVec, dims := buildVectors(task, node)
	if dims == 0 {
		return 0, nil
	}

	score := lambdaGScore(nodeVec, podVec, dims)

	klog.V(7).Infof("lambdag: task %s/%s -> node %s: score=%.2f (dims=%d)",
		task.Namespace, task.Name, node.Name, score, dims)

	return score, nil
}

// buildVectors constructs normalized free-capacity and request vectors
// from node and task resource info. Returns the vectors and the number
// of active dimensions.
func buildVectors(task *pod_info.PodInfo, node *node_info.NodeInfo) (nodeVec, podVec [6]float64, dims int) {
	allocatable := node.AllocatableVector
	idle := node.IdleVector

	if node.VectorMap == nil {
		return nodeVec, podVec, 0
	}

	// Walk the resource vector map to build normalized vectors.
	// We use up to 6 dimensions: the first resources found in the map.
	dims = 0
	for resourceName, idx := range node.VectorMap.Mapping() {
		if dims >= 6 {
			break
		}

		total := allocatable.Value(idx)
		free := idle.Value(idx)

		if total <= 0 {
			continue
		}

		freeFrac := math.Max(0, float64(free)/float64(total))
		nodeVec[dims] = freeFrac

		// Get task request for this resource
		reqVal := task.ResReqVector.Value(idx)
		podVec[dims] = math.Min(1.0, float64(reqVal)/float64(total))

		_ = resourceName // available for debug logging
		dims++
	}

	return nodeVec, podVec, dims
}

// lambdaGScore computes the placement score using cosine alignment,
// symmetric exhaustion, and entropy leak penalty.
func lambdaGScore(nodeVec, podVec [6]float64, dims int) float64 {
	// Feasibility: can the node fit the task?
	for i := 0; i < dims; i++ {
		if nodeVec[i] < podVec[i] {
			return 0
		}
	}

	// Capacity gate on first two dimensions (typically CPU and Memory)
	if dims >= 2 && (nodeVec[0] < CapacityGateThreshold || nodeVec[1] < CapacityGateThreshold) {
		return 0.5
	}

	alignment := cosineSimilarity(nodeVec[:dims], podVec[:dims])
	exhaustion := symmetricExhaustionScore(nodeVec[:dims], podVec[:dims])
	penalty := entropyLeakPenalty(nodeVec[:dims], podVec[:dims])

	headroom := 0.0
	if dims >= 2 {
		headroom = (nodeVec[0] + nodeVec[1]) / 2
	}

	raw := PHI*alignment + exhaustion - penalty + headroom*0.3
	// Scale to fit within KAI's MaxHighDensity score range
	return math.Max(0, math.Min(float64(scores.MaxHighDensity), raw*float64(scores.MaxHighDensity)/4.0))
}

func cosineSimilarity(a, b []float64) float64 {
	dot, magA, magB := 0.0, 0.0, 0.0
	for i := range a {
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

func resourceEntropy(v []float64) float64 {
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

func symmetricExhaustionScore(nodeVec, podVec []float64) float64 {
	after := make([]float64, len(nodeVec))
	for i := range nodeVec {
		after[i] = math.Max(0, nodeVec[i]-podVec[i])
	}

	recovery := resourceEntropy(nodeVec) - resourceEntropy(after)

	magBefore, magAfter := 0.0, 0.0
	for i := range nodeVec {
		magBefore += nodeVec[i] * nodeVec[i]
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

func entropyLeakPenalty(nodeVec, podVec []float64) float64 {
	after := make([]float64, len(nodeVec))
	for i := range nodeVec {
		after[i] = math.Max(0, nodeVec[i]-podVec[i])
	}

	stranded := 0
	for i := range after {
		if after[i] > 0.70 && podVec[i] < 0.10 {
			stranded++
		}
	}
	return float64(stranded) * 0.15
}
