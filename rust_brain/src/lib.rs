/// Lambda-G 6D Scoring Engine
/// Dimensions: [CPU, RAM, GPU Core, GPU Memory, IOPS, Network]
/// φ = 1.618033988749895

const PHI: f64 = 1.618033988749895;
const DIMS: usize = 6;
const CAPACITY_GATE: f64 = 0.10;

fn cosine_similarity(a: &[f64; DIMS], b: &[f64; DIMS]) -> f64 {
    let dot: f64 = a.iter().zip(b.iter()).map(|(x, y)| x * y).sum();
    let mag_a: f64 = a.iter().map(|x| x * x).sum::<f64>().sqrt();
    let mag_b: f64 = b.iter().map(|x| x * x).sum::<f64>().sqrt();
    if mag_a < 1e-10 || mag_b < 1e-10 { return 0.0; }
    (dot / (mag_a * mag_b)).clamp(-1.0, 1.0)
}

fn resource_entropy(v: &[f64; DIMS]) -> f64 {
    let sum: f64 = v.iter().sum();
    if sum < 1e-10 { return 0.0; }
    v.iter()
        .map(|&x| { let p = x / sum; if p < 1e-10 { 0.0 } else { -p * p.ln() } })
        .sum::<f64>()
}

fn symmetric_exhaustion(node: &[f64; DIMS], pod: &[f64; DIMS]) -> f64 {
    let mut after = [0.0f64; DIMS];
    for i in 0..DIMS { after[i] = (node[i] - pod[i]).max(0.0); }
    let recovery = resource_entropy(node) - resource_entropy(&after);
    let mag_before: f64 = node.iter().map(|x| x * x).sum::<f64>().sqrt();
    let mag_after: f64 = after.iter().map(|x| x * x).sum::<f64>().sqrt();
    let utilization = if mag_before > 1e-10 { (mag_before - mag_after) / mag_before } else { 0.0 };
    PHI * recovery + utilization
}

fn entropy_leak_penalty(node: &[f64; DIMS], pod: &[f64; DIMS]) -> f64 {
    let mut after = [0.0f64; DIMS];
    for i in 0..DIMS { after[i] = (node[i] - pod[i]).max(0.0); }
    let stranded = (0..DIMS).filter(|&i| after[i] > 0.70 && pod[i] < 0.10).count();
    stranded as f64 * 0.15
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
    let node = [cpu_free, ram_free, gpu_core_free, gpu_mem_free, iops_free, net_free];
    let pod = [cpu_req, ram_req, gpu_core_req, gpu_mem_req, iops_req, net_req];

    // Feasibility
    for i in 0..DIMS {
        if node[i] < pod[i] { return 0.0; }
    }

    // Capacity gate on CPU and RAM
    if node[0] < CAPACITY_GATE || node[1] < CAPACITY_GATE { return 5.0; }

    let alignment = cosine_similarity(&pod, &node);
    let exhaustion_bonus = symmetric_exhaustion(&node, &pod);
    let penalty = entropy_leak_penalty(&node, &pod);
    let headroom = (node[0] + node[1]) / 2.0;

    let raw = PHI * alignment + exhaustion_bonus - penalty + headroom * 0.3;
    (raw * 30.0 + 50.0).clamp(0.0, 100.0)
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
        // Imbalanced: 90% CPU used, 10% RAM used
        let score_a = calculate_score_6d(0.10, 0.90, 0.5, 0.5, 0.5, 0.5,
                                          0.08, 0.05, 0.0, 0.0, 0.05, 0.05);
        // Balanced: 50/50
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
}
