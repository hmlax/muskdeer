# Muskdeer — Codex 工作约定

2026-11-10 用户确认新增只做多百分比网格：BTC/ETH 分别使用用户指定的独立 user/fund（具体绑定保存在私有配置）；10倍杠杆、CROSS全仓保证金，每笔100 USDT名义价值，启动开首笔，跌1%买/涨1%卖对应仓位，跌破撮合持仓均价0.5%全平。止损优先于加仓，止损后停止并需显式启动才开新轮。沿用grid策略类型，新增独立实现/模板，不改变原固定区间网格和现有19做市/19开发实例。金额按撮合tick/lot向下量化，禁止平仓反开空仓；新的网格状态/动作按Proto生成持久化，UNKNOWN请求不重发，允许独立只减仓保护，不恢复数据库进程租约或旧订单累计预留。用户已要求在当前系统配置并运行这两个现有账户；既有商户范围延续MuskdeerDemo，管理/业务库独立。

2026-11-09 用户最新要求：当前 WashTrade 策略订阅所选行情，按配置计算单次成交量后直接调用既有 SDK WashTrade；行情处理和发送不得有任何数据库读写。移除当前执行的开发批次、消耗序号、历史扫描、持久化去重及回执落库，不将这些记录转存 Redis/消息队列；旧数据仅保留历史。配置与启停仍由数据库控制面提供，独立刷新到内存，事件处理只读内存配置/开关。development_only、原数据源、频率、单次计算规则、精确小数、鉴权和有界单次请求保持，不扩展真实账户对刷。此条替代此前当前开发合成执行必须保留批次/序号/数据库去重的约定；不伪造旧记录终态或重发旧 UNKNOWN。

2026-11-09 用户进一步明确：做市铺单订单不记数据库。当前 market_maker 实例直接调用撮合下单/撤单，不写或读取本地下单意图、回执、撤单日志、订单观察及本地订单恢复记录；账户快照中的挂单同样不落库，当前快照仅在内存中短期保存并从撮合刷新。策略模板、实例参数、启停、品种和账户配置继续由数据库提供；资金/挂单/持仓以撮合为准，保留鉴权、参数检查、源新鲜度及有界请求，SDK 不自动重试下单。此条替代此前要求当前铺单持久化意图/单次领取/回执的约定。旧订单审计保留，不伪造终态或清空；网格与 development_only 合成成交的记录、批次和去重不扩大本次改动。

2026-11-09 用户最新要求：当前 strategy_instances 通道运行不再记录或查询数据库进程执行权、租约有效期，不再汇总本地历史订单的资金预留。行情、策略、执行、持仓及管理撤单路由一并移除当前租约依赖；撮合实时挂单、持仓和可用资金为权威。历史所有权及预留记录原样保留作审计，不删除、不伪造正 epoch、不转移到 Redis。参数修订/启停、授权、行情新鲜度、持久化请求身份与单次发送、开发合成成交去重和原批次序号保持。此条替代以前要求当前通道保留数据库租约/fencing 和未确认请求累计预留的约定。取消进程所有权后不再声称具备跨副本独占执行；当前部署每个核心服务一个进程，实例数量不设上限。

2026-11-09 用户明确管理后台归属：实例配置、交易账户、订单/撤单/成交及持仓由商户端管理，平台仅提供策略模板、品种、网关等公共配置。管理 operator 从可信身份及管理库绑定构造商户范围，平台管理员不得操作商户交易接口；模板及品种可供商户表单只读选择。用户已授权既有 19 账户/38 实例归到 MuskdeerDemo；保留全部参数、启停、订单身份和历史消耗。基础设施连接仍由 env/Nacos 提供，网关页面只显示当前加载连接，不暴露凭据或隐式热切换。

