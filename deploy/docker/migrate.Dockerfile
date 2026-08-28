ARG BASE_IMAGE=postgres:17.6-bookworm
FROM ${BASE_IMAGE}

ARG TARGET=linux-amd64
COPY cloud-agents-product-migrate-${TARGET} /usr/local/bin/cloud-agents-product-migrate
COPY cloud-agents-migrations-000016.tar /opt/cloud-agents/cloud-agents-migrations-000016.tar
RUN mkdir -p /opt/cloud-agents/migrations \
    && tar -xf /opt/cloud-agents/cloud-agents-migrations-000016.tar -C /opt/cloud-agents/migrations \
    && rm /opt/cloud-agents/cloud-agents-migrations-000016.tar \
    && chmod 0555 /usr/local/bin/cloud-agents-product-migrate

ENTRYPOINT ["/usr/local/bin/cloud-agents-product-migrate"]
CMD ["--repository-root", "/opt/cloud-agents/migrations", "--manifest", "services/control-plane/migrations/product/000016/manifest.json"]
