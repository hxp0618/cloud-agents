# `@synara/cloud-agent-platform-sdk`

This directory builds the generated TypeScript client for the independent Cloud
Agents Control Plane. The workspace package stays private to prevent accidental
registry publication; platform releases contain an installable public package
with the release version.

The current output contains generated common identity models, generated JSON
contract models, injected transports, plus generated proto3 messages and
ConnectRPC service descriptors. The Go JSON SDK also provides a standard
library `NewHTTPClient` for the independent Control Plane's production HTTP
surface; the SDK does not register server routes or provide provider operations,
database writes, deployment, publication, or Gate authority.

The public package contains only `dist`, package metadata, notices, and this
README. Internal generation manifests and their per-file provenance are not
consumer artifacts.
