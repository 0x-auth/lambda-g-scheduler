/// lambda_g_scheduler.rs
/// 
/// Lambda-G Kubernetes Scheduler Plugin — Core Scoring Engine
/// ===========================================================
/// Replaces kube-scheduler's default Score phase with vector-alignment
/// scoring that achieves Symmetric Exhaustion across cluster nodes.
///
/// The problem with legacy scoring:
///   Node A: 90% CPU used, 10% RAM used  → score: ~50
///   Node B: 50% CPU used, 50% RAM used  → score: ~50
///   They score the same. Node A is practically dead.
///
/// Lambda-G scoring:
///   Measures directional alignment between pod request vector
///   and node residual capacity vector.
///   CPU-heavy pod → steers toward RAM-heavy node → symmetric exhaustion.
///
/// Author: Space 🌌
/// ORCID:  0009-0006-7495-5039

use std::ffi::{CStr, CString};
use std::os::raw::{c_char, c_double, c_int};

const PHI: f64 = 1.618033988749895;
const DIMS: usize = 4; // CPU, RAM, IOPS, Network

/// Node residual capacity vector (normalized 0.0-1.0)
#[repr(C)]
pub struct NodeCapacity {
    pub cpu_free:     f64,   // fraction free (1.0 = empty, 0.0 = full)
    pub ram_free:     f64,
    pub iops_free:    f64,
    pub network_free: f64,
    pub node_id:      i32,
}

/// Pod resource request vector (normalized against node capacity)
#[repr(C)]
pub struct PodRequest {
    pub cpu_req:     f64,   // fraction of node capacity requested
    pub ram_req:     f64,
    pub iops_req:    f64,
    pub network_req: f64,
}

/// Score result
#[repr(C)]
pub struct Score {
    pub node_id:          i32,
    pub score:            f64,   // 0-100, higher = better placement
    pub alignment:        f64,   // cosine alignment component
    pub exhaustion_bonus: f64,   // symmetric exhaustion component
    pub entropy_penalty:  f64,   // penalty for creating stranded resources
}

/// Compute cosine similarity between two vectors
fn cosine_similarity(a: &[f64; DIMS], b: &[f64; DIMS]) -> f64 {
    let dot: f64  = a.iter().zip(b.iter()).map(|(x, y)| x * y).sum();
    let mag_a: f64 = a.iter().map(|x| x * x).sum::<f64>().sqrt();
    let mag_b: f64 = b.iter().map(|x| x * x).sum::<f64>().sqrt();

    if mag_a < 1e-10 || mag_b < 1e-10 {
        return 0.0;
    }
    (dot / (mag_a * mag_b)).clamp(-1.0, 1.0)
}

/// Compute entropy of resource distribution (lower = more balanced)
/// High entropy = resources are stranded in one dimension
fn resource_entropy(v: &[f64; DIMS]) -> f64 {
    let sum: f64 = v.iter().sum();
    if sum < 1e-10 { return 0.0; }

    v.iter()
        .map(|&x| {
            let p = x / sum;
            if p < 1e-10 { 0.0 } else { -p * p.ln() }
        })
        .sum::<f64>()
}

/// Symmetric exhaustion bonus:
/// How much more balanced will the node be AFTER placing this pod?
/// Balanced = good. One dimension stranded = bad.
fn symmetric_exhaustion_score(
    node: &[f64; DIMS],
    pod:  &[f64; DIMS],
) -> f64 {
    // Node residual after placing pod
    let after: [f64; DIMS] = [
        (node[0] - pod[0]).max(0.0),
        (node[1] - pod[1]).max(0.0),
        (node[2] - pod[2]).max(0.0),
        (node[3] - pod[3]).max(0.0),
    ];

    let entropy_before = resource_entropy(node);
    let entropy_after  = resource_entropy(&after);

    // We want entropy to DECREASE after placement (more balanced)
    // Reward = how much we reduced entropy
    let reward = entropy_before - entropy_after;

    // Also reward magnitude reduction (we're using resources, not wasting them)
    let mag_before: f64 = node.iter().map(|x| x * x).sum::<f64>().sqrt();
    let mag_after:  f64 = after.iter().map(|x| x * x).sum::<f64>().sqrt();
    let utilization = if mag_before > 1e-10 {
        (mag_before - mag_after) / mag_before
    } else { 0.0 };

    // Combined: entropy reduction weighted by phi + utilization
    PHI * reward + utilization
}

