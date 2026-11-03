# 开发测试成交策略

用户确认的接口是现有 Go SDK `WashTrade`：只发布开发行情的合成成交和 K 线，不创建订单，也不修改账户、仓位或资金。该数据不进入 Muskdeer 的真实成交、持仓或收益投影。撮合和原 SDK 源码保持不变。

策略位于 `strategy/native`，由现有 strategy 进程拥有；execution 通过 mTLS 内部 RPC 接收意图，重新读取选定来源并按最新买一/卖一及原生网格定价，最终调用 SDK。买卖方向按发送序号交替。行情失效、原生市场关闭或 tick/lot 改变时不发布，不隐式换源。它没有资金账户参数，与旧订单撤单审计分开。

## 有限运行配置

默认不配置此字段，策略不会启动。以下仅为 Nacos Proto JSON 示例，在 strategy 和 execution 的 `business.himalaya` 中加入同一段配置；交易对也必须已在 `himalaya.profiles` 选择来源和网格：

```json
"synthetic_tape": {
  "enabled": false,
  "development_only": true,
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
- `quantity` 为每次基础资产数量；`maximum_notional` 为该品种一轮全部尝试的价格×数量上限，使用报价资产单位。均为精确十进制字符串，不自动扩大或补充。
- SDK 前验证市场精度、数量上下限与最小名义金额。示例数值可能不满足后来改变的市场规则，不能作为启动成功保证。

## 持久化与重复请求

发送前提交独立审计记录，唯一键为 run_id、symbol、sequence。次数和名义金额包含 UNKNOWN 槽，不因超时、重启或换主恢复。最终 SDK 发送持有执行、策略和行情授权行，并受期限约束；回复未知不补发同一槽。上游没有请求去重或查询合成成交意图的接口，因此 UNKNOWN 只表示未确认；它可能消耗槽但没有生成一笔可见成交。不能宣称端到端恰好一次。

`SYNTHETIC_PUBLISHED` 记录 SDK 返回的成交 ID、实际价格、数量和时间；`SYNTHETIC_UNKNOWN` 不当作已发布成交。重复读取回执标记 `replayed=true`，不代表新发布。重启从已有槽读取结果后继续，达到次数上限后等待停止，不自动创建下一轮。审计不自动删除，不能靠清空记录重复同一轮。

## 启用与停止

1. 重建四核心服务镜像。部署前使用新镜像的维护命令显式升级 SDK 扩展：`docker compose --profile tools run --rm --no-deps --entrypoint /himalayainit tools -schema-only`。此模式不启用或改变基金预算，不修改原生资金/订单；没有自动启动迁移。
2. 在 Nacos 为 strategy/execution 写入审核后的相同配置，将 `enabled` 设为 `true`，显式重启四核心进程以使用新 schema/合同。配置不热切换；仅 execution 环境含 SDK token。现有开发目标校验仍拒绝公网 SDK 地址。
3. 停止时在 strategy/execution 设 `enabled=false` 并显式重启相关进程。原生已经接收的事件不能撤回；本轮计数及 UNKNOWN 审计保留。

本次代码交付默认关闭，未向当前开发行情实际发布合成成交。数据库、mTLS、原 SDK 与测试对端的组合回归不替代真实开发环境行情消费验收。
