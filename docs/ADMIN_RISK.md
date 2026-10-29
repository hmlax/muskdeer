# 策略风险开关

P07.29提供一个策略范围的仓储事务和原生 PostgreSQL 约束。P07.30接入独立 typed gRPC 查询/控制和管理 HTTP，复用既有管理会话、mTLS、CSRF、审计查询及同一连接；P07.31交付策略详情内的风险与额度面板。

## 语义与权限

`risk.strategy.block` 需要当前 operator/admin 会话、CSRF、明确策略 ID、管理风险版本、幂等键和原因。它关闭该策略既有 `StrategyBudget.Enabled`，并关闭其缓存风险许可。已有 LIVE、提交中、UNKNOWN、待撤单及动作义务仍保留；不改运行目标、实际状态、配置、来源、所有权、额度、预留或核心冻结。受原 fencing 约束的撤单仍可执行，释放继续依赖核心终态确认。

`risk.strategy.unblock` 仅允许当前 admin。除风险版本外，必须显式提交确认时看到的 requested 配置版本和 desired 运行目标；任一已变化则冲突。恢复要求自身预留为零，OMS、核心、动作与撤单均无未终态义务；账户/产品及账户、策略、交易对、平台额度均可用，对账准确，所选来源的当前行情和质量有效，实际库存及未终态买单不超过请求配置与共享已生效配置中最严格的硬限。

恢复只更新独立资格开关，并保持缓存许可关闭。它不启动、恢复运行、激活配置、切换行情、发单或直接签发交易许可。后续正常评估和最终提交仍须检查各自的当前条件；共享账户、交易对及平台开关不会被此操作打开。

## 原子性与重放

持久化结构来自 [Model Proto](../deer/proto/model/v1/admin_risk.proto)，实现为 [风险事务](../internal/repository/admin_risk.go)、[恢复检查](../internal/repository/admin_risk_recovery.go) 和 [当前快照](../internal/repository/admin_risk_state.go)。风险管理版本从不可变收据最大版本推导，尚无收据时为 1；它独立于频繁变化的资金预算 Revision。新控制在策略互斥下检查版本并递增，普通资金释放不使风险确认版本失效。

操作者与幂等键共同唯一，规范化请求以确定性二进制编码计算摘要。同键同内容返回原历史收据，同键改内容冲突；重放仍须当前权限及 CSRF，恢复历史重放仍需当前 admin。后来的新控制不改变旧收据，也不会因旧请求重放而重新写开关。

权限、预算旗标、缓存风险关闭、审计及收据在一个 READ COMMITTED 事务内提交。锁顺序遵循既有策略→交易对→核心账户→账户预算→策略预算协议。存储失败、当前权限失效、恢复失败、超时或提交约束失败均无部分业务结果。当前读取使用已认证 viewer 及可重复读快照，精确返回预留、额度、所选请求来源、版本和状态。

## 最终数据库保护

[迁移 29](../internal/migrate/sql/0029_admin_strategy_risk.sql) 新增带自增 Id、非空创建/更新时间及 nullable 删除时间的收据表；历史禁止改写、删除和软删除。提交时独立检查当前会话、角色、凭据版本、会话到期、相符审计、独立风险版本、实际旗标及已关闭的缓存许可。恢复证明受当前选择行情的有效期、事件/接收年龄、发布租约及最多 5 秒限制；提交时已过期或仍有义务则整笔回滚。

模拟核心对已管理策略的全部新增提交检查当前预算旗标，包括没有 OMS intent ID 的直接核心提交。裸核心历史场景没有策略预算时保持原合同，迁移不自动创建或启用预算。取消保留原授权/fencing 检查。历史迁移 1–28 的字节和校验和不改变，升级不重写现有预算、金额、状态或权限。

## Typed 管理入口

