# 开发测试成交策略

用户确认的接口是现有 Go SDK `WashTrade`：只发布开发行情的合成成交和 K 线，不创建订单，也不修改账户、仓位或资金。该数据不进入 Muskdeer 的真实成交、持仓或收益投影。撮合和原 SDK 源码保持不变。

策略位于 `strategy/native`，由现有 strategy 进程拥有；execution 通过 mTLS 内部 RPC 接收意图并重新读取权威来源，最终调用 SDK。`BOOK` 模式保持原买一/卖一定价和序号交替方向；`AGG_TRADE` 模式参考 Himalaya tools/market-maker，按 Binance 最新聚合成交的主动方向、价格及 `venue.quantity_fraction` 缩放数量，裁剪到单次上限后向下取整到数量步长，价格按买向下/卖向上对齐原生 tick。低于原生最小数量或金额的样本跳过，不消耗发送槽。它没有资金账户参数，与订单撤单审计分开。

market 使用独立的 `aggTrade` 连接、缓存与数据库发布租约，通过强类型 `GetTrade` 提供来源 ID、原始价格/数量、方向和交易/接收时间。按 [Binance USDm WebSocket 路由](https://developers.binance.com/en/docs/products/derivatives-trading-usds-futures/websocket-market-streams/Connect) 使用 `/market/ws/<symbol>@aggTrade`，深度继续使用 `/public`；成交流断开时立即清除该缓存，失效不会改用深度价格或其他交易所。新模式仍按有限频率采样最新事件，不能描述为逐笔全量复制 Binance 成交量；SDK 回执时间仍是本次开发发布的时间，原始交易时间保存在来源审计中。

## 有限运行配置

默认不配置此字段，策略不会启动。以下仅为 Nacos Proto JSON 示例，在 strategy 和 execution 的 `business.venue` 中加入同一段配置；账户/交易对、来源和网格必须已在数据库 TradingAccountPolicy 中显式维护，不能再通过 `venue.profiles` 配置：

```json
"synthetic_tape": {
  "enabled": false,
  "development_only": true,
  "mode": "SYNTHETIC_TAPE_MODE_AGG_TRADE",
  "run_id": "dev-tape-001",
  "interval": "1s",
  "maximum_events": 60,
  "profiles": [
    {"symbol": "BTCUSDT", "quantity": "0.001", "maximum_notional": "12000"},
    {"symbol": "ETHUSDT", "quantity": "0.01", "maximum_notional": "6000"}
  ]
}
```

- `run_id` 为 1..20 位小写字母、数字、下划线或短横线，标识固定的一轮测试。修改运行政策需要新的 ID，同一 ID 的政策不能中途改变。
- `interval` 为每品种发送间隔，范围 100ms..1m；服务端使用数据库时间强制限制。
- `maximum_events` 为每品种一轮最多消耗的发送槽数，范围 1..1000。
- `mode` 缺省为兼容的 `SYNTHETIC_TAPE_MODE_BOOK`；新模式必须显式选择 `SYNTHETIC_TAPE_MODE_AGG_TRADE`，未知值拒绝。切换模式使用新的 `run_id`。
- `quantity` 在 BOOK 模式是固定基础资产数量，在 AGG_TRADE 模式是单次基础资产数量上限；缩放比例沿用 `venue.quantity_fraction`。`maximum_notional` 为该品种一轮全部尝试的价格×数量上限，使用报价资产单位。均为精确十进制字符串，不自动扩大或补充。
- SDK 前验证市场精度、数量上下限与最小名义金额。示例数值可能不满足后来改变的市场规则，不能作为启动成功保证。

## 持久化与重复请求

发送前提交独立审计记录，唯一键为 run_id、symbol、sequence。次数和名义金额包含 UNKNOWN 槽，不因超时、重启或换主恢复。最终 SDK 发送持有执行、策略和行情授权行，并受期限约束；回复未知不补发同一槽。上游没有请求去重或查询合成成交意图的接口，因此 UNKNOWN 只表示未确认；它可能消耗槽但没有生成一笔可见成交。不能宣称端到端恰好一次。

AGG_TRADE 模式额外保存完整来源观察到计划与回执的 `source_trade`。同一 run/symbol 的来源成交 ID 必须严格递增，执行租约行锁串行化跨节点的发送槽声明；同一事件不能借下一序号、重启、并发或未知回复再发送。最终 SDK 阶段还锁定独立成交流发布租约，并以交易、接收时间及租约的最短剩余期限制发送；来源观察最多 1.5 秒有效，深度授权也仍需新鲜。旧 wire 缺少明确来源意图时在新模式拒绝，执行端重新读取最新事件，并把实际选中的来源写回回执。

`SYNTHETIC_PUBLISHED` 记录 SDK 返回的成交 ID、实际价格、数量和时间；`SYNTHETIC_UNKNOWN` 不当作已发布成交。重复读取回执标记 `replayed=true`，不代表新发布。重启从已有槽读取结果后继续，达到次数上限后等待停止，不自动创建下一轮。审计不自动删除，不能靠清空记录重复同一轮。

## 启用与停止

1. 重建四核心服务镜像。部署前使用新镜像的维护命令显式升级 SDK 扩展：`docker compose --profile tools run --rm --no-deps --entrypoint /venueinit tools -schema-only`。此模式不启用或改变基金预算，不修改原生资金/订单；没有自动启动迁移。
2. 在 Nacos 为 market/strategy/execution 写入审核后的相同合成运行控制，将 `enabled` 设为 `true`，显式更新这三个核心进程的合同版本；market 需要新模式配置才能启动成交流。AGG_TRADE 来源审计本身沿用已有 typed JSON；后续数据库业务配置需要扩展 0006，并在停止四核心后完成显式市场绑定导入，见 [Himalaya 开发做市](HIMALAYA.md)。相同参数映射保留旧合成轮次的指纹，不重置计数或未知槽。配置不热切换；仅 execution 环境含 SDK token。现有开发目标校验仍拒绝公网 SDK 地址。
3. 停止时在 strategy/execution 设 `enabled=false` 并显式重启相关进程。原生已经接收的事件不能撤回；本轮计数及 UNKNOWN 审计保留。

新配置仍默认关闭；实际开发运行的有限轮次、成功与未知回执，以及行情消费核验分别记录在 [开发进度](PROGRESS.md) 的 P17.3 及后续启动步骤。数据库、mTLS、原 SDK 与测试对端的组合回归不替代真实开发环境行情消费验收。
