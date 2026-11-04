# 架构与状态归属

2026-11-04 用户进一步要求：Proto 源文件保留在 deer/proto，生成的 Go 消息、GORM 模型、gRPC stub 和 Hertz 注解统一放到 deer/pb，按原服务子目录对应；go_package 和 Go 导入路径同步更新。所有配置仍共用 common/config/v1/config.proto，协议全名、字段和 RPC 方法保持。Wire injector 继续由各服务 cmd 管理。

2026-11-04 用户进一步确认：所有服务的配置消息统一定义在 deer/proto/common/config/v1/config.proto，不再按服务拆配置 Proto。data.yaml/service.yaml 仍是两份运行配置，保留原消息全名、字段编号与 YAML/JSON 格式；其他业务 Proto 继续按服务归属。

2026-11-04 最新 Proto 目录约定：deer/proto 第一层按 common、market、strategy、execution、portfolio、cron 组织，移除顶层 config/model/rpc/hertz 分类。服务 RPC、模型和专属配置分别位于所属服务的 v1、model/v1、config/v1；共享配置、模型及框架注解归 common。保持 Proto package、字段号、RPC 方法与数据库结构不变，HimalayaAuthority 不改。

2026-11-04 最新目录边界：market、strategy、execution、portfolio、cron 各自拥有入口和服务实现，Deer 只保留公共依赖、交易合同、跨服务事务仓储和 Proto。operator、API 入口及 DeerApi 封装直接删除，替代此前保留通用 API 封装/组合回归入口的约定。五个服务均要求标准 gRPC 健康接口；cron 当前只提供独立生命周期与健康能力，没有已实现的定期业务，不加入默认四核心 Compose。历史数据库迁移和用户未提交业务修改保留。

2026-11-04 最新配置边界：Nacos 每服务提供 `<service>.data.yaml` 与 `<service>.service.yaml`，分别对应 DataConfig 和 ServiceSettings；前者持有基础连接与业务运行控制，后者持有监听、身份、TLS、日志及生命周期，并以 data_sha256 绑定前者原始字节。启动同时校验版本/服务身份，拒绝半次发布。ServiceConfig 仅为兼容现有 Deer/业务接口的内存装配结果，不再作为核心配置源；此项替代下文历史单份 Proto JSON 描述，数据库业务归属保持。

2026-11-03 更新：根 api/web、管理 HTTP 应用及 OpenAPI/浏览器工具已移除；gRPC 契约统一位于 deer/proto/rpc，四核心服务通信及数据库历史迁移保持。通用 Hertz 仅保留公共运行层和测试用途，不装配管理路由。

## 服务边界
2026-11-02 用户后续明确指令：管理后台不再需要，核心模块分别独立 Kratos 进程。当前部署仅包含下表四个服务，替代 P12 的 admin/operator 两应用边界；原业务租约、预算及交易正确性边界继续有效。

| 服务 | 公共封装 | 对外接口与职责 |
|---|---|---|
| market | deer.Deer | 行情采集/质量工作循环、行情 gRPC |
| strategy | deer.Deer | 策略账户工作循环，通过 gRPC 调用行情与执行 |
| execution | deer.Deer | 执行 gRPC、风控、OMS、outbox 和模拟核心恢复循环 |
| portfolio | deer.Deer | JetStream 成交消费、持仓投影及执行恢复查询 |

保留单仓库、单 go.mod，各服务通过独立 cmd/镜像/端口/Nacos 身份运行，每个进程装配一次公共依赖。核心入口拒绝 HTTP 管理配置及管理 RPC 开启标志，不构造管理身份或加载 React。服务间保留 mTLS 身份及超时，实例/账户/行情继续用持久租约和最终 fencing 支持多节点。当前本地启动使用回放行情及禁用、未注资的模拟账户；定期报表/归档尚无实现，不部署空 cron。启动说明见 [核心服务](CORE_SERVICES.md)。

各核心服务按独立项目目录组织，继续共用根 go.mod。`<service>/cmd` 统一为 main、wire injector 声明及 wire_gen.go；各自 `internal/app` 定义 Config、WorkPlan、ServiceSpec 与 App 的 ProviderSet，由 Wire 生成静态调用顺序。业务仍调用对应 `business.NewMarket/NewStrategy/NewExecution/NewPortfolio` typed 构造函数，在 Deer 开好基础设施后同步形成工作计划，不另开资源。公共 Deer 负责启动、排空和清理；每服务只有自己的 RPC/worker，没有共用 Run 的 name 分发。operator/API 命令已删除，其余维护命令保留；服务回归入口已迁到所属项目，公共 testprocess 只提供生命周期支撑。具体结构和生成门禁见 [Wire 装配](WIRING.md)。

核心入口无 flags 或本地 JSON：Nacos 连接从 env 引导，服务及业务运行参数从 Nacos 严格解析为 Proto 配置，启动时读取并校验；变更显式重启。持仓、订单、资金与业务版本的存储权威仍按下文执行。具体环境变量见 [Nacos 配置](NACOS_CONFIG.md)。

2026-11-04 用户明确将 Himalaya 账户/基金、交易对、额度、杠杆、行情源及 tick/step 移入 PostgreSQL 的 HimalayaBudget，避免 Nacos 重复保存资金策略。四核心在各自业务装配中借用 Deer 的同一进程数据库连接池，只读生成 Model 并形成不可变启动快照；缺失、无效或重复绑定拒绝启动。Nacos 不再接受 profiles，仍提供连接与循环/有限测试运行控制。公共快照 Proto 仅用于业务传递和既有审计指纹，不能用作 Nacos 配置或新的数据库模型。

