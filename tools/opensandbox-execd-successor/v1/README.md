# OpenSandbox execd PTY terminal successor v1

This directory is the complete, reviewable input for a local successor build of OpenSandbox `execd`. The full Foundation qualification and `--remote-worker-only` build this recipe by default and use its immutable local image ID. Other focused modes retain their existing execd pin until separately qualified. This recipe does not publish an image.

The editable authority is `source.json`. It binds the official upstream repository, immutable commit and Git tree, the reviewed patch bytes, copied Apache-2.0 license, Go builder image digest, target platform and the existing runtime base image digest. The runtime base supplies the existing entrypoint and runtime assets only. Its exact build source is not known, so this recipe does not claim that it was built from the bound upstream commit.

The patch makes terminal sessions start once, exposes a completed exit code through the existing status and WebSocket paths, orders output before the exit frame, rejects incomplete output as a runtime error, and bounds cleanup of descendants that inherit terminal descriptors. Its tests cover exit 0/7, reconnect order, PTY and pipe tail integrity, takeover, atomic terminal attachment and descendant cleanup.

Run the repository builder with a new external output directory:

```sh
mise exec -- zsh -lc 'node scripts/build-opensandbox-execd-successor.ts \
  --output-root /absolute/external/output \
  --tag cloud-agents/opensandbox-execd-pty-exit-successor:v1'
```

The builder fetches only the bound commit into the output root, verifies its commit and tree before applying the hash-bound patch, builds with the pinned Go container, extracts the final binary, and writes `build-evidence.json`. The evidence contains the immutable local image ID; use that ID for isolated qualification. A successful local build or qualification does not publish an image or prove persistence across an execd process restart.
