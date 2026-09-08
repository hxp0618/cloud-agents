# BASE-M5 Admin visual and accessibility acceptance

Run from the repository root with a new output directory:

```sh
FOUNDATION_FULL_ADMIN_CAPTURE=1 \
CLOUD_AGENTS_FOUNDATION_BROWSER_PYTHON="$(command -v node)" \
CLOUD_AGENTS_FOUNDATION_BROWSER_SCRIPT="$PWD/scripts/test-platform-compose-admin-web.mjs" \
CLOUD_AGENTS_FOUNDATION_BROWSER_OUTPUT="$PWD/docs/plan/cloud-agents-platform/evidence/base-m5-admin-acceptance-docker-NEW" \
node scripts/test-foundation-controller-docker.mjs \
  "$PWD/docs/plan/cloud-agents-platform/evidence/base-m5-admin-acceptance-docker-NEW" \
  --snapshot-cleanup-only
```

The recorded run used source HEAD `0893895dbc4e4ac5fa09a3ddf08b457ba9a32f1a`, product migration `000088`, OrbStack Docker `29.4.0`, PostgreSQL `17.6`, and Chromium `152.0.7977.83`.

- `evidence.json` SHA-256: `bb9dcc85546a27a5df1016e9351315564abe2579a8f79be36fbce4820b7af108`
- `admin-acceptance/browser-evidence.json` SHA-256: `92802fc19ea4043dd8a4137e0ae6d184f8df08d3dfef8c0da768aa9f4f9bdd77`
- The browser evidence records hashes for all 145 generated screenshots. This directory retains the 24 core locale/theme/viewport captures plus representative navigation, filter, confirmation, error, Toast, permission, Overview, Runtime Profile, and Sandbox states.

The run proves the local Docker/PostgreSQL/Admin Web boundary stated in `evidence.json`; it does not prove Kubernetes/SSH Snapshot backends or aggregate BASE-ADMIN-V1 infrastructure acceptance.