2026-11-07 用户新增要求：管理后台在独立 github.com/hmlax/admin 框架内实现，复用 Hertz admin、Kratos operator、React 菜单与 RBAC，管理库和做市业务库独立；admin/operator 只能通过经过 mTLS 认证的 gRPC 与四核心交互。允许在既有核心服务新增对应当前实例结构的 Console 管理 RPC，此条替代此前一律不注册管理 RPC 的限制；不在 Muskdeer 恢复 admin/operator 进程或旧会话表，不让后台直连业务数据库。菜单、读取及每个写操作分别授权。基础设施配置仍 env 连接 Nacos，具体业务参数仍为业务库权威，参数版本更改必须明确重启生效，不暗中热切换交易状态。

2026-11-07 用户最新要求：开发 WashTrade 不再要求有限额度，maximum_events=0 和 maximum_notional="0" 分别表示次数及累计名义量不限制。全部已配置开发实例按此修改后继续原批次和原已消耗序号，不清零历史、重发 UNKNOWN 或新建批次来规避消耗。其余数据源、频率、单次数量、development_only、去重、所有权和最终 fencing 保持；此条替代此前仅有限开发批次的限制，不授权真实账户对刷。

2026-11-07 用户进一步明确：做市以撮合侧为权威，不要求本地历史订单对账完成后才报价，全部已配置铺单及开发合成行情实例恢复运行。当前独立实例使用交易通道的实时订单、持仓、可用资金与新鲜观察，不因旧下单/撤单回执缺失冻结整个账户；这替代此前 BNB/ETC 等未知结果的整账户阻断要求。历史 DISPATCHED、撤单记录、原身份及名义预留审计保留，不伪造终态、审批引用或重发原请求。尚未确认的新请求仍按完整名义金额纳入预算，但不触发整账户停机；旧已确认请求不作为第二份累计账本重复限制当前撮合敞口。已有预算、来源新鲜度、实例/所有权最终 fencing 保持。WashTrade 仍只使用既有有限 development_only 合成行情配置，不新增真实账户对刷或重置已消耗配额。

2026-11-07 用户最新明确改为需要策略模板并继续整体迁移。新增 strategy_templates 保存做市铺单、网格、WashTrade 的实现标识、数据依赖和参数合同；strategy_instances 显式关联模板及修订号，并保存每个实例的完整具体参数和启停。本条替代 2026-11-06 “不另建模板表”的选择；不设实例数量上限、数据库业务参数权威、历史审计保留、未知资金状态及 BNB/ETC 阻断等其余要求保持。模板不引入隐式运行参数合并或 Nacos 业务参数回退。

2026-11-06 用户最新要求：不以固定实例数/账户数/工作器数限制策略运行，按实际 strategy_instances 装配，不截断查询。三种策略为做市铺单、网格、WashTrade；用户明确选择 strategy_instances 每行独立保存策略类型、数据源、实现标识和完整参数，不另建策略模板表。代码按类型注册实际实现逻辑，旧参数版本仅保留历史审计，不能与实例参数成为双重运行配置来源。本次明确要求清理无用表、整体迁移有用表和数据后重新运行；资金、订单、未知状态、预留、幂等及 fencing 的有用数据和身份保持，既有不隔离 BNB/ETC 新未知的决定不因迁移重置。本条替代此前以 StrategyConfigVersion 作为当前策略参数权威的约定。

2026-11-04 用户最新要求：铺单、网格和 WashTrade 的具体业务执行参数统一存入数据库，Nacos 不再承载这些参数。品种、数量/比例、档数、预算、频率、测试轮次及策略选择等由数据库提供；Nacos 仅保留基础设施/网关连接、监听、认证、日志和进程生命周期配置。沿用网格与通用做市的版本化 StrategyConfigVersion，不复制参数来源。应用只读启动快照，缺失/非法参数拒绝启动，不回退旧 Nacos 业务配置；显式维护和重启生效。本条替代此前 data.yaml 保存业务运行控制与有限测试成交参数的约定。

2026-11-04 用户最新要求本次通道接口全部改用业务名称，不保留旧协议兼容。公共消息使用 Venue*，通道 RPC 使用 muskdeer.<service>.venue.v1，运行配置使用 business.venue 与 execution_venue=gateway，维护命令改为 venueinit/venuebaseline；旧消息、RPC 路由、配置键和命令不提供别名或转发。本条替代此前保留 HimalayaAuthority 名称、旧 RPC 全名和配置键的约定。第三方 SDK 自身协议保持原样，数据库持久化身份与历史迁移不因接口重命名而清空或重建。

