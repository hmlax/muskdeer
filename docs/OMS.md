# OMS 与事务边界

## P04.1 接收意图

`repository.NewIntents` 借用 Deer 的数据库。Accept 只做持久化接收，不调用交易网关；调用者必须有 native deadline，并提出当前策略与账户执行租约。策略/账户/交易对/active 配置对应关系及两类租约使用数据库时间检查；策略须 RUNNING 且 desired_running。

价格和数量先校验20位整数/18位小数，再按交易对 tick/lot/min_notional 检查。当前支持 GTC/POST_ONLY 和有现货的卖出 reduce_only；不支持的输入在分配预算前拒绝。稳定 client_order_id 为 account_id 与 idempotency_key 的 SHA-256，业务 fingerprint 包含配置、账户、交易对、方向、规范化价格/数量、TIF 和 reduce_only，排除请求编号、期限与进程 incarnation。

一个事务锁定共享 AccountBudget 和从属 StrategyBudget，检查短期 RiskSnapshot，增加两处 reserved_notional，写入 OrderIntent 及 typed OutboxEvent。相同身份/内容返回原意图且不再预留，内容变化拒绝；outbox 失败时所有写入回滚。查询通过账户+intent ID 或账户+client ID，不能随意生成新业务身份。

预算单位是账户指定 quote asset 的未完成订单名义金额，价格×数量以 numeric(76,36) 精确保留；不把不同币种相加。账户预算限制所有策略的合计，策略预算是其下属上限，两个方向当前合并计算。费用及真实资产冻结由核心独立执行。这些预算不代替后续持仓/单笔/交易对/平台限额。

风险快照必须明确允许新风险且已对账，匹配账户、交易对、active 配置及选定来源；时间基于数据库，观察时间不能在未来，最长有效5秒，过期或来源错配拒绝。5秒是当前内部上限，不是行情 SLA；后续生产者须按参数和质量缩短期限。初始化不自动造出健康快照或开放预算，测试数据仅由隔离夹具显式准备。

## Typed 事务 outbox

事件 envelope 包含版本、时间、producer、aggregate ID/revision 和请求关联。IntentEvent/FillEvent 都是具体 Proto 字段，小数为 string；不经过无类型 JSON/map。使用具体可空消息字段并由数据库要求恰好一个匹配 event_type 的 payload，避免把生成 oneof 的 Go interface 直接交给标准 JSON 存储反序列化。Envelope 的 event_id/event_type 与记录列必须一致。

接收事件 ID 为 `intent:<id>:<revision>`，与意图同事务创建，初始 PENDING。发布者和消费接入进度见下节；写入 outbox 不等同于已投递或已产生业务效果。

P04.1 仅完成接收与预算基础。后续接入 SUBMITTING/UNKNOWN、查后重试、核心回报/预算释放、成交去重与投影、outbox 发布/inbox、恢复和实际业务 RPC；发送前与最终模拟目标仍须重新检查授权和风险。完整交易闭环及风险层级继续按 P04–P10 验收。

## P04.2 派发与查询恢复

`usecase.NewIntentService` 仅依赖 domain 仓储和 TradingGateway。先接收意图，再重新验证租约、配置、风险快照与预算开关，事务写 SUBMITTING 及状态事件后才调用网关。单次尝试的 context 期限和持久 submission_deadline 都被风险快照到期时间进一步限制；不延长调用方期限。

OMS 调用总是携带持久 intent ID。版本8在模拟目标增加对应外键及事务末尾的风险/预算/身份检查，风险变更可回滚已写入的核心订单和资金。没有 intent ID 的路径只用于已有受信任直接模拟场景，不由 OMS 派发或管理 API 提供。

明确核心拒绝使意图 REJECTED 并原子释放两处预算；不确定错误进入 UNKNOWN，预留保留。调用方取消导致状态无法回写时，已持久化的 SUBMITTING 仍可恢复。重复 Submit 对已派发意图走原 client ID 查询，不再调用提交；查不到且结果未知时保持 UNKNOWN/预留，安全再派发策略继续 P09，不把一次不存在查询当作可盲发新单的证明。

