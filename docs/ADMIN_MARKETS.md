# 管理行情监控

P07.38交付`AdminMarkets`只读仓储及领域合同，P07.39增加具体gRPC/HTTP入口，关联R01/R08、A18/A23/A24/A27。管理页面继续接入，当前不代表行情监控浏览器验收通过。

`List`必须明确LIVE或REPLAY，支持规范来源、执行交易对ID及升序feed ID游标；页长1–100。未选来源表示列出各独立绑定，不选择替代行情。候选/未知来源、未知模式、负ID和无界页长拒绝；明确但不存在的交易对返回不存在。`Detail`只按明确feed ID读取，缺失绑定返回不存在。

每次读取在同一个PostgreSQL只读、REPEATABLE READ事务内校验当前viewer，再读取生成Model对应的feed、交易对、快照和发布者租约。列表批量读取关联行，查询次数不随页内数量增长；不获取租约，不启动外部连接，不发布行情或改变策略/风险/资金。退出会话、凭据变更、父取消和存储失败均有明确结果，失败无部分数据或数据库诊断泄漏。

返回真实执行产品、外部来源/符号/模式、质量策略及数据库评估时间。没有快照时`Snapshot=nil`，保留绑定本身，不借用其他源的价格，也不伪造健康数据。有快照时区分以下事实：

- `PublishedQuality/PublishedReason/PublishedAt/PublishedValidUntil`是持久发布诊断，历史HEALTHY不等于当前可用。
- `Reading`按当前数据库时间重新检查年龄、时钟、价差/离群策略及当前发布者。所有价格、数量和参考价在Go中保持decimal.Decimal，序列、修订与epoch保持int64。
- 发布者身份/epoch不匹配、释放或过期时，`PublisherCurrent=false`，当前读数为UNAVAILABLE/DISCONNECTED，全部价格及数量清零。`PublisherExpiresAt`是当前可见租约的到期时间，仅作诊断；到期时间非零不证明该快照属于当前发布者。
- 发布者仍有效但行情过期时为STALE/AGE，当前价格、数量及有效期限清零，保留历史发布诊断。新发布者必须实际发布自身快照，接管本身不使旧快照健康。

监控结果只是一个读取时刻的观察，不是下单权限；最终执行仍独立验证来源、所有权、风险和时效。P07.38未修改Proto/model/迁移或外部依赖。实际数据库与失败诊断证据见[P07.38](validation/P07-38.md)。

## Typed管理入口

[StrategyMarketQuery](../api/strategy/v1/markets.proto)提供ListMarkets/GetMarket，随`business.strategy.management_enabled`显式注册，要求已验证api.grpc mTLS身份、当前viewer会话、≤5秒原生期限、≤2KiB且无未知字段的请求。管理查询不要求账户或执行/行情所有权，不注册新写入或后台任务。

| GET路径 | 条件与结果 |
|---|---|
| /api/v1/markets | 必须唯一mode=LIVE或REPLAY；可选唯一source、正int64 instrument_id、after_id/limit；返回markets与next_after_id |
| /api/v1/markets/:feedID | 正规范int64，无body/query；返回明确feed的market |

HTTP随`strategy_queries_enabled`开放，沿API已有单个Deer连接调用strategy，≤3秒且服从更早父取消。唯一Cookie和当前会话校验，无body、未知/重复参数、非规范ID或候选来源拒绝；依赖失败503、输入400、失效会话401并清Cookie、权限403、不存在404。GET无CSRF写语义，POST不存在写路由。所有响应no-store，int64与小数均为JSON字符串；缺快照保留presence，不借用其他来源或模式。

具体MarketMonitor包含feed、执行产品、数据库评估时间及可空snapshot。快照保留发布诊断、epoch/修订和当前读数，不返回发布者owner身份、会话或内部键。两层均验证嵌套未知字段、绑定、精度、来源/模式过滤和升序游标；健康读数必须有当前发布者、未过期的原发布/租约/数据时效、符合所选政策的时钟与价差、精确中价。无效成功返回503/Unavailable且无部分数据。P07.39增加API Proto生成，不修改持久化model或schema；实际进程和协议证据见[P07.39](validation/P07-39.md)。
