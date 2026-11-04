# 按服务归属组织 Proto

项目 Proto 源文件的第一层目录只允许 common、market、strategy、execution、portfolio、cron。业务 RPC 位于 `<service>/v1`，模型位于 `<service>/model/v1`，所有服务配置统一位于 `common/config/v1/config.proto`；通道内部 RPC 位于对应服务的 `<service>/venue/v1`。公共通道消息统一为 Venue*，定义在 common/v1/venue.proto 和 venue_policy.proto。生成的 Go 消息、GORM 模型、gRPC stub单独放在 deer/pb，按相同的 common/服务子目录对应；deer/proto 不再保存 Go 文件。

| 目录 | 定义归属 |
|---|---|
| common/v1 | 跨服务交易值、事件、行情与 SDK 观察合同 |
| common/config/v1 | 单一 config.proto：data.yaml/service.yaml 信封、基础连接和各服务运行参数 |
| common/model/v1 | 共享资金预算、交易品种、身份审计、通用租约及迁移账本 |
| common/probe/v1 | 公共框架 gRPC/消息探针 |
| market | 行情 RPC、通道行情读取 RPC、feed/snapshot/行情发布租约 |
| strategy | 策略 RPC、实例/配置版本/动作/预算/租约和持久管理命令 |
| execution | 执行 RPC、通道执行 RPC、OMS/outbox/撤单/模拟网关/风险和测试成交记录 |
| portfolio | 通道持仓读取 RPC、成交/inbox/持仓观察模型 |
| cron | 当前使用标准 grpc.health.v1.Health，没有自定义业务 Proto |

所有配置消息由同一个 config.proto 定义并生成到同一 configv1 包；各服务按需读取对应字段，data.yaml/service.yaml 继续分别承载基础/业务数据和服务监听/生命周期。model Proto 定义本项目数据库实体，生成带 GORM 标签的 Go 结构，仓储直接使用；不定义第三方数据库或仅供演示的技术模型。模型目录仅表达定义归属，共用 repository 的资金、幂等和 fencing 事务边界保持；服务仍可引用其他服务拥有的生成模型，不复制模型或重划数据库。

通道协议使用独立的 `muskdeer.execution.venue.v1`、`muskdeer.market.venue.v1` 和 `muskdeer.portfolio.venue.v1` 命名空间；公共消息与配置类型改为 `Venue*`。旧 `muskdeer.himalaya.v1` 路由和 `Himalaya*` 消息不再注册，不提供别名或转发。配置仅接受 `business.venue` 与 `execution_venue=gateway`，旧 `business.himalaya`/`himalaya_sdk` 拒绝启动；客户端、服务端和 Nacos 配置必须配套发布。所有小数字段仍是 string，状态、资金、租约和最终 fencing 语义保持。Go 导入使用 github.com/hmlax/muskdeer/deer/pb/...，源文件 import 引用 deer/proto/...；第三方原始源码与协议保留在 deer/third_party，不跟随内部消息改名。生成统一使用 make generate，禁止手改 pb.go。

通道持久化模型使用业务名称 TradingAccountPolicy、VenueOrderIntent、VenueCancellation、VenueAccountSnapshot、ExecutionRecoveryBaseline 和 VenueSchemaMigration，分别归 common/execution/portfolio；原 Himalaya 专属 Model 已移除。存量数据由 deer/migrate/venue 的显式 0007 升级原子保留；内部 RPC 不接受旧名称；数据库身份与历史 SQL 保留用于持久化一致性和审计。
