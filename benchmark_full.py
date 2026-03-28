#!/usr/bin/env python3
"""
Lambda-G Full Benchmark
========================
Tests three scheduling strategies on a 3-node minikube cluster:
1. Default K8s scheduler (LeastAllocated)
2. Lambda-G Simple (2D: CPU + RAM entropy)
3. Lambda-G Full (4D: cosine alignment + symmetric exhaustion + entropy penalty)

Deploys 15 pods with varied CPU/RAM ratios, measures:
- Resource distribution across nodes
- Stranded resources (entropy leak)
- Effective utilization
- Cluster balance score

Run: python3 benchmark_full.py
Requires: minikube running with 3 nodes, kubectl configured
"""

import subprocess
import json
import time
import math
import sys
import os

PHI = 1.618033988749895

# ─── Pod Definitions (varied CPU/RAM ratios to stress test) ───

PODS = [
    # CPU-heavy pods
    {"name": "cpu-heavy-1", "cpu": "400m", "ram": "64Mi"},
    {"name": "cpu-heavy-2", "cpu": "500m", "ram": "32Mi"},
    {"name": "cpu-heavy-3", "cpu": "300m", "ram": "48Mi"},
    {"name": "cpu-heavy-4", "cpu": "600m", "ram": "64Mi"},
    {"name": "cpu-heavy-5", "cpu": "350m", "ram": "32Mi"},
    # RAM-heavy pods
    {"name": "ram-heavy-1", "cpu": "50m",  "ram": "512Mi"},
    {"name": "ram-heavy-2", "cpu": "100m", "ram": "256Mi"},
    {"name": "ram-heavy-3", "cpu": "50m",  "ram": "384Mi"},
    {"name": "ram-heavy-4", "cpu": "80m",  "ram": "512Mi"},
    {"name": "ram-heavy-5", "cpu": "60m",  "ram": "300Mi"},
    # Balanced pods
    {"name": "balanced-1", "cpu": "200m", "ram": "256Mi"},
    {"name": "balanced-2", "cpu": "150m", "ram": "192Mi"},
    {"name": "balanced-3", "cpu": "250m", "ram": "256Mi"},
    {"name": "balanced-4", "cpu": "200m", "ram": "200Mi"},
    {"name": "balanced-5", "cpu": "180m", "ram": "220Mi"},
]


def run(cmd, timeout=30):
    """Run a kubectl command and return output"""
    try:
        r = subprocess.run(cmd, shell=True, capture_output=True, text=True, timeout=timeout)
        return r.stdout.strip()
    except:
        return ""


def get_node_resources():
    """Get current resource usage per node"""
    nodes_json = run("kubectl get nodes -o json")
    if not nodes_json:
        return {}

    nodes = json.loads(nodes_json)["items"]
    result = {}

    for node in nodes:
        name = node["metadata"]["name"]
        cap = node["status"]["allocatable"]

        # Parse capacity
        cpu_cap = float(cap["cpu"]) * 1000  # to millicores
        mem_str = cap["memory"]
        if "Ki" in mem_str:
            mem_cap = float(mem_str.replace("Ki", "")) / 1024  # to Mi
        elif "Mi" in mem_str:
            mem_cap = float(mem_str.replace("Mi", ""))
        elif "Gi" in mem_str:
            mem_cap = float(mem_str.replace("Gi", "")) * 1024
        else:
            mem_cap = float(mem_str) / (1024 * 1024)

        # Get pods on this node
        pods_json = run(f"kubectl get pods --all-namespaces --field-selector spec.nodeName={name} -o json")
        pods = json.loads(pods_json)["items"] if pods_json else []

        cpu_used = 0
        mem_used = 0
        pod_count = 0

        for pod in pods:
            # Only count our test pods + system pods with requests
            for container in pod["spec"].get("containers", []):
                reqs = container.get("resources", {}).get("requests", {})
                if reqs:
                    cpu_str = reqs.get("cpu", "0m")
                    if "m" in cpu_str:
                        cpu_used += float(cpu_str.replace("m", ""))
                    else:
                        cpu_used += float(cpu_str) * 1000

                    mem_str = reqs.get("memory", "0Mi")
                    if "Gi" in mem_str:
                        mem_used += float(mem_str.replace("Gi", "")) * 1024
                    elif "Mi" in mem_str:
                        mem_used += float(mem_str.replace("Mi", ""))
                    elif "Ki" in mem_str:
                        mem_used += float(mem_str.replace("Ki", "")) / 1024
            pod_count += 1

        result[name] = {
            "cpu_cap": cpu_cap,
            "mem_cap": mem_cap,
            "cpu_used": cpu_used,
            "mem_used": mem_used,
            "cpu_pct": cpu_used / cpu_cap * 100 if cpu_cap > 0 else 0,
            "mem_pct": mem_used / mem_cap * 100 if mem_cap > 0 else 0,
            "pods": pod_count,
        }

    return result


