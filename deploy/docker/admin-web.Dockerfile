ARG BASE_IMAGE=node:24.18.1-bookworm-slim@sha256:235600a8101ab264e117b1768e925532262668dc9b581ef1dd7d96ced463b8e7
FROM ${BASE_IMAGE}

COPY web/server.mjs /opt/cloud-agents/web/server.mjs
COPY web/platform.mjs /opt/cloud-agents/web/platform.mjs
COPY web/index.mjs /opt/cloud-agents/web/index.mjs
COPY admin-web/dist /opt/cloud-agents/web/dist

ENV CLOUD_AGENTS_WEB_SCOPE=admin CLOUD_AGENTS_WEB_PORT=4174
USER 1000:1000
ENTRYPOINT ["node", "/opt/cloud-agents/web/server.mjs"]