[风险 Proto](../api/strategy/v1/risk.proto) 明确区分 StrategyRiskQuery.GetStrategyRisk 与 StrategyRiskControl.ChangeStrategyRisk：前者随 management/query opt-in 注册，后者仅随 control opt-in 注册。查询进程不注册修改 RPC。两者都要求已验证 API mTLS 身份、当前持久会话和至多 5 秒的调用预算；≤2KiB、未知字段和嵌套会话/输入检查保留，无新连接、后台任务、资金或预算初始化。

| 方法/路径 | 返回 | 证明/输入 |
|---|---|---|
| GET /api/v1/strategies/:strategyID/risk | 200 StrategyRiskState | 唯一会话 Cookie、当前 viewer、规范正 int64 路径，无 query/body |
| POST /api/v1/strategies/:strategyID/risk | 200 StrategyRiskReceipt | 固定 Origin、唯一会话 Cookie、独立 CSRF、唯一 Idempotency-Key、≤2KiB application/json，无 query |

POST 显式 action、string expected_revision、reason；恢复另需 string expected_config_version 和 expected_desired_state，关闭必须省略这两个字段。拒绝数字型版本、null、重复/未知字段、别名、尾随数据及非规范整数。规范化后的原原因和确认字段完整送达交易事务，不从浏览器角色或本地显示推断授权。单次 HTTP 下游调用至多 3 秒，响应 no-store；Location 指向当前风险 GET，收据本身仍是历史事实。

当前状态包含独立管理 revision、Enabled、quote_asset、精确 string 额度/预留、实际状态/desired 目标、requested 配置版本/所选来源及预算行 updated_at_ms；金额、ID、修订、版本和毫秒均按 Proto JSON string 输出。预算更新时间可能随普通资金变化，不能替代管理版本。缺少既有预算返回 404，不自动开通；超额、错误身份/状态/来源/确认字段的成功响应被拒绝为 503，无部分业务数据。

当前权限/CSRF错误为401/403，非法请求400，确认版本冲突409 CONFLICT；恢复条件未满足为gRPC FailedPrecondition、HTTP409 RECOVERY_DENIED及固定公开原因，内部诊断不返回。查询能力未开时无该接口；只有查询能力时无修改RPC，HTTP同路径POST返回405。接口尚不提供平仓或独立管理撤单，其余管理页面与完整A27继续后续。

## 风险与额度页面

策略详情的“策略风险与额度”在明确打开后读取，展示当前独立管理版本、资格开关、精确额度/预留和资产、实际状态/请求目标、请求配置/所选行情及预算UTC更新时间。预算更新时间与风险管理版本分别解释，当前快照与历史操作收据分别呈现。缺少预算、读取失败、权限/协议失败时清空旧数据，可明确刷新；401清空当前页面并回登录，不以旧状态继续写入。

viewer与查询能力只读，operator可关闭，admin可恢复。关闭和恢复都填写原因，恢复确认明确配置/来源/运行目标；后台变化导致冲突时重新读取、重新选择并确认。结果不明时冻结原正文与UUID键，按原请求重试；即使读取发现开关已变化，也不换新键掩盖未确认结果。明确恢复拒绝或冲突才清除旧确认。

金额用有界十进制文本校验和比较，始终string，不转Number；ID/版本以BigInt验证，只有整数UTC毫秒用于日期。原因按React文本显示，无浏览器持久存储。`make admin-risk-browser`是CI/e2e第七组，实际资金/原键重试、真实三角色、待确认取消/当前目标变化及移动端精度边界见[P07.31](validation/P07-31.md)；页面协议注入与真实账本证明分别记录。

## 验证边界

原生测试覆盖真实资金、撤单确认、并发幂等、不同操作者竞争、审计/收据失败回滚、提交时权限和恢复证明过期、共享门禁、请求配置/目标绑定、库存及当前行情。结果、失败证据和未运行项记录于 [当前进度](PROGRESS.md)。模拟器通过仅证明 L1 共享数据库合同，真实交易环境的最终风控仍属于独立适配验收。
