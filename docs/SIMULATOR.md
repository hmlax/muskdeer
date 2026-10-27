# 核心交易模拟器

模拟器与 OMS 使用独立模型：SimulatorCommand 是持久化网关命令，SimulatorOrder 是模拟核心订单。后续 OMS 记录自己的意图、预算与投影，不能将这些记录当作真实核心账本。

## P03.8 网关模型与授权边界

版本5迁移加入命令、订单及其关联约束。命令按账户/command_key 唯一，订单按账户/client_order_id 唯一；初始命令、账户、客户端订单、策略、配置与交易对由复合外键绑定。一条 SUBMIT 命令必须与对应订单同事务提交，一条 CANCEL 命令必须与订单撤单效果同事务提交，不能先保存未使用的授权再在以后使用。命令记录不可更新或删除。

网关数据库使用 deferred constraint trigger，在事务结束时锁定策略状态和两类租约，比较 owner/epoch、逻辑删除、数据库时间及请求期限。提交新单还要求策略 RUNNING、desired_running 和当前 active_config_version 对应完整 schema=1 参数；撤单保留受控入口。接管与网关检查通过锁串行化。单独在应用进程检查租约后再发请求不足以替代此处检查。

订单和命令在同一事务内回滚；不得捕获 fencing 失败后换 owner 或重新生成 client_order_id 盲发。表中小数为 numeric 字符串，金额产品保存为 numeric(76,36)，避免将两个 numeric(38,18) 的乘积压回原精度。新增约束明确排除 NaN，并对原交易对字段补充有限值检查。输入格式与精度仍由 string/decimal 边界负责，不能依赖数据库隐式舍入。

