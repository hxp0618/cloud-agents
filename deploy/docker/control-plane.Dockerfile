ARG BASE_IMAGE=gcr.io/distroless/static-debian12:nonroot
FROM ${BASE_IMAGE}

ARG TARGET=linux-amd64
COPY cloud-agents-control-plane-${TARGET} /usr/local/bin/cloud-agents-control-plane

USER nonroot:nonroot
ENTRYPOINT ["/usr/local/bin/cloud-agents-control-plane"]
