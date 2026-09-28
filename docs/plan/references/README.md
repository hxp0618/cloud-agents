# References 与 Contracts 索引

本目录是协议、契约和接口约束的规范来源。文件保留 wire format、字段语义、兼容性和校验规则原文，不压缩成状态摘要，也不记录当前测试结果。当前实际结果见 [06](../cloud-agents-platform/06-status-tracker.md)。

## Contracts

- [provider-host-v2.md](contracts/provider-host-v2.md)
- [runtime-event-v2.md](contracts/runtime-event-v2.md)
- [runtime-event-v2.schema.json](contracts/runtime-event-v2.schema.json)
- [worker-protocol-v1.md](contracts/worker-protocol-v1.md)
- [worker-protocol-v2.md](contracts/worker-protocol-v2.md)

## 阅读边界

- 修改契约前先核对相关 ADR；契约原文和 schema 是接口约束来源。
- 运行证据、独立复核和 Gate 状态按 [evidence 索引](../cloud-agents-platform/evidence/README.md) 访问。
- 本目录不代表部署状态、发布批准或当前平台完成度。
