# Anchor Infer runtime images (llama.cpp server)
#
# Tags produced (match agent/internal/platform selectBuild):
#   :x86_64 | :arm64 | :arm64-dotprod | :arm64-i8mm | :arm64-i8mm-sve | :arm64-sve2-i8mm
#
# Build (from repo root):
#   ./infer/docker/build.sh ghcr.io/OWNER/anchor-infer
# Push (requires docker login to GHCR):
#   ./infer/docker/build.sh ghcr.io/OWNER/anchor-infer --push
#
# Point the agent at published images:
#   export ANCHOR_INFER_IMAGE_BASE=ghcr.io/OWNER/anchor-infer

ARG LLAMA_CPP_REF=b4689
FROM ubuntu:24.04 AS build

ARG LLAMA_CPP_REF
ARG CMAKE_ARGS="-DGGML_NATIVE=OFF -DLLAMA_BUILD_SERVER=ON -DLLAMA_CURL=OFF"
ARG TARGETARCH

RUN apt-get update && DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends \
      build-essential cmake git ca-certificates curl \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /src
RUN git clone --depth 1 --branch "${LLAMA_CPP_REF}" https://github.com/ggml-org/llama.cpp.git . \
    || (git clone --depth 1 https://github.com/ggml-org/llama.cpp.git . && git checkout "${LLAMA_CPP_REF}")

# KleidiAI on arm64; generic CPU path otherwise.
RUN if [ "$TARGETARCH" = "arm64" ]; then \
      cmake -B build ${CMAKE_ARGS} -DGGML_CPU_KLEIDIAI=ON && \
      cmake --build build --config Release -j"$(nproc)" --target llama-server llama-bench; \
    else \
      cmake -B build ${CMAKE_ARGS} && \
      cmake --build build --config Release -j"$(nproc)" --target llama-server llama-bench; \
    fi

FROM ubuntu:24.04 AS runtime
RUN apt-get update && DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends \
      ca-certificates libgomp1 curl \
    && rm -rf /var/lib/apt/lists/*
COPY --from=build /src/build/bin/llama-server /usr/local/bin/llama-server
COPY --from=build /src/build/bin/llama-bench /usr/local/bin/llama-bench
EXPOSE 8080
ENTRYPOINT ["llama-server"]
CMD ["--host", "0.0.0.0", "--port", "8080"]
