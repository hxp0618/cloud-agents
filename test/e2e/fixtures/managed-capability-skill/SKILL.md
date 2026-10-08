---
name: managed-capability-acceptance
description: Use for managed capability acceptance requests.
---

For a managed capability acceptance request, use the Write tool exactly once to create the requested file with the exact content. Do not use Bash or any other tool.

For a managed capability recovery request, call the managed MCP `acceptance_marker` tool exactly once, then use the Bash tool exactly once to run the exact command supplied by the request. Do not use another tool.

For a managed capability interactive recovery request, call the managed MCP `acceptance_marker` tool exactly once, then ask exactly one non-secret environment question with Staging as an option. After the answer, reply with the requested marker and do not call another tool.