/// Entropy leak penalty:
/// Penalize placements that would strand resources
/// (e.g., placing CPU-heavy pod on already CPU-heavy node)
fn entropy_leak_penalty(
    node: &[f64; DIMS],
    pod:  &[f64; DIMS],
) -> f64 {
    // Find dimensions where pod request > 50% of remaining capacity
    // These are "pressure dimensions"
    let pressure_dims: Vec<usize> = (0..DIMS)
        .filter(|&i| node[i] > 1e-10 && pod[i] / node[i] > 0.5)
        .collect();

    if pressure_dims.is_empty() {
        return 0.0;
    }

    // Find dimensions that would be stranded (very low utilization after)
    let after: [f64; DIMS] = [
        (node[0] - pod[0]).max(0.0),
        (node[1] - pod[1]).max(0.0),
        (node[2] - pod[2]).max(0.0),
        (node[3] - pod[3]).max(0.0),
    ];

    // Stranded = after > 70% free AND pod used < 10% of that dimension
    let stranded_dims: usize = (0..DIMS)
        .filter(|&i| after[i] > 0.70 && pod[i] < 0.10)
        .count();

    // Penalty proportional to stranded dimensions
    stranded_dims as f64 * 0.15
}

/// MAIN SCORING FUNCTION
/// Called once per (pod, node) pair during scheduling
/// Returns score 0-100 (higher = better)
#[no_mangle]
pub extern "C" fn lambda_g_score(
    node: *const NodeCapacity,
    pod:  *const PodRequest,
) -> Score {
    let node = unsafe { &*node };
    let pod  = unsafe { &*pod  };

    let node_vec: [f64; DIMS] = [
        node.cpu_free,
        node.ram_free,
        node.iops_free,
        node.network_free,
    ];
    let pod_vec: [f64; DIMS] = [
        pod.cpu_req,
        pod.ram_req,
        pod.iops_req,
        pod.network_req,
    ];

    // 1. Feasibility check — can node fit the pod at all?
    let feasible = (0..DIMS).all(|i| node_vec[i] >= pod_vec[i]);
    if !feasible {
        return Score {
            node_id: node.node_id,
            score: 0.0,
            alignment: 0.0,
            exhaustion_bonus: 0.0,
            entropy_penalty: 0.0,
        };
    }

    // 2. Alignment: pod request vector vs node residual vector
    // High alignment = pod "fits" the shape of available resources
    let alignment = cosine_similarity(&pod_vec, &node_vec);

    // 3. Symmetric exhaustion bonus
    let exhaustion_bonus = symmetric_exhaustion_score(&node_vec, &pod_vec);

    // 4. Entropy leak penalty
    let entropy_penalty = entropy_leak_penalty(&node_vec, &pod_vec);

    // 5. Combine with phi-weighted formula
    // PHI weights alignment higher (it's the primary signal)
    // exhaustion_bonus rewards good long-term bin packing
    // entropy_penalty punishes resource stranding
    let raw_score = PHI * alignment
        + exhaustion_bonus
        - entropy_penalty;

    // Normalize to 0-100 for K8s compatibility
    // K8s expects integer scores 0-100
    let score = (raw_score * 30.0 + 50.0).clamp(0.0, 100.0);

    Score {
        node_id: node.node_id,
        score,
        alignment,
        exhaustion_bonus,
        entropy_penalty,
    }
}

/// Batch scoring: score all nodes for a given pod
/// Returns index of best node
#[no_mangle]
pub extern "C" fn lambda_g_best_node(
    nodes:     *const NodeCapacity,
    n_nodes:   c_int,
    pod:       *const PodRequest,
    scores_out: *mut Score,
) -> c_int {
    let n      = n_nodes as usize;
    let nodes  = unsafe { std::slice::from_raw_parts(nodes, n) };
    let scores = unsafe { std::slice::from_raw_parts_mut(scores_out, n) };

    let mut best_idx   = -1i32;
    let mut best_score = f64::NEG_INFINITY;

    for (i, node) in nodes.iter().enumerate() {
        let s = lambda_g_score(node as *const NodeCapacity, pod);
        if s.score > best_score {
            best_score = s.score;
            best_idx   = i as i32;
        }
        scores[i] = s;
    }

    best_idx
}

