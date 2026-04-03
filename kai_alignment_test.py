import math

def cosine_similarity(v1, v2):
    dot = sum(a*b for a, b in zip(v1, v2))
    mag1 = math.sqrt(sum(a*a for a in v1))
    mag2 = math.sqrt(sum(b*b for b in v2))
    return dot / (mag1 * mag2) if mag1 * mag2 > 0 else 0

# Node: 10% VRAM free, 80% Compute free (Stranded Compute)
node_free = [0.1, 0.8] 

# Pod A (Compute Heavy): Needs [0.05 VRAM, 0.4 Compute] -> GOOD FIT
pod_a = [0.05, 0.4]

# Pod B (VRAM Heavy): Needs [0.09 VRAM, 0.05 Compute] -> BAD FIT (will exhaust VRAM, strand compute)
pod_b = [0.09, 0.05]

score_a = cosine_similarity(pod_a, node_free)
score_b = cosine_similarity(pod_b, node_free)

print(f"Alignment Score Pod A (Compute Heavy): {score_a:.4f}")
print(f"Alignment Score Pod B (VRAM Heavy):    {score_b:.4f}")
print("-" * 40)
if score_a > score_b:
    print("RESULT: Lambda-G logic correctly prioritizes Balance over Exhaustion.")
