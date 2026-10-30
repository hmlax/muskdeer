# 指定订单的管理撤单

P07.35提供持久请求/结果仓储及实际策略控制器处理，P07.36增加 typed gRPC/HTTP 入口。管理页面仍待后续接入，当前不能从订单页面提交本操作。关联 R05/R08/R09、A18/A27，模拟通过不代表真实网关接通。

`AdminOrderCancellations.Request`要求当前 operator/admin 会话、独立 CSRF、操作者范围内16–64字符幂等键、明确策略/OMS订单 ID、该订单的确认 revision 以及非空原因。金额、方向、账户、产品和客户端订单身份从原有订单读取，不接受管理请求覆盖。用户→会话→策略→订单顺序加锁；确认旧版本返回冲突，兄弟策略订单返回不存在。同一事务追加不可变审计和 ACCEPTED 收据，提交时再次检查会话有效性。没有策略/订单/资金或新执行租约的隐式创建。

同一操作者/键/规范化内容返回原收据及其当前结果；同键不同内容拒绝。重放仍检查当前权限与 CSRF。`Receipt`在当前 viewer 权限下使用只读 RR，明确绑定策略 ID 和收据 ID；读取不执行撤单、确认订单或释放预留。业务 DTO 不公开幂等键、会话、指纹或内部动作身份。

请求只针对指定订单，不改变策略的运行目标、配置来源、预算开关或持仓。仍在运行的策略可以随后根据当前目标重新报价；需要保持关闭时应另行明确暂停、停止或关闭风险。它不代表账户全部撤单或平仓，也不替代后续紧急撤单范围的验收。

## 执行与结果

实际 strategy 账户控制器在取得并续租当前策略所有权、恢复原动作后，优先处理管理撤单，再读取新报价输入。`StrategyOrderCancellations.Prepare`每次只读取一个 ACCEPTED 收据，游标只负责公平调度，遍历到底后下一轮从头恢复；每策略待处理请求最多256条。控制器未配置该账户时请求保持 ACCEPTED，不将管理节点在线视作已执行。

在同一策略互斥事务内，已有同目标的活动取消动作会被关联复用；否则以 `admin-order-cancel:<receipt_id>` 创建取消动作并原子绑定收据。动作与业务 RPC 键沿用现有持久日志/期限/最终 fencing，管理会话不充当策略租约。崩溃后 PENDING 动作由原恢复路径派发；已尝试动作只能沿原键查询。失去租约或旧 epoch 无法准备或发送新动作。关联与动作创建任一失败整体回滚，不留下脱离收据的新动作。

- ACCEPTED/pending：只表示请求已保存。PENDING、WAITING、UNKNOWN 及未确认的核心事实都不代表完成，保留原有义务及预留。
- SUCCEEDED/order_terminal：OMS 已明确 FILLED、CANCELED 或 REJECTED，该订单预留为零，且没有对应的活动模拟核心订单。保存观察到的 OMS 状态、revision 和数据库完成时间；FILLED 表示订单已成交结束，不冒称成功撤销成交。
- FAILED/cancel_rejected 或 cancel_not_accepted：原动作已明确被拒绝，或既有 L1 截止/受理缺失证明已永久封住原键。记录当时订单状态，保留未完成订单的预留；该管理请求不换键重发。

核心取消已确认但 OMS 仍 LIVE/PENDING 时，收据继续 ACCEPTED，OMS 预留仍在；只有原执行恢复流程确认终态并处理预算后才完成。撤单期间成交继续通过原账本和成交事实核算。收据身份、原因、初始确认、动作关联及终态不可重写或删除；数据库独立验证关联目标和完成证明。

生成模型为 [AdminOrderCancellation](../deer/proto/model/v1/admin_cancellation.proto)，迁移30只增加该表、索引和约束，不改写已有订单、预算或运行目标。验证见 [P07.35](validation/P07-35.md)。

## Typed gRPC 与同源 HTTP

[具体 Proto 合同](../api/strategy/v1/cancellations.proto)分开声明 `StrategyOrderCancellationQuery.GetOrderCancellation` 和 `StrategyOrderCancellationControl.RequestOrderCancellation`。前者随 `business.strategy.management_enabled` 注册；写入额外要求 `control_enabled`。管理节点未配置账户时可以保存请求和查询结果，但不会取得策略/执行租约，也不会凭受理完成撤单。原账户执行者可在接管后处理这些请求。

两个 RPC 都要求当前已验证 `api.grpc` mTLS 身份、规范会话、正数且最多5秒原生期限、≤2KiB请求及无未知字段；写入还要求独立 CSRF、操作者幂等键、明确策略/订单及确认 revision。业务事务独立验证当前权限。返回具体收据及可空 `OrderCancellationObservation`，不包含会话、指纹、幂等键或动作键。

| 方法/路径 | 结果 | 能力与证明 |
|---|---|---|
| POST /api/v1/strategies/:strategyID/orders/:orderID/cancellations | ACCEPTED 为202，终态原键重放为200；Location指向具体收据 | strategy_controls_enabled，固定Origin、唯一Cookie/CSRF/Idempotency-Key，当前operator/admin |
| GET /api/v1/strategies/:strategyID/order-cancellations/:cancellationID | 200，持久收据 | strategy_queries_enabled，唯一Cookie，当前viewer，无body/query |

POST体只接受 `expected_order_revision` 和 `reason` 两个字符串；订单身份只能来自规范正int64路径，不能通过body、全撤标志或未知参数扩大范围。拒绝数字 revision、null、重复/未知字段、尾随输入、非法UTF-8或超过2048字节。所有ID、revision和毫秒在HTTP JSON保持字符串；ACCEPTED无完成时间或终态观察，SUCCEEDED/FAILED须有相符的时间、版本、状态及结果码。FILLED证明订单终止，不冒称成交已撤销；FAILED是业务结果，GET及原键重放仍返回200。

API复用同一 Deer gRPC 连接和原生3秒上限，服从更早父取消，不透明重试写入，也不在本地读写订单/收据表。响应身份、确认、原因、presence或终态证明不符一律503且无部分业务数据；冲突409、输入400、无效会话401并清Cookie、权限/CSRF/Origin403、不存在404，公开错误固定。读取不执行取消或结果观测，未开启能力的路由不注册。

动作决策键与RPC幂等键维持原有持久身份。发送时追踪字段使用 `strategy.action.<action_id>`，符合执行RPC的ASCII和长度限制；这只是关联元数据，不替换原决策/幂等键、期限或不明结果恢复流程。验证和实际进程边界见 [P07.36](validation/P07-36.md)。
