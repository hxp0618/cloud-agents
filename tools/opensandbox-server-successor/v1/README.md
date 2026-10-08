# OpenSandbox server Cookie isolation successor v1

This directory is the complete review input for the local linux/amd64 and linux/arm64 successors of the fixed OpenSandbox server image. It prevents the process-scoped `httpx.AsyncClient` cookie jar from carrying a response cookie from one Sandbox into another Sandbox request. The proxy still forwards an exact Cookie supplied by endpoint authority. Caller Cookie, authorization, forwarding and OpenSandbox API-key headers keep their existing filtering behavior.

`source.json` binds the immutable multi-platform base index and each qualified platform child digest, the exact `proxy.py` bytes extracted from those children, the patch, expected patched bytes, [Cookie isolation regression](../../../../test/scripts/cookie-isolation-regression.py), Dockerfile and copied Apache-2.0 license. The extracted file exactly matches the recorded file at official OpenSandbox commit `f31696b429b2b67c197b9336583a78e114d7d36a`; this is only a file-level match. The complete base-image build source remains unknown.

Run the builder with a new external output directory and a disposable local tag:

```sh
mise exec -- zsh -lc 'node scripts/build-opensandbox-server-successor.ts \
  --output-root /absolute/external/output \
  --tag cloud-agents/opensandbox-server-cookie-isolation-successor:v1'
```

Without `--platform`, the builder selects the platform reported by the Docker daemon. An explicit qualified platform can be selected with `--platform linux/amd64` or `--platform linux/arm64`. The matching child digest must already be present in the local Docker daemon.

The builder never pulls. It extracts and verifies the original source from the selected immutable child, applies the hash-bound patch, runs the cross-Sandbox CookieJar regression inside the derived image build, verifies the base and output platform and resulting bytes, and writes platform-bound `build-evidence.json`. It does not push or publish an image. A successful build is bounded to the selected platform; full Foundation qualification remains a separate consumer of the immutable local image ID.
