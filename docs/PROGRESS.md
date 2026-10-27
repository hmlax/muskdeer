# 当前进度

更新：2026-10-27。

## 已完成
- 准备工程规则、需求、架构、接口约定、Proto/ORM 验证要求、实施任务与验收矩阵。
- 准备不覆盖既有文件的本地安装脚本与文件清单。

这些是准备资料完成，不是交易功能完成。2026-10-26 初次调查时仅有 Muskdeer-preparation 准备包及压缩包，无 Go 业务代码或 git 仓库；本轮按用户指定的公开仓库及逐步提交要求，进行根目录规范安装与首个 Git 文档基线交付，实际结果见最新记录及提交报告。

## 开发任务
| 任务 | 状态 | 证据 |
|---|---|---|
| P01 | done | P01.1–P01.3 工具链、生成/PostgreSQL 与框架兼容验证通过；见 docs/validation/P01-01.md 至 P01-03.md |
| P02 | done | P02.1–P02.11 及 Linux CI 40439453258 success；六角色、多进程发现/mTLS、持久开发、固定 Go 镜像与运行层 E2E 通过 |
| P03 | done | P03.1–P03.10 值/生成模型/迁移、typed 合同、数据库租约、最终原子 fencing、模拟现货及提交后响应丢失/延迟通过；后续 OMS/节点恢复按各阶段验收 |
| P04 | done | P04.1–P04.10 持久OMS/原子预算/撤单、成交inbox/损益、自动事件与真实发布/重投、typed RPC/mTLS、Deer工作器及实际独立业务命令进程闭环通过 |
| P05 | done | P05.1–P05.9 双来源独立簿/协议/网络/A25、质量/持久发布/typed读取、owned续租/回放/接管及默认market独立命令进程闭环通过 |
| P06 | in_progress | 接入做市、目标差分、风险联动、来源失效撤单与30分钟模拟验收 |
| P07–P10 | todo | 尚未实现和运行 |
| P11 | blocked | 缺真实接口合同及测试环境 |

## 下一步
P01–P05 公共运行层、模拟交易合同、OMS/消息/投影及Binance/OKX行情层已完成，继续P06做市与行情失效撤单、后续管理/对冲/恢复/运维。每个最小步骤相关测试/检查通过后提交并推送远程，确认同步再继续；完整L1尚未完成，真实交易合同与测试环境继续单列P11。

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

## 2026-10-26：P02.7.2 实例注册、心跳与注销

- 改动：可选 Nacos registration Proto/生成配置、typed 实例写接口、受 Deer 拥有的 Publication；监听/依赖就绪后发布，停止先撤就绪/取消并 join 心跳/注销。light heartbeat 丢失实例时失败退出，不重建记录；注册结果未知也有注销回滚。
- 验证：make fmt、make verify、integration tag vet、差异/文档检查通过；5 项 Publication 单元行为、race/goleak、真实两个 Deer 节点发现/gRPC 调用、无心跳对照失效、节点停止与外部删除，以及原四种基础设施回归通过。见 docs/validation/P02-07-02.md，临时容器/网络已清理。
- 审查：复用 named 协议常量、保持 typed metadata；明确 Nacos HTTP 地址身份没有条件删除/fencing，并发 publisher 不得复用发布端点。两个节点在同一测试进程内，不冒充独立业务进程。
- 初次强化对照测试发生一次查询失败，未扩大一秒预算或增加重试；安全状态诊断后单独与完整回归均通过，未复现，保留记录。
- 远程：前一步 d0d6aa6 已推送 main 并核对 SHA，Ubuntu CI 40324554459 结果另行核验；本步验证通过后独立提交/推送并核对 SHA。
- 未完成：gRPC ClientFactory/watch、生产 TLS/ACL/HA、独立业务进程、配置/指标、API 管理界面及交易业务；完整 P02/A02/A26 仍未完成。
- 下一步：完成 gRPC 调用工厂及身份/期限边界，再接入配置、健康/指标与实际进程入口。

## 2026-10-26：远程 Nacos 验证结果

