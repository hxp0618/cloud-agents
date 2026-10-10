ARG BASE_IMAGE=gcr.io/distroless/static-debian12:nonroot@sha256:afa5c872c891853ca7fcf1f12c3edb23f7eeef36189728842dd51042ff57f7ab
FROM ${BASE_IMAGE}

ARG TARGETOS
ARG TARGETARCH
COPY cloud-agents-identity-${TARGETOS}-${TARGETARCH} /usr/local/bin/cloud-agents-identity

USER 65532:65532
ENTRYPOINT ["/usr/local/bin/cloud-agents-identity"]
