import ctypes
import os
import glob
import sys

# Smart path resolution — works on Mac AND inside Docker
base_dir = os.path.dirname(os.path.abspath(__file__))
possible_patterns = [
    os.path.join(base_dir, "../rust_brain/target/release/liblambda_g_math.*"),
    os.path.join(base_dir, "rust_brain/target/release/liblambda_g_math.*"),
    "rust_brain/target/release/liblambda_g_math.*",
    "/app/rust_brain/target/release/liblambda_g_math.*",
    os.path.join(base_dir, "bin/liblambda_g_math.*"),
]

lib_path = None
for pattern in possible_patterns:
    matches = glob.glob(pattern)
    # Prefer .dylib (macOS) or .so (Linux) over .rlib (Rust static lib, not loadable)
    matches = [m for m in matches if m.endswith('.dylib') or m.endswith('.so')]
    if matches:
        lib_path = matches[0]
        break

if not lib_path:
    print("❌ Error: Lambda-G Math library not found!")
    print(f"  Searched from: {base_dir}")
    sys.exit(1)

lib = ctypes.CDLL(os.path.abspath(lib_path))

# 6D scoring function
lib.calculate_score_6d.argtypes = [
    ctypes.c_double, ctypes.c_double,  # cpu_free, ram_free
    ctypes.c_double, ctypes.c_double,  # gpu_core_free, gpu_mem_free
    ctypes.c_double, ctypes.c_double,  # iops_free, net_free
    ctypes.c_double, ctypes.c_double,  # cpu_req, ram_req
    ctypes.c_double, ctypes.c_double,  # gpu_core_req, gpu_mem_req
    ctypes.c_double, ctypes.c_double,  # iops_req, net_req
]
lib.calculate_score_6d.restype = ctypes.c_double

# Legacy 2D (backward compatible)
lib.calculate_score.argtypes = [ctypes.c_double, ctypes.c_double, ctypes.c_double, ctypes.c_double]
lib.calculate_score.restype = ctypes.c_double


def get_rust_score_6d(cpu_free, ram_free, gpu_core_free, gpu_mem_free, iops_free, net_free,
                       cpu_req, ram_req, gpu_core_req, gpu_mem_req, iops_req, net_req):
    """6D scoring: CPU, RAM, GPU Core, GPU Memory, IOPS, Network."""
    return lib.calculate_score_6d(cpu_free, ram_free, gpu_core_free, gpu_mem_free, iops_free, net_free,
                                   cpu_req, ram_req, gpu_core_req, gpu_mem_req, iops_req, net_req)


def get_rust_score(cpu_free, ram_free, cpu_req, ram_req):
    """Legacy 2D scoring (backward compatible)."""
    return lib.calculate_score(cpu_free, ram_free, cpu_req, ram_req)


if __name__ == "__main__":
    print(f"✅ Bridge Loaded: {lib_path}")
    print(f"🧠 2D Score: {get_rust_score(0.5, 0.5, 0.1, 0.1):.2f}")
    print(f"🧠 6D Score: {get_rust_score_6d(0.5, 0.5, 0.5, 0.5, 0.5, 0.5, 0.1, 0.1, 0.05, 0.05, 0.05, 0.05):.2f}")
    print(f"🧠 GPU imbalanced: {get_rust_score_6d(0.5, 0.5, 0.1, 0.9, 0.5, 0.5, 0.05, 0.05, 0.08, 0.05, 0.05, 0.05):.2f}")