// ── PURE RUST TEST (no FFI needed) ───────────────────────────────────────────
#[cfg(test)]
mod tests {
    use super::*;

    fn make_node(cpu: f64, ram: f64, iops: f64, net: f64, id: i32) -> NodeCapacity {
        NodeCapacity { cpu_free: cpu, ram_free: ram,
                       iops_free: iops, network_free: net, node_id: id }
    }
    fn make_pod(cpu: f64, ram: f64, iops: f64, net: f64) -> PodRequest {
        PodRequest { cpu_req: cpu, ram_req: ram,
                     iops_req: iops, network_req: net }
    }

    #[test]
    fn test_entropy_leak_detection() {
        // Node A: 90% CPU used, 10% RAM used (entropy leak)
        let node_a = make_node(0.10, 0.90, 0.50, 0.50, 0);
        // Node B: balanced 50/50
        let node_b = make_node(0.50, 0.50, 0.50, 0.50, 1);

        // CPU-heavy pod
        let pod = make_pod(0.08, 0.05, 0.10, 0.10);

        let score_a = lambda_g_score(&node_a, &pod);
        let score_b = lambda_g_score(&node_b, &pod);

        println!("Node A (90% CPU used): score={:.2}  alignment={:.3}  exhaust={:.3}  penalty={:.3}",
            score_a.score, score_a.alignment, score_a.exhaustion_bonus, score_a.entropy_penalty);
        println!("Node B (balanced):     score={:.2}  alignment={:.3}  exhaust={:.3}  penalty={:.3}",
            score_b.score, score_b.alignment, score_b.exhaustion_bonus, score_b.entropy_penalty);

        // Lambda-G should prefer balanced node B
        assert!(score_b.score > score_a.score,
            "Lambda-G should avoid the entropy-leaking node A");
    }

    #[test]
    fn test_symmetric_exhaustion_steering() {
        // Pod is RAM-heavy
        let pod = make_pod(0.05, 0.40, 0.10, 0.10);

        // Node X: RAM-heavy free (good match)
        let node_x = make_node(0.80, 0.50, 0.70, 0.70, 0);
        // Node Y: CPU-heavy free (bad match — wastes RAM)
        let node_y = make_node(0.50, 0.10, 0.70, 0.70, 1);

        let score_x = lambda_g_score(&node_x, &pod);
        let score_y = lambda_g_score(&node_y, &pod);

        println!("Node X (RAM-heavy free): score={:.2}", score_x.score);
        println!("Node Y (CPU-heavy free): score={:.2}", score_y.score);

        // Should prefer node X — better alignment, better exhaustion
        assert!(score_x.score > score_y.score,
            "RAM-heavy pod should prefer RAM-heavy-free node");
    }

    #[test]
    fn test_infeasible_node_scores_zero() {
        // Node is full on CPU
        let node = make_node(0.01, 0.90, 0.90, 0.90, 0);
        // CPU-heavy pod can't fit
        let pod  = make_pod(0.50, 0.10, 0.10, 0.10);

        let score = lambda_g_score(&node, &pod);
        assert_eq!(score.score, 0.0, "Infeasible node must score zero");
    }

    #[test]
    fn test_batch_scoring() {
        let nodes = vec![
            make_node(0.10, 0.90, 0.50, 0.50, 0), // entropy leak
            make_node(0.50, 0.50, 0.50, 0.50, 1), // balanced
            make_node(0.80, 0.20, 0.60, 0.60, 2), // CPU-heavy free
        ];
        let pod = make_pod(0.08, 0.05, 0.10, 0.10);
        let mut scores = vec![Score {
            node_id:0, score:0.0, alignment:0.0,
            exhaustion_bonus:0.0, entropy_penalty:0.0
        }; 3];

        let best = lambda_g_best_node(
            nodes.as_ptr(), 3,
            &pod as *const PodRequest,
            scores.as_mut_ptr(),
        );

        println!("Best node index: {}", best);
        for s in &scores {
            println!("  node={} score={:.2}", s.node_id, s.score);
        }
        assert!(best >= 0, "Should find a valid node");
    }
}
