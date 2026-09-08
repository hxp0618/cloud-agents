ARG BASE_IMAGE=node:24.18.1-bookworm-slim
FROM ${BASE_IMAGE}

COPY web/server.mjs /opt/cloud-agents/web/server.mjs
COPY user-web/dist /opt/cloud-agents/web/dist

ENV CLOUD_AGENTS_WEB_SCOPE=user CLOUD_AGENTS_WEB_PORT=4173
USER 1000:1000
ENTRYPOINT ["node", "/opt/cloud-agents/web/server.mjs"]
