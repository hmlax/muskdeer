# 数据库策略实例

策略定义保存在 `strategy_templates`：三种模板包含名称、策略类型、实现标识、所需输入、强类型参数合同、启用和修订号。实现代码由编译期注册表提供，不从数据库执行脚本。具体运行配置保存在 `strategy_instances`。每行保存策略类型（原 `template` 列）、`implementation`、`market_data_source`、完整强类型 JSONB `parameters`、账户/产品关联、启停目标和修订号。每个实例显式关联 `template_id` / `template_revision`；缺失、禁用、不匹配或当前二进制未实现的模板拒绝启动。模板定义不作为隐式运行参数默认值，实例仍保存完整实参。启动读取独立快照，缺少或非法的当前参数拒绝启动；修改后显式重启，不查询旧版本或 Nacos 作为回退。

三种模板由 `deer/trading/strategyregistry` 注册并在显式迁移时写入数据库；启动只读验证，不自动补建：

| 类型 | 实现标识 | 数据依赖 | 参数 |
| --- | --- | --- | --- |
| `market_maker` | `depth_maker_v1` | 所选盘口、账户观察 | 档数、数量比例、替换比例、交接档位 |
| `grid` | `price_grid_v1` | 所选盘口、账户观察 | 价格区间、网格数、总预算、单笔金额、库存上限、最大行情年龄 |
| `wash_trade` | `wash_trade_v1` | 所选盘口；聚合成交模式另需成交流 | 开发开关、模式、运行 ID、间隔、总次数、单次数量、累计名义量上限、数量比例 |

每行的 `execution` 保存轮询、操作期限、租约期限及杠杆。共享账户的总资金政策仍由 `trading_account_policies` 持有；各实例不能分别透支。资金账户目前绑定一个通道产品，行情源失效不会隐式切换；当前原生期货通道只接收 Binance USD-M，其他接入不冒充已支持。

网格与既有 spot 网格共用精确算术价格逻辑；期货分配单独核对双向库存与预算，最终提交计入全部活动订单及本批已提交订单。开发 WashTrade 只发布有限行情，不产生资金账户撮合或仓位成交。

策略工作器和执行 RPC 都携带实例 ID/修订号。相同资金账户共用门禁、持久预算和所有权；每个实例有独立挂单前缀和报价有效期。其他实例的订单继续计入风险及交叉检查，但不作为自己的订单撤销。最终 SDK 写入事务再次锁定当前实例、模板、产品、执行/策略/行情所有权并验证修订、启用、资金关联和数据源。

## 维护与迁移

`venueinit -instances FILE` 接受 `StrategyInstanceImport` 的 Proto JSON。每项完整指定 `template_id`、`template_revision`、`instance_key`、`market_data_source`、`account_id`、`instrument_id`、`desired_running`、`parameters` 和 `expected_revision`。策略类型和实现标识从明确选择的模板取得；如输入同时携带这两项则必须一致。先停止所有交易节点；有有效租约拒绝维护。相同输入不追加版本，冲突和任一非法尾项使整批回滚。已有账户/产品/算法身份不可变，参数更新追加不可变历史；变更已消耗的 WashTrade 运行参数必须使用新的显式运行身份，不能清零旧配额。

`instancemigrate` 从 execution 的 Nacos 基础连接读取源库，以 `MUSKDEER_INSTANCE_TARGET_DATABASE` 指定同一 PostgreSQL 上的空目标库。命令没有核心进程参数，也不自动创建库。操作顺序为：停止生产者、受控撤单、停止四核心、等待租约过期、保存完整备份、运行迁移、核验目标、修改四服务基础连接、重启。

新工作库只安装 13 张有用表：

- `instruments`、`strategy_templates`、`strategy_instances`、`strategy_config_versions`。
- `trading_account_policies`、`execution_shard_leases`。
- `venue_order_intents`、`venue_cancellations`、`synthetic_tape_intents`。
- `venue_account_snapshots`、`execution_recovery_baselines`。
- `schema_migrations`、`migration_histories`。

`strategy_config_versions` 只做不可变历史审计，当前参数不从它加载。`schema_migrations` 只记录确实执行的新基线；旧核心、通道、运行配置三个账本的原 ID/时间/版本/校验和留在 `migration_histories`，不重放旧迁移，不伪造已应用记录。旧迁移 SQL 保持历史身份。

迁移先只读验证源库，拒绝有效租约、未知表或未支持表中的非空数据。目标 DDL、复制和核验在一个事务中完成；逐批比较资金、订单、撤单、恢复基线及合成槽的生成模型，包括 ID/时间/摘要/未知状态/审批/全部预留，并保留序列高水位。原铺单 ID 保持，嵌入的 WashTrade 拆为独立实例；既有运行摘要绑定转换后的参数，启动从所有已消耗槽的下一序号继续，包括结果未知的槽。

旧库与完整备份保留供回滚，切换后不再供运行服务使用。管理/模拟器等空表不进入新工作库，但历史源码和用户未提交修改不因本次数据库精简而删除。迁移本身不调用 SDK，不注资、平仓、审批未知或释放预留。