2026-11-04 用户要求 Himalaya 只作为第三方 SDK/gRPC 适配层，不再定义 Himalaya 专属持久化模型。订单、撤单、额度、持仓观察与恢复基线由业务模块持有；历史数据通过显式版本化升级保留，交易状态、预算预留、未知恢复和最终 fencing 合同保持。

2026-11-04 用户要求 model 目录只保留业务及必要运行记录的 Proto。移除仅供技术验证的 StorageProbe 源文件和生成代码；持久化/基础设施测试复用 Instrument、AccountBudget 等真实业务模型。业务模型仍由 Proto 生成 GORM 结构，原有业务表与版本化迁移保持。

2026-11-04 用户要求移除 Hertz Proto HTTP 注解：删除生成包、第三方 api.proto 和 hz 生成/校验工具链。公共算术探针继续用于 gRPC/监控回归，HTTP 监听继续直接使用 Hertz 运行时。

2026-11-04 用户进一步要求：Proto 源文件保留在 deer/proto，生成的 Go 消息、GORM 模型、gRPC stub统一放到 deer/pb，按原服务子目录对应；go_package 和 Go 导入路径同步更新。所有配置仍共用 common/config/v1/config.proto，协议全名、字段和 RPC 方法保持。Wire injector 继续由各服务 cmd 管理。

2026-11-04 用户进一步确认：所有服务的配置消息统一定义在 deer/proto/common/config/v1/config.proto，不再按服务拆配置 Proto。data.yaml/service.yaml 仍是两份运行配置，保留原消息全名、字段编号与 YAML/JSON 格式；其他业务 Proto 继续按服务归属。

## 目标与优先级
开发连接既有交易所的自营做市与策略交易 MVP。先阅读 README.md、docs/PRD.md、docs/ARCHITECTURE.md、docs/IMPLEMENTATION_PLAN.md、docs/PROGRESS.md。用户最新明确指令优先；既有仓库指令与本文件冲突时先记录并解决，不静默覆盖。

