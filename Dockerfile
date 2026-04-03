# Lambda-G Controller — Multi-stage build
# Stage 1: Compile Rust brain for Linux x86_64
# Stage 2: Python runtime with controller + compiled .so

# ── Stage 1: Rust compilation ──
FROM rust:1.82-slim AS rust-builder

WORKDIR /build
COPY rust_brain/Cargo.toml rust_brain/Cargo.lock ./
COPY rust_brain/src ./src

# Build shared library
RUN cargo build --release
RUN ls -la target/release/liblambda_g_math* || true

# ── Stage 2: Python runtime ──
FROM python:3.12-slim

WORKDIR /app

# Install Python dependencies
RUN pip install --no-cache-dir kopf kubernetes colorama

# Copy Rust brain (.so for Linux)
COPY --from=rust-builder /build/target/release/liblambda_g_math.so /app/rust_brain/target/release/liblambda_g_math.so
# Fallback if .so doesn't exist (dylib on some platforms)
COPY --from=rust-builder /build/target/release/liblambda_g_math* /app/rust_brain/target/release/

# Copy Python code
COPY coherence_engine/ /app/coherence_engine/

# Set working directory for brain_bridge.py imports
WORKDIR /app/coherence_engine

# Health check
HEALTHCHECK --interval=30s --timeout=5s --retries=3 \
  CMD python -c "import kubernetes; print('ok')" || exit 1

# Labels for AWS Marketplace
LABEL maintainer="Abhishek Srivastava <bitsabhi@gmail.com>"
LABEL description="Lambda-G: Symmetric Exhaustion Scheduler for Kubernetes"
LABEL version="0.2.0"

# Default: run the controller
CMD ["python", "-m", "kopf", "run", "controller.py", "--verbose"]
