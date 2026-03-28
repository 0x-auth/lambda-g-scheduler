#[no_mangle]
pub extern "C" fn calculate_score(cpu_free: f64, ram_free: f64, cpu_req: f64, ram_req: f64) -> f64 {
    let phi = 1.618033988749895;
    
    if cpu_req > cpu_free || ram_req > ram_free { return -100.0; }
    
    let initial_entropy = (cpu_free - ram_free).abs();
    let after_cpu = cpu_free - cpu_req;
    let after_ram = ram_free - ram_req;
    let final_entropy = (after_cpu - after_ram).abs();
    
    let recovery = initial_entropy - final_entropy;
    let exhaustion = 1.0 - (after_cpu + after_ram);
    
    (recovery * phi * 100.0) + (exhaustion * 10.0)
}
