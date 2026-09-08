ARG BASE_IMAGE=postgres:17.6-bookworm
FROM ${BASE_IMAGE}

ARG TARGETOS
ARG TARGETARCH
COPY cloud-agents-product-migrate-${TARGETOS}-${TARGETARCH} /usr/local/bin/cloud-agents-product-migrate
COPY @PLATFORM_MIGRATION_ARCHIVE@ /opt/cloud-agents/@PLATFORM_MIGRATION_ARCHIVE@
RUN mkdir -p /opt/cloud-agents/migrations \
	&& tar -xf /opt/cloud-agents/@PLATFORM_MIGRATION_ARCHIVE@ -C /opt/cloud-agents/migrations \
	&& rm /opt/cloud-agents/@PLATFORM_MIGRATION_ARCHIVE@ \
    && chmod 0555 /usr/local/bin/cloud-agents-product-migrate

USER 999:999
ENTRYPOINT ["/usr/local/bin/cloud-agents-product-migrate"]
CMD ["--repository-root", "/opt/cloud-agents/migrations", "--manifest", "@PLATFORM_MIGRATION_MANIFEST@"]
