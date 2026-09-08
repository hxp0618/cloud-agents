ARG BASE_IMAGE=node:24.18.1-bookworm-slim
FROM ${BASE_IMAGE}

COPY admin-web /opt/cloud-agents/admin-web

USER 1000:1000
ENTRYPOINT ["node", "/opt/cloud-agents/admin-web/server.mjs"]
