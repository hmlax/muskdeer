# Typed 策略参数

P03.5 以 [参数 Proto](../deer/proto/common/v1/strategy_parameters.proto) 定义做市/网格字段及 oneof，当前参数 schema=1。数字小数全部为 string，整数档数及整毫秒时间分别使用 int32/int64；未知字段、空模板、legacy schema=0 和未来 schema 被业务边界拒绝。

`internal/strategyconfig` 显式映射字段为纯 domain 值，使用 ParseDecimal，不经过浮点、JSON/map 转换。金额、数量和价格限制为 20 位整数+18 位小数；BPS 为 5 位整数+10 位小数。时间转换先检查 0..一小时，实际启用的间隔/时效要求至少 1ms，避免乘法溢出。下列是计算上界及交叉校验，不是容量/SLA 或生产参数推荐。

| 参数组 | 单位与校验 |
|---|---|
| 做市档位/价差 | quote_levels 1..20；spread/reprice BPS>0 且<10000；多档 spacing>0，单档可为0；最远买档仍须保持正价格 |
| 做市资金 | level_notional、side_budget 为 quote；side_budget 至少覆盖本侧全部档位的名义金额，精确到最小输入小数位 |
| 库存 | target/soft/hard 为 base；target≥0、soft>0、hard≥soft 且 target≤hard |
| 刷新/质量 | min_refresh_interval≤max_quote_age；所有启用的刷新/年龄在 1ms..1h；实际所选行情完整性/健康仍另行验证 |
| 对冲 | 显式 hedge_enabled；开启时 threshold>0 且≤hard、backlog≥threshold、等待为正间隔；关闭时 threshold/backlog 明确传 string "0"、等待0 |
| 网格 | lower/upper 为 quote/base 且0<lower<upper；grid_count 2..200；per_order_notional/total_budget 为 quote，预算覆盖全部格位；inventory_limit 为正 base |

账户共享预算、真实可用资金、instrument tick/lot/min_notional、当前行情质量与执行授权仍由后续激活/执行流程检查。参数验证通过不代表已获下单许可。

## 持久化

StrategyConfigVersion 的 maker_parameters/grid_parameters 是具体 Proto message 字段，直接由 gotag 生成 GORM `serializer:json`/JSONB，使用固定 GORM 的原生 typed serializer。JSON 只发生在实际存储边界，保留价格、金额等十进制字符串，不使用手写 PO、无类型 payload 或普通对象间 JSON 复制。

迁移版本 3 增加 parameters_schema_version、template 和两种 typed 参数；数据库检查模板只能与实例匹配、只存在对应的一组参数、schema=1、已知键及必需小数字段必须是 JSON string。旧版本 2 的 metadata-only 行保留 schema=0/NULL 参数，Decode 拒绝将其作为可计算配置；后续控制 API 须显式提交完整 schema=1 版本，不用隐式填值让旧行运行。

实际 PostgreSQL 直接往返生成 Model，使用 proto.Equal 验证两模板全部字段及高精度预算保持；错误模板、空/双参数、未来 schema、未知 JSON 键和 JSON number 小数均拒绝。验证见 [P03.5](validation/P03-05.md)。策略 API、算法、共享风险与真实交易仍按后续任务交付。
