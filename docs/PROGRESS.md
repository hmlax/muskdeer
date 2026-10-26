# 当前进度

更新：2026-10-26。

## 已完成
- 准备工程规则、需求、架构、接口约定、Proto/ORM 验证要求、实施任务与验收矩阵。
- 准备不覆盖既有文件的本地安装脚本与文件清单。

这些是准备资料完成，不是交易功能完成。2026-10-26 初次调查时仅有 Muskdeer-preparation 准备包及压缩包，无 Go 业务代码或 git 仓库；本轮按用户指定的公开仓库及逐步提交要求，进行根目录规范安装与首个 Git 文档基线交付，实际结果见最新记录及提交报告。

## 开发任务
| 任务 | 状态 | 证据 |
|---|---|---|
| P01 | done | P01.1–P01.3 工具链、生成/PostgreSQL 与框架兼容验证通过；见 docs/validation/P01-01.md 至 P01-03.md |
| P02 | in_progress | P02.1–P02.7.1 生命周期、日志、PostgreSQL/Redis/JetStream 及 Nacos 查询装配通过；自动注册/心跳、两节点调用及进程入口待接入 |
| P03 | todo | 无实际 Proto、迁移或模拟器 |
| P04–P10 | todo | 尚未实现和运行 |
| P11 | blocked | 缺真实接口合同及测试环境 |

## 下一步
P01 已完成，从 P02 的 Deer/DeerApi 与基础设施装配继续。每个最小步骤相关测试/检查通过后提交并推送远程，确认同步再继续；保持模块及工具版本固定，不重复已完成的兼容探针，不将其误读成交易功能完成。

## 每轮追加模板
日期/任务 ID：
关联需求与验收：
改动文件：
实际命令和结果：
提交/推送分支及远程核验：
未运行/失败项：
阻碍及影响范围：
下一任务：

## 2026-10-26：补充数据库强制规范

- 关联需求：用户要求每张表有 Id 自增主键、createAt/updateAt NOT NULL、deleteAt 可为 NULL；全部 GORM 数据操作使用 Model，禁止 Table 字符串选表。
- 改动：AGENTS、alwaysApply 数据库规则、Proto/ORM、deer、决策、启动指令与实施计划；细化 A01 并新增 A22，覆盖脚本、测试、事务与公共组件。
- 验证：检查文件清单、文档引用、包内 SHA-256、安装 dry-run、已有文件保留、重复安装及符号链接拒绝；同步重新打包。实际命令与结果见包根 VALIDATION.txt。
- 未运行：Go 构建、Proto 生成、数据库迁移及 A01/A22；当前没有业务实现、生成工具或数据库。
- 下一步：将包内文档安装到项目根目录后从 P01 开始，按新增规范验证生成路线。

## 2026-10-26：补充接口与 Go 小数强制规范

- 关联需求：用户要求所有接口的小数字段定义为 string，Go 小数统一使用 decimal。
- 改动：AGENTS、alwaysApply 小数规则、Proto/ORM、接口、需求、决策、启动指令及实施计划；新增 A23，覆盖全部小数字段、边界转换、精度、舍入和非法输入。
- 验证：更新安装清单与 SHA-256，检查文档引用、dry-run、完整安装、已有文件保留、重复安装、符号链接拒绝和重新打包；命令与结果见包根 VALIDATION.txt。
- 未运行：Go 构建、Proto/OpenAPI 生成及 A23 的 decimal/数据库行为测试；仍无业务代码、Go 依赖或实际数据库。
- 下一步：P01 核验并锁定统一 decimal 库，P03 将 string 契约与无损 decimal 转换落实到实际接口、模型及模拟器。

## 2026-10-26：明确外部行情源与实例选择