## 已确认工程约束
- 2026-11-04 最新 Proto 目录约定：deer/proto 第一层按 common、market、strategy、execution、portfolio、cron 组织，移除顶层 config/model/rpc/hertz 分类。服务 RPC、模型和专属配置分别位于所属服务的 v1、model/v1、config/v1；共享配置、模型及框架注解归 common。保持 Proto package、字段号、RPC 方法与数据库结构不变，VenueAuthority 不改。
- 2026-11-04 最新目录边界：market、strategy、execution、portfolio、cron 各自拥有入口和服务实现，Deer 只保留公共依赖、交易合同、跨服务事务仓储和 Proto。operator、API 入口及 DeerApi 封装直接删除，替代此前保留通用 API 封装/组合回归入口的约定。五个服务均要求标准 gRPC 健康接口；cron 当前只提供独立生命周期与健康能力，没有已实现的定期业务，不加入默认四核心 Compose。历史数据库迁移和用户未提交业务修改保留。
- Go 版本在 go.mod 声明，当前为 1.27.3；开发、代码生成、测试、CI 与构建使用对应工具链。按用户最新要求，应用、维护命令及探针不再用 runtime.Version() 做启动版本判断，不硬编码运行时版本拒绝条件。P01 工具链核验及构建版本锁定记录保留；禁止 rc/beta、latest 或浮动版本代替。
- 公开 GitHub 仓库 https://github.com/hmlax/muskdeer.git；单仓库、单 go.mod，module 固定 github.com/hmlax/muskdeer。公共目录与包名 deer，构造函数 deer.NewDeer，统一 Kratos 服务封装 Deer；DeerApi/NewDeerApi 和 operator/API 运行入口按 2026-11-04 最新要求直接移除，Hertz 仅保留公共本地监控能力，详见 docs/DEER.md。
- 外部 HTTP 使用 Hertz，服务生命周期使用 Kratos，内部通信使用 gRPC；注册发现使用 Nacos。禁止替换成 Kitex、Gin 或 Kratos HTTP。
- 2026-11-02 后续明确指令优先：当前仅交付核心 Kratos 服务，各模块分别独立进程、独立 cmd 入口，不使用共用命令加 role 参数。已实现的 market、strategy、execution、portfolio 分别使用 Deer 提供 gRPC/业务工作循环；默认构建和 Compose 不再部署 admin、operator 或管理前端。保留单仓库、单 go.mod；每进程公共依赖只装配一次。此项替代此前两应用部署约定。
- 四个核心服务分别使用 <service>/cmd/{main.go,wire.go,wire_gen.go} 与各自 internal/app/{config.go,workplan.go,server.go,app.go,providers.go}；Wire ProviderSet 生成配置、工作计划、日志、服务定义及 App 的依赖调用。main 从 env/Nacos 加载配置并调用生成 injector，internal/app 独立维护本服务 Deer 生命周期及 typed 业务构造/RPC/worker 注册，继续共用根 go.mod；其他 cmd 命令原样保留。禁止用共用 Run(ctx, name, ...) 或按服务名分发的工厂装配核心进程。coreentry 只保留健康/取消工具，历史组合装配仅供旧入口兼容。
- Wire 版本固定在 go.mod 的 tool/require 与 tools/versions.json。make wire 生成、make wire-check 只比较且不写回；两者纳入生成/verify 流程，禁止手改 wire_gen.go。每进程只有一个 Deer；业务工作计划在 Deer 基础设施就绪后同步装配，不在 provider 中启动 goroutine 或另开池。App.Run/Close 保留有 context 且返回 error 的清理合同，不能转成吞错的 func()。
- Deer 保存 Kratos App、日志及公共依赖，NewDeer 通过 WithDB/WithRedis/WithLogger 等选项支持注入已有客户端；注入依赖为借用，调用方在 Deer 清理后关闭，未注入的依赖仍由 Deer 按配置创建和清理。Wire 透传 []deer.Option，不重复创建池；构造、启动与运行 readiness 保留依赖检查。
- 核心服务无命令行参数：不使用 flag、-config 或 -check-ready，不从本地 JSON 启动。Nacos 连接地址、命名空间、分组、cluster、认证和 TLS 从 env 获取；服务配置使用强类型 service.yaml、连接与业务运行控制使用 data.yaml，分别存放 Nacos，启动时校验配对版本/内容并读取，不回退本地文件；账户业务参数仍以数据库为权威。配置变更显式重启，不隐式热切换交易状态；健康探针为独立无参数 readycheck。
- 2026-11-03 用户进一步确认移除根 api/web 目录：删除管理 HTTP、前端及相关应用入口，必要 gRPC 协议迁入 deer/proto/rpc，保留四核心服务通信；此项替代此前仅保留管理源码但不部署的约定。历史验收、已存在的数据库迁移及用户未提交业务修改保留；核心服务保留 mTLS 身份认证、业务风控、幂等、状态机和 fencing，不启用管理 RPC。
- PostgreSQL + GORM；Redis；可靠消息优先 NATS JetStream；Prometheus/Grafana；JSON 日志 + lumberjack 按大小轮转。
- config Proto、持久化 model Proto 放 deer/proto；Proto 是持久化结构唯一来源，禁止手写重复 Model、手改生成代码。
- gRPC 服务合同位于 deer/proto/<service>/v1，生成 Go 代码位于 deer/pb/<service>/v1，通道内部 RPC 位于各服务 venue/v1；根目录不再保留 api。通道内部 Proto package 与消息名称按最新要求使用新合同，不注册旧方法；其他接口字段和事务语义保持。
- 2026-11-03 用户确认业务实现按项目下沉，公共领域、租约及跨服务事务仓储继续共用，不重划资金/幂等/fencing 事务边界。行情/策略/执行/持仓代码归所属项目，公共交易合同与事务仓储归 deer/trading，数据库迁移归 deer/migrate，进程与测试支撑归 deer；不再使用顶层 internal。继续单 go.mod，不复制共用交易实现，项目 internal 包遵守 Go 可见性限制。
- 每个核心项目使用自身 business 包及对应 usecase；公共 runtimeconfig 只校验/借用资源，不装配业务。旧 deer/testbusiness、testentry、testservice 已删除；服务专属回归夹具位于各项目 testservice 与 strategy/testbusiness，公共 deer/testprocess 只接受 typed builder。核心生产依赖图不得包含回归夹具或其他项目的 business。协议客户端适配和公共交易合同可复用，目录说明见 docs/PROJECT_LAYOUT.md。
- 仓库内维护的第三方源码、Proto 注解、OpenAPI schema 及许可证统一放 deer/third_party，继续共用根 go.mod；生成工具、Go 导入及镜像许可收集使用该路径。
- 每张表及其持久化 Model 必须有 Id 自增主键、createAt NOT NULL、updateAt NOT NULL、deleteAt 可为 NULL；字段映射、生成与迁移要求见 docs/PROTO_AND_MODEL.md。
- 所有 GORM 数据操作显式使用 db.Model(&model.X{}) 或事务中的 tx.Model(&model.X{})，禁止 db.Table("xxx")、tx.Table("xxx") 及其他变量上的 .Table(...)；适用于业务、公共组件、脚本与测试。
- 所有接口中的小数字段统一定义为 string，覆盖 HTTP/OpenAPI、gRPC/Proto、WebSocket、事件的请求与响应；金额、价格、数量、费率、比例、百分比等均须遵守。Go 小数值及其计算统一使用 decimal.Decimal，禁止 float32/float64 及浮点中间转换；细则见 docs/PROTO_AND_MODEL.md。
- 数据库版本化迁移；禁止生产启动 AutoMigrate。不得在 protobuf 运行时内部字段上添加数据库语义。
- 已实现的实时扫描、outbox、投影和租约循环由各核心进程拥有；尚未实现的定期报表/归档任务不部署空 cron。后续定期任务仍需独立生命周期、幂等及防重叠，不依赖进程内独占。
- 外部行情首批必须接入 Binance、OKX；Bitget（用户原文 bitgit）、Bybit 为候选。每个策略实例通过 market_data_source 独立选择数据源，所选源失效不隐式换源；详细需求见 docs/PRD.md。

