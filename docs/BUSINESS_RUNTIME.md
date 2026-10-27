# L1 业务进程装配

P04.10/P05.9 把执行、发布、消费、持仓及行情用例装配进现有 `muskdeer -role ... -config ...` 命令。market、execution和portfolio仍各为独立Deer进程；策略循环继续P06，api管理页面继续P07，cron按后续阶段交付。

## 显式启用

ServiceConfig 增加可选 `business`。省略或 enabled=false 保持已有基础设施探针行为。execution 示例片段：

```json
{
  "business": {
    "enabled": true,
    "executionVenue": "simulator",
    "pollInterval": "0.1s",
    "operationTimeout": "3s"
  }
}
```

portfolio 额外指定 `portfolioConsumer`，例如 `portfolio_v1`。当前接受明确的simulator及market/execution/portfolio角色；pollInterval为10毫秒至5秒，operationTimeout为100毫秒至5秒，均为整数毫秒且单次操作不超过shutdownTimeout。doctor同时验证业务要求；不读取凭据值或连接外部服务来完成配置检查。

两角色都要求 PostgreSQL、JetStream、mTLS gRPC listener 和显式 Nacos registration。portfolio 还要求出站 mTLS gRPCClients，允许目标 `execution.grpc`，其客户端证书身份为 `portfolio.grpc`。流、subjectPrefix 和 durable 由环境显式准备；portfolio 使用和 execution 相同的事件 stream/prefix，过滤精确的 `<prefix>.execution.order.v1`。durable 约束见 [Deer](DEER.md)。

market要求PostgreSQL、mTLS gRPC listener和Nacos registration；它使用当前PG快照提供读取，不要求无用的JetStream。额外配置最多16个不重复的已有feed ID及leaseTtl（100ms–30s，严格大于2倍operationTimeout），示例为明确REPLAY绑定：

```json
{
  "business": {
    "enabled": true,
    "executionVenue": "simulator",
    "pollInterval": "0.02s",
    "operationTimeout": "0.2s",
    "market": {
      "leaseTtl": "3s",
      "feeds": [{
        "feedId": "1",
        "replayBid": "99.99",
        "replayAsk": "100.01",
        "replayQuantity": "1",
        "replayInterval": "0.1s"
      }]
    }
  }
}
```

feed ID和模式先由显式初始化准备，不接受不存在的产品；模式来自不可变数据库绑定。LIVE条目只写feedId，拒绝回放字段；REPLAY必须完整提供字符串价格/数量及10ms–1s更新间隔，间隔还必须小于绑定最大时效。模式不匹配在构造计划时拒绝，启动不建绑定、不准备资金。实连不成功仍保持LIVE失效，不改用回放。

## 启动与交易路径

服务启动只读检查 SchemaMigration 全部版本、名称与校验和，要求与本二进制完整一致。空库、缺版本或改写历史均启动失败；不会创建迁移表、执行迁移、准备 stream、制造余额/预算或开放风险。数据库升级仍使用显式 migrate 命令。模拟账户余额、预算和不可变持仓起点必须在交易前显式准备；真实资金初始化不属于此模式。

execution 注册完整 typed Execution RPC，按进程 incarnation 提议账户执行租约；持久接收、预算、SUBMITTING/UNKNOWN、核心模拟效果及最终 fencing 与已验证用例一致。其 owned outbox 工作器逐条领取、同步发布并完成记录。

P06.7增加可选的execution风险任务。显式配置 `business.risk.accountIds`（1–16个不重复的已有模拟账户）和 `business.executionLeaseTtl`（100ms–30s，严格大于2倍operationTimeout），例如：

```json
{
  "business": {
    "enabled": true,
    "executionVenue": "simulator",
    "pollInterval": "0.02s",
    "operationTimeout": "0.2s",
    "executionLeaseTtl": "3s",
    "risk": {"accountIds": ["sim-maker-one"]}
  }
}
```

启动只读验证这些账户及账户预算已存在，不制造资产或开启政策。每个账户一个Deer-owned任务，与RPC共享同一execution owner；先按数据库时间取得/续期租约，standby不评估。每轮按ID读取该账户desired_running实例，最多16个，逐个执行已有事务评估器并等待pollInterval；超限或数据库错误返回Deer，撤销readiness并停止。省略risk不启动周期风险任务；旧配置仍使用30秒执行租约。风险许可保留原行情的有限期限，最终下单仍逐笔检查实际当前租约/行情/全部额度。进程退出后不再刷新，许可和租约自然到期。

portfolio 的 owned 工作器绑定已有 durable，解码具体 ExecutionEvent，核对 broker ID/envelope ID 和字段，再消费成交。持仓/inbox 提交后，通过 Deer-owned unary factory 新鲜发现 execution.grpc、mTLS 查询原意图，同步 OMS/预算，成功后确认 ACK。客户端只实现观察接口，不持有执行 owner、不提交订单。控制类事件校验后确认；当前 portfolio gRPC 仅有标准健康服务，管理读取业务接口随 P07 接入。

market只读加载绑定并装配按feed命名的owned工作器及真实MarketData RPC。备用不建流，持有者运行固定公开TLS或明确回放、续租/恢复基准/提交授权快照。任一节点都可从数据库返回准确来源的当前快照；读取不依赖它自己拥有该feed，也不把Nacos在线当发布授权。详情见 [行情说明](MARKET_DATA.md)。

每次循环操作有独立 native timeout，空队列后才等待 pollInterval；有积压时逐条处理，不创建无界队列。失败返回给 Deer，进程撤销 readiness/注册并停止，未确认消息或未完成领取保留在持久层；本阶段不在同一调用内重放、不偷偷重启工作器。恢复/长期未知/MaxDeliver 人工处理继续 P09。

## 实际验证范围

`make integration` 在独立临时数据库/JetStream/Nacos 中显式迁移和准备模拟夹具，实际启动两个命令进程，再通过 Nacos/mTLS 下单。核心测试驱动产生真实模拟成交，execution 自动发布、portfolio 自动消费并经 RPC 同步预算。最终持仓10、现金1001.8、费用0.2、预算0，只有2条事实/2条inbox，outbox 全部完成。夹具中的行情/风险许可为明确模拟输入，不作为 Binance/OKX 实连证据。

market进程用例使用两个不同PID分别发布Binance/OKX的显式REPLAY簿，经Deer unary factory/Nacos/mTLS读取19位mid及独立来源；SIGTERM其中一个后只该源无价格，另一源继续健康，未创建任何账户或订单。真实公开TLS接入另有A25证据，不能把回放当实连。

风险进程用例显式准备模拟资产、投影和额度，启动独立market/execution命令；execution取得新epoch并从实际发布写出含500ms实例年龄上限的READY许可。停止所选market后由工作器持久关闭为MARKET_UNAVAILABLE，不换源、不创建订单。

P05行情层完成，完整L1、策略失效撤单、管理页面、对冲、多节点全链路与运维验收继续P06–P10；真实交易环境继续P11。