- 关联需求：用户指定 Binance、OKX 为必须接入的行情源，Bitget（原文 bitgit）、Bybit 为候选；每个策略实例可指定数据源。
- 改动：PRD 的 R01/R02/R04 与公共实例配置、AGENTS、架构、接口、模型、实施计划、决策、启动指令及运维；新增 A24/A25，细化 A04/A05，移除此前未指定交易所、单首源及默认聚合的冲突表述。
- 验证：更新文件清单和 SHA-256，检查文档引用、安装 dry-run、完整安装、已有文件保留、重复安装、符号链接拒绝及 ZIP 一致性；命令与结果见包根 VALIDATION.txt。
- 未运行：Go 构建、Proto/OpenAPI 生成、行情适配器和 A24/A25；当前仍只有准备资料，未实际连接交易所。
- 下一步：P03 定义来源字段及模拟合同，P05 实现 Binance/OKX，P07 完成实例选择/切换；按 A25 单独记录真实公共行情证据。

## 2026-10-26：上一轮公共组件命名记录（已被最终约定替代）

- 关联需求：此前仅修正公共构造函数拼写，最终公共模块及服务封装约定以下一节为准。
- 改动：上一轮已统一公共文档中的构造函数引用；本轮进一步确定模块、Proto 路径、统一服务封装与 API 封装。
- 验证：检查全部准备文件无旧命名残留，更新 manifest 的 SHA-256，检查文档引用、安装行为与 ZIP 内容一致性；命令与结果见包根 VALIDATION.txt。
- 未运行：Go 构建及 A02 初始化行为测试；当前仅有准备文档，没有 Go 实现。
- 下一步：按下节最终约定更新 P02。

## 2026-10-26：确定 deer、Deer 与 DeerApi

- 关联需求：用户明确公共模块 deer、构造函数 deer.NewDeer、Kratos 服务统一封装 Deer，并增加 DeerApi；澄清答复确认各业务服务分别使用统一 Deer 封装。
- 改动：公共组件规范更名为 docs/DEER.md，统一全部模块与 Proto 路径；定义 Deer 的服务生命周期与公共依赖职责，DeerApi 复用 Deer 托管 Hertz，配套 deer.NewDeerApi；同步需求、架构、接口、README、AGENTS、启动指令、P02、A02 与新增 A26。
- 验证：检查旧公共模块/类型/文件名引用残留及文档链接，更新 manifest 路径和 SHA-256，检查安装行为与 ZIP 内容一致性；命令与结果见包根 VALIDATION.txt。
- 未运行：Go 构建、Deer/DeerApi 构造及生命周期测试；当前无 Go 实现，A02/A26 尚未执行。
- 下一步：P02 实现 deer.NewDeer/deer.NewDeerApi，并完成两条装配路径的初始化失败与启停验收。

## 2026-10-26：固定 Go 1.27.3

- 关联需求：用户明确选用 Go 1.27.3。
- 改动：将 Go 1.27.3 从候选改为强制固定版本，同步 AGENTS、README、DECISIONS、CODEX_START、P01/P02、Proto/ORM 与验收环境要求；开发、生成、测试、CI 和构建统一该版本。
- 验证：检查旧版本范围及候选表述残留，更新 manifest 的 SHA-256，检查文档引用、安装行为与 ZIP 一致性；命令与结果见包根 VALIDATION.txt。
- 未运行：Go 1.27.3 工具链获取、依赖兼容、构建与测试；当前没有 go.mod、CI 或构建镜像配置，本轮仅更新规范。
- 下一步：P01 获取并实测固定版本；P02 将模块版本声明、工具链、镜像、CI 和 doctor 落实为 Go 1.27.3。

## 2026-10-26：指定公开仓库与逐步提交流程

