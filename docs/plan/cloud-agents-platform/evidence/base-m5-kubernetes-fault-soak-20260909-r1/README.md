# BASE-M5 Kubernetes fault and bounded soak evidence

Run from `/Users/huang/devel/project/huang/business/cloud-agents`:

```sh
node scripts/test-foundation-controller-kubernetes.mjs \
  docs/plan/cloud-agents-platform/evidence/base-m5-kubernetes-fault-soak-20260909-r1 \
  --fault-soak-only
```

The run used source HEAD `d787916a0f547805dbd565bbbfeb3f638465a926`, product migration `000088`, OrbStack Kubernetes `v1.35.6+orb1`, and PostgreSQL `17.6`.

It ended one Cloud Agents Controller test process after physical Kubernetes creation and before settlement, restarted the OpenSandbox controller Deployment, and deleted the running BatchSandbox Pod. Kubernetes replaced both Pods with new UIDs. A fresh Controller process reaped the expired claim, adopted the same runtime, Operation, and retained PVC, then read back the exact Workspace digest. The measured upper bounds were `27214.302 ms` from Controller-process exit to settlement, `1882.746 ms` for the OpenSandbox controller Pod, and `22239.822 ms` for the workload Pod. No Operation row or Workspace byte was lost.

The recovered process completed 64 cycles of three generated-SDK Admin reads plus one ordinary-user `403`: 256 requests total. Successful read P50/P95 were `1.764/3.069 ms`; denied read P50/P95 were `0.176/0.400 ms`. Process start through claim reap/adoption to the first successful Admin response was `289.872 ms`, and the Admin projection digest stayed constant.

`evidence.json` SHA-256: `52e5a675c5ebae86094289e9f697a3e6648ced7f41b4508af175bedd377e6cf5`. `fault-prepare.log`: `c21c64ad1ef121e106dd4eb8f63b42e1fa6868599c2a5f5b0739f315f0995cac`. `fault-recover.log`: `545c08eae6765f2e90da89ce6d4afbf9566c797275843a0a74c49b5d224327cb`. `opensandbox-controller.log`: `faca033eda485d08f85848bf3451b1e997b0f604c13621874cd64dffab15aac6`. `opensandbox-server.log`: `06947673d1f5be7e9fbfbfc6dd791145f5388c9b5aa06468a10ab1d237233b3d`.

This is a bounded local measurement, not an SLO. It does not cover an external cluster, SSH, write saturation, Kubernetes control-plane/node loss, or multi-Region recovery. The run stopped the Sandbox, removed its BatchSandbox and Pod, then deleted only its UUID namespaces and RBAC resources; no test container, namespace, cluster role, or retained PVC remains.
