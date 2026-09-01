# `@cloud-agents/cloud-agent-platform-sdk`

This directory builds the generated TypeScript client for the independent Cloud
Agents Control Plane. The workspace package stays private to prevent accidental
registry publication; platform releases contain an installable public package
with the release version.

The current output contains generated common identity models, generated JSON
contract models and clients, plus generated proto3 messages and ConnectRPC
service descriptors. The JSON clients include `createHTTPClient(baseURL,
bearerToken)` from `@cloud-agents/cloud-agent-platform-sdk/platform`; it uses the
native `fetch`, requires HTTPS except for literal loopback addresses, refuses
redirects, and limits JSON response bodies to 2 MiB and managed Artifact
downloads to 16 MiB. Injected transports remain available for tests and custom
environments.

The public package contains only `dist`, package metadata, notices, and this
README. Internal generation manifests and their per-file provenance are not
consumer artifacts.