- 关联需求：用户指定 https://github.com/hmlax/muskdeer.git 为公开仓库，并要求每步处理完成、相关测试通过后提交及推送。
- 改动：固定 module 为 github.com/hmlax/muskdeer；同步 AGENTS、README、决策、实施计划、启动指令、验收与运维的交付流程，更新此前远程推送限制；准备根目录项目规范及忽略本地准备包的 .gitignore。
- 验证：git ls-remote 匿名读取成功且无分支；检查文档引用、manifest 校验和、安装行为、ZIP 一致性及安装到项目根目录的文件一致性。具体命令与结果见提交报告。
- 交付：本步骤为首个文档基线；提交前检查暂存范围及格式，提交后立即推送并核对远程 SHA，在提交报告提供本次 SHA、分支及结果。
- 未运行：Go 构建、业务测试及 P01 工具链/数据库兼容验证；当前仍无 Go 实现。
- 下一步：P01 检查 Go 1.27.3 与依赖兼容，之后每步验证通过即提交/推送，不积累为阶段末尾大提交。

## 2026-10-26：P01.1 固定工具链与模块

- 改动：增加 .go-version、go.mod、scripts/go.py、实际 doctor/test-tooling 目标；缓存隔离到 .cache，执行 Go 命令前强制核验实际版本。
- 验证：make doctor 返回 go1.27.3 darwin/arm64；make test-tooling 的 4 项编译器门禁/退出码测试通过；git diff --check 通过。记录见 docs/validation/P01-01.md。
- 远程：前一基线提交 19b942b 已同步 main；本步检查通过后独立提交并推送 main，SHA 由本步交付报告提供。
- 未运行：业务构建、生成器兼容、PostgreSQL 往返、Hertz/Kratos 启停；本步不将 P01 标为完成。
- 下一步：P01.2 固定生成工具与依赖，验证 Proto tag 直接生成 GORM 类型及精确小数/nullable 时间的数据库往返。

## 2026-10-26：P01.2 生成模型与真实 PostgreSQL

- 改动：固定 protoc/Go 插件/GORM/pgx/decimal/lint 版本，增加隔离生成及 generate-check、StorageProbe Proto、SQL fixture、独立 PostgreSQL 测试环境、真实 verify/integration 命令和 GitHub Actions；路线见 ADR 0001。
- 验证：3 项纯 Go 契约测试、5 组真实 PostgreSQL 16.15 场景、4 项工具链门禁通过；生成差异检测与原文件保护通过；格式、lint、vet、race、构建检查通过。具体命令与版本见 docs/validation/P01-02.md，CI 结果以实际运行记录为准。
- 发现并处理：显式置空自动 updateAt 时普通 Update 会与时间回调重复赋值；数据库 NOT NULL 探针改用 UpdateColumn 关闭回调，普通业务更新仍独立验证自动时间。初次 lint 默认缓存写入受限，已将 lint 缓存统一放在项目 .cache。
- 远程：本步验证通过后独立提交/推送 main，并核对远程 SHA；前一步为 a994a62。
- 未运行：业务实体/生产迁移/自动软删除、Hertz/Kratos/Nacos/gRPC/hz 兼容及交易链路；P01 仍为 in_progress。
- 下一步：P01.3 固定并验证 Hertz、Kratos、gRPC、Nacos 与 RPC/HTTP 生成工具，再开始 P02 的 Deer/DeerApi 实现。

## 2026-10-26：P01.3 框架与生成工具兼容

- 改动：固定 Hertz/Kratos/gRPC/Nacos SDK/registry/hz 版本，增加 string 小数 Probe RPC 与生成 stub、Hertz HTTP 注解、共享 DTO query tag 生成、hz 双次生成及编译检查、Kratos 生命周期/取消/状态码、Hertz 绑定/JSON、Nacos SDK 契约与 goleak 验证；固定支持 Go 1.27 的 Sonic v1.15.4。
- 验证：make verify、make integration、vet 和文档/差异检查通过；6 项 Go 契约、5 组真实 PostgreSQL、4 项工具链门禁及 hz 生成编译通过。详细矩阵见 docs/validation/P01-03.md。
- 远程：P01.2 的 65c699c 已推送且 GitHub Actions 40287708423 success；本步骤独立提交/推送 main，核对远程 SHA，新 CI 结果按实际运行记录更新。
- 状态：P01 的工具链、依赖、Proto/GORM/PostgreSQL 和框架最小兼容验证完成，标 done。
- 未运行：真实 Nacos 接入、Deer/DeerApi 构造及所有交易业务链路；SDK fake 通过不代表真实注册发现成功。
- 下一步：P02.1 实现 Deer/DeerApi 公共生命周期和部分初始化失败清理，再分步加入实际基础设施客户端、日志轮转及多实例调用验收。

