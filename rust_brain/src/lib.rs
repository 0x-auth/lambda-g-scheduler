/// Lambda-G V3 Hybrid Scoring Engine
/// Dimensions: [CPU, RAM, GPU Core, GPU Memory, IOPS, Network]
/// score = 0.6*variance + 0.2*alignment + 0.1*headroom - pressure - strand

const DIMS: usize = 6;
const CAPACITY_GATE: f64 = 0.10;
const W_VAR: f64 = 0.6;
const W_ALIGN: f64 = 0.2;
const W_HEAD: f64 = 0.1;

fn cosine_similarity(a: &[f64], b: &[f64]) -> f64 {
    let dot: f64 = a.iter().zip(b.iter()).map(|(x, y)| x * y).sum();
    let mag_a: f64 = a.iter().map(|x| x * x).sum::<f64>().sqrt();
    let mag_b: f64 = b.iter().map(|x| x * x).sum::<f64>().sqrt();
    if mag_a < 1e-10 || mag_b < 1e-10 { return 0.0; }
    (dot / (mag_a * mag_b)).clamp(-1.0, 1.0)
}

/// 6D scoring: [cpu_free, ram_free, gpu_core_free, gpu_mem_free, iops_free, net_free]
/// Returns 0-100, higher = better placement.
#[no_mangle]
pub extern "C" fn calculate_score_6d(
    cpu_free: f64, ram_free: f64,
    gpu_core_free: f64, gpu_mem_free: f64,
    iops_free: f64, net_free: f64,
    cpu_req: f64, ram_req: f64,
    gpu_core_req: f64, gpu_mem_req: f64,
    iops_req: f64, net_req: f64,
) -> f64 {
    let node_free = [cpu_free, ram_free, gpu_core_free, gpu_mem_free, iops_free, net_free];
    let pod_req = [cpu_req, ram_req, gpu_core_req, gpu_mem_req, iops_req, net_req];

    // Feasibility
    for i in 0..DIMS {
        if node_free[i] < pod_req[i] { return 0.0; }
    }

    // Capacity gate on CPU and RAM
    if node_free[0] < CAPACITY_GATE || node_free[1] < CAPACITY_GATE { return 5.0; }

    // Identify active dimensions (node has real capacity, not the 0.5 sentinel with 0.05 req)
    // All dims are active for scoring — sentinel values (0.5 free / 0.05 req) produce
    // moderate used fractions (~0.55) which naturally contribute without distortion.

    // Compute after-placement used fraction: used = 1.0 - (free - req)
    let mut after_used = [0.0f64; DIMS];
    for i in 0..DIMS {
        after_used[i] = 1.0 - (node_free[i] - pod_req[i]);
    }

    // --- Component 1: Variance score (post-placement balance) ---
    let mean: f64 = after_used.iter().sum::<f64>() / DIMS as f64;
    let variance: f64 = after_used.iter().map(|&v| (v - mean).powi(2)).sum::<f64>() / DIMS as f64;
    let variance_score = (1.0 - variance * 4.0).max(0.0) * 100.0;

    // --- Component 2: Alignment score (cosine similarity) ---
    let alignment = cosine_similarity(&node_free, &pod_req);
    let alignment_score = alignment * 100.0;

    // --- Component 3: Headroom score ---
    let headroom: f64 = node_free.iter().sum::<f64>() / DIMS as f64;
    let headroom_score = headroom * 100.0;

    // --- Component 4: Pressure penalty (near exhaustion) ---
    let mut pressure = 0.0f64;
    for &v in after_used.iter() {
        if v > 0.92 {
            pressure += (v - 0.92) * 500.0;
        } else if v > 0.85 {
            pressure += (v - 0.85) * 50.0;
        }
    }

    // --- Component 5: Strand penalty (imbalanced dim pairs) ---
    let mut strand_penalty = 0.0f64;
    for i in 0..DIMS {
        for j in (i + 1)..DIMS {
            if (after_used[i] > 0.80 && after_used[j] < 0.20)
                || (after_used[j] > 0.80 && after_used[i] < 0.20)
            {
                strand_penalty += 15.0;
            }
        }
    }

    let raw = W_VAR * variance_score
            + W_ALIGN * alignment_score
            + W_HEAD * headroom_score
            - pressure
            - strand_penalty;

    raw.clamp(0.0, 100.0)
}

/// Legacy 2D scoring (backward compatible)
#[no_mangle]
pub extern "C" fn calculate_score(cpu_free: f64, ram_free: f64, cpu_req: f64, ram_req: f64) -> f64 {
    calculate_score_6d(cpu_free, ram_free, 0.5, 0.5, 0.5, 0.5,
                       cpu_req, ram_req, 0.05, 0.05, 0.05, 0.05)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_balanced_beats_imbalanced() {
        // Imbalanced node: 90% CPU used (0.10 free), 10% RAM used (0.90 free)
        let score_a = calculate_score_6d(0.10, 0.90, 0.5, 0.5, 0.5, 0.5,
                                          0.08, 0.05, 0.0, 0.0, 0.05, 0.05);
        // Balanced node: 50/50
        let score_b = calculate_score_6d(0.50, 0.50, 0.5, 0.5, 0.5, 0.5,
                                          0.08, 0.05, 0.0, 0.0, 0.05, 0.05);
        assert!(score_b > score_a, "balanced should beat imbalanced: {} vs {}", score_b, score_a);
    }

    #[test]
    fn test_gpu_imbalance_detected() {
        // GPU VRAM full, compute idle
        let score_bad = calculate_score_6d(0.50, 0.50, 0.10, 0.90, 0.5, 0.5,
                                            0.05, 0.05, 0.08, 0.05, 0.05, 0.05);
        // GPU balanced
        let score_good = calculate_score_6d(0.50, 0.50, 0.50, 0.50, 0.5, 0.5,
                                             0.05, 0.05, 0.08, 0.05, 0.05, 0.05);
        assert!(score_good > score_bad, "GPU balanced should beat GPU imbalanced: {} vs {}", score_good, score_bad);
    }

    #[test]
    fn test_infeasible_returns_zero() {
        let score = calculate_score_6d(0.01, 0.50, 0.5, 0.5, 0.5, 0.5,
                                        0.50, 0.05, 0.0, 0.0, 0.05, 0.05);
        assert_eq!(score, 0.0);
    }

    #[test]
    fn test_legacy_2d_still_works() {
        let score = calculate_score(0.5, 0.5, 0.1, 0.1);
        assert!(score > 0.0, "legacy 2D should return positive score: {}", score);
    }

    #[test]
    fn test_pressure_penalty_reduces_score() {
        // Node near exhaustion on CPU (0.10 free, asking for 0.08 -> used=0.98)
        let score_tight = calculate_score_6d(0.12, 0.50, 0.5, 0.5, 0.5, 0.5,
                                              0.10, 0.05, 0.05, 0.05, 0.05, 0.05);
        // Node with breathing room
        let score_comfy = calculate_score_6d(0.50, 0.50, 0.5, 0.5, 0.5, 0.5,
                                              0.10, 0.05, 0.05, 0.05, 0.05, 0.05);
        assert!(score_comfy > score_tight, "comfortable should beat tight: {} vs {}", score_comfy, score_tight);
    }

    #[test]
    fn test_score_in_range() {
        let score = calculate_score_6d(0.60, 0.60, 0.5, 0.5, 0.5, 0.5,
                                        0.10, 0.10, 0.05, 0.05, 0.05, 0.05);
        assert!(score >= 0.0 && score <= 100.0, "score should be 0-100: {}", score);
    }
}