def calculate_cluster_metrics(nodes):
    """Calculate cluster-level metrics"""
    if not nodes:
        return {}

    # Entropy: variance in utilization across nodes (lower = more balanced)
    cpu_pcts = [n["cpu_pct"] for n in nodes.values()]
    mem_pcts = [n["mem_pct"] for n in nodes.values()]

    cpu_mean = sum(cpu_pcts) / len(cpu_pcts) if cpu_pcts else 0
    mem_mean = sum(mem_pcts) / len(mem_pcts) if mem_pcts else 0

    cpu_var = sum((x - cpu_mean)**2 for x in cpu_pcts) / len(cpu_pcts) if cpu_pcts else 0
    mem_var = sum((x - mem_mean)**2 for x in mem_pcts) / len(mem_pcts) if mem_pcts else 0

    # Stranded resources: nodes where one dimension is >70% and other is <30%
    stranded = 0
    for n in nodes.values():
        if (n["cpu_pct"] > 70 and n["mem_pct"] < 30) or (n["mem_pct"] > 70 and n["cpu_pct"] < 30):
            stranded += 1

    # Balance score: 100 = perfect balance, 0 = terrible
    max_var = 2500  # 50% stddev squared
    balance = max(0, 100 - (cpu_var + mem_var) / max_var * 100)

    # Total utilization
    total_cpu = sum(n["cpu_used"] for n in nodes.values())
    total_cap_cpu = sum(n["cpu_cap"] for n in nodes.values())
    total_mem = sum(n["mem_used"] for n in nodes.values())
    total_cap_mem = sum(n["mem_cap"] for n in nodes.values())

    return {
        "cpu_variance": cpu_var,
        "mem_variance": mem_var,
        "stranded_nodes": stranded,
        "balance_score": balance,
        "total_cpu_pct": total_cpu / total_cap_cpu * 100 if total_cap_cpu else 0,
        "total_mem_pct": total_mem / total_cap_mem * 100 if total_cap_mem else 0,
    }


def deploy_pods_default(pods):
    """Deploy pods using default K8s scheduler"""
    for pod in pods:
        yaml = f"""
apiVersion: v1
kind: Pod
metadata:
  name: {pod['name']}
  namespace: default
  labels:
    benchmark: default-scheduler
spec:
  containers:
  - name: worker
    image: busybox
    command: ["sleep", "3600"]
    resources:
      requests:
        cpu: "{pod['cpu']}"
        memory: "{pod['ram']}"
      limits:
        cpu: "{pod['cpu']}"
        memory: "{pod['ram']}"
"""
        run(f"echo '{yaml}' | kubectl apply -f -")
    # Wait for scheduling
    time.sleep(10)


def deploy_pods_lambdag(pods, strategy="simple"):
    """Deploy pods using Lambda-G scoring (simulated — we score and use nodeSelector)"""
    nodes_before = get_node_resources()
    node_names = [n for n in nodes_before.keys() if "control-plane" not in str(nodes_before[n])]
    if not node_names:
        node_names = list(nodes_before.keys())

    for pod in pods:
        # Parse pod resources
        cpu_req = float(pod["cpu"].replace("m", "")) / 1000
        mem_req = float(pod["ram"].replace("Mi", "")) / 1024

        # Score each node
        best_node = None
        best_score = -999

        current_nodes = get_node_resources()

        for node_name, n in current_nodes.items():
            cpu_free = 1.0 - (n["cpu_pct"] / 100)
            ram_free = 1.0 - (n["mem_pct"] / 100)

            cpu_norm = cpu_req / (n["cpu_cap"] / 1000) if n["cpu_cap"] > 0 else 1
            ram_norm = mem_req / (n["mem_cap"] / 1024) if n["mem_cap"] > 0 else 1

            if strategy == "simple":
                score = simple_score(cpu_free, ram_free, cpu_norm, ram_norm)
            else:
                score = full_score(cpu_free, ram_free, cpu_norm, ram_norm)

            if score > best_score:
                best_score = score
                best_node = node_name

        yaml = f"""
apiVersion: v1
kind: Pod
metadata:
  name: {pod['name']}
  namespace: default
  labels:
    benchmark: lambda-g-{strategy}
spec:
  nodeName: {best_node}
  containers:
  - name: worker
    image: busybox
    command: ["sleep", "3600"]
    resources:
      requests:
        cpu: "{pod['cpu']}"
        memory: "{pod['ram']}"
      limits:
        cpu: "{pod['cpu']}"
        memory: "{pod['ram']}"
"""
        run(f"echo '{yaml}' | kubectl apply -f -")

    time.sleep(10)