- 测试复用现有 Docker infra 的 PostgreSQL、Redis、Nacos、NATS 等基础服务；只创建隔离的测试逻辑资源，不额外部署基础服务，不停止或删除共享容器。
- 2026-11-04 用户进一步要求忽略现有历史订单的对账阻塞，实际启动 BTC/ETH 做市和开发测试成交。本次可通过明确的旧未知意图清单记录批准和稳定 SDK 观察，保留原 DISPATCHED、身份和全额预留后隔离历史阻塞；这替代此前“新增未知一律只能等待终态”的本次启动限制。仅隔离清单内既有意图，不自动批准未来未知，不扩大资金额度，不改撮合或 SDK。

## 阅读与迭代流程
1. 检查 git status、远程、现有实现、就近 AGENTS.md；保留用户未提交修改。
2. 从 IMPLEMENTATION_PLAN 选择依赖满足的首个任务，写明本轮验收目标。
3. 分小步完成纵向功能；资金、状态机、去重、所有权等行为先写有意义测试再实现。
4. 运行代码生成检查、格式、构建、相关测试、静态检查；涉及并发运行 race 测试。
5. 失败时定位原因并修复，不降低断言、跳过测试或屏蔽错误来通过。
6. 每个可独立验证的最小步骤完成后，先通过该步骤相关测试与检查，再更新 PROGRESS，提交本步代码、测试、生成物和文档，立即推送到指定远程的工作分支；确认远程分支包含该提交后再继续下一步骤。不得攒到阶段末尾统一提交；此流程已获用户授权，无需逐次确认。
7. 只有阻止后续独立工作的关键事实缺失才停下询问；依赖外部接口时继续模拟器及其他独立任务。

