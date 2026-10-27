# Muskdeer — Codex 工作约定

## 目标与优先级
开发连接既有交易所的自营做市与策略交易 MVP。先阅读 README.md、docs/PRD.md、docs/ARCHITECTURE.md、docs/IMPLEMENTATION_PLAN.md、docs/PROGRESS.md。用户最新明确指令优先；既有仓库指令与本文件冲突时先记录并解决，不静默覆盖。

## 已确认工程约束
- Go 版本固定为 1.27.3，开发、代码生成、测试、CI 与构建统一使用该精确版本；P01 核验官方工具链可获取性及依赖兼容并记录结果，不擅自更换版本。禁止 rc/beta，禁止使用 latest 或浮动版本代替。
- 公开 GitHub 仓库 https://github.com/hmlax/muskdeer.git；单仓库、单 go.mod，module 固定 github.com/hmlax/muskdeer。公共目录与包名 deer，构造函数 deer.NewDeer，统一 Kratos 服务封装 Deer；API 服务封装为 DeerApi，配套构造函数 deer.NewDeerApi，详见 docs/DEER.md。
- 外部 HTTP 使用 Hertz，服务生命周期使用 Kratos，内部通信使用 gRPC；注册发现使用 Nacos。禁止替换成 Kitex、Gin 或 Kratos HTTP。
- 各业务服务分别使用统一 Deer 封装，保持独立进程和服务身份；API 使用复用 Deer 的 DeerApi 托管 Hertz，公共依赖在每个进程内只装配一次，统一管理启动、停止和清理。
- api 用作平台管理界面，交付可操作的管理前端页面和配套后端接口；通过 DeerApi/Hertz 托管页面入口与管理 API，通过 gRPC 调用业务服务。管理页面、权限与实际操作闭环按 R08/A27 验收。
- PostgreSQL + GORM；Redis；可靠消息优先 NATS JetStream；Prometheus/Grafana；JSON 日志 + lumberjack 按大小轮转。
- config Proto、持久化 model Proto 放 deer/proto；Proto 是持久化结构唯一来源，禁止手写重复 Model、手改生成代码。
- 每张表及其持久化 Model 必须有 Id 自增主键、createAt NOT NULL、updateAt NOT NULL、deleteAt 可为 NULL；字段映射、生成与迁移要求见 docs/PROTO_AND_MODEL.md。
- 所有 GORM 数据操作显式使用 db.Model(&model.X{}) 或事务中的 tx.Model(&model.X{})，禁止 db.Table("xxx")、tx.Table("xxx") 及其他变量上的 .Table(...)；适用于业务、公共组件、脚本与测试。
- 所有接口中的小数字段统一定义为 string，覆盖 HTTP/OpenAPI、gRPC/Proto、WebSocket、事件的请求与响应；金额、价格、数量、费率、比例、百分比等均须遵守。Go 小数值及其计算统一使用 decimal.Decimal，禁止 float32/float64 及浮点中间转换；细则见 docs/PROTO_AND_MODEL.md。
- 数据库版本化迁移；禁止生产启动 AutoMigrate。不得在 protobuf 运行时内部字段上添加数据库语义。
- 定期任务独立 cron；其他服务必须支持多节点。cron 单实例仍需任务幂等及防误部署重叠机制。
- 外部行情首批必须接入 Binance、OKX；Bitget（用户原文 bitgit）、Bybit 为候选。每个策略实例通过 market_data_source 独立选择数据源，所选源失效不隐式换源；详细需求见 docs/PRD.md。

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