def simple_score(cpu_free, ram_free, cpu_req, ram_req):
    """Simple 2D scoring (current Rust brain) + capacity gate"""
    if cpu_req > cpu_free or ram_req > ram_free:
        return -100

    # Capacity gate: penalize nodes that are already >80% on any dimension
    if cpu_free < 0.20 or ram_free < 0.20:
        return -50

    initial_entropy = abs(cpu_free - ram_free)
    after_cpu = cpu_free - cpu_req
    after_ram = ram_free - ram_req
    final_entropy = abs(after_cpu - after_ram)

    recovery = initial_entropy - final_entropy
    exhaustion = 1.0 - (after_cpu + after_ram)

    # Add spread bonus: prefer nodes with more headroom
    headroom = (cpu_free + ram_free) / 2

    return (recovery * PHI * 100) + (exhaustion * 10) + (headroom * 20)


def full_score(cpu_free, ram_free, cpu_req, ram_req):
    """Full 4D scoring (from lambda_g_scheduler.rs)"""
    node_vec = [cpu_free, ram_free, 0.5, 0.5]  # IOPS + Net default to 0.5
    pod_vec = [cpu_req, ram_req, 0.05, 0.05]

    # Feasibility
    for i in range(4):
        if node_vec[i] < pod_vec[i]:
            return 0

    # Capacity gate: avoid overloaded nodes
    if node_vec[0] < 0.20 or node_vec[1] < 0.20:
        return 5  # Very low score, not zero (still feasible, just bad)

    # Cosine similarity
    dot = sum(a * b for a, b in zip(pod_vec, node_vec))
    mag_a = math.sqrt(sum(a**2 for a in pod_vec))
    mag_b = math.sqrt(sum(b**2 for b in node_vec))
    alignment = dot / (mag_a * mag_b) if mag_a > 0 and mag_b > 0 else 0

    # Symmetric exhaustion
    after = [max(0, node_vec[i] - pod_vec[i]) for i in range(4)]
    s_before = sum(node_vec)
    s_after = sum(after)
    ent_before = sum(-((x/s_before) * math.log(x/s_before)) if x/s_before > 1e-10 else 0 for x in node_vec) if s_before > 0 else 0
    ent_after = sum(-((x/s_after) * math.log(x/s_after)) if x/s_after > 1e-10 else 0 for x in after) if s_after > 0 else 0
    recovery = ent_before - ent_after

    mag_before = math.sqrt(sum(x**2 for x in node_vec))
    mag_after = math.sqrt(sum(x**2 for x in after))
    utilization = (mag_before - mag_after) / mag_before if mag_before > 0 else 0

    exhaustion_bonus = PHI * recovery + utilization

    # Entropy leak penalty
    stranded = sum(1 for i in range(4) if after[i] > 0.7 and pod_vec[i] < 0.1)
    penalty = stranded * 0.15

    raw = PHI * alignment + exhaustion_bonus - penalty
    return max(0, min(100, raw * 30 + 50))


def cleanup():
    """Remove all benchmark pods"""
    run("kubectl delete pods -l benchmark --force --grace-period=0 2>/dev/null")
    time.sleep(5)


