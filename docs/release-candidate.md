# Release candidate boundary

This page describes the TS Agent Runtime candidate, not completion of the new infrastructure foundation. [BASE-READY](plan/cloud-agents-platform/05-gates-and-acceptance.md) requires independent no-Agent infrastructure evidence; user CloudAgents consumes that foundation later. These Runtime gates remain mandatory for their own candidate scope and are not blanket prerequisites for documentation or each foundation development slice.

The RC versions are `@cloud-agents/cloud-agent-runtime@0.2.0-rc.1` and `0.1.0-rc.1` for the other six packages. All internal edges are exact peer pins so hosts can install the coordinated GitHub tarball closure without an npm publication or an exotic-subdependency override.

The Runtime, Provider, Distribution, and Testkit packages intentionally retain dual ESM/CJS output for the compatibility window. Their build commands allow tsdown's generic "prefer ESM" warning while the external pack/install smoke verifies both formats.

Run the smoke with Node.js 24:

```sh
node scripts/cloud-agent-release-smoke.ts --output-dir candidate
```

The new output directory contains seven package tarballs, `cloud-agent-runtime-standalone.mjs`, `candidate-manifest.json`, `checksums.sha256`, `sbom.spdx.json`, and `provenance.json`. The smoke builds from source, validates package allowlists and licenses, installs each transitive tarball closure into fresh external projects, imports ESM and CommonJS entrypoints, typechecks both NodeNext declaration branches, performs a clean pnpm 11.10 coordinated-peer install, exercises the packed bin, and confirms that artifact bytes did not change during validation.

Deterministic packed-bin coverage includes Protocol 2.2/2.3 negotiation, invalid-frame failure, correlation and generation metadata, bounded multiplexing/backpressure, independent processes, runtime crash observation, and no-tool policy. Client suites cover subscribe-before-command, ordered listener acknowledgement/drain, late terminal tombstones, bounded close/forced stop, ambient-secret exclusion, credential descriptor mapping, and alias conflicts.

The following cannot be honestly proven without a real authenticated provider and remain release gates:

- authenticated StartSession/ResumeSession/StopSession lifecycle;
- provider-originated late terminal behavior after interrupt or crash;
- real provider secret redaction and workspace/artifact path containment;
- sustained provider tool/artifact backpressure;
- end-to-end behavior in each consuming host.

The candidate manifest records those gates. A GitHub RC is an immutable engineering input for host validation, not an npm release, production deployment, public beta, or GA approval.
