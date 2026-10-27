# 运行与故障处理基线

状态：P02 已提供服务进程及公共运维入口；交易恢复、管理业务与 P10 故障处理仍按后续任务完善。

## 本地进程

`make build` 使用固定 Go 1.27.3 生成 `.cache/bin/muskdeer`。每次命令只运行一个角色，例如：

```sh
.cache/bin/muskdeer -role market -config configs/local/market.json
```

角色为 api、market、strategy、execution、portfolio、cron；配置 name 必须与角色一致。六份 local 配置仅用于公共运行层，无外部资源、交易能力或自动迁移。api 使用 DeerApi/Hertz，其他角色使用独立 Deer；market/strategy/execution/portfolio 要求 gRPC，cron 可只提供 monitoring。api 管理页面及实际业务接口在 P07 实现。

local 配置的 monitoring 端口依次为 api 59100、market 59101、strategy 59102、execution 59103、portfolio 59104、cron 59105，均只绑定 loopback；api HTTP 为 59000，其余四个业务 gRPC 为 59001–59004。多实例应分别配置 instance_id 和不同端口。`/livez`、`/readyz`、`/metrics` 及指标定义见 [Deer](DEER.md) 和 [指标说明](METRICS.md)。`service.initialized` 仅表示装配完成，必须继续检查 readiness。

SIGTERM/CTRL-C 触发停止：先撤就绪、停止心跳并注销，再 drain transports、逆序释放业务/公共依赖、恢复 SDK 日志、关闭自有日志。正常取消返回 0；配置/初始化/独立清理错误非零，取消不会隐藏同时发生的清理失败。该进程层停止还未包含撤单、对账等后续业务步骤。

框架日志只在新进程入口安装一次，禁止在共享进程的 Deer 构造器中切换 Kratos/Hertz 全局 logger。SDK 日志统一输出 JSON `framework.event`，含级别、框架、函数/行号和参数数量；SDK 原始格式及参数值可能包含敏感请求，因此不输出。可诊断的生命周期事件由应用使用安全结构化字段单独记录。Hertz 的公开恢复 API 以原 default logger 重建 system wrapper，要求安装前没有自定义 system logger。应用结构化日志保留 request_id；框架没有 context 的接口无法自动提供请求编号。

## 环境与凭据
开发、仿真、测试、生产分别使用 Nacos namespace、数据库、Redis 前缀、NATS subject 和账户。所有环境默认禁止实盘；测试凭据不提交仓库。外部交易密钥只授权所需交易能力，提现权限不属于策略服务。

## 启动顺序
基础设施 → 经审查的数据库迁移 → execution/portfolio 恢复 → market 就绪 → strategy 恢复 → api 放流量 → cron。服务可并行启动但 readiness 必须体现依赖及恢复状态，策略不能因为进程启动就自动跳过对账。

## 停止顺序
停止新增风险 → 撤单并确认 → 决定是否继续对冲 → 持久化/对账 → 停止业务实例 → 关闭连接。终止进程的超时路径触发独立网关失联撤单。平仓单独授权，不将停止按钮默认实现为市价卖出全部资产。

## 告警
- 行情年龄/序列缺口、多源分歧：按 venue+instrument 定位来源，保护并撤销受影响实例报价；换源必须显式修改该实例配置并等待新源健康，不隐式切换，其他实例按各自选择独立运行。
- UNKNOWN 订单增长：查询核心，禁止盲目重下，保留预算。
- 对冲积压：核查外部余额/限流/故障，缩量或停新风险。
- owner 变化频繁：检查租约时钟、DB 与网络；确认旧 epoch 被拒绝。
- 对账差异：冻结受影响策略新增风险，定位缺失成交/费用/冻结，不手工修改账本掩盖。
- outbox/inbox 异常：观察最老积压时间和重试，隔离毒消息，人工重放须保留审计。

告警阈值以模拟测试值起步，生产按容量、延迟及损失预算确定；未确认数值不能当 SLA。

## 备份与恢复
PostgreSQL 备份及恢复演练覆盖配置、意图、成交、幂等键和所有权。数据库恢复后禁止直接恢复交易，先与核心系统对账并重建投影；旧 fencing epoch 不得重新获得授权。Redis 可以重建。JetStream 保留期与可用恢复窗口匹配。

## 发布
代码交付使用公开仓库 https://github.com/hmlax/muskdeer.git；每步相关测试/检查通过后提交并推送远程工作分支，核对 SHA。此代码同步已获用户授权；生产部署继续遵守独立授权边界。
固定镜像/工具版本；灰度按策略实例或账户分片，不按随机请求混用不兼容执行者。迁移采用兼容扩展再收缩。回滚不能丢失新增成交及订单状态；代码回滚与资金事实回滚不同。生产部署和真实资金启用需要用户独立授权。
