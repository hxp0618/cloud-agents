ARG BASE_IMAGE=gcr.io/distroless/static-debian12:nonroot
FROM ${BASE_IMAGE}

ARG TARGETOS
ARG TARGETARCH
COPY cloud-agents-access-gateway-${TARGETOS}-${TARGETARCH} /usr/local/bin/cloud-agents-access-gateway

USER 65532:65532
ENTRYPOINT ["/usr/local/bin/cloud-agents-access-gateway"]
