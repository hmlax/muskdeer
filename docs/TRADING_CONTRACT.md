# Typed 执行合同

P03.6 的 [Execution Proto](../api/execution/v1/execution.proto) 明确 SubmitIntent、CancelOrder、FindOrder、ListOrders、ListFills、GetAccount 的业务字段；P04.8 增加 FindCancellation、独立 CancellationView 及 core_revision。[公共枚举](../deer/proto/common/v1/trading.proto) 定义方向、TIF、订单/撤单状态，零值均为 UNSPECIFIED。`internal/delivery/executiongrpc` 已实现全部方法，可通过 Deer 注册；默认命令入口的业务装配继续后续步骤。

提交含 request_id、独立幂等键、策略/配置/owner/epoch、账户、instrument、明确方向/价格/数量/TIF、reduce_only 和 Timestamp deadline。idempotency_key 稳定于同一意图，不能使用 request_id、随机重试编号或 Nacos 在线状态代替。owner 字段只是待验证声明；最终执行必须检查真实租约和 fencing，输入解析本身不授权交易。

价格/数量为正的 plain decimal string，最多 20 位整数+18 位小数；Go 从 string 直接转 decimal.Decimal，乘积精确保留额外小数位。金额、累积量、成交、费用、余额接口也都定义为 string，fee_asset 单独标明。档位、分页、ID、序列、版本/epoch 等整数字段保持整数；时间为 protobuf Timestamp。

Submit/Cancel 边界要求原生 context deadline 和消息 deadline，消息截止必须晚于当前时间、不晚于原生截止且最长一分钟；时钟由调用方显式传入供确定性测试。取消/超时保留 native context cause。未知字段、空/非法标识、未指定方向/TIF、零/负/超精度小数均拒绝。通过后仍须检查 instrument tick/lot/min_notional、账户资金、风险、行情和所有权；解析不持久化、不预留预算、不发送交易。

有效 context request_id 优先进入业务关联，输入中的另一个编号不覆盖可信上下文；该编号不参与账户权限或幂等。撤单有自己的幂等键及明确 client_order_id；CancelState 与 OrderState 分开，接受取消不等于最终撤单/释放资金。UNKNOWN 作为非终态，后续 OMS 按持久意图和稳定 client_order_id 查询恢复。

FindOrder 使用 typed oneof 的 intent_id/client_order_id，查询原核心身份后同步现有意图；不重新提交。FindCancellation在明确账户内按持久请求ID或原idempotency_key查询，两者必须恰好提供一个；键支持与撤单请求一致的1–128字符标识。首次回复丢失、不知道服务端ID时仍能查询原请求，不领取新owner、不再次撤单；未找到返回NotFound，不能将其自行解释为可换新键重试。响应的cancellation（含原业务键）与order分别展示请求状态及订单事实。接口增量字段保持原编号，客户端/服务器生成物一同更新。

订单/成交列表明确账户/策略/instrument、page_size/after_id，采用自增 ID 升序 keyset；默认20，最多100，负游标/非法范围拒绝。open_only 只保留 INTENT/SUBMITTING/LIVE/PARTIALLY_FILLED/UNKNOWN。列表是持久观察值，当前状态通过 FindOrder 同步；不能把列表当作核心执行确认。账户余额在同一核心账户 SHARE 锁下读取 revision、资产和数据库观察时间，最多128种资产。所有 GORM 操作绑定生成 Model。

方法权限只依据已验证 mTLS 证书的准确服务 DNS SAN：strategy.grpc/cron.grpc 可执行写操作，api.grpc/portfolio.grpc 仅可读取及查询恢复。metadata 不能提升权限。调用必须有 native deadline，最长一分钟；Submit/Cancel 消息 deadline 进一步收紧。响应不包含 owner incarnation。此服务层信任已认证业务进程；管理用户/角色与账户范围由 P07 管理入口校验。

P06.10增加客户端路由依据 `repository.NewExecutionRouting(...).ExecutionInstance`：每次按准确账户读取数据库当前未删除、正epoch且未过期的ExecutionShardLease，使用数据库时间；从经ownerid.Valid校验的incarnation解析完整实例名（支持名字中的点）。缺失、过期、退役或畸形owner返回ErrLeaseLost，不读取Nacos来推定所有者、不创建租约或缓存旧路由。它仅给出待选实例；发送端仍须用Deer factory的明确InstanceID目标，所选执行端取得/验证自身租约并在最终效果重新fencing。实际策略客户端接入随后交付，不能将此读取器等同于已运行的策略路由闭环。

模拟器和真实网关必须分别确认幂等/查询、部分成交、撤单竞态、STP、失联撤单、fencing 能力，见 [接入合同](EXCHANGE_CONTRACT.md)。当前合同不代表本所真实接口已实现。[验证](validation/P03-06.md) 覆盖类型、精度及输入期限。