- d0d6aa6 的 [CI 40324554459](https://github.com/hmlax/muskdeer/actions/runs/40324554459) success，确认 Ubuntu runner 的 Nacos cgroup 探测修复及全套基础设施回归通过。
- 379a41f 的 [CI 40325043690](https://github.com/hmlax/muskdeer/actions/runs/40325043690) success，确认注册/心跳/注销及两个 Deer 节点调用在 Linux 也通过；两个节点仍是同一测试进程中的独立实例，不扩大完成声明。

## 2026-10-26：P02.7.3 gRPC 双向 TLS 与服务身份

- 改动：Proto/生成配置、rpcsecurity 的独立 PEM/CA 加载、TLS 1.3、精确服务 DNS SAN 身份和显式调用方许可，接入 Deer 的 Kratos gRPC server。非 loopback 明文监听在 I/O 前拒绝，loopback 仅供隔离测试。
- 验证：先红后绿的真实 Deer/TLS/HTTP2 场景，受信 API 调用成功；六种认证/许可失败均未执行 handler，自身身份/通配许可/过期服务器证书及无效配置拒绝通过。make fmt、make verify、integration tag vet、make integration、文档/差异检查通过，race/goleak 未屏蔽。见 docs/validation/P02-07-03.md，临时四容器/网络已清理。
- 远程：前一步 379a41f 已核对 main SHA 且 Linux CI success；本步独立提交/推送并核对 SHA。
- 未完成：生产 CA/分发/轮换、证书热加载/运行中到期健康告警、gRPC 调用工厂、用户权限及业务进程；不将完整 P02 标完成。
- 下一步：将新服务身份边界用于受 Deer 拥有的 gRPC 调用工厂，补齐发现、期限和连接清理。

## 2026-10-27：P02.7.4 有界实时发现的 unary RPC 工厂

- 改动：GRPCClients Proto/生成配置、独立 mTLS/服务许可、准入/期限/字节上限、typed unary facade、每次实时 Nacos 发现与严格指定实例路由；Deer.GRPCClients() 及 API 共用/逆序释放。每次连接返回即关闭，工厂关闭取消并 join 在途请求；暂不引入地址缓存、watch 或连接复用。
- 正确性：仅 WithDisableRetry 不能禁止 SDK 透明重试；发送前通过公开 ClientStream.Context 固定尝试，按原生 unary 生命周期验证响应和 EOF。原始 mTLS HTTP/2 REFUSED_STREAM+PING ACK 门禁通过，删除固定操作的负对照观察到两次请求并失败。负缓存值曾触发 SDK 空指针，已移除；细节见 ADR 0004。
- 验证：typed 响应/大小、Unavailable 不重放、实时发现、owner 缺失不回退、期限、关闭及纯配置场景；真实 Hertz API→Nacos→两个 mTLS Deer 节点链路、停 A 后 A=503/B 正常；make fmt、make verify、integration tag vet、make integration、文档/差异检查通过，race/goleak 保留。见 docs/validation/P02-07-04.md，临时容器/网络已清理。
- 远程：ac69e54 已核对 main SHA，[CI 40325975993](https://github.com/hmlax/muskdeer/actions/runs/40325975993) success；本步独立提交/推送并核对 SHA。
- 未完成：独立业务进程、配置加载/doctor、日志桥接、健康/指标、流 RPC、管理业务页面/接口和交易链路；完整 P02 仍在进行。
- 下一步：接入配置加载与配置校验入口，再完成公共健康/指标、日志桥接和真实进程骨架。

## 2026-10-27：P02.8.1 严格配置加载与 doctor

- 改动：有界普通文件/严格 Proto JSON、相对日志/PEM 路径、统一纯配置校验、安全配置错误及保留 cause；实际 cmd/doctor、服务/API 示例和 make doctor/verify 接入。先验配置不打开引用、日志或网络，依赖实连仍由构造/健康阶段验证。
- 验证：路径解析、无副作用、未知/重复/错误字段、超大/目录输入、缺身份、秘密样例值不回显、文件/取消原因；CLI 成功/失败退出状态。make doctor、make fmt、make verify、integration tag vet、文档/差异检查及完整 make integration 通过；race/goleak 保留，临时四容器/网络已清理。见 docs/validation/P02-08-01.md 和 docs/CONFIGURATION.md。
- 远程：b5059f2 已核对 main SHA，[CI 40430641267](https://github.com/hmlax/muskdeer/actions/runs/40430641267) success；本步独立提交/推送并核对 SHA。
- 未完成：实际环境/PEM/网络的 doctor 探测、日志桥接、健康/指标、独立进程入口和业务/管理页面；示例配置不计为交易部署。
- 下一步：公共健康/指标与日志桥接，然后完成隔离开发资源、运行入口及独立进程验收。

## 2026-10-27：P02.8.2 HTTP/gRPC 实时健康

- 改动：独立 loopback monitoring Proto/生成配置及 Hertz /livez、/readyz；Live 生命周期定义、标准 gRPC Health Check/List/Watch 实时依赖检查、有限 Watch 和 context/ticker 所有权。复用 API 的标准 Hertz transport/日期/请求期限/drain，不共享 listener。
- 验证：依赖关闭只使 ready/Serving 失败、liveness 保持，Watch 变化/容量/期限与停止状态通过；实际隔离 API/Nacos/mTLS 链路扩展同样通过。make fmt、make verify、integration tag vet、make integration、文档/差异检查、race/goleak 全部通过，临时四容器/网络已清理。见 docs/validation/P02-08-02.md。
- 远程：7f8f7db 已核对 main SHA，[CI 40431338750](https://github.com/hmlax/muskdeer/actions/runs/40431338750) success；本步独立提交/推送并核对 SHA。
- 未完成：Prometheus/trace/日志桥接、远程监控授权、业务风险就绪、独立进程及业务/管理页面；完整 P02 仍在进行。
- 下一步：受实例拥有的指标及日志桥接，再完成开发资源和实际进程入口。

## 2026-10-27：隔离测试动态端口

- 指标实施的 verify/单元/race 通过，但完整集成两次在 Redis 固定 55440 端口绑定失败，Go 场景尚未启动；lsof/Docker 列表未找到当前占用者，未停止其他进程或重启 Docker。
- compose fixture 改由 Docker 分配 loopback 端口；仅查询当前随机 project，将经过严格验证的实际地址传给当前测试进程和 Nacos fixture。Go integration helper 无固定地址 fallback。
- 7 项 Python 工具测试、fmt/lint、integration tag vet、完整 make integration 通过，四容器/网络已清理。见 docs/validation/P02-test-port-isolation.md。
- 单独提交此工具修复；指标代码保留工作区，随后独立交付。前一步 ef0d8cf 已核对 main，[CI 40432295463](https://github.com/hmlax/muskdeer/actions/runs/40432295463) success。

## 2026-10-27：P02.9.1 实例独立 Prometheus 指标

- 改动：Registry/Deer.Metrics()、monitoring /metrics、http/grpc/probe 计数/错误/latency histogram 与 live/lifecycle_ready gauge；固定状态域，无 ID/原路径标签，无全局注册/后台 exporter/队列，scrape 不计自身或探测依赖。
- 精度：time.Duration 纳秒直接转 decimal.Decimal 秒并累加，以标准 0.0.4 数值文本输出；业务 JSON/Proto string 合同不变。复用已锁定 prometheus/common v0.32.1（Apache-2.0）作官方格式解析门禁，仅调整为测试直接依赖。
- 验证：纳秒精确 sum、并发快照 count/+Inf 一致、未知状态合并、双 API 隔离、自抓取稳定、gRPC 成功/参数错/无期限流拒绝；make fmt、make verify、integration tag vet、动态端口完整集成、文档/差异检查、race/goleak 全部通过，临时四容器/网络已清理。见 docs/METRICS.md、docs/validation/P02-09-01.md。
- 远程：4364503 已核对 main SHA；本步独立提交/推送并核对 SHA，CI 按实际运行结果更新。
- 未完成：request_id/框架日志、trace/exemplar、业务指标、Grafana/告警、生产远程抓取授权、容量及独立业务进程；完整 P02 保持进行。
- 下一步：请求关联与日志桥接，再完成运行入口/开发资源和多进程验收。

## 2026-10-27：P02.9.2 HTTP/gRPC/JSON 日志请求关联

- 改动：typed requestid、合法单值 header 沿用/异常独立生成、HTTP/gRPC 响应及 context、unary 出站 metadata 复制透传、根 JSON 日志保护/分组关联；stream wrapper 只持有验证后的 ID，不保存 context。编号不作为权限/幂等/执行权或指标标签。
- 审查修复：Kratos 应用 Logger 选项实际调用全局 SetLogger，试接后 race 暴露多实例冲突，已移除该设置；保留并发/race，不切换库全局 logger。完整框架桥接留至单进程入口初始化。
- 验证：ID 格式/随机/取消期限、分组日志 root 防覆盖及原日志/轮转回归、HTTP 四类输入与响应/context/日志一致；真实 API→Nacos→mTLS gRPC edge ID 透传。make fmt、make verify、integration tag vet、完整 make integration、文档/差异检查通过，race/goleak 无屏蔽，临时四容器/网络已清理。见 docs/validation/P02-09-02.md。
- 远程：f9bbc6b 已核对 main SHA，[CI 40434382225](https://github.com/hmlax/muskdeer/actions/runs/40434382225) success；本步独立提交/推送并核对 SHA。
- 未完成：框架日志进程桥接、trace/exemplar、管理业务 envelope、开发资源及独立业务进程；完整 P02 仍在进行。
- 下一步：实现独立进程入口和一次性框架日志初始化，再完成开发资源和多进程验收。

## 2026-10-27：P02.10.1 独立进程与框架日志桥接

- 改动：实际 cmd/muskdeer、六角色装配与本地配置、固定工具链 build 产物、启动/停止退出码；ServiceSpec 进程日志 hook 及独立入口 Kratos/Hertz 适配器。通用多实例构造保持不修改全局 logger；框架非结构化参数值不进日志。
- 验证：六角色独立 race 子进程的 JSON/就绪/SIGTERM、Deer/DeerApi 回滚次序、取消不隐藏清理失败；真实二进制启动双 market 进程，经 Nacos 发现并 mTLS Health 调用，停止 A 后 B 继续服务，全部退出后注销为空。make fmt、make verify、integration tag vet、完整 make integration、文档/差异检查通过，race/goleak 保留；隔离容器/网络/卷已清理。见 [P02.10.1](validation/P02-10-01.md)。
- 远程：4eee6c9 已核对 main，[CI 40435494123](https://github.com/hmlax/muskdeer/actions/runs/40435494123) success；本步检查通过后独立提交/推送并核对 SHA。
- 未完成：持久开发资源、固定构建镜像及业务合同/交易/管理页面；P02 继续进行。业务进程入口不计为业务功能完成。
- 下一步：完成隔离 dev-up/dev-down 与固定版本构建镜像，再转入 P03 的持久化合同和模拟执行链路。

## 2026-10-27：P02.10.2 持久隔离开发资源

- 改动：固定 digest 的 compose.dev.yml、工作区项目身份/互斥、实际 dev-up/down/status、开发配置/环境生成及独立临时 workspace 的 dev-test；down 保留数据卷、失败不销毁数据、不接受 state 指定其他项目。新测试加入 CI。
- 验证：10 项 Python 工具测试、重复 up/六配置 doctor/实际 Deer 依赖就绪与 SIGTERM、Redis 跨 down/up 保留及 Nacos 原身份恢复、构建/Python/Compose/文档/差异检查通过；独立测试容器/网络/卷已清理。见 [P02.10.2](validation/P02-10-02.md)。
- 远程：41e4cc5 已推送 main 并核对 SHA，CI 40437513589 结果另行核验；本步检查通过后独立提交/推送并核对 SHA。
- 未完成：固定 Go 构建镜像、业务 E2E、交易合同及业务功能；开发 NATS 尚未自动创建业务 stream，P02 继续进行。
- 下一步：固定构建镜像及进程级运行检查，然后进入 P03。

## 2026-10-27：修复远端期限先返回时的错误链

- 远程 [CI 40437513589](https://github.com/hmlax/muskdeer/actions/runs/40437513589/job/121969631957) 在既有 TestRPCFactoryAppliesConfiguredDeadline 失败：业务仅调用一次，wire status 已是 DeadlineExceeded，但 errors.Is(context.DeadlineExceeded) 为 false；生成/格式/lint/其余已运行单元通过，集成未运行。不将该 CI 记成功。
- 缩减为仍有效的 caller context 收到远端 DeadlineExceeded/Canceled，新增确定性回归先红；此前仅依据本地 ctx.Err 转换，忽略远端先结束的路径。修复保留原 status/原因并附 canonical context cause，明确不暗示 caller ctx 已结束。
- 相关工厂/原始 HTTP2 不重放/race 回归重复 10 次、全包 race、fmt/lint/构建/差异检查通过，未降低断言、延长预算或重放 RPC。此前 bff52c9 的开发资源已推送 main 并核对 SHA；本修复独立提交/推送并跟进 Linux CI。

## 2026-10-27：P02.11 固定镜像与运行层 E2E

- 改动：固定 digest 的 Go 1.27.3 toolchain/build/scratch runtime、白名单 context、非 root/CA/许可证；实际本地 readiness exec 探针及 image/image-test/e2e，镜像 CI 接入。探针只验证本地监控，不构造服务或跟随代理/跳转。
- 验证：HTTP 状态/body/大小/取消/重定向；make fmt、make verify、完整 integration、make image、image-test 与运行层 make e2e、Python/文档/差异检查通过，race/goleak 保留。实际 arm64 非 root/只读/禁网容器 ready、JSON/SIGTERM 退出 0，临时资源清理；见 [P02.11](validation/P02-11.md)。
- 远程：fe2005c 已推送 main 并核对 SHA，[CI 40438145450](https://github.com/hmlax/muskdeer/actions/runs/40438145450) success，修复了前次截止错误链失败，并覆盖持久开发测试。新镜像 amd64 CI 在本次推送后核验，当前不提前标 P02 done。
- 下一步：核验 Linux 镜像与全套 CI，完成 P02 状态，再进入 P03 的 Proto、迁移、适配合同与模拟器。

## 2026-10-27：P03.1 小数与来源业务边界

- 在 P02 本机封装/E2E 已通过、镜像 Linux CI 等待期间，先实现不依赖镜像的纯领域值对象；业务只依赖标准库和用户指定 decimal，框架/数据库/Proto 在外层转换。
- 改动：显式字段精度/符号/零规则、plain string 小数解析、BUY/SELL 与精确 tick/lot 量化；集中四来源及 bitgit 别名，候选和未知来源分别拒绝，不设默认源。
- 验证：行为先红后绿，高精度/格式/零/符号/量化边界、来源选择、race、5 秒 594346 次 fuzz、make verify、imports 和文档/差异检查通过；无新依赖，无运行层变化，未重复容器测试。见 [P03.1](validation/P03-01.md) 和 [小数说明](DECIMALS.md)。
- 远程：196c3c3 已推送 main 并核对 SHA，镜像 CI 40439453258 结果单独核验；本步检查通过后独立提交/推送并核对 SHA。
- 下一步：生成版本化迁移记录/基础业务 Model 和持久化行为，继而实现交易合同及模拟器/lease。

## 2026-10-27：P02 Linux 镜像 CI 验收完成

- 196c3c3 的 [CI 40439453258](https://github.com/hmlax/muskdeer/actions/runs/40439453258) success，包含生成/lint/单元/race/构建、真实隔离基础设施/多进程、持久开发资源、固定 Go 1.27.3 的 linux/amd64 镜像构建及非 root 只读禁网运行检查。
- 与本机 linux/arm64 镜像及 make e2e 证据合并，P02 标 done，覆盖公共封装 A02/A26；不扩大为业务交易、管理页面或生产部署完成。
- 本步骤仅更新状态与证据，文档引用和差异检查通过；P03.2 迁移实现保留工作区，后续单独验证交付。

## 2026-10-27：P03.2 版本化迁移与生成记录模型

- 改动：migration Proto/生成 GORM Model、内嵌版本 SQL/精确 checksum、事务 advisory lock/历史前缀/回滚、显式 migrate -apply 工具与 build、LF 行尾；无业务启动 AutoMigrate，所有记录数据访问 Model，已审查 DDL 与固定无表锁 SQL 单独受控。
- 验证：单元先红后绿；真实 PG 并发/幂等、Id/自动时间/NOT NULL/nullable、失败 DDL 原子回滚、篡改/未来/逻辑删除/空历史拒绝、锁等待取消及实际二进制命令两次升级通过。make fmt、make verify、integration tag vet、完整 make integration、文档/差异检查通过，race/goleak 保留；随机 schema 及整体隔离资源清理。见 [P03.2](validation/P03-02.md) 和 [迁移说明](MIGRATIONS.md)。
- 远程：8601634 已推送 main 并核对 SHA；02d1036 的 [CI 40439933015](https://github.com/hmlax/muskdeer/actions/runs/40439933015) success。本步验证通过后独立提交/推送并核对 SHA。
- 下一步：追加实际基础业务 Model/迁移及 typed 交易合同，再实现模拟器与 lease/fencing；P03 保持进行。

## 2026-10-27：P03.3 可执行规范与负例门禁

- 改动：固定 protoc 描述符/Go AST 检查，生成前与 verify 的 contract-check；已有 linter 的 forbidigo 类型分析，禁止 Table/AutoMigrate/浮点和非迁移 Raw/Exec；policy-test 实测 DB/import/type 别名和方法值负例及合法 Model/元数据对照，接入 CI。
- 验证：所有目标违规诊断命中、合法对照零问题；Proto 字段/标签/nullable/嵌套 float 和 AST 正反例、race、make verify、Python/文档/差异检查通过；无新依赖及业务运行变化，未重复容器。初次正样例格式/包注释不合格，已修正样例，未降低门禁。见 [P03.3](validation/P03-03.md) 和 [规范检查](POLICY_CHECKS.md)。
- 远程：9fcaaad 已推送 main 并核对 SHA，[CI 40441442938](https://github.com/hmlax/muskdeer/actions/runs/40441442938) success，含真实迁移及完整运行层回归。本步通过后独立提交/推送并核对 SHA。
- 下一步：基础业务 Model/迁移及 typed 交易合同；结构门禁不代替最终 SQL、精度和业务行为测试。

## 2026-10-27：P03.4 交易对与策略来源身份

- 改动：三个新生成 GORM Model、版本 2 schema、集中模板/生命周期/交易元数据常量；requested/active 的来源与版本分别保存，deferred 复合 FK 保持实例/版本/来源一致；执行 venue 独立。当前不隐藏参数 JSON，typed 参数在后续合同加入。
- 验证：v1 既有库升级且旧 ledger 不变、双模板独立来源、请求/active 分离、writer 关闭后新连接恢复、另一实例不变、精确 numeric、false 更新、公共字段和 nullable；候选/错配拒绝并回滚。make generate/contract-check、fmt、verify、最终 lint、integration tag vet、完整集成、文档/差异检查通过，race/goleak 保留；独立 schema 与容器资源清理。见 [P03.4](validation/P03-04.md)。
- 远程：1b10631 已推送 main 并核对 SHA，[CI 40443330529](https://github.com/hmlax/muskdeer/actions/runs/40443330529) success；本步检查通过后独立提交/推送并核对 SHA。
- 下一步：typed 模板参数、交易意图/回报合同及模型，继而模拟器与 lease/fencing；不把持久化字段验收等同于安全换源或交易功能。

## 2026-10-27：P03.5 Typed 模板参数

- 改动：common typed 参数/oneof、domain decimal/时间及交叉校验、未知/legacy/future schema 拒绝；生成 Model 的具体 Proto JSONB 字段与版本3模板/形状/未知键/小数字符串约束。无手写PO或新依赖，JSON仅在实际存储边界。
- 验证：高精度资金刚好覆盖与最小单位不足、格式/库存/时间溢出/对冲/union边界；真实PG两模板全字段往返、非法schema/模板/形状及小数number拒绝，原完整集成回归通过。make generate/contract-check、fmt、verify、lint/vet、完整 make integration、文档/差异检查通过，race/goleak 保留；随机资源清理。见 [P03.5](validation/P03-05.md) 和 [参数说明](STRATEGY_PARAMETERS.md)。
- 远程：10e6b4b 已推送 main 并核对 SHA，[CI 40444711131](https://github.com/hmlax/muskdeer/actions/runs/40444711131) success；本步通过后独立提交/推送并核对 SHA。
- 下一步：typed 交易意图/回报合同及模型，模拟器与租约/fencing；仍不标整个 P03 完成。

## 2026-10-27：P03.6 Typed 执行合同与输入边界

- 改动：公共方向/TIF/订单/撤单枚举、Execution 提交/取消/查询/列表/账户 typed RPC；纯 domain 订单值与 Submit/Cancel 字符串小数、身份语法、native/Timestamp deadline 和请求关联。未注册空业务 handler。
- 验证：实际 Proto JSON typed 往返、高精度乘积、非法小数/方向/epoch/标识/期限/未知字段、撤单目标与取消 cause；make generate/contract-check、fmt、make verify、独立 race、文档/差异检查通过；无新依赖、未改运行层，未重复容器。见 [P03.6](validation/P03-06.md) 和 [交易合同](TRADING_CONTRACT.md)。
- 远程：6b98f4e 已推送 main 并核对 SHA，[CI 40446178125](https://github.com/hmlax/muskdeer/actions/runs/40446178125) success，含 typed JSONB 与完整回归。本步检查通过后独立提交/推送并核对 SHA。
- 下一步：执行/核心模拟器数据模型、数据库时钟 lease 和最终执行 fencing，再接入真实业务处理器；当前解析不授权交易或预留资金。

## 2026-10-27：P03.7 数据库时钟租约

- 改动：策略/账户执行租约的生成 Model 和版本4迁移；进程独立 incarnation、行锁及数据库时钟 Acquire/Renew/Release，保留 epoch，不使用 Nacos 在线状态作授权。
- 验证：真实 PG 四节点竞争一个 winner，两类租约的续期/释放接管/过期/旧 owner 拒绝/伪造本地时间/新连接恢复通过；make fmt、make verify、integration tag vet、完整 make integration、文档/差异检查通过，race/goleak 保留。见 [P03.7](validation/P03-07.md)。
- 远程：110a645 已推送 main 并核对 SHA，[CI 40447054348](https://github.com/hmlax/muskdeer/actions/runs/40447054348) success。本步通过后独立提交/推送并核对 SHA。
- 下一步：最终模拟网关原子 fencing、执行模型与可控交易效果；租约协调层不等同于 A12 或整个 P03 完成。

## 2026-10-27：P03.8 模拟目标事务约束

- 改动：模拟命令/核心订单的生成 Model、版本5及最终事务 owner/epoch/数据库时间检查，命令必须与效果同事务；复合身份、防重与状态约束，numeric NaN 防护。
- 验证：有效订单提交、两类错误授权、提交前受控租约失效、旧节点复活/新 epoch、无效果命令/改写拒绝、精确小数及 NaN；make generate/contract-check、fmt、make verify、integration vet、完整 make integration、文档/差异检查通过，race/goleak 保留。见 [P03.8](validation/P03-08.md)。
- 远程：bee8ab5 已推送 main 并核对 SHA。本步通过后独立提交/推送并核对 SHA。
- 下一步：模拟资金/冻结/成交及受控故障处理器，再接入 OMS；模型数据库约束不等于交易闭环完成。

## 2026-10-27：P03.9 模拟现货资金、成交与撤单

- 改动：版本6与直接模拟 adapter，稳定业务 fingerprint、资金冻结/费用/价差/部分成交/撤单确认、共享账户串行资金和主体 STP；原子最终 fencing 覆盖实际资金变更；所有迁移表的公共字段元数据检查。
- 验证：真实 PG 正常/重复/冲突/并发/撤单竞态/同主体跨账户/最终回滚通过；最终 make verify、race、vet、lint、完整 make integration、文档/差异检查通过，race/goleak 与隔离资源清理保留。修正期限和注入器夹具；一次 Nacos 注册失败后补 cause 诊断，独立完整重跑通过。见 [P03.9](validation/P03-09.md)。
- 远程：2c9f120 已推送 main 并核对 SHA，[CI 40448833177](https://github.com/hmlax/muskdeer/actions/runs/40448833177) success。本步通过后独立提交/推送并核对 SHA。
- 下一步：可控响应丢失/延迟等模拟合同，再接 OMS/业务 RPC；仍不将直接 adapter 等同于管理页面或完整业务 E2E。

## 2026-10-27：P03.10 提交后响应故障与基础合同完成

- 改动：交易窄接口、显式提交后回包丢失/延迟装饰器、固定场景通道；查询直达核心、未知结果保留cause且不重试。记录模拟能力矩阵及 unsupported/后续边界。
- 验证：先红后绿；单次底层调用、明确拒绝、延迟取消/join/Gate及真实PG唯一订单、一次冻结、PENDING撤单和恢复通过；make verify、race/goleak、vet、完整 make integration、最终新增用例 race/lint、文档/差异检查通过。见 [P03.10](validation/P03-10.md)。
- 远程：8263830 已推送 main 并核对 SHA；本步通过后独立提交/推送并核对 SHA。
- 状态：P03 基础任务 done，A08/A12 整体业务与恢复仍在后续阶段，L1整体尚未完成。下一步 P04 OMS、预算、outbox/inbox 和成交投影。

## 2026-10-27：P04.1 接收意图与原子预算/outbox

- 改动：版本7、5个生成 Model、typed 事件、稳定 client ID/fingerprint、授权/元数据/新鲜风险快照及账户/策略预算接收事务；提取共用授权检查，保持模拟最终检查。
- 验证：真实PG重复/冲突、跨策略并发上限、outbox故障整体回滚、typed事件小数与来源/过期拒绝；make verify、vet、完整 make integration、文档/差异检查通过，race/goleak及隔离资源清理保留。见 [P04.1](validation/P04-01.md)。
- 远程：fbaea05 已推送 main 并核对 SHA，[CI 40450644842](https://github.com/hmlax/muskdeer/actions/runs/40450644842) success。本步通过后独立提交/推送并核对 SHA。
- 下一步：派发与 UNKNOWN 查询恢复、最终风险检查及预算保留/释放，然后成交投影和事件投递；P04保持进行。

## 2026-10-27：P04.2 持久派发与 UNKNOWN 查询恢复

- 改动：版本8、core_revision及OMS命令关联、框架无关用例、持久SUBMITTING/UNKNOWN/确认/拒绝及两层预算/outbox；最终目标重新验证风险，核心成交关闭未对账风险输入；修正扩展链路锁序和FK兼容行锁。
- 验证：真实PG唯一提交/未知查询、缺单不重发、部分/全部成交预留、明确拒绝/最终风险回滚、调用方丢失恢复、旧快照/同版本矛盾及未提交核心事务并发恢复通过；make verify、最终race/lint、vet、完整 make integration、文档/差异检查通过，goleak与隔离清理保留。见 [P04.2](validation/P04-02.md)。
- 远程：5072be8 已推送 main 并核对 SHA，[CI 40451701533](https://github.com/hmlax/muskdeer/actions/runs/40451701533) success。本步通过后独立提交/推送并核对 SHA。
- 下一步：OMS撤单、成交去重与持仓投影、发布/inbox和业务RPC；P04保持进行。

## 2026-10-27：P04.3 持久撤单与竞态

- 改动：版本9、独立撤单请求/状态/typed事件、稳定核心命令命名空间、未派发原子取消及已派发的查询/受控撤单；请求未知不覆盖核心事实。
- 验证：真实PG pending完整预留、部分/全部成交竞态、回包丢失查询恢复、重复/错目标请求及outbox故障整体回滚通过；make verify、vet、完整 make integration、文档/差异检查通过，17类真实模型公共字段、race/goleak及资源清理保留。见 [P04.3](validation/P04-03.md)。
- 远程：d732906 已推送 main 并核对 SHA，[CI 40453413908](https://github.com/hmlax/muskdeer/actions/runs/40453413908) success。本步通过后独立提交/推送并核对 SHA。
- 下一步：成交自然键/inbox、持仓与损益投影、事件发布及业务RPC；P04保持进行。

## 2026-10-27：P04.4 成交去重与持仓损益投影

- 改动：版本10、3个生成模型、严格typed事件/账本小数字符串、inbox与自然键事实/投影原子事务，显式不可变起点、平均成本费用、源完整性/资产对账与重建。
- 验证：真实买卖乱序/双重去重/同ID冲突/成本损益、损坏重建、起点拒绝及投影故障回滚重放通过；make verify、最终lint、vet、最终完整 make integration、文档/差异检查通过，20类模型公共字段、race/goleak及隔离清理保留。一次发现超时后修复失败清理并补诊断，保持原预算重跑通过。见 [P04.4](validation/P04-04.md)。
- 远程：3d2250b 已推送 main 并核对 SHA，[CI 40454749837](https://github.com/hmlax/muskdeer/actions/runs/40454749837) success。本步通过后独立提交/推送并核对 SHA。
- 下一步：可靠发布/消费与实际业务RPC；P04保持进行。

## 2026-10-27：P04.5 数据库领取与真实事件发布

- 改动：数据库时间/SKIP LOCKED 领取、过期 owner 完成拒绝、持久重试与坏事件隔离，typed 单次 JetStream 发布及稳定128字节事件键；失败时保留测试容器/Nacos诊断，安全暴露状态与超时分类。
- 验证：真实PG竞争/到期接管/期限持久、发布存储后完成失败及同ID恢复；真实消费提交后 NAK 重投保持2条事实/2条inbox及精确资产。make verify、最终相关 race/lint、test-tooling、最终完整 make integration、文档/差异检查通过，goleak与隔离清理保留。两轮Nacos超时后相同预算第三轮通过，根因未确认、不声明修复。见 [P04.5](validation/P04-05.md)。
- 远程：c95b55b 已推送 main 并核对 SHA，[CI 40456737971](https://github.com/hmlax/muskdeer/actions/runs/40456737971) success。本步通过后独立提交/推送并核对 SHA。
- 下一步：自动成交事件、实际消费工作器及业务RPC；P04保持进行。

## 2026-10-27：P04.6 核心自动事件与消费完成边界

- 改动：模拟核心成交同事务产生typed outbox，使用不可变原命令绑定意图；消费先持仓/inbox，再查询核心同步OMS，两处完成才可ACK，不重发订单。
- 验证：严格精确事件构造先红后绿；真实PG outbox失败回滚全部资金/订单/事实/风险，原交易重试一次；持仓提交后中断OMS及相同事件重投，预算最终0；真实JetStream链路改用自动事件。make verify、最终lint、完整 make integration、文档/差异检查通过，race/goleak和隔离清理保留。见 [P04.6](validation/P04-06.md)。
- 远程：1da243f 已推送 main 并核对 SHA；本步通过后独立提交/推送并核对 SHA。
- 下一步：有界消费客户端、独立进程装配与typed业务RPC；P04保持进行。

## 2026-10-27：P04.7 受控 durable 消费

- 改动：已有 durable 绑定、实时范围检查、单处理者/期限/消息边界及成功后确认ACK；错误不ACK/不重试/不自动丢弃，保留安全 delivery 诊断，未创建后台 goroutine。
- 验证：配置边界先红后绿；真实NATS缺少consumer不创建、过滤器改变拒绝、坏Proto保留未确认、处理/等待取消；真实PG/JetStream自动事件经公共客户端在提交后中断，AckWait重投且attempt>1，业务效果不重复、预算0。make verify、最终lint/race、完整 make integration、文档/差异检查通过，goleak与隔离清理保留。见 [P04.7](validation/P04-07.md)。
- 远程：81d6681 已推送 main 并核对 SHA；本步通过后独立提交/推送并核对 SHA。
- 下一步：独立服务业务装配、typed RPC 和实际进程闭环；P04保持进行。

## 2026-10-27：P04.8 Typed RPC 与读取

- 改动：7个真实 execution RPC、增量取消请求查询/视图与核心版本、严格wire响应、作用域/有界读取、账户同revision快照；mTLS方法权限及执行租约提议，明确STP拒绝释放预留。
- 验证：高精度响应先红后绿；真实PG/Deer/mTLS下单/去重/改体拒绝/分页/账户隔离/部分成交费用/撤单查询；回包丢失1次Submit后查询恢复；STP拒绝保留原卖单预算101。make generate/verify、最终lint/race、最终完整 make integration、文档/差异检查通过。发现夹具先证明进程ready再计时查询，各3秒/总15秒和停止断言保留，最终通过。见 [P04.8](validation/P04-08.md)。
- 远程：647ec44 已推送 main 并核对 SHA；已验证的 execution_rpc_integration_test.go 单独补交并推送。前序 [CI 40459706274](https://github.com/hmlax/muskdeer/actions/runs/40459706274) success。
- 下一步：默认命令入口的业务工作器/客户端装配和独立进程闭环；P04保持进行。

## 2026-10-27：P04.9 Deer 工作器所有权

- 改动：公共依赖后同步装配有界任务计划，Kratos托管启动/取消/join、readiness/实时Check和异常传播，依赖等工作器退出后关闭；各业务仍独立进程。
- 验证：停止顺序/失败兄弟退出/未运行清理/坏计划回滚/启动Check失败先红后绿；真实Nacos任务异常后退出并注销。make verify、最终lint/Deer race、完整 make integration、文档/差异检查通过，goleak与隔离清理保留。见 [P04.9](validation/P04-09.md)。
- 远程：0334ade 已补齐上一笔RPC集成用例并推送 main，远程SHA已核对；本步通过后独立提交/推送并核对 SHA。
- 下一步：实际 execution/portfolio 工作器、业务配置和默认命令装配；P04保持进行。

## 2026-10-27：P04.10 实际独立业务进程

- 改动：显式业务 Proto/doctor/入口、只读完整历史检查、execution RPC/outbox和portfolio消费/RPC回查 owned 工作器；严格typed观察解码不制造执行凭证，增加reduce_only响应及窄恢复接口。
- 验证：配置/观察解码先红后绿；空库检查不建表、改写历史拒绝；真实命令双PID经Nacos/mTLS/JetStream自动完成2事实/2inbox、持仓10/现金1001.8/费用0.2/预算0及全部outbox。make generate/verify、最终lint、最终完整 make integration、文档/差异检查通过，race/goleak及隔离清理保留。修正校验和负例格式后完整重跑通过。见 [P04.10](validation/P04-10.md)。
- 远程：7bae910 已推送 main 并核对 SHA，[CI 40462194943](https://github.com/hmlax/muskdeer/actions/runs/40462194943) success。本步通过后独立提交/推送并核对 SHA。
- 状态：P04 done；完整L1仍待后续。下一步P05双行情协议/订单簿/质量/来源隔离及实连证据。

## 2026-10-27：P05.1 双来源订单簿

- 改动：有界单actor深度/typed Frame、Binance快照衔接、OKX序列链/空更新/维护重置；缺口失效、不输出旧价格、不混源，精确BBO/mid。官方核验checksum已停用，按当前序列合同实现。
- 验证：正常/重复/乱序/缺口恢复、两源隔离、删档/重复价/交叉/容量、输入内存独立及19位参考价先红后绿；make verify、文档/差异检查通过，race保留。无持久化/依赖/进程改动，未重复容器。见 [P05.1](validation/P05-01.md)。
- 远程：bb3c9cf 已推送 main 并核对 SHA；本步通过后独立提交/推送并核对 SHA。
- 下一步：typed行情JSON及两家网络适配、质量/持久化和实例选择；P05保持进行。

## 2026-10-27：P05.2 Typed 行情输入

- 改动：具体两家行情DTO/外部产品绑定/严格string小数及序列、元数据、元组校验；有界准确JSON字段/schema扫描，不用业务值map桥或浮点，不以弃用CRC判断完整性。
- 验证：精确18位价格/极小数量、正常序列/0/删档、坏类型/重复/大小写/科学记数法/NaN/缺失/NULL和取消先红后绿；make verify、最终lint/race、10秒fuzz 442362次、文档/差异检查通过。无持久化或进程改动，未重复容器。见 [P05.2](validation/P05-02.md)。
- 远程：340f648 已推送 main 并核对 SHA；本步通过后独立提交/推送并核对 SHA。
- 下一步：有界WS/REST会话及断连重建、行情质量/持久化与实例接入；P05保持进行。

## 2026-10-27：P05.3 双行情网络会话与实连

- 改动：固定公共TLS端点、Binance缓冲/REST桥接与快照刷新、OKX typed控制/订阅/心跳、消息/缓存/期限/同源重连和任务取消/join；增加只读marketprobe，无账户/交易能力。
- 验证：本地TLS两家缺口重建、原PING回显、心跳不刷新价格、慢快照溢出/取消、observer失败、超长/二进制/不可信TLS/重定向/并行Run；make verify、最终lint/相关race/goleak及文档/差异检查通过。两家实际公共行情各2个连接、快照/增量与主动断连后恢复，均退出0，A25实连子项通过；时效质量仍单列。见 [P05.3](validation/P05-03.md)。
- 远程：fa0e5a3 已推送 main 并核对 SHA，[CI 40466627525](https://github.com/hmlax/muskdeer/actions/runs/40466627525) success。本步通过后独立提交/推送并核对 SHA。
- 下一步：深度覆盖边界、行情质量/持久发布与market命令、实例按源消费；P05保持进行，P11真实交易资料缺口不影响独立工作。

## 2026-10-27：P05.4 深度覆盖与来源独立质量

- 改动：快照已知覆盖边界、typed行情/质量/模式/原因、精确价差/跳变与队列时效/时钟、来源独立基准、有界恢复及消费端再次校验。
- 验证：快照删穿反例先红后绿；静默过期/排队旧事件/未来和回退/价差/坏mid/极端指数、不同序列恢复/新连接generation/离群不污染基准或另一来源、来源/模式选择及19位mid通过。make verify、文档/差异检查通过，race保留。见 [P05.4](validation/P05-04.md)。
- 远程：f1d5601 已推送 main 并核对 SHA；本步通过后独立提交/推送并核对 SHA。
- 下一步：持久来源绑定/基准/质量与发布者fencing、market业务命令及实例按源消费；P05保持进行。

## 2026-10-27：P05.5 来源政策绑定与行情租约

- 改动：迁移11、具体Policy及两个生成模型、准确来源/产品/LIVE或REPLAY/政策不可变持久绑定；底层资产对应与数据库改写保护、独立行情租约及作用域互斥。
- 验证：边界/期限溢出/18位bps、真实PG来源/模式隔离/创建幂等/错产品/改政策拒绝、数据库改绑/删除/底层资产拒绝及新客户端恢复；4节点只有一个owner，过期与接管epoch、旧owner拒绝。make generate/verify、最终lint、最终完整make integration、文档/差异检查通过，22类模型公共字段、race/goleak及隔离清理保留。首次集成发现旧升级断言仍为10，更新当前11后全量通过。见 [P05.5](validation/P05-05.md)。
- 远程：a3d17d6 已推送 main 并核对 SHA，[CI 40473273622](https://github.com/hmlax/muskdeer/actions/runs/40473273622) success。本步通过后独立提交/推送并核对 SHA。
- 下一步：行情快照/基准持久化及最终发布fencing、market命令/typed RPC/实例按源消费；P05保持进行。

## 2026-10-27：P05.6 快照发布目标与基准恢复

- 改动：迁移12/生成快照、数据库时间/权限/持久revision/原生序列、事务提交fencing与质量、保存基准跳变保护、读取重验和处理器基准恢复；typed政策SQL检查明确拒绝NULL缺字段。
- 验证：精确19位mid/基准、旧revision/同序列改价/旧序列/异源、静默过期且租约有效、排队旧事件、接管基准/离群保护、旧owner与伪造时间；提交前失权整体回滚、NaN/缺字段拒绝通过。make generate/verify、最终lint/相关race、最终完整make integration、文档/差异检查通过，23类模型公共字段、race/goleak和隔离清理保留。首次Nacos1秒超时根因未确认，同预算第二轮完整通过。见 [P05.6](validation/P05-06.md)。
- 远程：046a979 已推送 main 并核对 SHA，[CI 40474885990](https://github.com/hmlax/muskdeer/actions/runs/40474885990) success。本步通过后独立提交/推送并核对 SHA。
- 下一步：market业务命令/租约续期/真实与显式回放装配、typed行情读取及实例来源选择；P05保持进行。

## 2026-10-27：P05.7 Typed 精确来源读取

- 改动：GetReference实际RPC、20个生成文件、窄只读handler/client、验证证书服务身份/原生deadline/具体scope；价格/数量/mid始终string，consumer类型与Proto不暴露baseline。
- 验证：19位精度/坏数/字段/metadata伪造/角色/期限/范围、客户端坏回复/错源模式/失败不重试与传输后过期；真实PG/Deer/mTLS读精确参考价，OKX缺失不回退、两源独立健康/过期、LIVE不消费REPLAY。make generate/verify、相关race、完整make integration、文档/差异检查通过，goleak与隔离清理保留。见 [P05.7](validation/P05-07.md)。
- 远程：4a25f23 已推送 main 并核对 SHA，[CI 40477173486](https://github.com/hmlax/muskdeer/actions/runs/40477173486) success。本步通过后独立提交/推送并核对 SHA。
- 下一步：market业务命令/owned租约续期/显式回放和真实连接、独立进程按源读取闭环；随后P06策略与行情失效撤单保护。

## 2026-10-27：P05.8 Owned 行情工作器

- 改动：standby/数据流/续租所有权、恢复持久revision/基准、原生期限提交与有序取消/join/释放，连接序列重置；LIVE与显式REPLAY工厂互斥，两协议同Book路径。
- 验证：DB不接受候选价不得污染本地基准、未请求cancel及panic前join先红后绿；精确回放两源、standby/失权/join及19位mid；真实PG两个owner续租/revision/有序接管，活动数据流峰值1、epoch和基准正确。make verify、最终相关race/lint、完整make integration、文档/差异检查通过，goleak与隔离清理保留。见 [P05.8](validation/P05-08.md)。
- 远程：755ed8e 已推送 main 并核对 SHA，[CI 40478906296](https://github.com/hmlax/muskdeer/actions/runs/40478906296) success。本步通过后独立提交/推送并核对 SHA。
- 下一步：默认market业务配置/Deer装配及独立命令进程按源读取；随后P06策略/风控/失效撤单闭环。

## 2026-10-27：P05.9 默认 market 独立命令

- 改动：market显式Proto配置/doctor、角色依赖与模式/参数校验、只读绑定与Deer工作器/RPC装配；不创建来源/资金/风险或要求无用broker。
- 验证：配置/重复ID/小数/期限/回放边界通过；真实双market命令PID经Nacos/mTLS/Deer factory提供准确独立参考价（Binance19位/OKX200），停止一源不影响另一源且未建任何账户/意图。make generate/verify、完整make integration、文档/差异检查通过，race/goleak与隔离清理保留。见 [P05.9](validation/P05-09.md)。
- 远程：9d5cdba 已推送 main 并核对 SHA；本步通过后独立提交/推送并核对 SHA。
- 状态：P05 done；P06 in_progress。继续做市目标/差分、风险快照生产及最终行情门禁、失效撤单和30分钟模拟运行，完整L1仍待后续。

## 2026-10-27：P05.10 远程进程撤销观察回归

- 发现：a0a94cf 的远程 CI 40482916402 在主动停止 Binance 后，旧发现地址连接超时返回 DeadlineExceeded，测试观察循环未将其作为暂态；本地 P05.9 通过记录保留，远程失败单独记录。
- 修正：确定性回归先红，只读观察循环沿用原期限重新发现；生产调用无透明重试，最终仍要求同源无价格及另一源健康。见 [P05.10](validation/P05-10.md)。
- 验证：确定性回归先红后绿；相关race、fmt/lint（0 issues）、完整make integration、文档/差异检查通过，双命令用例2.15秒。做市算法独立保留，未混入本步提交。
- 远程：a0a94cf 已推送 main 并核对SHA；此前9d5cdba的CI 40481675564 success。本步检查通过后独立提交/推送并核对SHA，远程新CI另行跟进。

## 2026-10-27：P06.1 精确做市目标

- 改动：纯目标计算、来源/模式/质量及发布授权校验、精确有理库存偏移、被动价格/量化、最小金额、含费用现金/基础币/库存及侧预算约束，见 [目标语义](QUOTING.md)。
- 验证：完整make verify、最终lint/race、5秒2worker fuzz 147818次、文档/差异检查通过；含19位精度、非法/失效行情和资金硬界，见 [P06.1](validation/P06-01.md)。不将目标等同于预留或实际订单。
- 远程：P05.10 c75e6c3 已推送 main 并核对SHA；本步独立提交/推送并核对SHA，随后继续目标差分、风险联动/失效撤单和30分钟模拟验收。P06保持进行。