## 2026-10-26：P02.1 共用资源所有权层

- 改动：deer.Cleanup/Initializer 与初始化前计划校验、失败逆序回滚、原始/清理错误保留、同步且有上下文 deadline 的幂等并发释放；作为 NewDeer/NewDeerApi 的前置基础。
- 验证：7 项行为测试先红后绿；test -race ./deer、goleak、make lint、make verify 与 diff 检查通过；证据见 docs/validation/P02-01.md。
- 远程：P01.3 的 ccd7f13 已推送 main 并核对 SHA，[CI 40291340017](https://github.com/hmlax/muskdeer/actions/runs/40291340017) success；本步骤测试通过后独立提交/推送。
- 未完成：完整 Deer/DeerApi 构造、真实基础设施客户端及日志轮转；不将 A02/A26 或 P02 标 done。
- 下一步：P02.2 接入配置 Proto、Kratos/gRPC 与 Hertz 生命周期，落实 NewDeer/NewDeerApi 并测试启动失败回滚、独立实例及优雅停止。

## 2026-10-26：P02.2 Deer/DeerApi 构造与启停

- 改动：ServiceConfig/Listener Proto 与生成类型、NewDeer/NewDeerApi、独立 Kratos 实例、gRPC 注册与 Hertz 路由、实际监听 readiness、请求超时/流式 deadline、Run 与幂等 cleanup。API 组合一个 Deer，不重复初始化依赖。
- 验证：deer 包 24 项顶层测试（含 7 个配置拒绝分支）、race 重复 10 轮及 goleak 通过；make verify、make integration 的真实 PostgreSQL 5 组场景、vet、文档链接与 diff 检查通过。证据见 docs/validation/P02-02.md。
- 发现并修复：Hertz 进程级日期/断连 goroutine、异常 accept 循环结束后未排空请求、错误停止时 readiness 撤销偏晚；保留原断言和完整 goleak，不启用无法实例级关闭的可选 poller，Date 逐响应设置。
- 远程：P02.1 的 a23f18f 已推送 main、SHA 一致，[CI 40293644826](https://github.com/hmlax/muskdeer/actions/runs/40293644826) success；本步骤检查通过后独立提交/推送并核对 SHA。
- 未完成：真实 Nacos/DB/Redis/JetStream 工厂、JSON 日志/轮转、指标、API 认证/统一响应/OpenAPI 与六个业务进程；A02/A26 和 P02 保持未完成。
- 下一步：P02.3 实现按实例隔离的 JSON 日志及 lumberjack 轮转，再接入真实基础设施与两节点发现调用。

## 2026-10-26：明确 api 包含管理页面与后端接口

- 来源：用户明确 api 用作管理界面，并答复确认“包含管理页面和后端接口”。
- 改动：AGENTS 及 alwaysApply 管理界面规则、README、R08、服务架构、DeerApi 职责、接口、决策、启动指令及 P07；移除首版仅交付管理 API/OpenAPI 的旧默认，新增 A27 浏览器验收。管理页面覆盖登录、概览、策略/来源、订单/成交/持仓、风险/审计和异步结果；前端小数保留字符串。
- 实施约定：前端源码规划 web/admin，构建产物由 api/Hertz 托管；技术栈和构建版本在实施前锁定，未把助手默认布局记为用户指定框架。现有 DeerApi 公共 HTTP/gRPC 生命周期继续复用。
- 验证：20 份文档本地链接、旧默认清理、7 处关键范围记录及规则 frontmatter/长度检查通过；git diff --check 通过。本步骤只有文档和规则变更，未运行新的 Go、前端构建或浏览器测试，A27 尚未实现/验收。
- 远程：上一步 bc9ce8d 已同步 main，[CI 40296501012](https://github.com/hmlax/muskdeer/actions/runs/40296501012) success；本步骤文档检查通过后独立提交并推送 main，核对远程 SHA。
- 下一步：继续 P02.3 公共基础设施装配；P07 按本次明确范围交付可操作管理界面，不能只交付接口文档。

## 2026-10-26：P02.3 实例日志与同步 lumberjack 轮转

- 授权：用户要求继续按计划完成，并明确本路径内的计划命令默认 yes；持续执行已授权实现、验证和逐步提交/推送，系统强制审批仍遵守工具要求。
- 改动：日志 Proto/生成类型、deer/logging JSON/级别/脱敏/Kratos 适配、实例文件/独占锁、输出错误传播；NewDeer 先初始化日志并最后清理，API 复用。维护 lumberjack v2.2.1 的有来源 MIT 源码版本，改用同步保留/gzip、终态 Close、无覆盖备份名，避免无关闭入口的后台 worker；见 ADR 0002。
- 验证：28 项 Deer、12 项日志、7 项轮转顶层测试及现有框架/持久化回归通过；make verify、vet、完整 race/goleak、24 份文档链接和 git diff 检查通过。600 条实际 JSON 经大小轮转后逐条读取两份 gzip 和当前文件，无丢失/重复。证据见 docs/validation/P02-03.md。
- 发现并修复：未修改的上游后台协程残留、关闭后重开、异步维护、快速轮转覆盖；补充保留年龄/数量独立断言、根日志身份/inline group/大小写保护、错误 JSON 序列化绕过及目录不能被旋转为日志文件的回归。未新增 lint/goleak 忽略项。
- 远程：前一步 9e31977 已推送 main，[CI 40297970444](https://github.com/hmlax/muskdeer/actions/runs/40297970444) success；本步骤独立提交/推送并核对远程 SHA，Linux 与 PostgreSQL CI 结果按实际后续运行记录核验。
- 未完成：业务进程及框架默认 logger 桥接、自动请求字段注入、真实基础设施装配；日志同步 I/O 未作性能保证，A03/A26 和整个 P02 保持未完成。
- 下一步：P02.4 接入 PostgreSQL/Redis/JetStream 等实际客户端、显式配置/初始化与清理；继续 Nacos 两节点发现调用、健康/指标及进程入口装配。

## 2026-10-26：P02.4 PostgreSQL 池与实时依赖检查

- 改动：Postgres Proto/生成配置、密码环境变量引用、deer/postgres.Client、显式池/连接与服务器语句超时、TLS/schema/UTC 配置及幂等关闭。Deer 按日志→PostgreSQL→其他依赖初始化，API 失败逆序释放池；DB() 与 CheckReady(ctx) 提供借用访问和实时依赖检查。
- 装配接口：开发中的 RegisterGRPC/Routes 回调新增构造 ctx 与当前 *Deer，所有仓库内调用点、测试与文档同步更新；构造函数名/签名和业务 RPC/Proto 线协议不变。请求 handler 继续使用请求上下文，不捕获构造上下文。
- 验证：4 项客户端顶层单元测试（含 7 个配置拒绝分支）及注册必需依赖回归通过；make verify、带 integration tag 的 vet、race/goleak、文档/差异检查通过。真实 PostgreSQL 16.15 执行原 5 组数据库场景与新 6 组客户端场景，包括行锁产生 SQLSTATE 57014、错误密码产生 28P01、精确模型读写、池等待取消、依赖失效及 API 初始化失败关闭池。
- 隔离：复用独立测试 Compose project，在专用 muskdeer_p02 schema 使用有四个公共字段的技术 fixture，不做启动 AutoMigrate，不操作既有数据库；测试容器/网络已清理。证据见 docs/validation/P02-04.md。
- 远程：前一步 a1bb08c 已推送 main，[CI 40302800531](https://github.com/hmlax/muskdeer/actions/runs/40302800531) success；本步骤检查通过后独立提交/推送并核对远程 SHA。
- 未完成：Redis/JetStream/Nacos、业务进程入口、框架日志桥接、完整健康/指标与管理界面；真实生产 TLS/凭据、生产迁移及性能容量未验收，P02/A26 仍未完成。
- 下一步：P02.5 接入 Redis/JetStream 的真实客户端、健康与清理，再完成 Nacos 两节点发现调用和进程入口。

## 2026-10-26：P02.4 配置拒绝断言加固

- 审查发现：非法配置用例若只断言整个 New 返回错误，可能因密码引用缺失而误通过，不能独立证明配置校验拒绝。
- 修复：7 个非法配置分支直接断言无 I/O 的校验入口；连接、凭据、生命周期及真实服务器用例继续分别覆盖，未更改 PostgreSQL 运行行为。
- 验证：postgres 包单元/race 与差异检查通过后独立提交/推送。本次仅暂存测试和验证记录，Redis 实施中的依赖/Proto 变更保留至下一步。

## 2026-10-26：P02.5 Redis 缓存、期限和生命周期

- 改动：固定 go-redis v9.23.0，Redis Proto/生成配置、TLS 默认验证、口令引用、有界池与命令期限、键前缀及正 TTL 字符串缓存；Deer.Redis() 与启动/运行时健康接入，API 复用并失败回滚。禁用额外 pipeline 池和透明命令重试，超时写入保留结果未知语义。
- 验证：make verify、真实 Redis 7.2.16 的 6 组隔离场景、原 PostgreSQL 16.15 的 5+6 组场景、race/goleak、integration tag vet、26 份文档链接与差异检查全部通过；细节见 docs/validation/P02-05.md。临时容器及网络已清理。
- 发现并处理：失败 dial 恢复定时器不能在 Close 时被上游 join，用虚拟时间证明其在连接超时加一秒的已知界限内退出，保留全量 goleak；SDK WaitCount 不计取消等待，改为检查真实等待与硬连接数；socket/context 截止发布顺序边界保留 DeadlineExceeded。
- 远程：前一步 cc81575 已推送 main，[CI 40309965167](https://github.com/hmlax/muskdeer/actions/runs/40309965167) success；本步骤独立提交/推送并核对远程 SHA。
- 未完成：Redis Cluster/Sentinel、真实 TLS/生产凭据/容量、JetStream、Nacos 多节点发现、业务进程和管理页面；完整 P02/A26 保持未完成。
- 下一步：P02.6 接入 JetStream 的有界客户端、发布确认/健康/关闭，再按计划完成 Nacos、进程入口及业务闭环。

## 2026-10-26：P02.6 JetStream 同步存储确认

- 改动：固定 nats.go v1.55.0，JetStream Proto/生成配置、默认 TLS、token 引用、命名空间/已有流检查、有界同步 Protobuf 发布、稳定 ID/预期 stream 和明确存储回执；Deer/DeerApi 共用装配、实时健康及幂等关闭。禁用 SDK 发布重试/重连缓存/自动重连，连接失败安全拒绝，恢复监督后续接入。
- 验证通过：make verify、integration tag vet、race/goleak、27 份文档链接及差异检查；纯配置拒绝、准入 deadline、静默握手取消、缺少依赖注册，以及真实 NATS 2.12.15 的 8 组场景和原 PostgreSQL/Redis 回归通过，临时三容器/网络已清理；详见 docs/validation/P02-06.md。
- 审查处理：SDK 在 TCP 后另设握手 deadline 可能延长总预算，使用构造 context 关闭初始 socket 并回收取消回调；健康校验增加明确 subject 覆盖，避免流存在但本 namespace 不可发布的假就绪。
- 远程：P02.5 的 496c6f6 已推送 main，[CI 40312201177](https://github.com/hmlax/muskdeer/actions/runs/40312201177) success；本步骤检查通过后独立提交/推送并核对远程 SHA。
- 未完成：业务事件、outbox/inbox、consumer/顺序/隔离恢复、消息 HA/自动恢复、Nacos/进程入口/指标/管理页面；不将 A11/A16、完整 P02/A26 标为完成。
- 下一步：P02.7 接入真实 Nacos 注册发现并验证独立实例调用，再完成配置、日志桥接、健康/指标及业务进程。

## 2026-10-26：P02.7.1 Nacos 认证与出站发现

- 改动：Nacos Proto/生成配置、独立 HTTP transport、默认 TLS、密码环境引用、私有命名空间/分组/集群、认证缓存、有界实时查询/响应、请求取消与终态关闭；Deer.Discovery()、实际健康及 API 失败清理。SDK 全局日志/context 边界不适合直接套用，按 ADR 0003 选择官方 Naming OpenAPI；原 SDK fake 仅保留兼容契约回归。
- 验证：make verify、integration tag vet、race/goleak、真实 Nacos 2.5.2 的 4 组场景及原 PG/Redis/JetStream 回归通过；详见 docs/validation/P02-07-01.md。隔离 project 初始化两个私有 namespace、开启认证，四容器/网络已清理。
- 发现并修复：2.5.2 注册/注销实际 data 为字符串 ok，与手册 boolean 描述不同，按固定源码/实连严格断言；APIError 包装保留原 HTTPStatus/Code；拒绝 URL userinfo、重定向、畸形/超大响应，token 使用已实连验证的 Bearer header，不进 URL。
- 远程：e32b955 已推送 main，[CI 40314051234](https://github.com/hmlax/muskdeer/actions/runs/40314051234) success；本步文档/差异检查后独立提交/推送并核对 SHA。
- 未完成：生产注册/心跳、watch/gRPC ClientFactory、两节点调用、实际 TLS/ACL/跨版本与故障恢复、业务进程/指标/管理页面；P02/A02/A26 保持未完成。
- 下一步：P02.7.2 接入受 Deer 拥有的实例注册、心跳、注销，验证两个节点的发现和 gRPC 调用，再继续完整 P02。

## 2026-10-26：Nacos Linux CI 启动诊断

- 远程 [CI 40317906129](https://github.com/hmlax/muskdeer/actions/runs/40317906129) 的生成/lint/单元/race/构建通过，集成阶段 Nacos 容器以 1 退出；原脚本清理时未保留容器日志，暂未确认根因。
- 本机 arm64 的完整集成及额外隔离 linux/amd64 Nacos 启动均通过；不把本机模拟 amd64 视为 Ubuntu runner 通过证据。
- 先单独交付测试工具改进：启动异常时读取当前随机 project 的有界日志，再清理自身容器；日志读取失败仍保留原异常。5 项 Python 门禁/失败清理测试及差异检查通过，CI 步骤名称同步覆盖全部基础设施。
- 本次只暂存诊断脚本、测试、CI 步骤名和记录；P02.7.2 的已本机验证实现保留工作区，等待远程失败原因修复后独立提交。不将远程 CI 标成功。

## 2026-10-26：修复隔离 Nacos 的 JDK cgroup 探测

- CI 40322090207 的容器日志定位到旧 JDK `CgroupV2Subsystem.getInstance` 空指针，阻断 JMX/Derby 初始化；生成/lint/单元/race/构建仍通过。
- 仅对隔离测试 Nacos 设置 `JAVA_TOOL_OPTIONS=-XX:-UseContainerSupport`，显式堆大小及 Docker 隔离保持生效；不改业务客户端或跳过集成。
- 额外 linux/amd64 容器 readiness 和实际 JVM flag 检查、5 项 Python 工具测试、差异检查通过，临时容器/网络已清理；远程 Ubuntu 集成等待本次推送后的结果。
- 独立提交此修复；P02.7.2 注册/心跳代码和更严格两节点测试继续保留工作区，验证通过后另行交付。
