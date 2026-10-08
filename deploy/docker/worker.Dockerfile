ARG BASE_IMAGE=node:24.18.1-bookworm-slim@sha256:235600a8101ab264e117b1768e925532262668dc9b581ef1dd7d96ced463b8e7
FROM ${BASE_IMAGE} AS lock-inputs

ADD cloud-agents-deployment-*.tar /deployment/

FROM ${BASE_IMAGE}

ARG TARGETOS
ARG TARGETARCH
ARG DEBIAN_MIRROR=http://deb.debian.org/debian
ARG DEBIAN_SECURITY_MIRROR=http://deb.debian.org/debian-security
COPY cloud-agents-worker-${TARGETOS}-${TARGETARCH} /usr/local/bin/cloud-agents-worker
COPY cloud-agent-runtime-standalone.mjs /usr/local/bin/cloud-agent-runtime
COPY cloud-agents-landlock-run-${TARGETOS}-${TARGETARCH} /usr/local/bin/cloud-agents-landlock-run
COPY cloud-agents-landlock-notices.txt /usr/share/doc/cloud-agents/landlock-notices.txt
COPY --from=lock-inputs /deployment/deploy/docker/worker-tools/package.json /opt/cloud-agents-worker-tools/package.json
COPY --from=lock-inputs /deployment/deploy/docker/worker-tools/package-lock.json /opt/cloud-agents-worker-tools/package-lock.json
ENV CLOUD_AGENT_DEEPSEEK_HARNESS_BIN=/usr/local/bin/dsh
ENV NPM_CONFIG_REGISTRY=https://registry.npmmirror.com/
RUN sed -i \
        -e "s#http://deb.debian.org/debian-security#${DEBIAN_SECURITY_MIRROR}#g" \
        -e "s#http://deb.debian.org/debian#${DEBIAN_MIRROR}#g" \
        /etc/apt/sources.list.d/debian.sources \
    && apt-get update \
    && apt-get install --no-install-recommends --yes ca-certificates \
    && rm -rf /var/lib/apt/lists/* \
    && case "${TARGETARCH}" in \
        amd64) claude_arch=x64; other_claude_arch=arm64 ;; \
        arm64) claude_arch=arm64; other_claude_arch=x64 ;; \
        *) echo "unsupported Worker architecture: ${TARGETARCH}" >&2; exit 1 ;; \
    esac \
    && npm ci --prefix /opt/cloud-agents-worker-tools --ignore-scripts --omit=dev --no-audit --no-fund \
    && test -x "/opt/cloud-agents-worker-tools/node_modules/@anthropic-ai/claude-agent-sdk-linux-${claude_arch}/claude" \
    && test ! -e "/opt/cloud-agents-worker-tools/node_modules/@anthropic-ai/claude-agent-sdk-linux-${other_claude_arch}" \
    && ln -s /opt/cloud-agents-worker-tools/node_modules/.bin/codex /usr/local/bin/codex \
    && ln -s /opt/cloud-agents-worker-tools/node_modules/.bin/dsh /usr/local/bin/dsh \
    && ln -s "/opt/cloud-agents-worker-tools/node_modules/@anthropic-ai/claude-agent-sdk-linux-${claude_arch}/claude" /usr/local/bin/claude \
    && test "$(codex --version)" = "codex-cli 0.154.0" \
    && test "$(claude --version)" = "2.1.207 (Claude Code)" \
    && test "$(dsh --version)" = "0.1.2-rc.1" \
    && node -e "require('/opt/cloud-agents-worker-tools/node_modules/node-pty')" \
    && npm cache clean --force \
    && mkdir -p /workspace \
    && chown 1000:1000 /workspace \
    && chmod 0700 /workspace \
    && chmod 0555 /usr/local/bin/cloud-agents-worker /usr/local/bin/cloud-agent-runtime /usr/local/bin/cloud-agents-landlock-run

COPY cloud-agents-worker-oci-install-manifest.json /usr/share/doc/cloud-agents/worker-oci-install-manifest.json
COPY cloud-agents-worker-oci-notices.md /usr/share/doc/cloud-agents/worker-oci-notices.md
COPY --from=lock-inputs /deployment/scripts/lib/worker-oci-installed.ts /usr/share/doc/cloud-agents/worker-oci-installed.ts
COPY --from=lock-inputs /deployment/deploy/docker/worker-oci-supplemental-licenses/ /usr/share/doc/cloud-agents/worker-oci-supplemental-licenses/
RUN node /usr/share/doc/cloud-agents/worker-oci-installed.ts "linux/${TARGETARCH}" > /usr/share/doc/cloud-agents/worker-oci-installed-inventory.json

USER 1000:1000
ENTRYPOINT ["/usr/local/bin/cloud-agents-worker"]
