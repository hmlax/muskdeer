# 核心交易模拟器

模拟器与 OMS 使用独立模型：SimulatorCommand 是持久化网关命令，SimulatorOrder 是模拟核心订单。后续 OMS 记录自己的意图、预算与投影，不能将这些记录当作真实核心账本。

## P03.8 网关模型与授权边界

版本5迁移加入命令、订单及其关联约束。命令按账户/command_key 唯一，订单按账户/client_order_id 唯一；初始命令、账户、客户端订单、策略、配置与交易对由复合外键绑定。一条 SUBMIT 命令必须与对应订单同事务提交，一条 CANCEL 命令必须与订单撤单效果同事务提交，不能先保存未使用的授权再在以后使用。命令记录不可更新或删除。

网关数据库使用 deferred constraint trigger，在事务结束时锁定策略状态和两类租约，比较 owner/epoch、逻辑删除、数据库时间及请求期限。提交新单还要求策略 RUNNING、desired_running 和当前 active_config_version 对应完整 schema=1 参数；撤单保留受控入口。接管与网关检查通过锁串行化。单独在应用进程检查租约后再发请求不足以替代此处检查。

订单和命令在同一事务内回滚；不得捕获 fencing 失败后换 owner 或重新生成 client_order_id 盲发。表中小数为 numeric 字符串，金额产品保存为 numeric(76,36)，避免将两个 numeric(38,18) 的乘积压回原精度。新增约束明确排除 NaN，并对原交易对字段补充有限值检查。输入格式与精度仍由 string/decimal 边界负责，不能依赖数据库隐式舍入。

实现依据：[PostgreSQL 16 CREATE TRIGGER](https://www.postgresql.org/docs/16/sql-createtrigger.html) 的事务末尾 deferred constraint trigger 与行锁语义，以及 [numeric 的 NaN 排序规则](https://www.postgresql.org/docs/16/datatype-numeric.html)。触发器是目标数据库的业务约束；所有 Go 数据读写继续使用生成 Model，迁移中的已审查 DDL 不构成业务 Raw/Exec 通道。

本步通过实际生成模型直接验证目标数据库的命令/订单提交及拒绝。当前尚无模拟交易处理器、资金账本、撮合或业务 RPC 路由，不能把模型验证宣称为完整下单成功或 P03 完成。后续处理器必须将资金冻结、撤单变更及命令放进上述同一事务，使用固定锁序，并通过合同故障测试。真实网关的 fencing 能力另行验收；本地数据库规则不能替外部真实网关提供保证。
