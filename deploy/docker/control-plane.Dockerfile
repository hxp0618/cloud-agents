ARG BASE_IMAGE=gcr.io/distroless/static-debian12:nonroot
FROM ${BASE_IMAGE}

ARG TARGETOS
ARG TARGETARCH
COPY cloud-agents-control-plane-${TARGETOS}-${TARGETARCH} /usr/local/bin/cloud-agents-control-plane

USER nonroot:nonroot
ENTRYPOINT ["/usr/local/bin/cloud-agents-control-plane"]
