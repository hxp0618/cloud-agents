ARG BASE_IMAGE=scratch

FROM ${BASE_IMAGE} AS verified
COPY server/opensandbox_server/api/proxy.py /app/opensandbox_server/api/proxy.py
COPY cookie-isolation-regression.py /tmp/cookie-isolation-regression.py
RUN /app/.venv/bin/python /tmp/cookie-isolation-regression.py

FROM ${BASE_IMAGE}
ARG BASE_IMAGE
ARG ORIGINAL_PROXY_SHA256
ARG PATCH_SHA256
ARG PATCHED_PROXY_SHA256
LABEL org.opencontainers.image.source="https://github.com/alibaba/OpenSandbox" \
      org.opencontainers.image.licenses="Apache-2.0" \
      cloud-agents.opensandbox-server.base-image="${BASE_IMAGE}" \
      cloud-agents.opensandbox-server.base-build-source-status="UNKNOWN_BUILD_SOURCE" \
      cloud-agents.opensandbox-server.original-proxy-sha256="${ORIGINAL_PROXY_SHA256}" \
      cloud-agents.opensandbox-server.patch-sha256="${PATCH_SHA256}" \
      cloud-agents.opensandbox-server.patched-proxy-sha256="${PATCHED_PROXY_SHA256}"
COPY --from=verified /app/opensandbox_server/api/proxy.py /app/opensandbox_server/api/proxy.py
COPY LICENSE /usr/share/licenses/opensandbox/LICENSE
