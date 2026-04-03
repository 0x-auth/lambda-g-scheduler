import math
import random

def score_lambda_g(pod, node):
    dot = sum(a*b for a, b in zip(pod, node))
    mag_p = math.sqrt(sum(a**2 for a in pod))
    mag_n = math.sqrt(sum(a**2 for a in node))
    return dot / (mag_p * mag_n) if mag_p * mag_n > 0 else 0

def simulate_cluster():
    # 100 Nodes with random "Stranded" resources
    nodes = [[random.uniform(0.1, 0.9), random.uniform(0.1, 0.9)] for _ in range(100)]
    # 500 Pods with "Jagged" requests
    pods = [[random.uniform(0.05, 0.2), random.uniform(0.05, 0.2)] for _ in range(500)]
    
    total_alignment = sum(max(score_lambda_g(p, n) for n in nodes) for p in pods)
    avg_match = total_alignment / 500
    
    print(f"Average Cluster Resonance: {avg_match:.4f}")
    print("STATUS: Manifold is Coherent." if avg_match > 0.85 else "STATUS: High Fragmentation.")

if __name__ == "__main__":
    simulate_cluster()
