# P0 R1 immutable byte snapshot

This directory preserves the exact generated inputs referenced by the first P0 phase records before the canonical
working outputs advance. It is evidence history, not the current Gate pointer.

| File                           | SHA-256                                                            |
| ------------------------------ | ------------------------------------------------------------------ |
| `frozen-baseline.json`         | `c28df16a4d47f1ee0764c8ec5ff1465cfa71c2173ef441471672baaff63d9af3` |
| `synara-file-inventory.tsv`    | `f3858852538ef67ec6879a6db101246f7b3bf65ba6301f9e5e9274200d716aa1` |
| `synara-inventory-graph.json`  | `784d5c36babc85e05053450c01c6aa3737274c5d6d46371606b1f7596bdd0e76` |
| `synara-inventory-summary.md`  | `21240cde05da0afc427d61d38848e7af78172107286902865537d2b6b4cc566b` |
| `baseline-characterization.md` | `66a31147187496fbc8c595c977cc8c14b841c5ef20e841c23e310fab0540aeda` |
| `provenance-summary.md`        | `4a20e6dd908110427822d1c1277ddae7720332c724508ca66122cb8f6b2d8dca` |

The R1 tracker entries remain `IN PROGRESS`. A later record must supersede rather than overwrite this snapshot.
