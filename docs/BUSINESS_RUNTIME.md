# L1 业务进程装配

P04.10 把执行、发布、消费与持仓用例装配进现有 `muskdeer -role ... -config ...` 命令。execution 和 portfolio 仍是两个独立 Deer 进程；api 管理页面继续 P07，market/strategy/cron 的业务循环按后续阶段交付。

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

portfolio 额外指定 `portfolioConsumer`，例如 `portfolio_v1`。当前只接受明确的 simulator 及 execution/portfolio 角色；pollInterval 为10毫秒至5秒，operationTimeout 为100毫秒至5秒，均为整数毫秒且单次操作不超过 shutdownTimeout。doctor 同时验证业务要求；不读取凭据值或连接外部服务来完成配置检查。

两角色都要求 PostgreSQL、JetStream、mTLS gRPC listener 和显式 Nacos registration。portfolio 还要求出站 mTLS gRPCClients，允许目标 `execution.grpc`，其客户端证书身份为 `portfolio.grpc`。流、subjectPrefix 和 durable 由环境显式准备；portfolio 使用和 execution 相同的事件 stream/prefix，过滤精确的 `<prefix>.execution.order.v1`。durable 约束见 [Deer](DEER.md)。

## 启动与交易路径

服务启动只读检查 SchemaMigration 全部版本、名称与校验和，要求与本二进制完整一致。空库、缺版本或改写历史均启动失败；不会创建迁移表、执行迁移、准备 stream、制造余额/预算或开放风险。数据库升级仍使用显式 migrate 命令。模拟账户余额、预算和不可变持仓起点必须在交易前显式准备；真实资金初始化不属于此模式。

execution 注册完整 typed Execution RPC，按进程 incarnation 提议账户执行租约；持久接收、预算、SUBMITTING/UNKNOWN、核心模拟效果及最终 fencing 与已验证用例一致。其 owned outbox 工作器逐条领取、同步发布并完成记录。

portfolio 的 owned 工作器绑定已有 durable，解码具体 ExecutionEvent，核对 broker ID/envelope ID 和字段，再消费成交。持仓/inbox 提交后，通过 Deer-owned unary factory 新鲜发现 execution.grpc、mTLS 查询原意图，同步 OMS/预算，成功后确认 ACK。客户端只实现观察接口，不持有执行 owner、不提交订单。控制类事件校验后确认；当前 portfolio gRPC 仅有标准健康服务，管理读取业务接口随 P07 接入。

每次循环操作有独立 native timeout，空队列后才等待 pollInterval；有积压时逐条处理，不创建无界队列。失败返回给 Deer，进程撤销 readiness/注册并停止，未确认消息或未完成领取保留在持久层；本阶段不在同一调用内重放、不偷偷重启工作器。恢复/长期未知/MaxDeliver 人工处理继续 P09。

## 实际验证范围

`make integration` 在独立临时数据库/JetStream/Nacos 中显式迁移和准备模拟夹具，实际启动两个命令进程，再通过 Nacos/mTLS 下单。核心测试驱动产生真实模拟成交，execution 自动发布、portfolio 自动消费并经 RPC 同步预算。最终持仓10、现金1001.8、费用0.2、预算0，只有2条事实/2条inbox，outbox 全部完成。夹具中的行情/风险许可为明确模拟输入，不作为 Binance/OKX 实连证据。

完整 L1、数据源、策略、管理页面、对冲、多节点与运维验收继续 P05–P10；真实交易环境继续 P11。
