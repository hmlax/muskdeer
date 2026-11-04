# 按服务归属组织 Proto

项目 Proto 源文件的第一层目录只允许 common、market、strategy、execution、portfolio、cron。业务 RPC 位于 `<service>/v1`，模型位于 `<service>/model/v1`，所有服务配置统一位于 `common/config/v1/config.proto`；Himalaya 内部 RPC 位于对应服务的 `<service>/himalaya/v1`。HimalayaAuthority 继续保留在 common/v1，字段和含义不变。生成的 Go 消息、GORM 模型、gRPC stub 和 Hertz 注解单独放在 deer/pb，按相同的 common/服务子目录对应；deer/proto 不再保存 Go 文件。

| 目录 | 定义归属 |
|---|---|
| common/v1 | 跨服务交易值、事件、行情与 SDK 观察合同 |
| common/config/v1 | 单一 config.proto：data.yaml/service.yaml 信封、基础连接和各服务运行参数 |
| common/model/v1 | 共享资金预算、交易品种、身份审计、通用租约及迁移账本/框架探针 |
| common/probe/v1 | 公共框架探针；第三方 HTTP 注解仅生成到 deer/pb/common/hertz |
| market | 行情 RPC、SDK 行情读取 RPC、feed/snapshot/行情发布租约 |
| strategy | 策略 RPC、实例/配置版本/动作/预算/租约和持久管理命令 |
| execution | 执行 RPC、SDK 执行 RPC、OMS/outbox/撤单/模拟网关/风险和测试成交记录 |
| portfolio | SDK 持仓读取 RPC、成交/inbox/持仓观察模型 |
| cron | 当前使用标准 grpc.health.v1.Health，没有自定义业务 Proto |

所有配置消息由同一个 config.proto 定义并生成到同一 configv1 包；各服务按需读取对应字段，data.yaml/service.yaml 继续分别承载基础/业务数据和服务监听/生命周期。模型目录仅表达定义归属，共用 repository 的资金、幂等和 fencing 事务边界保持；服务仍可引用其他服务拥有的生成模型，不复制模型或重划数据库。

迁移保留既有 Proto package、消息全名、字段号/类型、GORM tag 和 RPC 方法名；仅源文件位置与 go_package/Go 导入路径改变。原 rpc/himalaya 中三个服务拆为各自文件，仍使用既有 muskdeer.himalaya.v1 线协议名称。数据库不需要迁移，配置 YAML 不需要改写。Go 源码使用方需要切换到新的导入路径；运行中的 gRPC 客户端继续使用原有方法全名与消息字段。Go 导入使用 github.com/hmlax/muskdeer/deer/pb/...，源文件 import 仍引用 deer/proto/...；第三方原始源码保持在 deer/third_party。生成统一使用 make generate，生成比较与目录/模型规范门禁不能通过手改 pb.go 绕过。
