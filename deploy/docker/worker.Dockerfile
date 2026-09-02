ARG BASE_IMAGE=node:24.18.1-bookworm-slim
FROM ${BASE_IMAGE}

ARG TARGETOS
ARG TARGETARCH
COPY cloud-agents-worker-${TARGETOS}-${TARGETARCH} /usr/local/bin/cloud-agents-worker
COPY cloud-agent-runtime-standalone.mjs /usr/local/bin/cloud-agent-runtime
RUN apt-get update \
    && apt-get install --no-install-recommends --yes ca-certificates \
    && rm -rf /var/lib/apt/lists/* \
    && case "${TARGETARCH}" in \
        amd64) claude_arch=x64 ;; \
        arm64) claude_arch=arm64 ;; \
        *) echo "unsupported Worker architecture: ${TARGETARCH}" >&2; exit 1 ;; \
    esac \
    && npm install --global --ignore-scripts --omit=dev --no-audit --no-fund \
        @openai/codex@0.150.1 \
        "@anthropic-ai/claude-agent-sdk-linux-${claude_arch}@0.3.207" \
    && ln -s "/usr/local/lib/node_modules/@anthropic-ai/claude-agent-sdk-linux-${claude_arch}/claude" /usr/local/bin/claude \
    && test "$(claude --version)" = "2.1.207 (Claude Code)" \
    && npm cache clean --force \
    && mkdir -p /workspace \
    && chown 1000:1000 /workspace \
    && chmod 0700 /workspace \
    && chmod 0555 /usr/local/bin/cloud-agents-worker /usr/local/bin/cloud-agent-runtime

USER 1000:1000
ENTRYPOINT ["/usr/local/bin/cloud-agents-worker"]
