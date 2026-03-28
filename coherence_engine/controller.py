import kopf
import kubernetes
from brain_bridge import get_rust_score

@kopf.on.create('pods', annotations={'schedulerName': 'lambda-g'})
def schedule_pod(name, namespace, spec, **kwargs):
    core_api = kubernetes.client.CoreV1Api()
    
    # 1. Parse Pod Resources
    container = spec['containers'][0]
    requests = container.get('resources', {}).get('requests', {})
    
    # Convert CPU (m) and RAM (Mi/Gi) to normalized floats
    p_cpu = float(requests.get('cpu', '100m').replace('m', '')) / 1000.0
    p_ram_str = requests.get('memory', '128Mi')
    if 'Gi' in p_ram_str: p_ram = float(p_ram_str.replace('Gi', ''))
    else: p_ram = float(p_ram_str.replace('Mi', '')) / 1024.0

    # 2. Find the "Coherence Winner"
    nodes = core_api.list_node().items
    best_node = None
    max_score = -100

    for node in nodes:
        if node.spec.unschedulable: continue
        
        # Get Node Vectors (Simplified: Using Capacity for POC)
        cap = node.status.capacity
        n_cpu = float(cap['cpu'])
        n_ram = float(cap['memory'].replace('Ki', '')) / (1024**2)
        
        # Calculate score using the RUST BRAIN
        score = get_rust_score(1.0, 1.0, p_cpu/n_cpu, p_ram/n_ram)
        
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
