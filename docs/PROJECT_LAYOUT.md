# 项目目录与业务归属

顶层原 internal 曾同时放四服务业务、公共领域、跨表事务、迁移和测试夹具。Go 的 internal 只限制导入可见性，不会自动形成服务边界；原来四项目主要只有入口与生命周期装配。

2026-11-03 用户确认按业务下沉，公共部分继续共用。现在不保留顶层 internal，仍是单仓库、单 go.mod、五个独立入口（默认部署四个交易核心），没有复制事务仓储或拆数据库。

| 目录 | 归属与职责 |
|---|---|
| market/business | 行情 typed 构造、RPC 与工作计划 |
| market/marketbook、marketfeed、marketquality、marketview、marketwire、marketworker | 订单簿、接入、质量、合同转换与工作器 |
| market/delivery/marketgrpc | 行情 RPC 服务端与客户端适配 |
| strategy/business、strategy/usecase | 账户扫描、策略生命周期与持久动作调度 |
| strategy/quoting、strategywire、delivery/strategygrpc | 报价/差分、合同转换及保留的管理 gRPC 实现 |
| execution/business、execution/usecase | OMS、租约执行权、风险、撤单与 outbox 工作计划 |
| execution/orderwire、outbox、simulator、delivery/executiongrpc | 订单合同、消息发布、模拟网关与 RPC 适配 |
| portfolio/business、portfolio/usecase | 成交消费、幂等投影、权威订单恢复与 ACK 合同 |
| 各项目 internal/app 与 cmd | 私有配置快照、Wire provider、唯一 Deer 的 Run/Close 和进程入口 |
| deer/trading/domain | 共用交易值、端口接口、状态与小数合同；不依赖数据库、框架或服务实现 |
| deer/trading/repository | 当前共用的数据库事务仓储，包含资金、幂等、lease/fencing 和历史管理数据实现 |
| deer/trading/runtimeconfig、strategyconfig、eventwire | 共用配置校验和事件/参数合同；不启动业务工作器 |
| deer/migrate | 单份版本化迁移、显式维护及跨模块集成测试 |
| deer/coreentry、processlog、processfailure、runloop | 健康/取消、日志、退出错误和生命周期轮询工具 |
| cron/cmd、cron/internal/app | 独立 Wire 装配、日志、gRPC 健康与生命周期；不装配空任务 |
| 各交易项目 testservice、strategy/testbusiness | 所属服务的历史回归入口/策略管理夹具，只供测试，不进入生产依赖图 |
| deer/testprocess、testapp | 公共进程/Nacos 配置测试支撑；接收 typed 工厂，不按服务名装配业务 |
| deer/trading/outboxcontract | 仓储、发布工作器和消费者共用的持久 claim/错误/subject 合同 |

Deer 的基础设施组件、Proto 和第三方源码继续留在 deer。corelocal 是显式本地初始化工具的共用实现，不在服务启动时迁移、开户或注资。

## 依赖约束

各 WorkPlan 先在唯一 Deer 的数据库池就绪后通过 runtimeconfig.ResolveDatabasePolicy 读取自身参数，关闭状态仅提供健康能力；启用时直接调用本项目 business.NewMarket/NewStrategy/NewExecution/NewPortfolio；这些构造借用唯一 Deer 的资源。共享 runtimeconfig.Data 只校验配置、检查迁移账本并借出资源，不根据服务名选择或装配业务。provider 不启动 goroutine，启动、关闭和清理错误合同保持。

业务/适配子包保留必要的 Go 导出接口，供本项目装配、跨项目协议客户端和现有集成/历史夹具复用；不复制一套实现来绕过 internal 限制。各项目 internal/app 仍仅允许所属项目导入。核心进程之间仍通过认证 gRPC、消息和已有持久化合同协作，历史组合夹具不是新的默认运行入口。

订单终态判断归公共 OrderState.Terminal；回放配置是单份 domain.MarketReplayConfig，marketworker.ReplayConfig 保留类型别名。公共配置校验不再依赖 market 工作器，不让其他核心服务因校验配置而引入行情运行实现。没有改变状态集合、数字精度、超时、字段编号或 RPC 名称。

本轮保留共用 repository，是因为当前资金/意图/租约等事务跨现有表实现原子约束。未来按服务拆仓储需单独定义数据所有权和事务替代方案；本次目录整理不声称已经完成数据库或全部领域隔离。历史管理数据、迁移和原有未提交平仓代码继续保留，默认四服务不启用管理 RPC。

## 检查方式

`make verify` 验证生成、格式、静态门禁、工具、单元/race 与核心构建。`make core-test` 使用实际四核心命令验证接管、报价、成交投影及停止；`make integration` 继续读取 deer/migrate 和 deer 下的基础设施探针。

核心入口的生产依赖图必须包含自己的 business 包，且不能包含其他项目的 business 或任何 testbusiness/testservice 夹具；协议客户端适配和公共交易合同可复用。最终验证结果以 PROGRESS 为准，不能将原有未提交功能或历史全量验收合并宣称通过。

2026-11-04：删除旧 deer/testbusiness、deer/testentry、deer/testservice、cmd/operator、operator 示例，以及 DeerApi/API 服务观测实现与不再使用的 admin 服务观测 Proto。保留的策略管理回归装配已下沉 strategy/testbusiness；其他夹具只装配自己所属服务。默认交易入口继续拒绝管理 RPC。共享仓储不再反向导入 execution/outbox，corelocal 不再导入 marketquality；公共合同和模拟默认策略各有单一来源。`scripts/check-layout.py` 已纳入 lint，检查 Deer 无服务实现依赖及五个入口各自拥有装配图。

2026-11-04：Proto 第一层改为服务名，共享定义归 common；旧 config/model/rpc/hertz 顶层已移除。各所有者的定义与共享字段划分见 [Proto 目录](../deer/proto/README.md)。只改变声明位置和 Go 导入，不改变共用仓储的事务边界。

Proto 源文件集中于 deer/proto；生成 Go 代码及对应包文档集中于 deer/pb，按 common/服务子目录对应。所有业务引用 deer/pb 的生成类型；运行 make generate 更新。Wire injector 继续在各项目 cmd 下。

交易通道边界：execution/adapters/himalaya 只实现第三方 SDK/gRPC 和字段/单位转换；execution/venue/gateway 定义消费接口及强类型值，execution/venue 持有持久化、额度、fencing、撤单与报价恢复，装配层注入具体 Factory。共享 native 值与校验无数据库依赖，数据库策略读取独立在 deer/trading/venueconfig。服务布局门禁检查适配器的完整依赖图，并限制 SDK 只出现在适配器和测试对端中。
