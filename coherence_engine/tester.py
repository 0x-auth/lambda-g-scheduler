import math

PHI = 1.618033988749895

def get_coherence_score(node_vec, pod_vec):
    # node_vec = [cpu_free_%, ram_free_%]
    # pod_vec = [cpu_req_%, ram_req_%]
    
    # 1. Check Feasibility
    if pod_vec[0] > node_vec[0] or pod_vec[1] > node_vec[1]:
        return 0.0

    # 2. Current Imbalance (The 'Before' Entropy)
    # Node A [0.1, 0.9] has imbalance of 0.8
    # Node B [0.5, 0.5] has imbalance of 0.0
    current_imbalance = abs(node_vec[0] - node_vec[1])

    # 3. Projected Imbalance (The 'After' Entropy)
    after_cpu = node_vec[0] - pod_vec[0]
    after_ram = node_vec[1] - pod_vec[1]
    after_imbalance = abs(after_cpu - after_ram)

    # 4. THE LAMBDA-G SECRET:
    # We reward the REDUCTION in imbalance. 
    # If a pod makes a messy node cleaner, it's a huge win.
    healing_bonus = (current_imbalance - after_imbalance) * 100
    
    # 5. Tightness Factor (The PHI weighted packing)
    # We want to use as much of the node as possible.
    utilization = (1.0 - (after_cpu + after_ram)) * PHI * 10

    return healing_bonus + utilization

def run_test():
    print("🎯 Lambda-G: Symmetric Exhaustion Test")
    
    # A CPU-Heavy Pod (Needs 40% of a node's CPU, but only 5% of its RAM)
    p_vec = [0.4, 0.05]
    
    # Node A: Imbalanced (90% RAM free, but only 50% CPU free)
    # It's 'messy' because the resources are uneven.
    node_a = [0.5, 0.9] 
    
    # Node B: Perfectly Balanced (70% CPU free, 70% RAM free)
    # It looks 'better' to a normal scheduler.
    node_b = [0.7, 0.7]

    score_a = get_coherence_score(node_a, p_vec)
    score_b = get_coherence_score(node_b, p_vec)
    
    print(f"Node A Score: {score_a:.2f}")
    print(f"Node B Score: {score_b:.2f}")
    
    if score_a > score_b:
        print("✅ SUCCESS: Lambda-G chose to HEAL the imbalanced node!")
    else:
        print("❌ FAIL: Still choosing the 'safe' balanced node.")

if __name__ == "__main__":
    run_test()
import math

PHI = 1.618033988749895

def get_coherence_score(node_vec, pod_vec):
    # node_vec: [cpu_free_%, ram_free_%]
    # pod_vec:  [cpu_req_%, ram_req_%]
    
    # 1. Check Feasibility
    if pod_vec[0] > node_vec[0] or pod_vec[1] > node_vec[1]:
        return -100 # Won't fit

    # 2. Calculate "Imbalance" (Entropy)
    # Higher difference = Higher Entropy (Bad)
    initial_entropy = abs(node_vec[0] - node_vec[1])
    
    # 3. Calculate "After" state
    after_cpu = node_vec[0] - pod_vec[0]
    after_ram = node_vec[1] - pod_vec[1]
    final_entropy = abs(after_cpu - after_ram)
    
    # 4. THE LAMBDA-G DELTA
    # We reward the REDUCTION in entropy.
    # If final_entropy < initial_entropy, the pod "healed" the node.
    recovery = initial_entropy - final_entropy
    
    # 5. Reward "Symmetric Exhaustion"
    # If a node ends up with both resources near zero, it's a perfect pack.
    exhaustion_bonus = 1.0 - (after_cpu + after_ram)
    
    # Final Formula: Weight the recovery by PHI
    # We multiply by 100 to make the delta visible
    score = (recovery * PHI * 100) + (exhaustion_bonus * 10)
    
    return score

def run_test():
    print("🎯 Lambda-G: Symmetric Exhaustion Test")
    
    # Our Pod: CPU-Heavy (1.0 Core), RAM-Light (0.125 GB)
    # Normalized for a standard node:
    p_vec = [0.2, 0.02] 
    
    # Node A: Messy (Only 30% CPU left, but 90% RAM left) -> [0.3, 0.9]
    # Node B: Clean (60% CPU left, 60% RAM left) -> [0.6, 0.6]
    
    score_a = get_coherence_score([0.3, 0.9], p_vec)
    score_b = get_coherence_score([0.6, 0.6], p_vec)
    
    print(f"Node A (Imbalanced) Score: {score_a:.2f}")
    print(f"Node B (Balanced) Score: {score_b:.2f}")
    
    if score_a > score_b:
        print("✅ SUCCESS: Lambda-G chose Node A to HEAL the imbalance!")
    else:
        print("❌ FAIL: Still choosing the 'safe' balanced node.")

if __name__ == "__main__":
    run_test()
