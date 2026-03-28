import ctypes
import os
import glob
import sys

# Smart path resolution — works on Mac (local dev) AND inside Docker
base_dir = os.path.dirname(os.path.abspath(__file__))
possible_patterns = [
    os.path.join(base_dir, "../rust_brain/target/release/liblambda_g_math.*"),  # Docker: /app/coherence_engine/../rust_brain/
    os.path.join(base_dir, "rust_brain/target/release/liblambda_g_math.*"),     # Local: if run from coherence_engine/
    "rust_brain/target/release/liblambda_g_math.*",                              # Local: if run from project root
    "/app/rust_brain/target/release/liblambda_g_math.*",                         # Docker: absolute path
    os.path.join(base_dir, "bin/liblambda_g_math.*"),                            # Legacy: copied to bin/
]

lib_path = None
for pattern in possible_patterns:
    matches = glob.glob(pattern)
    if matches:
        lib_path = matches[0]
        break

if not lib_path:
    print("❌ Error: Lambda-G Math library not found!")
    print(f"  Searched from: {base_dir}")
    sys.exit(1)

lib = ctypes.CDLL(os.path.abspath(lib_path))
lib.calculate_score.argtypes = [ctypes.c_double, ctypes.c_double, ctypes.c_double, ctypes.c_double]
lib.calculate_score.restype = ctypes.c_double

def get_rust_score(cpu_free, ram_free, cpu_req, ram_req):
    return lib.calculate_score(cpu_free, ram_free, cpu_req, ram_req)

if __name__ == "__main__":
    print(f"✅ Bridge Loaded: {lib_path}")
    print(f"🧠 Test Score: {get_rust_score(0.5, 0.5, 0.1, 0.1):.2f}")
