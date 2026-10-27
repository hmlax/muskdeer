# 行情协议与独立订单簿

协议核对日期：2026-10-27。首批只选择 binance、okx，候选仍不启用；外部 symbol 由调用方显式配置，Frame 的 source/instrument/symbol 必须与目标 Book 完全一致。不匹配事件不会写入另一个来源的订单簿。

## P05.1 序列与深度

Binance 现货使用 REST 快照锚点与 `U/u` 增量范围：快照安装后为 SYNCHRONIZING，不输出价格；忽略已覆盖的增量，下一有效范围必须覆盖当前序列+1，再进入 READY。缺口使当前簿 INVALID，必须重新获取快照并衔接，不能继续使用旧价格。依据 [官方 Spot Streams](https://github.com/binance/binance-spot-api-docs/blob/master/web-socket-streams.md#how-to-manage-a-local-order-book-correctly)。

OKX `books` 快照以 prevSeqId=-1 建立完整簿，更新的 prevSeqId 匹配上一 seqId。空更新允许相同序列；内容相同的最后一个重复事件不再次应用或刷新数据时间。维护重置仅在前序仍匹配时接受 seqId 回退，同时增加本地 generation；其他乱序/缺口失效。依据 [官方 Sequence ID 说明](https://tr.okx.com/docs-v5/en/#order-book-trading-market-data-ws-order-book-channel)。

OKX 的 JSON books checksum 已于2026-06-23停用，字段固定为0，不能再用于完整性判定；本实现使用序列链，网络接入必须使用 TLS。依据 [官方弃用公告](https://www.okx.com/en-sg/help/okx-order-book-channels-checksum-field-deprecation)。

深度价格/数量使用 decimal，更新数量0删除对应档位；同一侧重复的规范化价格、零快照数量、空一侧、交叉簿和超过容量都拒绝。每侧最多5000档，超过界限不静默截断；使用新的完整快照恢复。本地 generation 标识簿重建/序列重置，不能作为跨进程所有权或执行 fencing。

Book 属于单个 actor，调用者串行操作；输入切片不保留，内部深度 map 不外露。Top 只返回同步且形态有效的 BBO、来源、时间与序列，mid 使用显式 QuoRem 保留半个最小价格单位；仍须经过后续新鲜度/离群质量与风险校验才能驱动报价。

## 当前边界

P05.1 仅提供有界 typed Frame 与协议状态机，回放覆盖恢复、重复、删档、来源隔离及精确参考价。尚未把实际 JSON、WebSocket/REST、持久质量或策略选择装配进运行服务。A04 的接入回放、A05/A24 的策略保护和 A25 的真实网络证据继续后续步骤；不以当前单元测试声明两家实连完成。