def print_report(label, nodes, metrics):
    """Print a formatted report"""
    print(f"\n  {'═' * 60}")
    print(f"  {label}")
    print(f"  {'═' * 60}\n")

    print(f"  {'Node':<16} {'CPU Used':>10} {'RAM Used':>10} {'CPU %':>8} {'RAM %':>8} {'Pods':>6}")
    print(f"  {'─' * 60}")

    for name, n in sorted(nodes.items()):
        cpu_bar = '█' * int(n['cpu_pct'] / 5) + '░' * (20 - int(n['cpu_pct'] / 5))
        print(f"  {name:<16} {n['cpu_used']:>8.0f}m {n['mem_used']:>8.0f}Mi {n['cpu_pct']:>7.1f}% {n['mem_pct']:>7.1f}% {n['pods']:>6}")

    print(f"\n  Cluster Metrics:")
    print(f"    CPU variance:    {metrics['cpu_variance']:.1f}")
    print(f"    RAM variance:    {metrics['mem_variance']:.1f}")
    print(f"    Stranded nodes:  {metrics['stranded_nodes']}")
    print(f"    Balance score:   {metrics['balance_score']:.1f}/100")
    print(f"    Total CPU util:  {metrics['total_cpu_pct']:.1f}%")
    print(f"    Total RAM util:  {metrics['total_mem_pct']:.1f}%")


def main():
    print(f"""
◊═══════════════════════════════════════════════════════════════◊
  LAMBDA-G COMPREHENSIVE BENCHMARK
  φ = {PHI}
  3 Nodes × 15 Pods × 3 Strategies
◊═══════════════════════════════════════════════════════════════◊
""")

    results = {}

    # ── Test 1: Default K8s Scheduler ──
    print("  ▸ Phase 1: Default K8s Scheduler...")
    cleanup()
    deploy_pods_default(PODS)
    nodes = get_node_resources()
    metrics = calculate_cluster_metrics(nodes)
    results["default"] = metrics
    print_report("DEFAULT K8S SCHEDULER (LeastAllocated)", nodes, metrics)
    cleanup()

    # ── Test 2: Lambda-G Simple (2D) ──
    print("\n  ▸ Phase 2: Lambda-G Simple (2D entropy)...")
    deploy_pods_lambdag(PODS, strategy="simple")
    nodes = get_node_resources()
    metrics = calculate_cluster_metrics(nodes)
    results["simple"] = metrics
    print_report("LAMBDA-G SIMPLE (2D: CPU+RAM entropy)", nodes, metrics)
    cleanup()

    # ── Test 3: Lambda-G Full (4D) ──
    print("\n  ▸ Phase 3: Lambda-G Full (4D vector alignment)...")
    deploy_pods_lambdag(PODS, strategy="full")
    nodes = get_node_resources()
    metrics = calculate_cluster_metrics(nodes)
    results["full"] = metrics
    print_report("LAMBDA-G FULL (4D: cosine + exhaustion + penalty)", nodes, metrics)
    cleanup()

    # ── Comparison ──
    print(f"""
◊═══════════════════════════════════════════════════════════════◊
  COMPARISON SUMMARY
◊═══════════════════════════════════════════════════════════════◊

  {'Metric':<25} {'Default':>12} {'LG Simple':>12} {'LG Full':>12}
  {'─' * 65}""")

    for metric in ['balance_score', 'cpu_variance', 'mem_variance', 'stranded_nodes']:
        d = results['default'].get(metric, 0)
        s = results['simple'].get(metric, 0)
        f = results['full'].get(metric, 0)

        # Highlight winner
        if metric == 'balance_score':
            winner = 'full' if f >= s and f >= d else ('simple' if s >= d else 'default')
        else:
            winner = 'full' if f <= s and f <= d else ('simple' if s <= d else 'default')

        d_str = f"{d:.1f}"
        s_str = f"{s:.1f}"
        f_str = f"{f:.1f}"

        if winner == 'default': d_str = f"*{d_str}*"
        elif winner == 'simple': s_str = f"*{s_str}*"
        else: f_str = f"*{f_str}*"

        print(f"  {metric:<25} {d_str:>12} {s_str:>12} {f_str:>12}")

    # Calculate improvement
    if results['default']['balance_score'] > 0:
        simple_improvement = ((results['simple']['balance_score'] - results['default']['balance_score'])
                             / results['default']['balance_score'] * 100)
        full_improvement = ((results['full']['balance_score'] - results['default']['balance_score'])
                           / results['default']['balance_score'] * 100)
    else:
        simple_improvement = 0
        full_improvement = 0

    print(f"""
  ─────────────────────────────────────────────────────────────
  Lambda-G Simple improvement: {simple_improvement:+.1f}% balance
  Lambda-G Full improvement:   {full_improvement:+.1f}% balance

  φ = {PHI}
  "Symmetric exhaustion > least allocation"
◊═══════════════════════════════════════════════════════════════◊
""")


if __name__ == "__main__":
    main()