## 旧管理范围与部署对应（历史说明）
| 服务 | 公共封装 | 职责 | 扩容与所有权 |
|---|---|---|---|
| api | deer.DeerApi | 管理前端页面及管理 API、认证、统一响应，gRPC 调业务 | 无状态 |
| market | deer.Deer | 外部连接、订单簿、行情质量、参考定价 | 按 venue+instrument 分片，热备允许，单个权威流有 epoch |
| strategy | deer.Deer | 做市/网格实例、目标订单、配置版本 | 每实例唯一有效 owner |
| execution | deer.Deer | 风控、预算、OMS、本所和对冲执行 | 按 account 分片，保守串行预留，支持多个账户并行 |
| portfolio | deer.Deer | 成交去重、持仓损益投影、对账 | 按 account 分片消费；事务和唯一键保证幂等 |
| cron | deer.Deer | 日历周期触发、清理、报表 | 单实例，仍做任务唯一键/锁 |

公共模块统一为 deer；market、strategy、execution、portfolio、cron 各自通过 deer.NewDeer 构造独立 Deer，统一 Kratos 应用、gRPC、注册发现、日志、健康与公共依赖。各服务保留独立进程、服务名、实例 ID 和端口。
api 通过 deer.NewDeerApi 构造 DeerApi，组合复用一个 Deer，增加 Hertz Server、HTTP 路由/中间件及业务 gRPC 客户端；Hertz 适配后由同一 Deer 的 Kratos 生命周期托管。每进程公共依赖只初始化和清理一次，详细契约见 DEER.md。Nacos 当前同时承担服务配置与注册发现，不得假设注册发现会将有状态 RPC 自动路由到 owner。

管理人员在浏览器访问 api 托管的管理前端，前端调用同源 /api/v1 管理接口，再由 api 通过 gRPC 编排业务服务。前端源码规划放 web/admin，构建产物由 api 的 Hertz 托管；这是实施布局约定，前端框架及构建版本待实现时锁定。页面路由刷新与静态资源缓存由 api 处理，/api/v1 的错误响应保留接口语义。策略执行和行情处理继续由各业务服务负责，管理页面范围见 PRD 的 R08。

## 实时链路
market → 带 epoch/seq 的行情 → strategy → gRPC 交易意图 → execution 风控与意图落库 → 交易适配器 → 核心撮合清算 → 订单/成交回报 → execution/portfolio → 策略状态反馈。
控制 API 不直接下单。RPC 到达非 owner 节点时按 owner 目录转发一次或返回可识别重定向；防转发环，过期 owner 则重新解析，不随机落到任意节点执行。

## 行情适配与实例路由

market 首批实现 Binance、OKX 独立适配器，Bitget、Bybit 保留为候选扩展。连接、订单簿、序列、质量和缓存按 venue+instrument 隔离，不能将两家交易所的增量混成同一订单簿。
strategy 从实例已生效配置读取 market_data_source，仅使用该来源与交易对的完整健康行情；同一交易对可供不同实例分别选用不同交易所。共享订阅连接不改变实例的选择，行情记录保留 venue、source_version 及 epoch/seq 以校验来源与时效。
来源配置和版本持久化，重启及 owner 接管恢复原选择；换源走暂停、撤旧单确认、新源就绪和配置版本切换流程，并拒绝旧源/旧版本排队数据。所选源失效时执行报价保护，不因另一源在线自动改用它。行情来源与 execution 的交易场所/账户独立配置。

## 存储权威
- 本所账本/撮合：资金、冻结、订单和成交最终权威。
- PostgreSQL：配置版本、实例状态、所有权 epoch、意图、订单投影、风险预留、outbox/inbox、持仓投影和审计。
- Redis：可重建缓存、行情快照辅助、分布式限流；丢失不能产生重复订单或资金超分配。
- NATS JetStream：需要恢复的业务事件；订单/资金事实仍以事务数据库与核心系统为准。
- 内存：订单簿、算法状态、短期缓存；接管必须恢复并对账。

## 所有权方案
初期用 PostgreSQL 事务维护 lease(owner_id、epoch、expires_at)。时间基于数据库时间；续租和接管 CAS，接管单调增加 epoch。策略租约与 execution 账户分片租约独立。
最终发送交易请求时必须确认当前授权，执行队列中的旧命令也需验证。核心交易网关应支持 fencing epoch；仅在本地数据库查租约后再发网络请求存在竞态，不能提供严格保证。P03/P11 必须验证网关能力；不支持时记录残余风险并阻止严格接管验收，不得用 Redis 锁掩盖。
新 owner 先恢复意图、有效订单、UNKNOWN、持仓、预算，再转换为 RUNNING。失去租约后关闭新单出口，处理撤单需明确的接管授权。

## 事务与事件
意图/风险预留/outbox 在同库事务提交；发布者独立循环投递并更新状态。消费者 inbox 与业务变更同事务，提交后 ACK。跨库不得伪装本地事务。成交乱序按核心序列/唯一成交事实处理；发现缺口触发拉取恢复。
行情可采用 Core NATS 最佳努力分发，但必须带快照恢复、版本和过期保护；需要重放的数据单独落盘/JetStream。不能用行情队列历史积压驱动当前报价。

## 故障与降级
DB 不可用：禁止新风险；独立网关失联撤单提供最终保护。Redis 不可用：禁用缓存或安全拒绝，不绕过预算。消息不可用：outbox 积压可恢复；权威状态/风险过期则停新风险。实例所选外部行情失效：该实例停止报价并撤旧单，不隐式切换交易所；使用其他健康源的实例可独立运行。对冲失效：积压纳入限额，保留本所撤单通道。

## 定期任务边界
日历对账、报表和历史归档由 operator 内的定期任务模块触发持久化幂等任务。订单超时扫描、outbox 投递、行情心跳、租约续期和网格定时驱动属于业务服务的实时恢复循环，不放到单点 cron。
