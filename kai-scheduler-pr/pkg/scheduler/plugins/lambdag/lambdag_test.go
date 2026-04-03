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

package lambdag

import (
	"math"
	"testing"
)

func TestCosineSimilarity(t *testing.T) {
	a := []float64{0.5, 0.5, 0.5, 0.5}
	if sim := cosineSimilarity(a, a); math.Abs(sim-1.0) > 0.001 {
		t.Errorf("identical vectors should give 1.0, got %f", sim)
	}

	b := []float64{1, 0, 0, 0}
	c := []float64{0, 1, 0, 0}
	if sim := cosineSimilarity(b, c); math.Abs(sim) > 0.001 {
		t.Errorf("orthogonal vectors should give 0.0, got %f", sim)
	}
}

func TestBalancedBeatsImbalanced(t *testing.T) {
	pod := [6]float64{0.08, 0.05, 0.0, 0.0, 0.10, 0.10}
	// Imbalanced: CPU nearly full, RAM mostly free
	nodeA := [6]float64{0.10, 0.90, 0.50, 0.50, 0.50, 0.50}
	// Balanced: 50/50
	nodeB := [6]float64{0.50, 0.50, 0.50, 0.50, 0.50, 0.50}

	scoreA := lambdaGScore(nodeA, pod, 6)
	scoreB := lambdaGScore(nodeB, pod, 6)

	if scoreB <= scoreA {
		t.Errorf("balanced node (%f) should score higher than imbalanced (%f)", scoreB, scoreA)
	}
}

func TestRAMHeavyPodSteeredToFreeRAM(t *testing.T) {
	pod := [6]float64{0.05, 0.40, 0.0, 0.0, 0.10, 0.10}
	// Node X: has free RAM
	nodeX := [6]float64{0.80, 0.50, 0.50, 0.50, 0.70, 0.70}
	// Node Y: RAM nearly full
	nodeY := [6]float64{0.50, 0.10, 0.50, 0.50, 0.70, 0.70}

	scoreX := lambdaGScore(nodeX, pod, 6)
	scoreY := lambdaGScore(nodeY, pod, 6)

	if scoreX <= scoreY {
		t.Errorf("node with free RAM (%f) should beat node without (%f)", scoreX, scoreY)
	}
}

func TestInfeasibleReturnsZero(t *testing.T) {
	node := [6]float64{0.01, 0.90, 0.50, 0.50, 0.90, 0.90}
	pod := [6]float64{0.50, 0.10, 0.0, 0.0, 0.10, 0.10}

	score := lambdaGScore(node, pod, 6)
	if score != 0 {
		t.Errorf("infeasible node should score 0, got %f", score)
	}
}

func TestCapacityGate(t *testing.T) {
	node := [6]float64{0.05, 0.80, 0.50, 0.50, 0.50, 0.50}
	pod := [6]float64{0.02, 0.05, 0.0, 0.0, 0.05, 0.05}

	score := lambdaGScore(node, pod, 6)
	if score > 1 {
		t.Errorf("node below capacity gate should score very low, got %f", score)
	}
}

func TestGPUImbalanceDetected(t *testing.T) {
	pod := [6]float64{0.05, 0.05, 0.08, 0.05, 0.05, 0.05}
	// GPU VRAM full, compute idle
	bad := [6]float64{0.50, 0.50, 0.10, 0.90, 0.5, 0.5}
	// GPU balanced
	good := [6]float64{0.50, 0.50, 0.50, 0.50, 0.5, 0.5}

	scoreBad := lambdaGScore(bad, pod, 6)
	scoreGood := lambdaGScore(good, pod, 6)

	if scoreGood <= scoreBad {
		t.Errorf("GPU balanced (%f) should beat GPU imbalanced (%f)", scoreGood, scoreBad)
	}
}

func TestPhiConstant(t *testing.T) {
	if math.Abs(PHI-1.618033988749895) > 1e-10 {
		t.Errorf("PHI should be 1.618033988749895, got %f", PHI)
	}
	if math.Abs((PHI-1)-(1/PHI)) > 1e-10 {
		t.Errorf("PHI should satisfy PHI - 1 = 1/PHI")
	}
}

func TestPluginName(t *testing.T) {
	pl := New()
	if pl.Name() != PluginName {
		t.Errorf("expected %s, got %s", PluginName, pl.Name())
	}
}

func BenchmarkLambdaGScore(b *testing.B) {
	node := [6]float64{0.60, 0.40, 0.50, 0.50, 0.50, 0.50}
	pod := [6]float64{0.15, 0.20, 0.0, 0.0, 0.05, 0.05}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		lambdaGScore(node, pod, 6)
	}
}