确认核心快照时核对业务字段、核心订单 ID、core_revision、成交量及状态。旧核心版本不能覆盖新事实，同版本矛盾拒绝；已知成交量不能回退。部分成交按原限价剩余数量减少名义预留，明确终态释放剩余预留；重复确认不二次释放。状态、两处预算和状态 outbox 同事务更新。

核心成交在自身事务中将账户风险快照 reconciled 置 false；因此预算释放后，新风险仍须等待成交投影/对账完成。当前尚未实现该投影生产者，不能通过刷新行情自动开放此标记。

锁顺序扩展为策略/租约→交易对→账户预算→策略预算→意图→风险快照；核心交易路径持有交易对及自身账户后，最终按预算→意图→风险快照读取。意图状态更新使用 FOR NO KEY UPDATE，兼容核心命令提前取得的 FK KEY SHARE，避免恢复与提交形成预算/意图反向等待。依据 [PostgreSQL 16 行锁兼容规则](https://www.postgresql.org/docs/16/explicit-locking.html)，实际交错通过受控测试验证。意图身份字段不由状态更新修改。

P04.2 的范围不包含撤单编排、成交自然键/inbox、持仓损益、风险重新开放、事件发布及业务 RPC；后续步骤逐项交付。

## P04.3 持久撤单请求

CancellationRequest 与订单最后确认的 core cancel 状态分开，使用独立 REQUESTED/SUBMITTING/PENDING/UNKNOWN/CONFIRMED/REJECTED 状态。取消接收、状态变化与 typed cancellation outbox 同事务写入。账户/idempotency_key 永久绑定原目标，不能复用到另一笔订单；核心 submit:/cancel: 命名空间分离，重试保留同一核心命令键。

未派发 INTENT 可在同一事务转换为 CANCELED、释放两处预留并确认撤单请求；不会发送核心命令。已派发订单先按原 client ID 查询，再持久化 SUBMITTING 后调用核心撤单。此时不要求新风险快照健康，仍必须有当前策略与账户执行授权；暂停或风险关闭不会自行删除撤单通道。

核心只接受请求时记录 PENDING，订单与资金预留继续保留。丢失回包时持久记录 UNKNOWN，不覆盖订单已知的核心状态，也不释放预算；查询到核心 pending/终态后再同步。部分成交及费用由原订单事实继续累计，撤单确认后按实际剩余量释放一次。全部成交先完成时保留 FILLED；请求 CONFIRMED 表示目标已无有效余单，页面/调用方仍须展示订单最终状态，不能解释成原订单一定是 CANCELED。

PENDING/UNKNOWN 的恢复当前只查询、不盲目重发；长期未知/旧 owner 的受控重派发与后台恢复继续 P09。订单、撤单请求及事件均持久可恢复，当前仍未接入业务 RPC、成交投影和消息发布。

## P04.5 有界 outbox 发布

`repository.NewOutbox` 使用数据库时钟和 `FOR UPDATE SKIP LOCKED`，每次最多领取一条到期 PENDING 或过期 PUBLISHING。领取事务持久增加 attempts，并绑定进程 incarnation、到期时间。完成、重试或隔离前先锁行，再读取数据库当前时间，校验原 owner/expiry；旧 owner 不能完成后继领取。失败重试时间写入数据库，不依赖进程内计时状态。

`outbox.Worker.RunOne` 要求调用方 deadline，只做一次同步发布。`outbox.NewPublisher` 借用 Deer 的 JetStream 客户端，校验 typed envelope/变体/十进制边界后，将稳定 event ID 作为 Nats-Msg-Id，发布至配置前缀下的 `execution.order.v1`。流必须预先存在，工作器不创建流、消费者或后台任务。调用失败会安排1秒后的重试；明确坏事件进入 FAILED，保留记录供处理。

存储 ACK 成功后才尝试数据库 PUBLISHED。完成失败时保留 PUBLISHING，后继在领取过期后用同一 event ID 重发。JetStream 的去重窗口只减少重复存储；窗口外或消费重投仍依靠事务 inbox 和成交自然键保证业务效果不会重复。真实容器测试覆盖发布存储后完成失败、接管重发，以及持仓事务提交后 NAK 重投/最终确认 ACK。

P04.5 提供可组合发布工作器，尚未启动生产后台循环或消费者；当时容器测试中的成交 outbox 由夹具显式写入。后续自动生产与消费处理见下节，运行层装配和业务 RPC 继续后续步骤。

## P04.6 模拟核心自动成交事件与消费效果

L1 模拟核心成交在同一事务写余额/冻结、订单、不可变 SimulatorFill、typed TradeFilled outbox 和风险失效。事件通过原始不可变 SimulatorCommand.intent_id 绑定 OMS 意图，ID 固定为 `fill:simulator:<source ID>`，源序列为持久成交 ID；不在回包成功后另写事件，不通过全局自增游标猜测提交顺序。重复自然成交返回原事实，不再次产生事件。无 OMS 关联的受信任直接模拟场景不会制造 OMS 事件。

`usecase.FillConsumer` 先提交持仓事实/inbox，然后调用 IntentService.Recover 查询权威核心、同步订单状态和释放名义预算；两步都成功才允许调用方 ACK。若在持仓提交后查询/OMS失败，原消息仍可重投：持仓 inbox/自然键和 OMS 核心版本检查分别防重复效果。消费只查询原订单，不重新提交；UNKNOWN 不允许成功 ACK，风险快照仍需后续风险生产者显式重算。

真实 PG/JetStream 故障窗口测试已改用核心自动生产的成交事件，覆盖存储后完成失败、提交后 NAK、最终订单预算归零。独立注入成交 outbox 写入失败，验证余额、冻结、核心订单、成交事实和风险变化整体回滚；另验证持仓已提交而 OMS 尚未完成时的重投。

这些组件尚未作为独立 execution/portfolio 进程后台工作启动。真实外部核心不能借用本模拟事务回滚能力，必须按真实合同使用可靠回报或带补偿扫描的采集；该能力继续 P11 独立验收。

## P04.7 受控 durable 消费客户端

Deer JetStream 客户端已支持只绑定已有 durable 的 `OpenConsumer` 和同步 `ConsumeOne`，配置/期限/取消边界见 [公共封装](DEER.md)。调用者使用具体 ExecutionEvent，核对 broker message ID 与 envelope ID、严格变体，再执行 FillConsumer；控制类事件只在校验成功后确认。客户端仅在处理成功后发送确认 ACK，业务失败时保留原 broker delivery，等待 AckWait 重投。

真实成交闭环测试已使用该公共客户端，注入业务效果提交后的错误；broker 超时重投后观察到 attempt>1，最终仍只有2条事实/2条inbox、预算0。另验证缺少 durable 不创建、运行中过滤范围变化拒绝、坏 Proto 不进入 handler/不 ACK、处理与等待准入均响应取消。没有新增后台任务，工作器装配与业务 RPC 继续下一步。

## P04.8 Typed execution RPC

`executiongrpc.New` 借用意图/撤单用例、读取仓储与账户执行授权，提供完整 typed RPC。`usecase.ExecutionOwner` 使用同一进程 incarnation，按账户 Acquire/Renew 数据库时间租约；最终执行仍重新 fencing。查询恢复不领取新执行权、不重发订单。

订单、成交和账户查询保持显式账户范围、有界 keyset 分页/资产列表。账户读在账户行 SHARE 锁下取得同一 revision 的余额和数据库观察时间。wire 输出先验证 decimal 范围，乘积、费用、余额均为精确 string；未知枚举/不合法存储值不会被默默转换为成功响应。权限、请求期限和合同字段见 [执行合同](TRADING_CONTRACT.md)。

明确 STP 拒绝纳入已确认拒绝分类，意图 REJECTED、预留释放；请求超时或响应丢失仍保留 UNKNOWN 和预算，按原身份查询。CancelOrder/FindCancellation 响应同时提供独立请求状态与核心订单状态，PENDING 不能解释为已撤单。

本步通过 Deer/mTLS 注册真实业务 handler 验证；默认 execution/portfolio 命令的后台/客户端装配继续后续步骤，尚未将直接测试服务等同于六进程业务闭环。
