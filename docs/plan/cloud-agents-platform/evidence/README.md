# Evidence 索引

第一阶段的可复现公开验收证据见 [docs/acceptance/phase-1.public.md](../../../acceptance/phase-1.public.md)；当前状态只认 [06](../06-status-tracker.md)。

本目录只保留仍被生成锁、closure profile 或 review digest 按路径和字节绑定的冻结记录。文件内容不可修改；其中指向已删除的过程材料的链接保留原样，属于历史事实。完整过程记录已在开源整理前归档，不随公开仓库发布。当前状态只认 [06](../06-status-tracker.md)。

## 冻结 Gate 记录

- [G-BASELINE/CAG-G-BASELINE-P0-20260823-R4.md](G-BASELINE/CAG-G-BASELINE-P0-20260823-R4.md)
- [G-CONTRACT/CAG-G-CONTRACT-P1-20260823-R1.md](G-CONTRACT/CAG-G-CONTRACT-P1-20260823-R1.md)
- [G-CONTRACT/CAG-G-CONTRACT-P1-20260823-R2.md](G-CONTRACT/CAG-G-CONTRACT-P1-20260823-R2.md)
- [G-CONTRACT/CAG-G-CONTRACT-P1-20260823-R3.md](G-CONTRACT/CAG-G-CONTRACT-P1-20260823-R3.md)
- [G-CONTRACT/CAG-G-CONTRACT-P1-20260823-R4-independent-review.md](G-CONTRACT/CAG-G-CONTRACT-P1-20260823-R4-independent-review.md)
- [G-CONTRACT/CAG-G-CONTRACT-P1-20260823-R4.md](G-CONTRACT/CAG-G-CONTRACT-P1-20260823-R4.md)
- [G-INVENTORY/CAG-G-INVENTORY-P0-20260810-R3.md](G-INVENTORY/CAG-G-INVENTORY-P0-20260810-R3.md)

## 记录规则

- 正式 closure record 按 [模板](../templates/gate-closure-record.md) 记录输入范围、结果、未覆盖项和审批。
- 不把原始日志、截图、Secret、数据库 dump 或 pairing/auth material 放入仓库；运行产物留在被忽略的 `.tmp/` 或 CI artifact。
