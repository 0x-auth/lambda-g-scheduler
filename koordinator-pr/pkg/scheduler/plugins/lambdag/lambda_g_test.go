package lambdag

import (
	"math"
	"testing"
)

func TestCosineSimilarity(t *testing.T) {
	// Identical vectors = 1.0
	a := [MaxDimensions]float64{0.5, 0.5, 0.5, 0.5, 0.5, 0.5}
	if sim := cosineSimilarity(a, a); math.Abs(sim-1.0) > 0.001 {
		t.Errorf("identical vectors should give 1.0, got %f", sim)
	}

	// Orthogonal vectors = 0.0
	b := [MaxDimensions]float64{1, 0, 0, 0, 0, 0}
	c := [MaxDimensions]float64{0, 1, 0, 0, 0, 0}
	if sim := cosineSimilarity(b, c); math.Abs(sim) > 0.001 {
		t.Errorf("orthogonal vectors should give 0.0, got %f", sim)
	}
}

func TestEntropyLeakDetection(t *testing.T) {
	// Node A: 90% CPU used (10% free), 10% RAM used (90% free) = entropy leak
	nodeA := [MaxDimensions]float64{0.10, 0.90, 0.50, 0.50, 0.50, 0.50}
	// Node B: balanced 50/50
	nodeB := [MaxDimensions]float64{0.50, 0.50, 0.50, 0.50, 0.50, 0.50}
	// CPU-heavy pod
	pod := [MaxDimensions]float64{0.08, 0.05, 0.0, 0.0, 0.10, 0.10}

	scoreA := lambdaGScore(nodeA, pod)
	scoreB := lambdaGScore(nodeB, pod)

	if scoreB <= scoreA {
		t.Errorf("balanced node B (%f) should score higher than imbalanced node A (%f)",
			scoreB, scoreA)
	}
}

func TestSymmetricExhaustionSteering(t *testing.T) {
	// RAM-heavy pod
	pod := [MaxDimensions]float64{0.05, 0.40, 0.0, 0.0, 0.10, 0.10}
	// Node X: has free RAM (good match for RAM-heavy pod)
	nodeX := [MaxDimensions]float64{0.80, 0.50, 0.50, 0.50, 0.70, 0.70}
	// Node Y: RAM is nearly full (bad match)
	nodeY := [MaxDimensions]float64{0.50, 0.10, 0.50, 0.50, 0.70, 0.70}

	scoreX := lambdaGScore(nodeX, pod)
	scoreY := lambdaGScore(nodeY, pod)

	if scoreX <= scoreY {
		t.Errorf("node X (%f) with free RAM should score higher than node Y (%f) for RAM-heavy pod",
			scoreX, scoreY)
	}
}

func TestInfeasibleNodeScoresZero(t *testing.T) {
	// Node nearly full on CPU
	node := [MaxDimensions]float64{0.01, 0.90, 0.50, 0.50, 0.90, 0.90}
	// CPU-heavy pod can't fit
	pod := [MaxDimensions]float64{0.50, 0.10, 0.0, 0.0, 0.10, 0.10}

	score := lambdaGScore(node, pod)
	if score != 0 {
		t.Errorf("infeasible node should score 0, got %f", score)
	}
}

func TestCapacityGate(t *testing.T) {
	// Node with very low CPU free (below gate threshold)
	node := [MaxDimensions]float64{0.05, 0.80, 0.50, 0.50, 0.50, 0.50}
	pod := [MaxDimensions]float64{0.02, 0.05, 0.0, 0.0, 0.05, 0.05}

	score := lambdaGScore(node, pod)
	if score > 10 {
		t.Errorf("node below capacity gate should score very low, got %f", score)
	}
}

func TestPhiWeighting(t *testing.T) {
	// Verify PHI constant
	if math.Abs(PHI-1.618033988749895) > 1e-10 {
		t.Errorf("PHI should be 1.618033988749895, got %f", PHI)
	}
	// Verify PHI self-referencing property: PHI - 1 = 1/PHI
	if math.Abs((PHI-1)-(1/PHI)) > 1e-10 {
		t.Errorf("PHI should satisfy PHI - 1 = 1/PHI")
	}
}

func BenchmarkLambdaGScore(b *testing.B) {
	node := [MaxDimensions]float64{0.60, 0.40, 0.50, 0.50, 0.50, 0.50}
	pod := [MaxDimensions]float64{0.15, 0.20, 0.0, 0.0, 0.05, 0.05}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		lambdaGScore(node, pod)
	}
}
