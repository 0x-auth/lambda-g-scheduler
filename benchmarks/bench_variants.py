#!/usr/bin/env python3
"""Variant comparison for koordinator-sh/koordinator#2839 review."""
import sys, math, random

src = open("benchmark_simulation.py").read().replace(
    'if __name__ == "__main__":', 'if False:')
bs = {}
exec(compile(src, "benchmark_simulation.py", "exec"), bs)

N_DIMS = bs["N_DIMS"]; cosine_sim = bs["cosine_sim"]; SCENARIOS = bs["SCENARIOS"]
calc_metrics = bs["calc_metrics"]; gen_workload = bs["gen_workload"]


def _parts(node, pod):
    active = [i for i in range(N_DIMS) if node.capacity[i] > 0]
    after = [(node.used[i] + pod.req[i]) / node.capacity[i] for i in active]
    mean = sum(after) / len(after)
    var = sum((x - mean) ** 2 for x in after) / len(after)
    variance_score = max(0, (1.0 - var * 4) * 100)
    ff = node.free_frac()
    nf = [ff[i] for i in active]
    pf = [pod.req[i] / node.capacity[i] if node.capacity[i] > 0 else 0 for i in active]
    alignment_score = cosine_sim(nf, pf) * 100
    headroom_score = (sum(nf) / len(nf)) * 100
    pressure = 0
    for u in after:
        if u > 0.92:   pressure += (u - 0.92) * 500
        elif u > 0.85: pressure += (u - 0.85) * 50
    strand = 0
    for i in range(len(after)):
        for j in range(i + 1, len(after)):
            if (after[i] > .80 and after[j] < .20) or (after[j] > .80 and after[i] < .20):
                strand += 15
    return variance_score, alignment_score, headroom_score, pressure, strand


def sc_v3(node, pod):
    if not node.can_fit(pod.req): return -1
    v, a, h, p, s = _parts(node, pod)
    return max(0, min(100, 0.6*v + 0.2*a + 0.1*h - p - s))

def sc_args_on_balanced(node, pod):
    """B: variance + strand + pressure, NO separate alignment term, no new plugin."""
    if not node.can_fit(pod.req): return -1
    v, a, h, p, s = _parts(node, pod)
    return max(0, min(100, v - p - s))


def simulate_gated(nodes, pods, eps):
    """A: rank by variance-only; among nodes within eps of best, alignment breaks tie."""
    pending = 0
    for pod in pods:
        cands = []
        for n in nodes:
            if not n.can_fit(pod.req): continue
            v, a, h, p, s = _parts(n, pod)
            cands.append((max(0, min(100, v - p - s)), a, n))
        if not cands:
            pending += 1; continue
        best = max(c[0] for c in cands)
        tied = [c for c in cands if best - c[0] <= eps]
        tied.sort(key=lambda c: -c[1])
        tied[0][2].place(pod.req)
    return calc_metrics(nodes, pending, len(pods))


def eval_fn(score_fn, seed=42):
    tb=ts=tw=0; res={}
    for sc in SCENARIOS:
        random.seed(seed)
        nodes = sc["cluster"](); pods = gen_workload(sc["n"], sc["mix"])
        random.shuffle(pods)
        m = bs["simulate"](nodes, pods, score_fn)
        res[sc["name"]]=m; tb+=m["balance"]; ts+=m["stranded"]; tw+=m["waste"]
    return {"results":res,"avg":round(tb/len(SCENARIOS),1),"stranded":ts,"waste":tw}

def eval_gated(eps, seed=42):
    tb=ts=tw=0; res={}
    for sc in SCENARIOS:
        random.seed(seed)
        nodes = sc["cluster"](); pods = gen_workload(sc["n"], sc["mix"])
        random.shuffle(pods)
        m = simulate_gated(nodes, pods, eps)
        res[sc["name"]]=m; tb+=m["balance"]; ts+=m["stranded"]; tw+=m["waste"]
    return {"results":res,"avg":round(tb/len(SCENARIOS),1),"stranded":ts,"waste":tw}


print("="*78)
print(" A. EPSILON-GATED ALIGNMENT vs UNCONDITIONAL 0.2 TERM")
print("    (koordinator#2839 review, 2026-07-21, point 1)")
print("="*78)
v3 = eval_fn(sc_v3)
bal = eval_fn(bs["sc_balanced"])
print(f"\n  {'variant':<34}{'avg bal':>9}{'stranded':>10}{'waste $':>11}")
print("  " + "-"*63)
print(f"  {'BalancedAllocation (baseline)':<34}{bal['avg']:>9}{bal['stranded']:>10}{bal['waste']:>11.0f}")
print(f"  {'V3 unconditional 0.2 alignment':<34}{v3['avg']:>9}{v3['stranded']:>10}{v3['waste']:>11.0f}")
for eps in (0.5, 1.0, 2.0, 5.0, 10.0):
    g = eval_gated(eps)
    print(f"  {'V3 eps-gated, eps=' + str(eps):<34}{g['avg']:>9}{g['stranded']:>10}{g['waste']:>11.0f}")

print()
print("="*78)
print(" B. ARGS-ON-BALANCEDALLOCATION vs SEPARATE PLUGIN")
print("    (koordinator#2839 review, 2026-07-21, point 2)")
print("="*78)
ab = eval_fn(sc_args_on_balanced)
print(f"\n  {'variant':<34}{'avg bal':>9}{'stranded':>10}{'waste $':>11}")
print("  " + "-"*63)
print(f"  {'BalancedAllocation (plain)':<34}{bal['avg']:>9}{bal['stranded']:>10}{bal['waste']:>11.0f}")
print(f"  {'+ strand + pressure args only':<34}{ab['avg']:>9}{ab['stranded']:>10}{ab['waste']:>11.0f}")
print(f"  {'full V3 (separate plugin)':<34}{v3['avg']:>9}{v3['stranded']:>10}{v3['waste']:>11.0f}")
print(f"\n  alignment term contributes: {v3['avg']-ab['avg']:+.1f} balance, "
      f"{v3['stranded']-ab['stranded']:+d} stranded, ${v3['waste']-ab['waste']:+,.0f} waste")
print("="*78)