实现依据：[PostgreSQL 16 CREATE TRIGGER](https://www.postgresql.org/docs/16/sql-createtrigger.html) 的事务末尾 deferred constraint trigger 与行锁语义，以及 [numeric 的 NaN 排序规则](https://www.postgresql.org/docs/16/datatype-numeric.html)。触发器是目标数据库的业务约束；所有 Go 数据读写继续使用生成 Model，迁移中的已审查 DDL 不构成业务 Raw/Exec 通道。

P03.8 交付时通过实际生成模型直接验证目标数据库的命令/订单提交及拒绝，当时尚无模拟交易处理器和资金账本。P03.9 的直接 adapter 见下一节；业务 RPC 路由继续后续接入，当时的模型验证不能代表 P03 整项完成。处理器必须将资金冻结、撤单变更及命令放进上述同一事务，使用固定锁序，并通过合同故障测试。真实网关的 fencing 能力另行验收；本地数据库规则不能替外部真实网关提供保证。

## P03.9 现货资金与显式成交

版本6增加 SimulatorAccount、SimulatorBalance、不可变 SimulatorFill，订单保存提交时的 quote_fee_rate。仓储工厂 `repository.NewSimulator` 借用 Deer 的数据库，不创建账户、不注资、不自动迁移或启动后台任务。只有显式测试/开发初始化程序可以准备模拟资金。

买单冻结限价金额及预计 quote 费用，卖单冻结已有 base 数量；available/frozen 不允许负数。手续费仅以 quote 计价，每笔成交按 notional×fee_rate 向零截取36位小数；买单价差及费用余量释放到 available，卖单从到账 quote 中扣费。此规则仅属于当前模拟器。金额乘积保持76位总精度/36位小数，输入价格与数量为20位整数/18位小数。

下单命令、核心订单、资金冻结及账户 revision 在同一事务提交，最终 fencing 失败全部回滚。稳定 idempotency_key/client_order_id 与规范化业务 fingerprint 绑定；请求编号、期限和进程 incarnation 不进入 fingerprint。同一业务重试返回现有订单，内容变化拒绝，不产生第二笔订单或二次冻结。

成交通过受控 `Fill` 显式事件驱动，校验 tick/lot、限价、剩余量和外部对手主体。trade_id 全局唯一，同事实重复返回已有结果，不再次计账；同 ID 的价格、数量、订单或对手变化拒绝。订单终态后只接受已知事实的重复查询，不新增成交。

Cancel 先保存 PENDING，不释放冻结，也不改变已成交数量。期间仍可成交；ConfirmCancel 处理受控确认，按实际剩余量释放一次，保留成交及费用。若全部成交先完成，订单保持 FILLED，不能被撤单回报改回未成交状态。ConfirmCancel 与 Fill 属于受信任模拟控制接口，后续管理 API 不直接暴露它们。

锁序为策略共享锁→策略租约→账户执行租约→交易对→账户→订单→按 asset 排序的余额；显式成交/撤单确认从交易对开始，不反向获取前面的锁。交易对锁保守串行化跨账户 STP；同 subject 的对向交叉订单及自有主体的成交事件被拒绝。POST_ONLY 不接受与已存在核心订单交叉的请求，GTC 与其他主体订单交叉时明确返回 unsupported，当前没有内部连续撮合引擎。IOC 及买入 reduce_only 暂不支持；卖出 reduce_only 使用已有现货，无负信用。

P03.9 提供直接调用的模拟 adapter，业务 RPC/OMS 与断连保护继续后续步骤；响应丢失控制见下一节。这些能力不代表已接入真实交易平台。

## P03.10 响应故障合同

`simulator.WithFaults` 装饰实际 TradingGateway，在底层成功返回后选择 normal/lost/wait；底层明确拒绝保持原错误。lost 隐去订单结果，返回可识别的 OutcomeUnknown 与 DeadlineExceeded；wait 在真实提交后发出 Committed 信号，等待 Gate 或调用方取消。所有入口要求 native deadline，保留取消 cause，不延长期限、睡眠、重发或生成新身份。通道归场景所有者管理，必须保持有效直到调用方 join；装饰器不创建后台任务。

查询直达原网关，响应丢失后用原 account/client_order_id 恢复。撤单回包丢失后仍查询到 PENDING，冻结继续保留；未知提交/撤单不能被当作失败重建新业务单。该装饰器仅供显式模拟接线，不从普通管理请求接受故障参数。

| 能力 | 当前模拟结果/范围 |
|---|---|
| 元数据与精度 | 生成 Instrument，提交按当前 tick/lot/min_notional 校验 |
| 现货资金 | 实际事务冻结、扣款、释放，quote 费用明确截取36位 |
| 幂等提交/查询 | account+command_key/client_order_id 唯一，业务内容冲突拒绝，查询保留历史 |
| 部分成交/去重 | 显式外部成交事件，trade_id 唯一；终态已知事实可重查 |
| 撤单与成交竞态 | PENDING 保留资金，可成交；确认或全成交决定终态 |
| 最终 fencing | 目标事务结束检查两类 owner/epoch、数据库时间及当前配置，失效回滚资金和订单 |
| STP | 同 subject 跨账户防交叉及拒绝同主体成交事件，按交易对锁串行 |
| 成功但响应丢失/延迟 | 真实核心提交后注入；一次底层调用，原身份查询恢复 |
| POST_ONLY/GTC | 支持静态挂单与显式成交；POST_ONLY 拒绝交叉，其他主体的内部交叉 GTC 明确 unsupported |
| IOC/买入 reduce_only | unsupported；卖出 reduce_only 只用现有现货 |
| 失联撤单 | 尚未实现，后续业务恢复/连接保护步骤验收 |
| 成交流/乱序/缺口恢复 | 持久事实已有，投递及重放在 P04/P09 验收 |
| 真实平台 | 未提供合同/测试环境，所有能力均未知，P11 单独验证 |

P03 的基础值、持久化、交易合同、模拟部分成交/超时及目标 fencing 按上述子集交付。OMS、消息效果、节点恢复、管理页面和全套 L1 验收分别在 P04–P10；不能把本表扩大为完整业务或真实交易完成。
