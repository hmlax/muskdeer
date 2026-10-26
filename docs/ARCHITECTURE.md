# 架构与状态归属

## 服务边界
| 服务 | 公共封装 | 职责 | 扩容与所有权 |
|---|---|---|---|
| api | deer.DeerApi | Hertz 接入、认证、统一响应，gRPC 调业务 | 无状态 |
| market | deer.Deer | 外部连接、订单簿、行情质量、参考定价 | 按 venue+instrument 分片，热备允许，单个权威流有 epoch |
| strategy | deer.Deer | 做市/网格实例、目标订单、配置版本 | 每实例唯一有效 owner |
| execution | deer.Deer | 风控、预算、OMS、本所和对冲执行 | 按 account 分片，保守串行预留，支持多个账户并行 |
| portfolio | deer.Deer | 成交去重、持仓损益投影、对账 | 按 account 分片消费；事务和唯一键保证幂等 |
| cron | deer.Deer | 日历周期触发、清理、报表 | 单实例，仍做任务唯一键/锁 |

公共模块统一为 deer；market、strategy、execution、portfolio、cron 各自通过 deer.NewDeer 构造独立 Deer，统一 Kratos 应用、gRPC、注册发现、日志、健康与公共依赖。各服务保留独立进程、服务名、实例 ID 和端口。
api 通过 deer.NewDeerApi 构造 DeerApi，组合复用一个 Deer，增加 Hertz Server、HTTP 路由/中间件及业务 gRPC 客户端；Hertz 适配后由同一 Deer 的 Kratos 生命周期托管。每进程公共依赖只初始化和清理一次，详细契约见 DEER.md。Nacos 只做注册发现。不得假设注册发现会将有状态 RPC 自动路由到 owner。

## 实时链路
market → 带 epoch/seq 的行情 → strategy → gRPC 交易意图 → execution 风控与意图落库 → 交易适配器 → 核心撮合清算 → 订单/成交回报 → execution/portfolio → 策略状态反馈。
控制 API 不直接下单。RPC 到达非 owner 节点时按 owner 目录转发一次或返回可识别重定向；防转发环，过期 owner 则重新解析，不随机落到任意节点执行。

## 行情适配与实例路由

market 首批实现 Binance、OKX 独立适配器，Bitget、Bybit 保留为候选扩展。连接、订单簿、序列、质量和缓存按 venue+instrument 隔离，不能将两家交易所的增量混成同一订单簿。
strategy 从实例已生效配置读取 market_data_source，仅使用该来源与交易对的完整健康行情；同一交易对可供不同实例分别选用不同交易所。共享订阅连接不改变实例的选择，行情记录保留 venue、source_version 及 epoch/seq 以校验来源与时效。
来源配置和版本持久化，重启及 owner 接管恢复原选择；换源走暂停、撤旧单确认、新源就绪和配置版本切换流程，并拒绝旧源/旧版本排队数据。所选源失效时执行报价保护，不因另一源在线自动改用它。行情来源与 execution 的交易场所/账户独立配置。

## 存储权威
- 本所账本/撮合：资金、冻结、订单和成交最终权威。
- PostgreSQL：配置版本、实例状态、所有权 epoch、意图、订单投影、风险预留、outbox/inbox、持仓投影和审计。
- Redis：可重建缓存、行情快照辅助、分布式限流；丢失不能产生重复订单或资金超分配。
- NATS JetStream：需要恢复的业务事件；订单/资金事实仍以事务数据库与核心系统为准。
- 内存：订单簿、算法状态、短期缓存；接管必须恢复并对账。

## 所有权方案
初期用 PostgreSQL 事务维护 lease(owner_id、epoch、expires_at)。时间基于数据库时间；续租和接管 CAS，接管单调增加 epoch。策略租约与 execution 账户分片租约独立。
最终发送交易请求时必须确认当前授权，执行队列中的旧命令也需验证。核心交易网关应支持 fencing epoch；仅在本地数据库查租约后再发网络请求存在竞态，不能提供严格保证。P03/P11 必须验证网关能力；不支持时记录残余风险并阻止严格接管验收，不得用 Redis 锁掩盖。
新 owner 先恢复意图、有效订单、UNKNOWN、持仓、预算，再转换为 RUNNING。失去租约后关闭新单出口，处理撤单需明确的接管授权。

## 事务与事件
意图/风险预留/outbox 在同库事务提交；发布者独立循环投递并更新状态。消费者 inbox 与业务变更同事务，提交后 ACK。跨库不得伪装本地事务。成交乱序按核心序列/唯一成交事实处理；发现缺口触发拉取恢复。
行情可采用 Core NATS 最佳努力分发，但必须带快照恢复、版本和过期保护；需要重放的数据单独落盘/JetStream。不能用行情队列历史积压驱动当前报价。

## 故障与降级
DB 不可用：禁止新风险；独立网关失联撤单提供最终保护。Redis 不可用：禁用缓存或安全拒绝，不绕过预算。消息不可用：outbox 积压可恢复；权威状态/风险过期则停新风险。实例所选外部行情失效：该实例停止报价并撤旧单，不隐式切换交易所；使用其他健康源的实例可独立运行。对冲失效：积压纳入限额，保留本所撤单通道。

## cron 边界
日历对账、报表和历史归档由 cron 触发 RPC/持久化任务。订单超时扫描、outbox 投递、行情心跳、租约续期和网格定时驱动属于业务服务的实时恢复循环，不放到单点 cron。
