import kopf
import kubernetes
from brain_bridge import get_rust_score_6d

# Koordinator GPU resource names
GPU_CORE_RESOURCE = "koordinator.sh/gpu-core"
GPU_MEM_RESOURCE = "koordinator.sh/gpu-memory"


def parse_resource(val, default="0"):
    """Parse K8s resource value to float."""
    if not val:
        return float(default)
    val = str(val)
    if val.endswith('m'):
        return float(val.replace('m', '')) / 1000.0
    if val.endswith('Ki'):
        return float(val.replace('Ki', '')) / (1024 * 1024)
    if val.endswith('Mi'):
        return float(val.replace('Mi', '')) / 1024.0
    if val.endswith('Gi'):
        return float(val.replace('Gi', ''))
    try:
        return float(val)
    except ValueError:
        return 0.0


@kopf.on.create('pods', annotations={'schedulerName': 'lambda-g'})
def schedule_pod(name, namespace, spec, **kwargs):
    core_api = kubernetes.client.CoreV1Api()

    # 1. Parse Pod Resources (6D)
    container = spec['containers'][0]
    requests = container.get('resources', {}).get('requests', {})

    p_cpu = parse_resource(requests.get('cpu', '100m'))
    p_ram = parse_resource(requests.get('memory', '128Mi'))
    p_gpu_core = float(requests.get(GPU_CORE_RESOURCE, 0))
    p_gpu_mem = float(requests.get(GPU_MEM_RESOURCE, 0))
    p_iops = float(requests.get('ephemeral-storage', 0)) if requests.get('ephemeral-storage') else 0.05
    p_net = 0.05  # Network request not directly in K8s spec, use default

    # 2. Find the "Coherence Winner" across 6 dimensions
    nodes = core_api.list_node().items
    best_node = None
    max_score = -100

    for node in nodes:
        if node.spec.unschedulable:
            continue

        alloc = node.status.allocatable or node.status.capacity or {}

        # Node capacity (6D)
        n_cpu = float(alloc.get('cpu', '0'))
        mem_str = str(alloc.get('memory', '0'))
        if 'Ki' in mem_str:
            n_ram = float(mem_str.replace('Ki', '')) / (1024 * 1024)
        elif 'Mi' in mem_str:
            n_ram = float(mem_str.replace('Mi', '')) / 1024.0
        elif 'Gi' in mem_str:
            n_ram = float(mem_str.replace('Gi', ''))
        else:
            n_ram = float(mem_str) / (1024 * 1024 * 1024) if mem_str != '0' else 0

        n_gpu_core = float(alloc.get(GPU_CORE_RESOURCE, 100))
        n_gpu_mem = float(alloc.get(GPU_MEM_RESOURCE, 100))

        # Sum pod usage on this node
        pods = core_api.list_pod_for_all_namespaces(field_selector=f"spec.nodeName={node.metadata.name}").items
        used_cpu, used_ram = 0.0, 0.0
        used_gpu_core, used_gpu_mem = 0.0, 0.0
        for pod in pods:
            for c in (pod.spec.containers or []):
                res = (c.resources.requests or {}) if c.resources else {}
                used_cpu += parse_resource(res.get('cpu', '0'))
                used_ram += parse_resource(res.get('memory', '0'))
                used_gpu_core += float(res.get(GPU_CORE_RESOURCE, 0))
                used_gpu_mem += float(res.get(GPU_MEM_RESOURCE, 0))

        # Normalized free capacity [0..1]
        cpu_free = max(0, (n_cpu - used_cpu) / n_cpu) if n_cpu > 0 else 0
        ram_free = max(0, (n_ram - used_ram) / n_ram) if n_ram > 0 else 0
        gpu_core_free = max(0, (n_gpu_core - used_gpu_core) / n_gpu_core) if n_gpu_core > 0 else 0.5
        gpu_mem_free = max(0, (n_gpu_mem - used_gpu_mem) / n_gpu_mem) if n_gpu_mem > 0 else 0.5
        iops_free = 0.5   # No direct K8s metric, default balanced
        net_free = 0.5     # No direct K8s metric, default balanced

        # Normalized pod request fractions
        cpu_req = p_cpu / n_cpu if n_cpu > 0 else 1.0
        ram_req = p_ram / n_ram if n_ram > 0 else 1.0
        gpu_core_req = p_gpu_core / n_gpu_core if n_gpu_core > 0 else 0.05
        gpu_mem_req = p_gpu_mem / n_gpu_mem if n_gpu_mem > 0 else 0.05

        # 6D scoring via Rust brain
        score = get_rust_score_6d(
            cpu_free, ram_free, gpu_core_free, gpu_mem_free, iops_free, net_free,
            cpu_req, ram_req, gpu_core_req, gpu_mem_req, 0.05, 0.05
        )

        if score > max_score:
            max_score = score
            best_node = node.metadata.name

    # 3. Binding: The "Injection"
    if best_node:
        target = kubernetes.client.V1Binding(
            metadata=kubernetes.client.V1ObjectMeta(name=name),
            target=kubernetes.client.V1ObjectReference(kind='Node', name=best_node)
        )
        core_api.create_namespaced_pod_binding(name, namespace, target)
        print(f"🚀 Lambda-G: Injected {name} into {best_node} (Score: {max_score:.2f})")

if __name__ == "__main__":
    print("🛰️ Lambda-G Coherence Controller is LIVE...")
