# Typed 执行合同

P03.6 的 [Execution Proto](../api/execution/v1/execution.proto) 明确 SubmitIntent、CancelOrder、FindOrder、ListOrders、ListFills、GetAccount 的业务字段；[公共枚举](../deer/proto/common/v1/trading.proto) 定义方向、TIF、订单/撤单状态，零值均为 UNSPECIFIED。当前只交付合同和 Submit/Cancel 输入边界，尚未注册这些业务 handler。

提交含 request_id、独立幂等键、策略/配置/owner/epoch、账户、instrument、明确方向/价格/数量/TIF、reduce_only 和 Timestamp deadline。idempotency_key 稳定于同一意图，不能使用 request_id、随机重试编号或 Nacos 在线状态代替。owner 字段只是待验证声明；最终执行必须检查真实租约和 fencing，输入解析本身不授权交易。

价格/数量为正的 plain decimal string，最多 20 位整数+18 位小数；Go 从 string 直接转 decimal.Decimal，乘积精确保留额外小数位。金额、累积量、成交、费用、余额接口也都定义为 string，fee_asset 单独标明。档位、分页、ID、序列、版本/epoch 等整数字段保持整数；时间为 protobuf Timestamp。

Submit/Cancel 边界要求原生 context deadline 和消息 deadline，消息截止必须晚于当前时间、不晚于原生截止且最长一分钟；时钟由调用方显式传入供确定性测试。取消/超时保留 native context cause。未知字段、空/非法标识、未指定方向/TIF、零/负/超精度小数均拒绝。通过后仍须检查 instrument tick/lot/min_notional、账户资金、风险、行情和所有权；解析不持久化、不预留预算、不发送交易。

有效 context request_id 优先进入业务关联，输入中的另一个编号不覆盖可信上下文；该编号不参与账户权限或幂等。撤单有自己的幂等键及明确 client_order_id；CancelState 与 OrderState 分开，接受取消不等于最终撤单/释放资金。UNKNOWN 作为非终态，后续 OMS 按持久意图和稳定 client_order_id 查询恢复。

FindOrder 使用 typed oneof 的 intent_id/client_order_id，订单/成交列表有明确账户/策略/instrument、page_size/after_id；响应为具体字段与列表，账户快照含各 asset 的 available/frozen、序列和观察时间。读取权限、分页范围及实际结果随业务服务实现验证。

模拟器和真实网关必须分别确认幂等/查询、部分成交、撤单竞态、STP、失联撤单、fencing 能力，见 [接入合同](EXCHANGE_CONTRACT.md)。当前合同不代表本所真实接口已实现。[验证](validation/P03-06.md) 覆盖类型、精度及输入期限。
