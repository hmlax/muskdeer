# 数据库业务执行参数

所有具体业务执行参数均以 PostgreSQL 为权威。Nacos 的 `data.yaml` 仅定义 PostgreSQL、Redis、JetStream 和 `venue_connection` 连接，`service.yaml` 定义服务身份、监听、TLS、日志和生命周期。两份文档都不接受 `business`，即使是空对象或关闭状态也拒绝。

## 参数归属

| 参数 | 数据库记录与字段 | Proto 定义 |
|---|---|---|
| 通道品种的账户、基金、交易对、杠杆、额度、行情源、tick/lot | `TradingAccountPolicy`；四核心读取同一份业务资料 | [trading_account.proto](../deer/proto/common/model/v1/trading_account.proto) |
| 模拟器的服务启用、执行路径、扫描账户、轮询/操作期限、租约、行情绑定和模拟场景 | `ServiceRuntimePolicy.parameters`，按 market/strategy/execution/portfolio 各一条 | [runtime_policy.proto](../deer/proto/common/model/v1/runtime_policy.proto)、[config.proto](../deer/proto/common/config/v1/config.proto) |
| 通道铺单的 `depth`、`quantity_fraction`、`replacement_fraction`、`bridge_slots` | `StrategyConfigVersion.maker_parameters.venue_runtime.venue`；分别是档数、来源数量比例、每轮安全旧档替换比例、每侧额外交接容量 | [config.proto](../deer/proto/common/config/v1/config.proto) |
| WashTrade 的启用、模式、轮次、频率、次数、币种和数量/金额上限 | `StrategyConfigVersion.maker_parameters.venue_runtime.venue.synthetic_tape`；四核心共读同一组实例请求版本 | [开发测试成交策略](SYNTHETIC_TAPE.md) |
| 通用做市的档数、间距、价差、每档金额、单侧预算、库存限制和刷新参数 | `StrategyConfigVersion.maker_parameters` | [strategy_parameters.proto](../deer/proto/common/v1/strategy_parameters.proto) |
| 网格的上下限价格、网格数、总预算、单笔金额、库存上限和行情时效 | `StrategyConfigVersion.grid_parameters`：`lower_price`、`upper_price`、`grid_count`、`total_budget`、`per_order_notional`、`inventory_limit`、`max_market_age_ms` | [strategy.proto](../deer/proto/strategy/model/v1/strategy.proto)、[strategy_parameters.proto](../deer/proto/common/v1/strategy_parameters.proto) |

网格和通用做市沿用既有的不可变参数版本，实例保存请求版本和当前激活版本。通道铺单和 WashTrade 同样使用 StrategyConfigVersion 的不可变版本（schema=2），每个品种一个实例；账户与财务限额仍只来自 TradingAccountPolicy，不复制到参数版本。通道四核心只读同一组实例版本，不使用 ServiceRuntimePolicy 作为第二份运行参数来源。一次启动的公共工作器控制必须一致，逐品种数量及金额上限分别保留。服务级参数没有另外复制策略实例参数。各类金额、价格、数量和比例均使用精确十进制字符串。

## 显式维护

`policyinit` 是一次性维护命令，服务启动不会调用它。先停止共享此数据库的交易节点并等待租约到期；工具也会拒绝活跃的执行、策略和行情 owner。此检查不能替代停止整个服务组，特别是没有持有租约的空闲节点。

1. 使用同版本 `/migrate -apply` 完成核心结构，再运行 `/policyinit -schema-only` 创建独立参数结构。两者从 Nacos 读取连接；SDK 通道已有的扩展仍由 `/venueinit -schema-only` 显式升级。
2. 准备 `RuntimePolicyImport` 的严格 Proto JSON，使用 `/policyinit -policies /path/to/reviewed-policies.json` 整批导入。公开的 [runtime-policies.example.json](../configs/examples/runtime-policies.example.json) 全部关闭，仅展示格式，不包含真实账户或凭据。
3. 四核心使用同一版本代码，Nacos 配对中删除整个 `business` 字段并重新计算 `data_sha256`，再显式重启。

首次创建使用 `expected_revision="0"`；变更使用当前数据库 revision。不同参数的陈旧 revision 导入失败，整批事务回滚；内容完全相同的重放保持原身份、时间和版本。删除、未知 schema 或摘要不符的记录拒绝读取，不自动修复。维护文件只接受强类型业务参数，不接受数据库连接、SDK 地址或密钥。

服务在 Deer 初始化唯一数据库池之后读取并校验自身记录，得到当前进程独立的启动快照。缺记录或参数无效即拒绝启动，不从 Nacos、环境变量或本地文件补业务参数，也不在启动时自动迁移或初始化。修改数据库后显式重启相关服务，没有在线热切换。配置完全省略 PostgreSQL 的基础健康探针不创建交易工作器；配置 PostgreSQL 的核心进程必须有自身记录，即使该记录表示关闭。

本地模拟器的 `/coreinit` 显式创建缺失的四服务参数，保留所有已有参数及禁用状态，同时仅准备两个回放绑定和一个禁用、未注资的账户，不生成策略实例或订单。网关账户资料仍通过 `/venueinit -profiles FILE` 单独维护；单独的资料导入不启用预算；`-enable` 是独立启用操作，`-strategies` 则明确把资料、版本和启用作为同一批事务维护，均不注资。

此次迁移不自动复制当前 Nacos 中的交易参数，不清空订单、风险预留、UNKNOWN、WashTrade 轮次或审计。有限测试轮次的指纹仍使用原业务合同，SDK 地址从纯连接配置装配，不改变已有轮次的身份。

## 通道品种维护

先停止四核心并等待全部租约到期，使用 `/venueinit -strategies /path/to/reviewed-strategies.json` 显式导入。输入为 VenueStrategyImport，按品种提供已审查的账户绑定、基础/报价资产、最小金额以及 BusinessRuntime；该输入不是 Nacos 文档。工具在同一事务中维护资金策略、追加 StrategyConfigVersion 并启用选中预算，不注资、不改原生订单或仓位。已有版本禁止修改；变更必须给当前 expected_revision，批次任一冲突会回滚全部参数。

启动以 `StrategyInstance.desired_running` 选择 gateway 品种，每项读取 requested_config_version，缺失、非法或不一致都拒绝；预算只加载这些版本明确选中的品种，其他原有预算和审计保持。Nacos 为四核心配置 venue_connection，从而选择数据库通道策略路径；SDK 凭据仍只注入 execution。网关扩展 0008 显式扩展已有强类型做市参数的 schema=2 形状和不可变约束，保留原核心迁移及历史交易记录。

已识别的历史无审计分支以原始 0007_stateless_orders/0008_plan_grace 的精确校验和区分。显式升级保留这八条历史记录，追加 0009 恢复未来业务审计结构、0010 使用相同的版本化策略参数约束；不会重新执行历史 DROP，也不会伪造已经被旧版本删除的审计。现有预算、持仓观察及历史 grace 列原值保持。未知校验和仍拒绝，服务启动不做该升级。标准新库和保留审计的旧库继续使用八版本路径。
