ARG BASE_IMAGE=postgres:17.6-bookworm
FROM ${BASE_IMAGE}

ARG TARGETOS
ARG TARGETARCH
COPY cloud-agents-product-migrate-${TARGETOS}-${TARGETARCH} /usr/local/bin/cloud-agents-product-migrate
COPY cloud-agents-migrations-000029.tar /opt/cloud-agents/cloud-agents-migrations-000029.tar
RUN mkdir -p /opt/cloud-agents/migrations \
    && tar -xf /opt/cloud-agents/cloud-agents-migrations-000029.tar -C /opt/cloud-agents/migrations \
    && rm /opt/cloud-agents/cloud-agents-migrations-000029.tar \
    && chmod 0555 /usr/local/bin/cloud-agents-product-migrate

USER 999:999
ENTRYPOINT ["/usr/local/bin/cloud-agents-product-migrate"]
CMD ["--repository-root", "/opt/cloud-agents/migrations", "--manifest", "services/control-plane/migrations/product/000029/manifest.json"]
