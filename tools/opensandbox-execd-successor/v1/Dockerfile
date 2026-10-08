ARG GO_IMAGE=scratch
ARG RUNTIME_BASE_IMAGE=scratch

FROM ${GO_IMAGE} AS builder
ARG EXPECTED_GO_VERSION
ARG GOPROXY
ARG GOSUMDB
ARG SOURCE_COMMIT
ARG BUILD_VERSION
ARG BUILD_TIME
ENV GOPROXY=${GOPROXY} GOSUMDB=${GOSUMDB} GOTOOLCHAIN=local
WORKDIR /src
COPY components/internal ./components/internal
COPY components/execd ./components/execd
COPY LICENSE /out/LICENSE
RUN test "$(go version)" = "go version ${EXPECTED_GO_VERSION} linux/arm64"
WORKDIR /src/components/execd
RUN CGO_ENABLED=0 go test ./pkg/runtime ./pkg/web/controller -count=1
RUN CGO_ENABLED=1 go test -race ./pkg/runtime ./pkg/web/controller \
    -run 'TestPTY(Session_(TerminalDoneFollowsReplayDrain|TerminalAttachHasClosedLiveSinks|TerminalCleanupKillsDescendants|OutputDrainTimeoutReportsIncomplete)|WS_(TerminalReconnectReplaysExactExitWithoutRestart|TerminalReconnectReplaysCompleteTail|ReplayOnReconnect|TakeoverEvictsHolder))$' \
    -count=1
RUN CGO_ENABLED=0 go vet ./pkg/runtime ./pkg/web/controller
RUN GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go test -c -o /tmp/runtime-windows.test.exe ./pkg/runtime && \
    GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go test -c -o /tmp/controller-windows.test.exe ./pkg/web/controller
RUN CGO_ENABLED=0 go build -trimpath -buildvcs=false \
    -ldflags "-buildid= -B none -X github.com/alibaba/opensandbox/internal/version.Version=${BUILD_VERSION} -X github.com/alibaba/opensandbox/internal/version.BuildTime=${BUILD_TIME} -X github.com/alibaba/opensandbox/internal/version.GitCommit=${SOURCE_COMMIT}" \
    -o /out/execd ./main.go && \
    touch -d "${BUILD_TIME}" /out/execd /out/LICENSE

FROM ${RUNTIME_BASE_IMAGE}
ARG SOURCE_COMMIT
ARG SOURCE_TREE
ARG PATCH_SHA256
ARG BUILDER_IMAGE
ARG RUNTIME_BASE_IMAGE
LABEL org.opencontainers.image.source="https://github.com/alibaba/OpenSandbox" \
      org.opencontainers.image.revision="${SOURCE_COMMIT}" \
      org.opencontainers.image.licenses="Apache-2.0" \
      cloud-agents.execd.source-tree="${SOURCE_TREE}" \
      cloud-agents.execd.patch-sha256="${PATCH_SHA256}" \
      cloud-agents.execd.builder-image="${BUILDER_IMAGE}" \
      cloud-agents.execd.runtime-base="${RUNTIME_BASE_IMAGE}" \
      cloud-agents.execd.runtime-base-source-status="UNKNOWN_BUILD_SOURCE"
COPY --from=builder /out/execd /execd
COPY --from=builder /out/LICENSE /usr/share/licenses/opensandbox/LICENSE