## 交易正确性
- 策略输出交易意图、目标持仓或目标订单，不直接修改核心撮合、账户余额或清算账本。
- 核心账本与撮合回报是权威；本项目持仓为可重建投影。专用自营账户使用真实资金冻结链路。
- 小数从接口十进制字符串直接解析为 decimal.Decimal，计算及比较均使用 decimal，输出回十进制字符串；明确单位、精度与舍入，不用浮点或定点替代已约定的 decimal 类型。
- 下单意图先持久化；超时进入 UNKNOWN，先查后决定重试。未确认撤单前保留资金和敞口预留。
- 对账未完成、行情不可靠、租约丢失或风险快照过期时禁止新增风险；允许受控撤单和风险减少操作。
- 同一实例一个有效执行者；所有权 epoch/fencing 必须在最终下单路径验证，不能只依赖锁。
- 成交唯一键、事件 inbox、事务 outbox、预算原子预留；不能把 JetStream 去重当作业务恰好一次。
- 有效挂单、提交中及 UNKNOWN 订单都纳入风险。不同策略共享账户时共享总预算，禁止各自独立透支。
- STP 覆盖自营主体；生产交易需真实交易网关支持及验收，不得用应用层检查冒充撮合级保证。

## 工程规则
- context 贯穿调用、数据库与网络；goroutine 有所有者、退出条件及有界队列。
- gRPC 有超时、身份认证和明确重试白名单；非幂等下单禁止透明自动重试。
- 多节点业务不得依赖本地内存独占、进程内锁或 Nacos 实例在线状态作为唯一授权。
- Redis 是缓存或辅助设施，不能成为唯一订单/资金账本。Redis 锁不替代执行端 fencing。
- deer 统一基础设施装配，业务代码使用窄接口；禁止全局单例和吞错初始化。
- 日志禁止密钥、签名、口令及完整敏感请求；每节点独立文件。指标禁止 order_id 等高基数 label。
- 不提交真实凭据、生产地址和客户资料。配置示例必须能识别为示例。
- make contract-check 在生成/verify 前检查 Go/Proto 小数和模型四字段；make lint 的类型分析禁止 Table/浮点及非迁移 Raw/Exec，make policy-test 用实际正反样例验证门禁。结构门禁不替代对 WithContext/Model、动态调用、资产精度和 SQL 行为的审查与测试，见 docs/POLICY_CHECKS.md。

## 可执行能力与完成声明
命令由 P01/P02 实现后才能宣称可运行：make doctor、generate、generate-check、fmt、lint、test、test-race、integration、e2e、build、verify、dev-up、dev-down。
已实现的公共能力、业务和命令以实际代码、PROGRESS 及验证记录为准。禁止空命令、只输出 success 的 Makefile、空 RPC 或 TODO handler 冒充完成。
没有执行的测试写“未运行”；mock/simulator 通过不等于真实接口接通；无生产凭据不影响模拟开发，但必须阻止生产完成声明。
每个任务只有验收通过才能标 done；依赖下载/容器受限记 blocked，保留诊断证据。
每步报告相关检查结果、提交 SHA、远程分支和推送结果。测试失败不提交为完成；无相关代码测试的文档步骤运行文档检查并明确范围。推送失败保留本地提交，定位权限、网络或分支保护原因，不冒充同步成功，不擅自强推或改写远程历史。

## 发布边界
本地实现、测试、代码生成、模拟交易，以及相关测试通过后逐步提交并推送到 https://github.com/hmlax/muskdeer.git，均已获用户授权，可持续自主推进。生产密钥、真实资金策略、生产部署及破坏性迁移仍需独立授权。公开仓库只提交项目相关代码与可公开资料，保留用户其他未提交修改。
用户另行明确本工作区内完成计划所需命令默认 yes，不重复逐项确认；系统及工具的强制审批仍按实际限制执行，不修改全局审批设置来绕过。
