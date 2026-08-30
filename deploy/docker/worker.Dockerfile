ARG BASE_IMAGE=node:24.18.1-bookworm-slim
FROM ${BASE_IMAGE}

ARG TARGETOS
ARG TARGETARCH
COPY cloud-agents-worker-${TARGETOS}-${TARGETARCH} /usr/local/bin/cloud-agents-worker
COPY cloud-agent-runtime-standalone.mjs /usr/local/bin/cloud-agent-runtime
RUN npm install --global --ignore-scripts --omit=dev --no-audit --no-fund @openai/codex@0.150.1 \
    && npm cache clean --force \
    && chmod 0555 /usr/local/bin/cloud-agents-worker /usr/local/bin/cloud-agent-runtime

USER 1000:1000
ENTRYPOINT ["/usr/local/bin/cloud-agents-worker"]
