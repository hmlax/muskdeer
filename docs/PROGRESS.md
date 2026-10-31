# 当前进度

更新：2026-10-31。

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
| P06 | done | 做市/目标差分/最终风险门禁/双来源失效保护及实际30分钟808成交核算通过；P06.27记录基线，最终延迟/容量继续P10 |
| P07 | in_progress | 网格/持久控制循环、独立运行目标、授权目录/策略查询/创建/启停/发布换源/审计历史及策略风险与额度的typed gRPC/HTTP/页面、收据与待激活/预算门禁通过；双模板资金换源及风险资金关闭/恢复通过。真实OMS/核心订单及管理撤单/控制器确认/收据页面通过；行情监控双源独立进程页面、权威成交及账户持仓仓储/typed接口/页面通过，十二组浏览器线上通过；真实风险诊断历史、只读仓储及typed gRPC/HTTP通过，风险历史页面/十三组浏览器线上通过；运行概览的策略数据库观测仓储通过，服务观测/接口/页面等及完整A27待完成 |
| P08–P10 | todo | 尚未实现和运行 |
| P11 | blocked | 缺真实接口合同及测试环境 |

## 下一步
P01–P06公共运行层、模拟交易/OMS/投影、双行情、做市及实际30分钟核算阶段已完成，继续P07管理/网格及后续对冲/恢复/运维。每个最小步骤相关测试/检查通过后提交并推送远程，确认同步再继续；完整L1尚未完成，最终容量/延迟与版本验收继续P10，真实交易合同与测试环境继续单列P11。

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

## 2026-10-27：P06.2 差分、撤单屏障与创建时间

- 改动：保留兼容报价、剩余数量/部分成交/重复单处理、正常节流及超龄/保护撤单；任何撤单或不确定状态阻止全部补挂，未发送意图保护先红后绿。领域/仓库及typed OrderView加入真实创建时间，避免成交更新重置报价年龄。
- 验证：make generate/fmt、完整make verify、完整make integration、文档/差异检查通过，race/goleak保留；复杂度门禁发现后拆分校验通过，未降低断言或屏蔽。见 [P06.2](validation/P06-02.md)。
- 远程：5c80fe0 已推送 main 并核对SHA；本步独立提交/推送并核对SHA。继续风险快照生产/最终行情门禁、策略控制器及失效撤单/30分钟模拟。P06保持进行。

## 2026-10-27：P06.3 当前所选行情准入与最终门禁

- 改动：版本13及生成许可来源证据、实例不可变LIVE/REPLAY模式；准入/Prepare重读实际当前行情及发布授权，独立deferred模拟提交门禁，取消通道保留。升级关闭无来源证据的旧许可，不自动恢复。见 [风险准入](RISK_ADMISSION.md)。
- 验证：INVALID仍可凭旧flag准入的真实PG反例先红后绿；较新同owner行情可用，失权/旧epoch/未来revision/来源版本/模式/静默年龄拒绝；资金/订单已写后失权整体回滚，旧许可升级关闭。make generate/fmt、完整make verify、最终lint、最终完整make integration、文档/差异检查通过；修正故障helper未覆盖行情租约后重跑全部通过，race/goleak保留。见 [P06.3](validation/P06-03.md)。
- 远程：86848d5 已推送 main 并核对SHA，[CI 40486173886](https://github.com/hmlax/muskdeer/actions/runs/40486173886) success；此前P05.10/P06.1远程CI亦success。本步独立提交/推送并核对SHA。
- 下一步：实际风险许可生产与全范围原子限额、策略控制器及失效撤单/30分钟模拟；fixture不是生产器，P06保持进行。

## 2026-10-27：P06.4 事务风险评估与真实输入

- 改动：RiskEvaluator业务库、固定资格/拒绝原因及版本14生成模型；真实核心资产/成交序列与数量、持仓投影、显式账户/策略预算及当前所选行情同事务核验。许可按原始行情时间、实例最大年龄、数据/发布期限及5秒硬限裁剪，拒绝不编造发布者/余额。
- 验证：缺投影、未入账成交、对账后恢复、预算关闭、核心差异、发布失权、来源证据/500ms政策与100ms剩余期限、上下文边界通过；make generate/fmt、完整make verify、完整make integration、文档/差异检查通过，lint 0 issues、race/goleak保留。见 [P06.4](validation/P06-04.md)。
- 远程：0a06b37 已推送 main 并核对SHA，[CI 40487715315](https://github.com/hmlax/muskdeer/actions/runs/40487715315) success。本步独立提交/推送并核对SHA。
- 下一步：单笔/交易对/平台及最坏库存的原子限额、Deer拥有的显式风险调度与策略控制器、失效撤单和30分钟模拟。评估库尚未默认启动，P06保持进行。

## 2026-10-27：P06.4a 并行测试发现隔离

- 发现：基础设施与业务集成并行使用同一Nacos分组，market.grpc读到了另一测试CA的business-market-binance；mTLS正确拒绝。业务fixture改为独立MUSKDEER_BUSINESS_TEST，保留身份检查、期限与断言。
- 验证：最终fmt/lint、完整make integration、文档/差异检查通过，实际进程发现/退出1.99秒，race/goleak保留；同一工作树中的P06.5金额代码另笔提交。见 [P06.4a](validation/P06-04a.md)。
- 远程：744d365 已推送 main 并核对SHA，[CI 40489792346](https://github.com/hmlax/muskdeer/actions/runs/40489792346) success；本笔独立提交/推送并核对SHA，再交付已验证的金额额度步骤。

## 2026-10-27：P06.5 单笔、交易对与平台金额额度

- 改动：版本15/RiskBudget生成模型及24类四字段模型、按报价币的平台及跨账户交易对额度/单笔上限；意图/四范围预留/outbox同事务，确认释放同事务。最终模拟网关重验、持久义务总额核对和业务身份保护；迁移保留已有UNKNOWN义务且关闭新政策。
- 验证：单笔拒绝无预留、幂等、最终关闭拒绝并释放、两独立账户并发200+200争300仅一成功、计数/身份改写拒绝及历史义务迁移通过；完整make verify、最终lint、最终完整make integration、文档/差异检查通过，race/goleak保留。修正零值测试参数并单独提交发现隔离后全量通过。见 [P06.5](validation/P06-05.md)。
- 远程：eea5044 已独立推送 main 并核对SHA；本步独立提交/推送并核对SHA。
- 下一步：最坏库存的最终原子检查、显式Deer风险调度/策略控制器、行情失效撤单和30分钟实际模拟；P06保持进行。

## 2026-10-27：P06.5a 最终global门禁的锁顺序

- 发现：版本15最终global门禁先读意图/全局额度、再由另一门禁取账户预算，可能与另一意图释放形成环。受控真实PG等待点重现deadlock detected；没有将夹具未到达检查点的早期失败当作死锁证据。
- 修正：新迁移16统一账户/策略预算→意图→全局额度，保留15校验和及金额/身份断言。自有backend+测试schema检查点验证原交错先红后绿，全部SQL/任务有期限和取消/join。
- 验证：相关race、完整make verify、完整make integration、文档/差异检查通过，受控回归全套0.36秒，lint 0 issues及goleak保留。见 [P06.5a](validation/P06-05a.md)。
- 远程：b91b908 已推送 main 并核对SHA，[CI 40492051423](https://github.com/hmlax/muskdeer/actions/runs/40492051423) success；本步独立提交/推送并核对SHA。继续最坏库存与策略运行，P06保持进行。

## 2026-10-27：P06.6 最坏库存与共享配置保护

- 改动：版本17、准入/Prepare及模拟最终库存门禁；核心基础币总额+全部未终态买单剩余量，UNKNOWN保留、不用未确认卖单抵消；共享账户取同交易对全部已生效政策最低硬限，最多128个。激活与准入共用账户预算互斥，低限不得越过旧义务，typed/已引用版本不可改写。
- 验证：真实PG反例先红后绿，跨策略最低硬限、低限/版本改写拒绝、已写资产/订单超限整体回滚；完整make verify、最终lint、最终完整make integration、文档/差异检查通过，race/goleak保留。旧元数据用例明确验证生效版本不可删除并保留草稿NULL往返后全套通过。见 [P06.6](validation/P06-06.md)。
- 远程：03258fd 已推送 main 并核对SHA，[CI 40494869892](https://github.com/hmlax/muskdeer/actions/runs/40494869892) success；本步独立提交/推送并核对SHA。
- 下一步：显式Deer风险/模拟执行任务、策略所有者/持久决策及差分循环、失效撤单与30分钟实际模拟；P06保持进行。

## 2026-10-27：P06.7 Deer拥有的账户风险任务

- 改动：execution可选显式账户risk任务及有界执行租约，与RPC共享owner；启动只读检查已有账户/预算，standby不评估，候选按账户/desired_running过滤、ID排序且最多16个。每轮使用真实事务评估器，错误回到Deer撤销就绪并退出，不自动准备资产/政策。
- 验证：配置/standby/原始错误传递测试先红后绿；独立market/execution命令取得新epoch和准确500ms行情许可，停止所选market后实际持久关闭且无新订单。完整make verify、完整make integration、文档/差异检查通过，lint 0 issues、race/goleak保留；见 [P06.7](validation/P06-07.md)。
- 远程：7d0c742已推送main并核对SHA，[CI 40496425681](https://github.com/hmlax/muskdeer/actions/runs/40496425681) success；本步独立提交/推送并核对SHA。
- 下一步：显式模拟撤单确认、策略所有者/持久决策及差分循环、失效撤单和30分钟实际模拟；P06保持进行。

## 2026-10-27：P06.8 显式模拟撤单确认

- 改动：execution可选显式模拟账户/cancelDelay，与现有owner共享执行租约，standby不处理；单一Deer-owned任务有界确认到期PENDING。候选使用不可变撤单命令年龄，部分成交不重置计时；核心确认与OMS观察/风险释放继续分开。
- 验证：真实PG延迟/跨账户/部分成交及剩余资金保留先红后绿；独立execution命令实际确认，再经Nacos/mTLS观察终态才释放风险预算。完整make verify、完整make integration、文档/差异检查通过，lint 0 issues、race/goleak保留；见 [P06.8](validation/P06-08.md)。
- 远程：e1217ca已推送main并核对SHA；本步独立提交/推送并核对SHA。
- 下一步：策略执行客户端与稳定键查询、策略所有者/持久决策及差分循环、失效撤单和30分钟实际模拟；P06保持进行。

## 2026-10-27：P06.9 稳定业务键撤单恢复

- 改动：FindCancellation在账户范围按请求ID或原业务键恰好二选一查询，CancellationView回传原键；新增仓储/用例读取，复用原身份观察与OMS同步，不领owner、不重发取消。
- 验证：真实PG+mTLS反例先红后绿，首次服务端ID不保留且旧租约失效后，128字符原键可恢复PENDING及CONFIRMED/CANCELED；核心取消始终1次。选择器/长度/账户范围、完整make verify、完整make integration、文档/差异检查通过，race/goleak保留；见 [P06.9](validation/P06-09.md)。
- 远程：4a9a7d9已推送main并核对SHA；P06.7 [CI 40498399184](https://github.com/hmlax/muskdeer/actions/runs/40498399184) success。本步独立提交/推送并核对SHA。
- 下一步：策略执行客户端与当前owner路由、策略所有者/持久动作及差分循环、行情失效撤单和30分钟实际模拟；P06保持进行。

## 2026-10-27：P06.10 当前执行owner路由依据

- 改动：只读ExecutionRouting按账户及数据库当前有效租约解析经校验的完整实例名，不创建授权、不缓存旧路由；缺失、过期、退役及畸形owner关闭。此依据随后供策略客户端选择明确Deer目标，不能代替最终fencing。
- 验证：owner解析/真实PG隔离与过期/接管/畸形/退役测试先写后实现；两个独立execution命令中，B上线不改变A所有权，A退出经实际3秒TTL后B升epoch并更新路由，4.93秒通过。完整make verify、完整make integration、文档/差异检查通过，race/goleak保留；见 [P06.10](validation/P06-10.md)。
- 远程：07ed67f已推送main并核对SHA；P06.8 [CI 40499361180](https://github.com/hmlax/muskdeer/actions/runs/40499361180) success。本步独立提交/推送并核对SHA。
- 下一步：策略typed执行客户端接入明确owner、持久动作/所有者与差分循环、行情失效撤单和30分钟实际模拟；P06保持进行。

## 2026-10-27：P06.10a 风险操作期限与安全诊断

- 改动：命令日志以固定cause_kind识别包装后的deadline/canceled，私有cause继续隐藏；新增受控PG策略行锁的200ms风险进程故障用例，普通成功场景使用1秒有界预算、3秒租约，500ms行情/许可限制保持。
- 验证：首轮原普通场景200ms附近退出，旧通用日志不足以确认cause，未计通过；受控故障明确报告deadline并失败码1退出，风险关闭且revision不变，1.58秒通过；正常流程1.76秒。最终完整make verify、完整make integration、文档/差异检查通过，原六服务进程、lint/race/goleak保留；见 [P06.10a](validation/P06-10a.md)。
- 远程：cf203bc已推送main并核对SHA，[CI 40501113543](https://github.com/hmlax/muskdeer/actions/runs/40501113543) success；P06.9 [CI 40500269434](https://github.com/hmlax/muskdeer/actions/runs/40500269434) success。本步独立提交/推送并核对SHA，typed客户端另笔提交。

## 2026-10-27：P06.11 Typed策略执行客户端

- 改动：借用Deer factory/当前账户路由，Submit/Cancel绑定明确owner；原键查询/有界订单与资产观察使用无状态读取。无重试循环，已发写RPC失败/坏回包明确带ErrOutcomeUnknown并保留cause；精确typed decimal先限界后String，观察拒绝错scope/枚举/游标/资产重复，不制造授权。意图client ID公式收敛为共享纯函数并保持历史黄金值。
- 验证：先红后绿的writer目标/不明不重发/原键/精确小数/坏观察测试，真实market/execution风险和模拟任务经Nacos/mTLS完成客户端下单→查询/冻结观察→撤单终态，预算0、资金复原且核心单仅1个，2.11秒。最终完整make verify、完整make integration、文档/差异检查通过，原六进程、lint/race/goleak保留；见 [P06.11](validation/P06-11.md)。
- 远程：df469f1已推送main并核对SHA；本步独立提交/推送并核对SHA。
- 下一步：持久动作日志/策略所有者与差分循环、行情失效撤单和30分钟实际模拟；P06保持进行，完整L1仍待P07–P10。

## 2026-10-27：P06.12 持久策略动作

- 改动：生成StrategyAction/迁移18，PENDING先落库、派发前租约/revision CAS，稳定键永久绑定业务内容；WAITING与UNKNOWN均阻止新提交，重复派发/丢弃已尝试动作拒绝。只读原键OMS观察，当前owner可接管观察；独立提交末尾fence、业务身份/截止时间/状态约束和每实例256活动上限保留，未自动开权限/发单。
- 验证：先红后绿的真实PG去重/未知不重发/旧owner拒绝与原键接管、撤单期间成交/终态、未发丢弃/不复活、已写派发失权整体回滚及边界指数。完整make verify、完整make integration、文档/差异检查通过，21生成文件、25模型四字段、原六进程、lint/race/goleak保留；见 [P06.12](validation/P06-12.md) 和 [动作说明](STRATEGY_ACTIONS.md)。
- 远程：b4690cf已推送main并核对SHA，[CI 40504554295](https://github.com/hmlax/muskdeer/actions/runs/40504554295) success；本步独立提交/推送并核对SHA。
- 下一步：策略所有者/恢复与目标差分循环、行情失效撤单和30分钟实际模拟；P06保持进行，P07–P10尚待完成。

## 2026-10-28：P06.13 策略生命周期与运行身份

- 改动：迁移19及生成runtime owner/epoch，新incarnation先RECOVERING，revision/当前租约保护状态；停止/暂停完成及配置激活等待旧义务收敛。运行资格读取真实资产/投影/全部预算/所选行情，只关闭旧许可，execution另行评估开放。
- 验证：恢复、缺投影/活动动作/有效订单保护、终态停止、旧owner与epoch倒退拒绝、已写状态失权整体回滚先红后绿；最终完整make verify、make integration、文档/差异检查通过，业务73.598秒、lint/race/goleak保留。修正两处历史升级夹具新增列兼容后全套重跑，原断言保留；见 [P06.13](validation/P06-13.md) 和 [生命周期](STRATEGY_LIFECYCLE.md)。
- 远程：2a835e7已推送main并核对SHA，[CI 40508502953](https://github.com/hmlax/muskdeer/actions/runs/40508502953) success；本步独立提交/推送并核对SHA。
- 下一步：策略循环实际装配、行情失效撤单与30分钟模拟；P06保持进行，P07–P10继续执行。

## 2026-10-28：P06.14 当前报价与共享预算输入

- 改动：只读一致快照核对完整active spec，返回typed参数/精度/所选symbol/费用/核心最优价、完整订单集总额度及单笔上限、持久刷新时间和风险观察；不领取owner、发单或开放预算。见 [输入语义](STRATEGY_ENVIRONMENT.md)。
- 验证：本策略LIVE200+另一策略INTENT200争450，规划额度250且未改400预留/核心单1；旧版本/关闭预算拒绝、36位精度和边界测试先红后绿。最终完整make verify、make integration、文档/差异检查通过，业务66.473秒、lint/race/goleak保留；见 [P06.14](validation/P06-14.md)。
- 远程：d09eb0c已推送main并核对SHA；本步独立提交/推送并核对SHA后继续策略循环，P06仍在进行。

## 2026-10-28：P06.14a 进程预算与确定性发布超时

- 发现：完整集成的旧路由进程关闭与market工作器出现native deadline，原具体等待点未稳定复现；保持原设置的两个用例各独立重复3次通过，失败日志保留。
- 改动：普通成功夹具采用1秒操作/5秒清理，原3秒lease、100ms发布、500ms行情及资金/终态断言保留；新增200ms真实发布故障，通过测试schema触发器确定性阻塞UPDATE，验证回滚/退出及释放授权，生产代码未改变。
- 验证：聚焦故障/路由/客户端race、最终完整make verify与make integration、文档/差异检查通过，业务68.509秒、故障0.91秒；见 [P06.14a](validation/P06-14a.md)。控制器代码另笔交付。
- 远程：0383f89已推送main并核对SHA，[CI 40615426557](https://github.com/hmlax/muskdeer/actions/runs/40615426557) success；P06.13 [CI 40614861010](https://github.com/hmlax/muskdeer/actions/runs/40614861010) success。本步独立提交/推送并核对SHA。

## 2026-10-28：P06.15 策略恢复、报价与保护控制器

- 改动：借用业务端口的串行有界Tick，当前lease/standby、新owner恢复、原键动作查询、完整OMS观察、Maker/Diff与资金/总额/单笔/共享库存裁剪；失效撤单、终态停止及先记录Attempt再发命令，UNKNOWN绝不重发。见 [控制器](STRATEGY_CONTROLLER.md)。
- 验证：真实PG模拟器双边报价、失效保护/未确认不完成停止、确认后资产/预留归位；未知写及接管始终写调用1次；共享库存反例先红后绿、typed观察及standby/纯裁剪单元测试通过。最终完整make verify、make integration、文档/差异检查通过，业务68.509秒、lint/race/goleak保留，见 [P06.15](validation/P06-15.md)。复杂度拆分保留原断言，旧进程期限另笔交付。
- 远程：36ec6e0已推送main并核对SHA；本步独立提交/推送并核对SHA。
- 下一步：Deer拥有的strategy业务命令、实际行情失效联动/接管和30分钟连续模拟；P06保持进行，P07–P10继续完成。

## 2026-10-28：P06.16 独立strategy业务命令

- 改动：生成StrategyRuntime及显式账户/TTL配置；Deer-owned账户循环完整发现最多16实例，各自取得策略lease，通过现有Nacos/mTLS端口执行控制器；不依赖无用JetStream、不准备资金/权限。生命周期/输入revision并发变化等待下一轮观察，交易键冲突仍报错。见 [业务运行](BUSINESS_RUNTIME.md)。
- 验证：实际market/execution/双strategy进程报价→备用不抢占→旧节点退出/新epoch恢复报价→运行中原子创建新实例并自动发现→两实例终态停止、现金1000/BTC10/预留0；12.41秒通过。账户隔离/停止候选/超限拒绝及确定性并发控制请求先红后绿；完整make verify、make integration、文档/差异检查通过，业务96.972秒、lint/race/goleak保留；见 [P06.16](validation/P06-16.md)。
- 远程：d753c43已推送main并核对SHA，[CI 40617244341](https://github.com/hmlax/muskdeer/actions/runs/40617244341) success；P06.14a [CI 40617187447](https://github.com/hmlax/muskdeer/actions/runs/40617187447) success。本步独立提交/推送并核对SHA。
- 下一步：实际行情失效与成交/拒绝恢复联动、显式模拟成交任务及30分钟实际连续运行；P06保持进行，P07–P10继续。

## 2026-10-28：P06.17 未受理动作的截止证明

- 改动：迁移20增加NOT_ACCEPTED；仅当前L1合同下，原截止时间已过且持有准入互斥后无任何受理/核心历史，才可由当前owner和末尾SQL共同证明。旧键永久封闭，相关受理历史保留，取消父订单不解冻；普通缺失和已受理UNKNOWN仍保留保护。
- 修复：首轮集成策略超时后，以受控并发明确复现SHARE→NO KEY UPDATE升级和租约的死锁；统一在租约前取得策略NO KEY UPDATE，回归两笔事务成功且核心单1笔。进程失败增加有界标量状态诊断。
- 验证：原截止前/后、伪造或旧快照证明拒绝、旧RPC/核心键续期拒绝、跨截止在途准入仍WAITING、取消资金保留及真实新决策撤单归位先红后绿；最终完整make verify、make integration、文档/差异检查通过。独立进程接管/自动发现/终态停止10.45秒，lint/race/goleak保留；见 [P06.17](validation/P06-17.md)。
- 远程：6f90489已推送main并核对SHA，但[CI40618427899](https://github.com/hmlax/muskdeer/actions/runs/40618427899)接管等待失败，不计通过；本步独立提交/推送并核对SHA。
- 下一步：已受理不明结果权威恢复、实际失效/成交联动、模拟成交和30分钟运行；P06进行，P07–P10继续完成。

## 2026-10-28：P06.18 已受理恢复与双来源实际保护

- 改动：迁移21及显式NewSimulatorIntents，在原截止关闭、等待在途核心事务且无全部核心历史证据后，将INTENT/SUBMITTING/UNKNOWN原身份记REJECTED/CORE_NOT_ACCEPTED，同事务释放四级预算/outbox并永久封闭旧client ID。通用仓储保持未知/未发状态，不推广到外部网关。
- 修复：远程调用耗尽context后等待新轮观察/保护，同账户不传递已耗尽预算；固定白名单阶段/错误分类隐藏私有cause。受控测试明确复现instrument UPDATE阻塞另一策略动作外键，改NO KEY UPDATE保持核心互斥且允许KEY SHARE；资金/STP/成交/最终fence及相关并发回归全部通过。
- 验证：实际两market/execution/strategy进程中，分别停止Binance或OKX，失效实例撤清且不换源，健康实例继续新增报价，原来源恢复后重新双边报价，最终两实例终态停止且现金1000/BTC10/冻结及预留归零。最终完整make verify、make integration、文档/差异检查通过，业务99.119秒、双向保护12.78秒、lint/race/goleak保留；失败与诊断如实保留，见 [P06.18](validation/P06-18.md)。
- 远程：9037765已推送main并核对SHA，[CI40621673148](https://github.com/hmlax/muskdeer/actions/runs/40621673148)success；本步独立提交/推送并核对SHA。
- 下一步：已受理取消的无效果恢复、实际成交与30分钟连续运行；P06仍进行，P07–P10继续完成。

## 2026-10-28：P06.19 已受理取消的无效果证明

- 改动：迁移22及显式NewSimulatorCancellations，仅原期限关闭、等待在途核心事务且无全部CANCEL命令历史时，拒绝原请求并封闭旧核心键；不更新父订单、余额或任何预算，通用仓储保持原观察。核心提交/取消显式READ COMMITTED，数据库在首次意图查询前拒绝旧快照准入。
- 验证：未发取消截止前/后、旧键续期/核心调用/删除拒绝、新决策实际取消后才释放；有核心命令不误判、提前/旧快照/有证据伪造拒绝，以及跨期限在途核心事务末尾回滚的实际锁等待先红后绿。父LIVE/NONE、真实冻结及四级预留保持；完整make verify、make integration、文档/差异检查通过，lint/race/goleak保留，见 [P06.19](validation/P06-19.md)。
- 远程：5365fa3已推送main并核对SHA，[CI40627481803](https://github.com/hmlax/muskdeer/actions/runs/40627481803)success；本步独立提交/推送并核对SHA。
- 下一步：实际成交及30分钟真实时长的连续模拟；P06进行，P07–P10继续完成。

## 2026-10-28：P06.20 实际策略部分成交与消息投影

- 改动：新增四独立业务进程的真实时钟场景，策略自行报价，受拥有的外部流动性驱动按实际核心订单产生买卖部分成交；同自然键重复返回同ID/费用，实际outbox/JetStream/inbox/OMS/持仓自动收敛。
- 验证：6秒实际驱动后终态停止，四级预留和冻结归零；核心订单均有先行OMS，3个核心/投影/inbox成交事实，现金/持仓/费用可由事实逐项解释，成本/PnL精确重建一致。最终完整make verify、make integration、文档/差异检查通过，场景8.75秒、业务110.807秒、lint/race/goleak保留，见 [P06.20](validation/P06-20.md)。
- 远程：e5555ec已推送main并核对SHA；本步独立提交/推送并核对SHA。
- 下一步：有界长运行驱动及实际30分钟验收；本步6秒不代替A19，P06仍进行，P07–P10继续。

## 2026-10-28：P06.21 有界真实时长驱动

- 改动：新增固定30分钟和12秒短场景，共用四独立业务进程/实际成交驱动；每两秒一致快照验证核心资金/冻结、四级预算、最坏BUY敞口和投影，停止后精确重建。独有消息流有界扩容，安全日志/结果保存在.cache/soak；超时/异常只清理拥有的进程组及Compose，短场景或清理失败绝不标记长验收通过。
- 验证：聚焦实际12,000ms、6次部分成交通过；完整make verify、make integration、15项Python工具测试、117份文档链接/差异检查通过，lint/race/goleak及原资金断言保留。修正产物路径混用及新配置业务3秒/客户端2秒期限不匹配，失败日志如实保留，见 [P06.21](validation/P06-21.md) 和 [运行说明](STRATEGY_SOAK.md)。
- 远程：0c163c3已推送main并核对SHA，[CI40629411292](https://github.com/hmlax/muskdeer/actions/runs/40629411292)success；e5555ec的[CI40628438505](https://github.com/hmlax/muskdeer/actions/runs/40628438505)success。本步独立提交/推送并核对SHA。
- 下一步：从本步已同步版本执行实际30分钟并记录结果；A19尚未完成，P06保持进行，P07–P10继续。

## 2026-10-28：P07.1 网格纯报价算法

- 改动：独立纯算法按精确有理价格生成2..200个等差价位，复用实际资金/费用分配与所选行情校验；不借未确认SELL库存，不移动跨本地盘口的固定价位，越界返回保护原因。目标差分支持现有200订单上限；未装配管理API/控制器，不声明网格完整交付。
- 验证：固定价位、18位tick、薄资金/费用/硬库存、无base、越界/坏来源/过期/极端输入及200级差分测试，完整make verify、119份文档链接/差异检查通过，lint 0 issues、race保留。最小名义金额夹具修正记录于 [P07.1](validation/P07-01.md)，算法见 [网格说明](GRID_QUOTES.md)。本步纯算法不重复运行容器集成。
- 远程：b4bb208已推送main并核对SHA；本步独立提交/推送并核对SHA。
- 并行状态：b4bb208的正式长运行约80秒触发报价采样间隙断言，资金/投影检查未发现差异；两秒采样与频繁撤换报价存在相位问题，失败结果accepted=false已保留，随后提高真实报价观测频率并保持原10秒上限重跑。P06/P07均未完成，P08–P10继续。

## 2026-10-28：P06.22 报价观测及停止准入竞态

- 改动：实际核心双边报价改为每100ms观测，两秒资金快照/10秒间隙上限保留，gap和违规样本落盘，失败结果保留已观测时长/成交。新短场景另发现停止提交在目标计算与Plan/Attempt之间时，正确的本地风险拒绝会错误终止工作器；仅这两个本地拒绝等待下一轮重新观察，PENDING保留待丢弃，不调用执行端。
- 验证：两种真实PG竞态先红后绿，最终STOPPED、零执行写/核心单/活动动作及原资金/四级预算；非法输入/键冲突/失权/真实deadline仍报错的单元反例通过。完整make verify、原默认make integration、120份文档链接/差异检查通过，lint/race/goleak保留。首次长运行及新短场景失败均保留，不计A19通过，见 [P06.22](validation/P06-22.md)。
- 远程：6468bf0已推送main并核对SHA；b4bb208的[CI40630785541](https://github.com/hmlax/muskdeer/actions/runs/40630785541)success。本步独立提交/推送并核对SHA。
- 下一步：从本步已同步版本重新进行30分钟真实时长验收，同时继续P07的独立工作；完整L1尚未完成。

## 2026-10-28：P06.23 风险快照观测语义

- 发现：7a19bc2长运行30.96秒时误将保留的AllowNewRisk单独当成开放许可，未考虑核心成交同事务将Reconciled置false；原失败产物accepted=false保留。实际准入同时检查两位、期限及其他门禁。
- 改动：观测分别记录原allow、风险对账位、风险快照自身资格及源/投影序列；冻结/先行OMS/资金/风险错误明确定位，失败样本落盘。仅调整测试观测，不改变生产风险或资金行为。
- 验证：真实下单/部分成交导致投影滞后时旧测试先红，修复后观察通过且实际新意图仍被拒绝；伪造当前已对账风险必须继续报错，消费真实成交后追平。PG/race、fmt-check/lint 0 issues、四实际进程12秒smoke及121份文档链接/差异检查通过，原10秒间隙/成交/终态资金断言保留；本测试步骤未重复全套默认integration，见 [P06.23](validation/P06-23.md)。
- 远程：7a19bc2已推送main并核对SHA；本步独立提交/推送并核对SHA。
- 下一步：重新执行固定30分钟，P06/A19未完成；P07管理登录及后续P08–P10继续。

## 2026-10-28：P07.2 持久管理身份、会话与审计

- 改动：3个Proto生成模型/迁移23，角色/独立CSRF、bcrypt固定cost12、256bit随机会话仅摘要持久化；数据库时钟固定到期、登录失败5次/5分钟锁定、8会话上限/最旧撤销，credential_version关闭旧权限、不可变会话/审计。构造不创建账号；前端版本从官方源核验并锁定，传输与页面仍待接入。
- 验证：真实PG/race角色矩阵、摘要/成本、CSRF、注销不复活、失败计数、旧权限关闭、到期/禁止续期、会话上限/撤销审计、审计失败事务回滚、28模型四字段及并发迁移通过；完整make verify、原默认完整make integration、123份文档链接/差异检查通过，lint/race/goleak保留。修正空库测试未Apply及历史版本预期后全量重跑，见 [P07.2](validation/P07-02.md) 和 [认证基础](ADMIN_AUTH.md)。
- 远程：4eb6ae6已推送main并核对SHA，[CI40632680044](https://github.com/hmlax/muskdeer/actions/runs/40632680044)success；7a19bc2的[CI40631997376](https://github.com/hmlax/muskdeer/actions/runs/40631997376)success、6468bf0的[CI40631192734](https://github.com/hmlax/muskdeer/actions/runs/40631192734)success。本步独立提交/推送并核对SHA。
- 并行长运行：4eb6ae6在880,901ms/395次成交后因Nacos心跳及消费deadline退出，accepted=false；Nacos有5.18秒GC停顿记录，不仅据此归因，完整失败证据见P07.2。本步后保持原预算/断言独立重跑，不与另一套容器集成并行。A19未完成，后续管理HTTP/业务接口/页面和P08–P10继续。

## 2026-10-28：P06.26 重复确认独立期限与锁诊断

- 改动：成交驱动的首次成交及同自然键确认分别使用1秒context，区分调用失败与ID/费用不一致；真实PG受控600ms+600ms回归先红后绿，实际成交事实仍1条，核心资金/去重逻辑未变。长场景接入只读、有界且随测试取消join的进程锁链诊断，保留首个持续等待样本。
- 验证：PG/race预算回归及原核心部分成交/冻结/终态去重、最终fmt-check/lint 0 issues、四实际进程12秒/6次部分成交与终态精确核算、文档/差异检查通过。本步测试专用改动未重复全套默认integration，见 [P06.26](validation/P06-26.md)。
- 失败保留：67aef31长场景739,806ms/337次已观测成交后旧通用重复确认错误；首次新短场景strategy.actions等待策略行锁超时，加入诊断后未重现，不宣称根因已修复。两次失败accepted=false，12秒通过不代替A19。
- 远程：67aef31已推送main并核对SHA，[CI40634728857](https://github.com/hmlax/muskdeer/actions/runs/40634728857)success；本步独立提交/推送并核对SHA。继续固定30分钟及P07管理页面/后端；完整L1仍待P06–P10。

## 2026-10-28：P07.2a 认证测试的独立请求期限

- 发现：3f0470c已推送main并核对SHA，但[CI40637742239](https://github.com/hmlax/muskdeer/actions/runs/40637742239)在两个认证场景28.38/27.98秒失败；旧总context25秒、错误无cause，不能把“locked identity logged in”当作实际绕过证据。完整verify通过，其余集成无FAIL。
- 改动：独立开通/登录分别5秒上限，整组多次cost12顺序场景90秒；请求耗时/context/error可定位且不含秘密，原角色/锁定/到期/会话上限及审计断言保留，生产认证和HTTP期限不变。
- 验证：4个真实PG/race场景通过（24.53/24.81/5.25/28.09秒，包84.326秒），fmt-check/lint 0 issues、文档链接/差异检查通过；本步测试专用修改未重复全套Nacos集成，见 [P07.2a](validation/P07-02a.md)。本步独立提交、推送并核对SHA，远程CI重新验证；完整L1仍待P06–P10。

## 2026-10-28：P07.3 实际管理登录页面与身份接口

- 改动：DeerApi/Hertz借用唯一PG接入显式身份入口和本地账户开通CLI，固定Origin/Host、严格JSON、HttpOnly会话/独立CSRF、固定错误；React实际登录/会话/注销页面、深路由与嵌入产物，精确Node/npm/依赖及官方归档校验，有界工具链/浏览器/测试资源，CI加入前端重建/真实浏览器，镜像含产物/许可。
- 验证：完整make verify、身份与静态页面边界/race、工具链4边界用例、确定性重建、真实PG/CLI/API+Chromium登录/刷新/注销/旧会话拒绝/移动布局通过（9.86/3.89秒），实际镜像及非root只读运行、127份文档链接/差异检查通过。默认完整Nacos integration交CI再跑；沙箱失败及早期失败保留，最终详见 [P07.3](validation/P07-03.md) 和 [运行说明](ADMIN_HTTP.md)。
- 远程：dc7b3e1已推送main并核对SHA，认证修正CI仍待结果；本步独立提交/推送并核对SHA。
- 下一步：策略配置/来源/版本及运行管理的typed业务gRPC、实际查询/操作页面、网格控制器装配和完整A27；P06/A19实际30分钟仍在运行，完整L1尚未完成。

## 2026-10-28：P07.4 网格接入持久控制循环

- 改动：同一Controller按spec/typed参数选择Maker/Grid，混合/错配不回退；Grid固定价位0偏价容忍、100ms刷新及1小时维护年龄，沿用当前所选源、预算/最坏库存裁剪、持久动作/租约/最终核心门禁及越界RECOVERING撤单。
- 验证：实际PG/race两场景先红后绿，四固定价位稳定保持，越界只撤不补、原参考回归才恢复，停止必须核心确认终态后完成，现金/BTC基线、冻结/四级预留0；原Maker/UNKNOWN接管/并发停止回归通过（包28.165秒）。完整make verify、文档/差异检查通过，lint 0 issues及race保留，见 [P07.4](validation/P07-04.md)。默认完整Nacos integration交远程CI再验证。
- 远程：04b1f05已推送main并核对SHA，管理页面CI仍在运行；本步独立提交、推送并核对SHA。
- 下一步：typed管理业务gRPC及策略创建/参数/来源/运行操作页面，独立进程/两来源网格和完整A27；P06固定30分钟仍在运行，P08–P10继续。

## 2026-10-28：P06.27 实际30分钟时长与核算通过

- 结果：四实际业务进程连续1,800,006ms、808次部分成交，866资金样本无违规，最大报价/投影间隙5700/5757ms，最大预留39.999998066、最坏base10.21024028≤20；停止后核心/OMS终态、冻结/四级预留0、808事实投影/inbox及费用/成本/PnL精确重建一致。四进程/独有基础设施正常清理，accepted=true。
- 证据：实际make soak退出0；原产物、dirty基线3f0470c、同时开发负载、最终金额及摘要如实记录于 [P06.27](validation/P06-27.md) 和 [结果JSON](validation/P06-27-result.json)，本步文档/证据JSON检查后独立提交推送。原失败不覆盖；本次不声称后续P07控制器/迁移已运行30分钟。
- 远程：586339a已推送main并核对SHA；04b1f05管理页[CI40640011562](https://github.com/hmlax/muskdeer/actions/runs/40640011562)success，dc7b3e1认证修正[CI40639554093](https://github.com/hmlax/muskdeer/actions/runs/40639554093)success。
- 状态：P06 done；A19时长/核算部分通过，延迟/容量及最终版本仍待P10。继续P07持久暂停目标及管理业务前后端、P08–P10，完整L1未完成。

## 2026-10-28：P07.3a 原生 HTTP 测试资源清理

- 发现：586339a[CI40640597226](https://github.com/hmlax/muskdeer/actions/runs/40640597226)管理原生场景业务断言后，进程清理5秒耗尽而失败；旧测试借用默认全局HTTP连接池，未在自己API停止前关闭空闲连接。
- 改动：测试拥有有界Transport并先关闭客户端池再SIGTERM，原3秒请求/5秒停止、角色/CSRF/审计和进程0退出断言均保留，不改生产transport。
- 验证：真实PG/race原生场景及连续3次独立schema/进程通过，Deer在途请求/监听失败drain race、最终fmt-check/lint 0 issues及文档/差异检查通过；见 [P07.3a](validation/P07-03a.md)。本步独立提交/推送并核对SHA。
- 并行状态：P07.5迁移/暂停接管及历史带数据升级已通过聚焦验证；首次全套发现两处旧schema夹具漏省略新字段，修正后完整integration重跑中，尚未提交。P06长运行记录506450a已同步；完整L1继续P07–P10。

## 2026-10-28：P06.28 远端受理前风控拒绝保留原动作

- 改动：Attempt后SUBMIT明确ErrRiskDenied只等待新轮观察，原动作/键/截止保留；不重发或直接判无效果，只有现有数据库截止证明可NOT_ACCEPTED，其他错误及取消错误原样传出。测试诊断保留阶段/cause/SQL状态；Grid在原期限内等四档真实LIVE/CoreOrderID，阶段计数包括此前确认拒绝历史，不用固定Tick次数冒充收敛。
- 验证：单元及真实PG确定性先红后绿，Submit1次、原键查询、无核心/四预算效果、原期限证明后才解不确定并STOPPED；原Maker/Grid/UNKNOWN/停止/暂停回归通过（包39.766秒），完整make verify、文档/差异检查通过，lint 0 issues/race保留，见 [P06.28](validation/P06-28.md)。组合默认integration仍重跑中，由后续及远程CI验证，不计已通过。
- 边界：P07.5完整套件曾有一次粗错误、独立暂停3次通过；本步受控发现的明确风险拒绝不被当作该粗错误的已证实根因。迁移24/暂停字段继续单独交付，P06.27长运行不包含本修改；最终版本性能/完整L1继续P10。
- 远程：bd911fe已推送main并核对SHA，[CI40642383703](https://github.com/hmlax/muskdeer/actions/runs/40642383703)success。本步独立提交/推送并核对SHA；后续P07–P10继续。

## 2026-10-28：P07.5 持久请求目标与暂停接管

- 改动：Proto生成desired_state及迁移24，RUNNING/PAUSED/STOPPED与bool一致；新owner先RECOVERING后仍按持久请求完成暂停，明确停止即使bool不变也生效，恢复仍走原授权/健康/终态链路。仓储和最终SQL核对请求目标，运行状态不覆盖请求。
- 升级：42703先红；真实v23带数据回填暴露55006待处理触发器问题，在ALTER前执行排队约束校验并恢复原延迟模式后通过。v12/v14旧夹具省略尚不存在的新列，真实历史请求/UNKNOWN额度和风险关闭保留；旧迁移校验和不改。
- 验证：暂停接管、原取消不重发、明确停止/恢复/最终资金归零、非法目标/NULL/矛盾bool、最终SQL拒绝错终态并完整回滚、历史升级及原业务回归通过。最终完整make verify、原默认完整make integration通过（业务271.824秒），lint 0 issues/race/goleak、文档/差异检查保留，见 [P07.5](validation/P07-05.md)。早期粗错误不虚构根因，独立发现的远端风险拒绝已由P06.28单列修复。
- 远程：60b1a80已推送main并核对SHA；组合完整integration也验证P06.28，本步独立提交推送并核对SHA。后续typed管理业务gRPC、真实配置/运行操作页面及P08–P10继续，完整L1仍未完成。

## 2026-10-28：P07.6 授权策略列表、详情与版本查询

- 改动：每次只读RR事务内校验持久会话/用户/角色/到期，实例与版本按自增ID有界分页，详情区分请求/生效配置来源；Maker/Grid显式decimal参数、历史schema0不补默认值、故障固定公开文本。构造及查询不创建交易资源。
- 验证：真实PG/race三个场景17.442秒通过，注销/旧凭据拒绝、双模板双源、18位精度、游标/边界/不存在资源正确；独立事务中途插入新版本，RC反例先红、RR后绿，新的查询才见新版本。完整make verify、lint 0 issues、文档/差异检查通过，见 [P07.6](validation/P07-06.md) 和 [查询合同](ADMIN_STRATEGIES.md)。首次身份开通期限失败如实保留，未提高期限/降低断言；完整Nacos集成由CI再验证。
- 远程：b20fd3c已推送main并核对SHA；60b1a80的[CI40643956720](https://github.com/hmlax/muskdeer/actions/runs/40643956720)success。本步独立提交推送并核对SHA，继续管理gRPC/实际查询与操作页面及P08–P10，完整L1未完成。

## 2026-10-28：P06.30 本地真正到期后保留动作并重新观察

- 发现：P07.7组合验证的旧报价/暂停/双来源场景多次因strategy.quote原生期限退出；部分PG日志为策略行锁等待取消，完整阻塞根因未确定。另一次portfolio启动期限失败保留，未虚构共因或吞掉消费者错误。
- 改动：仅本地输入读取、Plan/Attempt当前ctx及错误链均确实DeadlineExceeded时等待新一轮；原持久动作、键、截止保持，未到期deadline和关键联合错误仍传播，不继续SQL/远端调用或直接判未受理。
- 验证：单元先红后绿，实际PG/race让真实Plan/Attempt事务先提交再到期，原动作保持、远端写零、原截止证明/Discard后STOPPED和资金/四级预留正确，包14.157秒通过；完整verify及后续组合结果见 [P06.30](validation/P06-30.md)。本步先单独交付可重复验证的修正，不将此前完整集成失败计作通过，P07.7及P09/P10继续。
- 远程：8eee86a已推送main并核对SHA，[CI40645724348](https://github.com/hmlax/muskdeer/actions/runs/40645724348)success。本步独立测试、提交推送并核对SHA；完整L1未完成。

## 2026-10-28：P07.7 typed管理查询gRPC与显式装配

- 实现：API-peer-only的typed列表/详情/版本RPC，持久viewer校验、原生期限/大小/未知字段门禁、精确参数编码，strategy显式management_enabled；管理模式无账户/交易工作器或资金/租约。账户发现与各实例Tick分别取得完整原native期限，生命周期父取消继续传播。
- 局部验证：两600ms操作共用1秒预算反例先红后绿；完整make verify、最新lint 0 issues、真实管理进程/API证书/会话、错误证书/注销拒绝、双模板来源及18位小数通过。NATS连续空拉取无handler/ACK通过；未修改消费实现或期限，见 [P07.7](validation/P07-07.md)。
- 组合限制：三次默认集成及首次聚焦组合分别在旧来源/消费者启动/暂停原生期限失败，原因如实保留；P06.30单独修正后，管理、暂停、成交及双来源恢复通过，但另一Grid仍未在原20秒收敛四档，组合99.776秒未通过。不标完整交易兼容/L1已绿，P07仍in_progress，P09/P10继续诊断。先交付已验证的只读管理边界，为独立HTTP/页面提供真实数据，未开放管理交易写入。
- 远程：9e0f49d已推送main并核对SHA，当前CI继续；本步提交推送并核对SHA，默认完整集成由CI继续验证。管理页面/操作与P08–P10持续推进。

## 2026-10-28：P07.8 实际业务gRPC的只读HTTP入口

- 改动：API显式strategy_queries_enabled，通过Deer已有Nacos/mTLS客户端提供三个GET查询，Cookie自构造业务会话、原生3秒期限/规范参数、typed Proto实际HTTP序列化，长int64与小数string，固定错误及能力标识；无本地业务表读取/交易写入。
- 验证：HTTP/race包1.458秒，长游标9007199254740993不舍入、18位精度/非法及重复参数/无Cookie不触发RPC/写方法405/错误脱敏；真实独立API→Nacos/mTLS→strategy→PG场景8.71秒通过，分页、双模板来源/版本、注销后旧Cookie业务拒绝、零核心订单及资源清理。完整make verify、lint 0 issues、文档/差异检查通过，见 [P07.8](validation/P07-08.md)。本步未重复完整交易套件/浏览器/镜像，CI继续。
- 远程：07cd36a已推送main并核对SHA；本步独立提交、推送并核对SHA。继续真实策略页面、配置/控制操作及P08–P10；原交易组合限制保留，P07/完整A27/L1未完成。

## 2026-10-28：P07.9 实际策略页面与浏览器查询闭环

- 改动：能力导航、列表/详情/版本分页和具体Maker/Grid参数，分别显示实际/请求状态及来源，未生效/旧schema不补数据；typed字符串小数/int64、BigInt ID校验，加载/真实空数据/404/故障重试/会话失效，深链接刷新及移动布局。无业务响应mock、交易写按钮或新增前端依赖。
- 验证：真实独立API/strategy/PG/Nacos/mTLS及Chromium，21实例/21版本分页、两来源/模板、18位小数、刷新、网络中断重试、注销会话、390px及真实空库通过。首次15.70秒，最终新浏览器17.44秒、原浏览器10.50秒/原生HTTP0.88秒再次通过，四类截图已查看。确定性4产物SHA相同，完整make verify、含browser夹具lint 0 issues、实际镜像、文档/差异检查通过，见 [P07.9](validation/P07-09.md)。
- CI：07cd36a的[CI40649914521](https://github.com/hmlax/muskdeer/actions/runs/40649914521)及118876f的[CI40650636833](https://github.com/hmlax/muskdeer/actions/runs/40650636833)success，包括原默认完整集成。新增策略浏览器进入CI/e2e，CI整体20分钟用于新增场景，业务/场景期限不变；本步未重复本地默认完整交易集成，旧失败保留供P09/P10。
- 远程：118876f已推送main并核对SHA；本步独立提交、推送并核对SHA，继续创建/配置版本/换源/控制的幂等操作与审计、其余管理查询及P08–P10，完整A27/L1未完成。

## 2026-10-28：P07.10 运行控制事务与结果证明基础

- 改动：Proto生成操作模型/迁移25、operator及事务内CSRF/到期、操作者幂等键/revision、请求目标/旧风险关闭/不可变审计/ACCEPTED同事务；被动观测只以实际状态、当前fenced owner或已清空订单/动作/预算证明SUCCEEDED，已替换目标记SUPERSEDED，身份和终态历史不可改删。
- 验证：真实PG/race重复/并发键、改内容/旧revision/权限及CSRF拒绝、审计失败完整回滚、等待锁时到期零效果、SQL禁止伪完成/改键、真实Grid未确认撤单不完成/确认后暂停/恢复/停止及资金冻结/四级预留正确；v24升级保留原状态来源参数、29模型公共字段及原迁移回归通过。完整make verify及最终fmt/lint 0 issues、140份文档链接/差异检查通过，见[P07.10](validation/P07-10.md)。
- 失败保留：聚焦组合148.983秒因四项身份准备超过原5秒期限未通过；只重测这四项，73.816秒全部通过。默认完整集成中原认证及新控制全部通过，但迁移包432.816秒因原Grid回归2档不收敛、双strategy接管recover期限及Binance恢复quote期限失败；原期限/断言/bcrypt成本未改，实际进程/锁链证据保留，不声称全套兼容或L1完成，继续诊断恢复/收敛路径。
- 远程：ca458ca已推送main并核对SHA，[CI40652422852](https://github.com/hmlax/muskdeer/actions/runs/40652422852)success。本步通过检查后独立提交推送并核对SHA；继续托管结果观测、typed写gRPC/HTTP/页面。当前仍未开放管理写传输，完整A27/L1未完成。

## 2026-10-28：P06.31 本地恢复与观察到期重读

- 发现：P07.10默认完整集成保留Grid回归收敛、双strategy接管recover及Binance恢复quote期限失败。检查确认Transition/Active/Discard/Observe尚未复用P06.30本轮到期处理，不能仅凭stage断定这些是全部失败共因。
- 改动：仅在明确本地仓储返回边界，本轮ctx及错误链都DeadlineExceeded时等待新一轮，原动作/owner/版本/截止保持；关键联合错误、初始取消Tick、租约获取/续期及远端交易错误分类保留，不延长期限或自动重发。
- 验证：四端口单元先红后绿、完整usecase/race1.534秒；实际PG先提交Transition/Observe再耗尽原2秒Tick，两场景3.06/3.11秒、包7.805秒通过。下一轮原键停止/实际核心取消终态，无重复提交，BTC10/现金1000、冻结及四级预留零。完整make verify、141文档链接/差异检查及实际PG/Nacos/mTLS聚焦交易73.720秒全部通过，含原Grid越界回归、双strategy接管、暂停/成交/停止和双来源恢复，原期限/档数/资金断言不变，见[P06.31](validation/P06-31.md)。默认完整全量未重复，由CI继续；原失败保留，最终容量/失效矩阵继续P09/P10。
- 远程：4e1d207已推送main并核对SHA，[CI40658831170](https://github.com/hmlax/muskdeer/actions/runs/40658831170)仍在运行；本步检查后独立提交推送并核对SHA，P07写传输/页面和P08–P10继续，完整L1未完成。

## 2026-10-28：P07.11 typed控制gRPC与Deer托管结果

- 改动：独立StrategyControl的ControlStrategy/GetOperation，API-peer-only、原生期限/大小/未知字段门禁、具体命令/结果/运行见证及固定错误；显式control_enabled依赖management。Deer-owned游标观测每页16条，越过未完成首屏，每个存储调用独立期限并服从父取消，不创建交易账户/资金/lease或授予风险。
- 验证：四相关包单元/race、虚拟时钟独立预算/父取消/公平翻页、真实PG/Nacos/mTLS及两独立strategy工作器11.42秒/原只读进程5.26秒通过，包18.213秒；实际权限/独立CSRF/回放/冲突/注销拒绝及工作器唯一终态见证、零资金交易资源，原模式未注册控制。完整make verify、lint 0 issues、文档链接/差异检查通过，见[P07.11](validation/P07-11.md)。本步未重复原完整交易集成/浏览器/镜像，由CI继续。
- 远程：ce59711已推送main并核对SHA，CI40660046474仍在运行；4e1d207的[CI40658831170](https://github.com/hmlax/muskdeer/actions/runs/40658831170)success，包括默认完整集成/两个浏览器/镜像。本步独立提交推送并核对SHA，继续HTTP/页面及创建/版本/换源、其余管理查询和P08–P10，完整A27/L1未完成。

## 2026-10-28：P07.12 快速目标替换保留中间事实

- 发现：实际PG连续启动→停止→启动、不运行观测器，旧启动收据仍ACCEPTED，回归9.17秒先红。旧目标可能在两次轮询间改变又改回，不能只靠异步比较当前值保留替换事实。
- 改动：控制事务在当前实例锁/CAS后，将目标/配置/来源不再匹配的旧ACCEPTED收据记SUPERSEDED，并绑定当时实际见证；沿用现有SQL证明，目标/风险/旧结果/新审计/新收据原子提交或全部回滚，无新迁移。
- 验证：新快速替换及末尾审计失败回滚9.20秒、原收据/权限/并发/到期/真实Grid资金启停及升级，六场景PG/race包45.468秒通过；旧键回放不复活、最终请求保持独立ACCEPTED，核心无虚假交易。完整make verify、lint 0 issues、文档链接/差异检查通过，见[P07.12](validation/P07-12.md)。本步未重复原全套交易/双进程/浏览器/镜像。
- 远程：6619210已推送main并核对SHA；ce59711 CI40660046474唯一失败为原控制审计场景登录夹具5780ms超原5秒、未抵达控制事务，实际日志已核对，交易恢复无FAIL，不称全套通过。后续分离持久会话夹具与真实登录验证，原期限/成本/认证覆盖保留。本步独立提交推送并核对SHA，继续HTTP/管理页面与P07–P10，完整A27/L1未完成。

## 2026-10-28：P07.13 同源控制HTTP与独立身份夹具

- 改动：API独立strategy_controls_enabled，借用同一Deer/Nacos/mTLS客户端提供POST具体实例control及GET具体operation；固定Origin/唯一Cookie/独立CSRF/幂等键、严格字符串JSON/revision、原生期限、202/Location/终态回放200/冲突409及固定错误，能力不代替持久权限。Hertz PeekAll借用缓存覆盖键的初次测试失败经固定版本源码确认，改GetAll复制后通过。
- 夹具：控制测试显式准备真实生成用户/会话，公开测试hash仍cost12，原认证代码/套件/5秒期限未改；实际API继续真实密码登录，不把预置会话当登录通过。防止控制断言先因无关重复instrumented密码准备超时，失败原记录保留。
- 验证：HTTP/race1.495秒、完整make verify/生成/Hertz/fmt/lint 0 issues/19工具测试/单元/race/构建通过；实际独立API/strategy/PG/Nacos/mTLS及原控制组合包52.101秒全部通过，新HTTP9.59秒覆盖真实登录、回放/409/终态及viewer/注销拒绝，真实资金Grid控制9.69秒、双控制节点/快速替换/到期/升级和原只读进程/HTTP均通过。文档链接/差异检查通过，见[P07.13](validation/P07-13.md)。本步未重复原本地全量交易/浏览器/镜像，前端未改。
- 远程：cbb09bf已推送main并核对SHA；[CI40661261060](https://github.com/hmlax/muskdeer/actions/runs/40661261060)（6619210）及[CI40662301281](https://github.com/hmlax/muskdeer/actions/runs/40662301281)（cbb09bf）success，含全套集成/两个浏览器/镜像。本步独立提交推送并核对SHA，继续控制页面、配置/换源/其余管理能力和P08–P10，完整A27/L1未完成。

## 2026-10-28：P07.14 实际运行控制页面与资金Grid浏览器

- 改动：按角色/能力提供启动、暂停、恢复、停止确认和原因表单，绑定修订/原幂等键；结果未知保留原请求，独立操作页有界观察实际见证，401/403/409及深链接/移动布局。旧结果不混入新提交，无浏览器持久存储或新增依赖；查询模式不暴露控制页面。
- 验证：四实际API/market/execution/strategy命令经真实密码登录及Chromium依次启动/暂停/恢复/停止，最终20.67秒/包22.302秒通过。每轮四档真实LIVE核心订单及精确冻结/四级预留，暂停/停止确认终态后BTC10/现金1000、冻结/预算零；最终8核心订单/0成交/4成功操作。真实202后丢失响应、原键重试仅一效果，错误CSRF拒绝后恢复凭据/原键完成，viewer无写按钮。原身份/查询/新控制组合浏览器均通过，最终三类截图已查看，见[P07.14](validation/P07-14.md)。
- 工程：确定性4产物SHA一致、完整make verify、lint 0 issues/单元/race/构建、实际非root只读镜像/readiness/SIGTERM及145文档链接/差异检查通过。首次lint的测试if/else问题修正为switch；未延长原业务期限或降低断言。本地默认完整交易未重复，新增资金浏览器进入CI/e2e。
- 远程：27ea59b已推送main并核对SHA，[CI40664320331](https://github.com/hmlax/muskdeer/actions/runs/40664320331)success，含默认完整集成/两个原浏览器/镜像。本步检查后独立提交推送并核对SHA；继续创建/参数/换源所需真实目录、写接口/页面及其余管理流程和P08–P10，完整A27/L1未完成。

## 2026-10-28：P07.15 创建前的授权持久目录

- 改动：独立AdminCatalog仓储返回真实L1账户/主体/enabled/精确费率、执行交易对精度/状态及指定instrument+LIVE/REPLAY的实际行情绑定。每次当前viewer、只读RR、1–100页/排他ID游标/软删除，缺失交易对明确not-found，不将候选源/绑定冒充健康或交易许可，不创建资金/策略/订单/lease/快照。
- 验证：repository/domain单元/race、两个真实PG目录及原控制收据PG/race通过（最终包8.264秒），18位费率及38位交易对精度、disabled/双源外部符号/模式隔离/分页/真实空数据、注销与credential变更后拒绝；并发独立提交新LIVE绑定当前快照不可见，下次可见。完整make verify、lint 0 issues/生成/单元/race/命令构建、146文档链接/差异检查通过，见[P07.15](validation/P07-15.md)。初次夹具遗漏迁移、角色修改违反原credential保护及测试lint失败均保留，已修正且未改变生产认证/原期限。
- 远程：ed097b3已推送main并核对完整SHA，CI40670898680运行中；本步检查后独立提交推送并核对SHA。此步只交付仓储，目录typed gRPC/HTTP、创建/版本/换源事务和表单随后接入；本地未重复浏览器/镜像/默认完整交易，完整A27/L1及P08–P10仍待完成。

## 2026-10-28：P07.16 typed目录gRPC与实际HTTP

- 改动：独立StrategyCatalog、具体账户/交易对/feed/质量策略Proto及26生成文件，随management_enabled显式装配；API借用同一Deer/Nacos/mTLS客户端提供三个只读GET。API-peer/current viewer、原生期限/大小/未知字段/规范Cookie/页/显式mode、小数string及long int64、固定错误/no-store，无新交易资源或前端修改。
- 验证：三个相关包单元/race及完整make verify/生成/Hertz/格式/lint 0 issues/单元/race/三个命令构建通过。最终真实PG/Nacos/mTLS及两独立命令组合包24.900秒通过：目录3.15秒、原查询HTTP8.66秒、原查询进程11.43秒；38位金额、双源分页/LIVE隔离、实际密码登录及逐次恢复旧Cookie仍401、TLS允许portfolio的业务权限拒绝、资金/预算/实例/租约/订单零，见[P07.16](validation/P07-16.md)。
- 诊断与启动检查：首次非API证书选到了TLS allowlist之外，修正夹具后另遇首个Nacos.New/Ping超时；原失败/GC/命名日志保留，不扩大原期限或虚构统一根因。预备原先只查console/namespace，新增实际鉴权命名查询，单次≤1秒、最多5次/总≤5秒的启动前传输观察，拒绝/异常结果立即失败；生产客户端/场景/容器期限不变。启动合同先红后绿、最终22工具测试/147文档链接/差异检查通过。
- 远程：004c4ea已推送main并核对SHA，ed097b3的[CI40670898680](https://github.com/hmlax/muskdeer/actions/runs/40670898680)success，含默认全套集成/三组管理浏览器/持久开发/镜像。本步检查后独立提交推送并核对SHA；本地未重复完整交易/浏览器/镜像，继续创建及配置/换源写事务和表单、其余管理流程和P08–P10，完整A27/L1未完成。

## 2026-10-28：P07.17 受审计策略创建事务

- 改动：具体创建请求/初始收据、Proto生成AdminStrategyCreation及迁移26；当前operator/CSRF/数据库到期、同一操作者键串行、按instrument→account共享锁校验实际账户/预算/持仓锚点/所选source+mode绑定及最小金额。实例CREATED/请求STOPPED、typed初始配置、零预留独立预算、审计及不可变收据原子提交，回放保留原效果。来源不fallback、不授予风险或创建账户资金；P08前显式拒绝未实现的hedge开启。
- 验证：真实PG先红后绿，两个模板×两来源的四初始定义及精确参数；同键并发仅一效果、改原因/来源冲突、同名新键冲突、审计失败整体回滚、跨真实数据库1秒会话到期后SQL拒绝且五类元数据全部回滚、历史收据不能改删。十项PG/race及原升级/迁移/30模型公共字段8.535秒通过，domain/repository单元/race及巨大指数边界通过，见[P07.17](validation/P07-17.md)及[创建合同](ADMIN_CREATION.md)。
- 工程：完整make verify、27生成文件一致性/Hertz/格式/lint 0 issues/规范正反例/22工具测试/全包单元/race/实际命令构建通过。原资金Grid控制8.52秒、实际CLI重复应用26为1.89秒，组合11.776秒；原暂停/运行请求历史升级0.64秒/包2.032秒通过。149文档链接/差异检查通过，原SQL文件无改动；首次残留未使用导入及测试过短键已修正，原门禁不放宽。
- 远程：c8bd3a0已推送main并核对SHA，[CI40673406777](https://github.com/hmlax/muskdeer/actions/runs/40673406777)运行中。本步检查后独立提交推送并核对SHA；尚未注册创建gRPC/HTTP/表单，下一步接入这些入口，再继续参数版本/换源及其余管理流程和P08–P10；完整A27/L1未完成。本地默认全量交易/浏览器/镜像未重复，由远程完整CI继续。

## 2026-10-28：P07.18 typed创建gRPC与同源HTTP

- 改动：独立StrategyCreation及具体CreateStrategyInput/CreationReceipt、29生成文件，复用control_enabled/strategy_controls_enabled显式写能力；API POST /api/v1/strategies由同一Deer客户端调用原子创建事务。具体参数/typed mapper、API-peer/未知nested字段/原生期限/大小、独立Cookie/CSRF/key/固定Origin，严格JSON重复/未知/小数string及规范身份string，201/Location/原键回放及固定400/401/403/409/503，无新资金或账户工作器。
- 验证：三个相关包单元/race1.448/1.453/1.441秒、完整make verify/生成/Hertz/fmt/lint 0 issues/规范正反例/22工具测试/全包单元/race/构建通过。真实独立API/strategy/PG/Nacos/mTLS组合11.923秒通过，新创建1.61秒含真实operator密码登录、双模板双源四创建/原键回放、初始参数及零预留/改内容409、注销后恢复旧Cookie原键仍401、真实viewer登录POST403/零核心订单；原目录3.70秒、查询进程5.07秒及查询模式Creation Unimplemented通过，见[P07.18](validation/P07-18.md)。150文档链接/差异检查通过。
- 远程：d7b6e03已推送main并核对SHA，c8bd3a0的[CI40673406777](https://github.com/hmlax/muskdeer/actions/runs/40673406777)success，含默认全套集成/三组管理浏览器/持久开发/镜像。本步检查后独立提交推送并核对SHA；创建表单、参数版本发布/换源和其余管理流程仍待后续P07，P08–P10/完整A27/L1未完成。本地前端未改，未重复浏览器/镜像/默认全量交易，远程完整CI继续。

## 2026-10-28：P07.19 实际目录驱动的创建页面

- 改动：列表按能力/角色开放创建表单，真实账户/交易对每页100及加载更多、禁用/HALTED、明确模式绑定与来源重选、实际精度、具体Maker/Grid参数及原因；价格/预算/库存/ID/毫秒保持string，档数有界整数，对冲关闭。6秒/卸载取消、同步提交锁、未知请求保留原键/全部内容，错误及初始收据/详情明确不等于运行，刷新列表不卸载请求记录，无新增依赖/浏览器持久存储。
- 修正与证据：精确下拉框标签超时后加显式可访问名称；禁用option在测试库isDisabled返回false而真实DOM属性true，以原生disabled及CSS :disabled双验证保留业务要求。手机390px root曾568，实际定位sr-only表头辅助文本逃出局部滚动容器，仅加table-scroll position:relative修复；不隐藏整页overflow或删辅助文本。失败日志/截图保留，见[P07.19](validation/P07-19.md)。
- 验证：真实独立API/strategy/PG/Nacos/mTLS及Chromium，102账户/102交易对分页、目录网络故障重试/禁用/暂停、空LIVE不回退、双模板×双源四实际创建、18位金额价格、真实201丢响应原键重试、详情刷新及viewer只读通过。SQL恰好4收据、四新实例CREATED/无owner/零预留，原核心余额全行不变/订单零。最终四浏览器组合通过：创建13.78秒/包15.575、原登录3.84秒及原生0.81秒、原查询15.60秒、资金Grid控制22.39秒；最终四类截图已查看。
- 工程：最终4产物重复构建SHA一致；完整make verify及资源最终再验证、29生成文件/Hertz/fmt/lint 0 issues/规范正反例/22工具测试/全包单元/race/实际命令构建、实际非root只读镜像/readiness/JSON日志/SIGTERM、151文档链接/差异检查均通过。创建浏览器进入CI/e2e，原业务及进程期限未增，本地未重复默认完整全量交易。
- 远程：0151034已推送main并核对SHA，[CI40677003379](https://github.com/hmlax/muskdeer/actions/runs/40677003379)success，含默认全量集成/三组原浏览器/持久开发/镜像。本步检查后独立提交推送并核对SHA；继续参数版本发布/换源、撤单/风险开关/其余管理业务页面及P08–P10，完整A27/L1未完成，不以四元数据创建冒充新实例真实报价或最终版本时长验收。

## 2026-10-29：P07.20 配置激活与预算同步

- 改动：首次激活/换active对时，按原交易准入锁序确认义务清理和策略预留严格为零，Maker两倍side_budget/Grid total_budget与配置、状态、风险关闭同事务提交。保留disabled及共享额度/资金，原active恢复/相同额度重放不改预算修订；迁移27增加提交前typed额度/零预留约束，旧SQL不改、模型仍30。见[P07.20](validation/P07-20.md)。
- 验证：先复现旧额度不变、残余预留激活及SQL绕过，再修复；真实PG/race组合16.627秒，双模板18位精度、指定OKX、原STOPPED/disabled、实际LIVE/UNKNOWN及cancel pending保留200预留直至核心确认、库存/提交失权全部回滚、26→27历史额度/预留不变、原控制/接管/迁移CLI通过。完整make verify、29生成文件/Hertz/fmt/lint 0 issues/规范/22工具/单元/race/构建及152文档链接/差异通过。
- 完整集成：首次业务包350.960秒唯一失败为原多进程接管的Nacos lookup及后续heartbeat/mutation deadline，具体停顿原因未证实；保留原期限/断言，单独原测试10.05秒通过，再默认完整组合复验通过：持久化0.474秒、基础设施31.673秒、全部业务327.761秒，接管8.86秒。失败记录保留，不把专项代替完整组合。
- 浏览器：受限进程先在configuration阶段失败，经桌面浏览器权限授权后原make admin-control-browser真实21.05秒/包22.533秒通过，启动/暂停/恢复/停止、丢响应原键/CSRF/viewer及SQL资金终态通过；最终停止结果截图已查看。无前端改动，未重复其他三组浏览器/镜像。
- 远程：97d2182已推送main并核对SHA，[CI40692087413](https://github.com/hmlax/muskdeer/actions/runs/40692087413)success，含默认全量集成/四浏览器/持久开发/镜像。本步测试后独立提交推送核对SHA；继续参数发布/换源管理入口及剩余P07、P08–P10，完整A27/L1尚未完成。

## 2026-10-29：P07.21 审计参数发布及持续风险门禁

- 改动：新增Proto生成不可变AdminStrategyPublication及迁移28。当前operator/CSRF、双乐观版本、actor幂等、明确来源/固定模板/typed规范参数、开通依赖校验；新版本取最大历史编号加一，只推进requested/revision，关闭旧风险并同步替代绑定旧对的控制收据，配置/审计/收据同事务。实际state/owner/desired/active及所有订单/预算/资金保留；SQL提交前权限/审计/效果证明及不可改写。见[发布合同](ADMIN_PUBLICATION.md)与[P07.21](validation/P07-21.md)，尚未装配RPC/HTTP/页面。
- 修复：先复现健康旧Binance源在发布同源参数或换OKX后被周期风控重新开放。requested≠active时，周期评估持续关闭CONFIGURATION_PENDING，规划/OMS准入及最终核心SQL独立拒绝新风险，旧撤单/成交观察继续可用；模拟旧写入者开放旧许可也无法新增核心订单。首次误选PG驱动范围主动取消并清理；缺feed夹具误改不可变绑定被既有SQL正确拒绝，改为未开通所选源的合法夹具，不改约束。
- 验证：最终真实PG/race组合22.452秒，双模板×双源及18位精度、六重原键单效果、两个真实操作者竞争单版本/单冲突、后续版本后原收据、审计失败回滚旧暂停替代、实际跨会话到期全部回滚、原LIVE/200预留不变、viewer/CSRF/候选/模板/缺绑定拒绝、不可改写/删除及注销后重放拒绝、全部31模型公共列通过。完整make verify、30生成文件/Hertz/fmt/lint 0 issues/规范/22工具/全包单元/race/命令构建、154文档链接/差异通过。
- 完整集成/浏览器：默认make integration一次完整通过：持久化0.421秒、基础设施34.328秒、全部业务348.642秒，含原所选源切换/未知/账务/恢复/管理HTTP/迁移CLI28，多进程接管9.34秒。实际资金Grid控制浏览器18.02秒/包19.596秒通过，四命令/原键/CSRF/viewer及终态资金预留；最终停止截图已查看。无前端改动，未重复其他三组浏览器/镜像。
- 远程：2d52e4f已推送main并核对SHA，[CI40801783746](https://github.com/hmlax/muskdeer/actions/runs/40801783746)success，含默认全量集成/四浏览器/持久开发/镜像。本步检查后独立提交推送并核对SHA；继续发布RPC/HTTP/页面及剩余P07、P08–P10，完整A27/L1未完成。

## 2026-10-29：P07.22 typed配置发布gRPC及HTTP

- 改动：独立StrategyPublication/具体Input/Receipt、共享typed映射，strategy control opt-in注册；API POST /api/v1/strategies/:id/configurations沿控制能力借用同一连接。规范路径及双版本字符串、完整未知/重复/小数检查、当前Cookie/独立CSRF/key/Origin/期限，201历史收据与详情Location、错配成功503，不代替运行证明。见[P07.22](validation/P07-22.md)。无新增schema/模型/依赖，生成32。
- 验证：相关gRPC/HTTP/business单元/race2.823/1.766/1.735秒，高位ID/修订/18位价格及独立身份、固定错误/无效字段边界、错配收据拒绝通过。完整make verify、32生成文件/Hertz/fmt/lint 0 issues/规范/22工具/单元/race/命令构建通过。实际API→Nacos/mTLS→strategy→PG组合17.811秒：新发布5.13、原目录4.09/创建1.51/查询5.46；真实operator双模板×双源四组发布/原键、409、非API peer拒绝、双侧查询模式关闭、注销旧Cookie401/viewer403通过。4收据/9版本、原实际state/目标/active/owner/预算不变、零订单预留及全余额行不变。
- 范围/远程：2767e2d已推送main并核对SHA，本步检查后独立提交推送核对；P07.21的[远程CI40804952067](https://github.com/hmlax/muskdeer/actions/runs/40804952067)继续核对，不预报结果。未改前端，未重复本地浏览器/镜像/默认全量交易；继续发布/换源表单及剩余P07、P08–P10，完整A27/L1未完成。

## 2026-10-29：P07.23 发布参数与换源页面

- 改动：详情按能力/角色/typed参数开放编辑，以实际requested参数填充、固定身份/模式/模板/code，实际来源绑定选择；基线修订/版本与最近requested/active/目标独立显示。共用创建的精确字符串草稿规则、无新依赖；未知/403冻结完整请求/原键，查询data=null仍保留表单，冲突后显式载入、成功历史收据不冒充激活。第五组浏览器进入CI/e2e。见[P07.23](validation/P07-23.md)及[页面合同](ADMIN_PUBLICATION.md)。
- 修复：新增命令最初遗漏web.py允许入口而未进入浏览器，修正映射/choices；实际丢响应后精确原因textarea定位超时，分阶段定位到禁用状态检查，创建/发布原因加稳定aria-label，保留全部断言/原期限。失败日志/截图保留。移动端无横向溢出，长账户摘要可折行。
- 验证：实际API/strategy/PG/Nacos/mTLS及Chromium双模板×双源四UI发布、真实并发推进后409/显式载入、真实201丢响应/观察新v3后原键重试、18位字段/刷新/来源读取故障、viewer/零存储通过；5发布收据/10版本，四新实例CREATED/STOPPED/无active/owner，零订单预留/原余额全行不变。最终五浏览器组合通过：原登录9.14及原生2.12秒、查询22.13秒、资金控制26.04秒、创建10.20秒、发布12.04秒，所有原资金/状态断言不变；最终四发布截图已查看。
- 工程/远程：最终4资产重复构建SHA相同；完整make verify、32生成文件/Hertz/fmt/lint 0 issues/规范/22工具/单元/race/构建、实际非root只读无网络镜像/readiness/JSON日志/SIGTERM、156文档链接/差异通过。61804d0已推送main并核对SHA，[CI40805760075](https://github.com/hmlax/muskdeer/actions/runs/40805760075)success，含默认全量交易/四原浏览器/持久开发/镜像；本步未重复本地默认全部交易。本步检查后独立提交推送核对SHA，继续资金发布换源闭环及剩余P07、P08–P10，完整A27/L1未完成。

## 2026-10-29：P07.24 双模板资金发布与换源浏览器

- 改动：第五浏览器命令增加Maker/Grid实际API/strategy/execution/双market进程场景。真实页面启动后四档核心LIVE及冻结/四级预留；发布更小额度到未运行OKX时，有界RR并行观察必须看到旧预留高于新额度且旧active/额度保留。撤单终态后冻结/预留归零，旧健康Binance不代替OKX；OKX就绪才激活新版本/来源/额度并形成新四档资金订单。
- 目标保留：暂停后发布保持PAUSED/旧active/旧额度，实际owner租约继续推进但无新意图，恢复后才生效新版本并报价；停止后再次发布保持STOPPED/旧active/旧额度，不隐式恢复。每模板3发布/4控制/0成交，最终USDT1000/BTC10、冻结/四级预留零。见[P07.24](validation/P07-24.md)，无应用行为/schema/生成物/嵌入资产改变。
- 验证：race浏览器一次通过，原元数据14.74秒、资金组合69.90秒（Maker39.67/Grid30.22），包86.364秒；原期限及断言不变，自有资源清理。最终运行/停止发布截图已查看，完整make verify/32生成/Hertz/fmt/lint 0 issues/规范/22工具/单元/race/构建、web-check通过。最后仅多字段事务字面量格式整理并重新编译；本地未重复默认全部交易/其他四浏览器/镜像。
- 远程：f35ba93已推送main并核对SHA，[CI40808840414](https://github.com/hmlax/muskdeer/actions/runs/40808840414)最近观察仍运行，未预报通过。本步检查后独立提交推送并核对SHA；继续撤单/风险开关/其余管理页面与P08–P10，完整A27/L1未完成。

## 2026-10-29：P07.25 当前权限下的审计历史查询

- 改动：独立AdminAudits仓储List/Detail，当前viewer、只读RR、精确actor/action/resource及有界UTF-8/页游标、明确可空前后版本/当时角色/原因/UTC时间，不暴露凭据/幂等资料；原生成审计直接读取，无新模型/schema/Proto/依赖、无写入或运行权限。合同与边界见[管理查询](ADMIN_STRATEGIES.md)、[P07.25](validation/P07-25.md)，此步尚未接RPC/HTTP/页面。
- 验证：domain/repository单元/race1.422/1.362秒；四真实PG场景覆盖实际创建/发布/控制历史及原键不重写、精确过滤/分页/详情、当前viewer/Logout/credential拒绝、权限与历史RR一致、int64修订/调用方指针隔离、不可改删及存储失败/父取消无部分数据。测试复杂度首次58被lint拒绝，拆辅助函数保留断言；完整make verify/32生成/Hertz/fmt/lint 0 issues/规范/22工具/单元/race/构建通过。
- 最终复验：首次PG专项宽前缀误选需要Nacos的原HTTP，在业务前因缺地址失败，失败记录保留；精确选定四审计及原两目录数据库场景最终4.189秒通过，原期限不变。158文档链接/差异通过，本地未重复默认全部交易/五浏览器/镜像。
- 远程：38a9fdc已推送main并核对SHA，[CI40810909656](https://github.com/hmlax/muskdeer/actions/runs/40810909656)最近仍运行；f35ba93的[CI40808840414](https://github.com/hmlax/muskdeer/actions/runs/40808840414)success，含默认全部交易/五浏览器/持久开发/镜像。本步检查后独立提交推送核对SHA；继续审计typed入口/页面、撤单/风险开关及剩余P07、P08–P10，完整A27/L1未完成。

## 2026-10-29：P07.26 typed审计gRPC及HTTP

- 改动：独立StrategyAudit ListAudits/GetAudit与具体历史字段/optional修订、生成34；管理opt-in注册、API-peer/current viewer及原期限/未知字段/大小，输出身份/筛选/升序页/游标/版本校验。API沿查询能力及原同一连接提供审计列表/详情GET，规范Cookie/URL条件/3秒/no-store/固定错误及错配成功503，所有int64字符串及presence保留，无新schema/依赖/任务/资产。见[P07.26](validation/P07-26.md)与[HTTP合同](ADMIN_HTTP.md)。
- 验证：相关单元/race、完整make verify/34生成/Hertz/fmt/lint 0 issues/规范/22工具/全包单元/race/构建通过。新夹具误用不存在的租约类型在编译时修正，按已准备lease断言无新增。真实两独立API/strategy/PG/Nacos/mTLS组合12.792秒，新审计3.56/原目录1.50/只读管理6.00秒；实际viewer密码登录/分页过滤/详情/历史角色及可空版本、非API有效证书拒绝/Logout旧Cookie401通过，5审计、零核心订单/无新增lease、全部原余额行不变。
- CI诊断：298858b已推送main并核对SHA，CI40811984985最近仍运行；38a9fdc的[CI40810909656](https://github.com/hmlax/muskdeer/actions/runs/40810909656)失败，默认全部交易和四原浏览器通过，发布组的原元数据冲突重载断言失败；新双模板资金65.49秒通过。完整日志已取回，原冲突重载等待正单独定位修复，不把CI整体报绿。
- 范围：本步检查后独立提交推送核对SHA；未重复本地全部交易/五浏览器/镜像。继续浏览器竞态修复、审计页面、撤单/风险开关及其余P07、P08–P10，完整A27/L1未完成。

## 2026-10-29：P07.27 发布冲突重载的确定性浏览器同步

- 诊断：完整远程失败日志定位原元数据场景冲突重载断言，新双模板资金65.49秒已通过。受控阻塞实际feed请求，参数已重置但来源仍loading/disabled时，原立即读取来源断言确定性失败4.85秒；首次路由清理错误另外保留，finally释放并等待handler后准确复现，未吞错。见[P07.27](validation/P07-27.md)。
- 修复/验证：保留网络gate、加载/禁用/原参数断言，释放请求并等真实来源option返回后再核对原来源，不用sleep、不延长期限。完整make admin-publication-browser通过：元数据9.17秒、资金43.17秒（Maker22.49/Grid20.68），包53.839秒，所有冲突/丢响应/原键及资金终态断言保留，自有资源清理。应用Go/Proto/迁移/前端源码/嵌入资产无改动；沿用4a29384完整verify及实际审计进程验证，本步web-check和文档/差异检查通过，不重复全部Go/交易集成/其他四浏览器/镜像。
- 远程：4a29384已推送main并核对SHA，[CI40817905370](https://github.com/hmlax/muskdeer/actions/runs/40817905370)及298858b的[CI40811984985](https://github.com/hmlax/muskdeer/actions/runs/40811984985)均success，含默认全部交易/五浏览器/持久开发/镜像；没有把间歇重跑代替确定性修复。本步测试后独立提交推送核对SHA，继续审计页面及其余P07、P08–P10，完整A27/L1未完成。

## 2026-10-29：P07.28 审计列表与详情页面

- 改动：查询能力下三角色审计导航、列表/精确筛选/ID分页/详情/深链接/URL刷新，缺失修订“—”、高位ID与修订string、历史角色/原因及UTC时间；参数错误、载入/空结果/403/依赖/协议/404/失效会话，无业务写入口或浏览器持久存储。HTML/API共用规范审计ID，关闭能力均404。第六组浏览器进入CI/e2e，无新模型/schema/Proto/依赖，见[P07.28](validation/P07-28.md)。
- 验证：最终4资产重复构建SHA一致，完整make verify/34生成/Hertz/fmt/lint 0 issues/规范/22工具/单元/race/构建通过。实际API/strategy/PG/Nacos/mTLS/Chromium首次8.36秒/包10.214秒通过，23真实创建/发布历史+1导入精度夹具+3真实登录/注销=27审计；两页/精确组合/刷新/空结果、HTML原文不执行、optional及高位修订、真实404/Logout旧Cookie失效/重新operator登录通过。数字ID/403注入仅验证错误呈现，真实重试通过；零业务写/控制/核心订单、无新增lease及原余额全行不变，四截图已查看/移动端无页面溢出。
- 共享资产回归：原登录9.31秒/CLI1.73、查询14.91、资金控制23.25通过；原创建浏览器前Nacos注册/发现deadline失败5.72秒，保留诊断且未改期限/断言、具体根因未证实。单独创建复验13.52秒及随后发布元数据11.60/资金70.48秒（包83.478）通过，六组都有最终资产通过记录，不把首轮组合称一次通过。最新非root只读无网络镜像/readiness/JSON日志/SIGTERM及161文档链接/差异检查通过。
- 远程/范围：0a0ef4d已推送main并核对SHA，[CI40820396271](https://github.com/hmlax/muskdeer/actions/runs/40820396271)最近观察仍运行，未预报。本步检查后独立提交推送核对SHA；本地未重复默认全量交易。继续撤单/风险开关、行情与订单/成交/持仓等管理能力及P08–P10，完整A27/L1未完成。

## 2026-10-29：P07.29 策略风险开关仓储与最终资金门禁

- 改动：生成 AdminStrategyRiskChange/迁移29（35生成/32模型）、独立管理风险版本、当前 viewer 快照、operator/admin 关闭及仅 admin 的确认配置/目标恢复；保留全部义务/冻结/预留、运行/来源/配置/所有权和共享开关。原子 CSRF/当前权限/审计/收据及提交时权限/恢复证明到期保护，旧历史不可改删。修复直接核心 intent ID=0 绕过关闭旗标，未管理裸核心合同保留。见[风险合同](ADMIN_RISK.md)、[P07.29](validation/P07-29.md)，本步尚未接 RPC/HTTP/页面。
- 验证：直接核心资金红测先复现成功绕过，再加最终约束后通过；原生资金撤单/管理恢复、零预留待动作、并发六重放/不同操作者竞争、审计及收据失败完整回滚、当前权限/CSRF/Logout及提交后到期、共享额度/对账、实际请求配置库存与目标变化均通过。完整 make verify、35生成/Hertz/fmt/lint 0 issues/规范正反例/22工具/全包单元/race/实际命令构建通过。
- 全量回归：首次交易413.087秒有登录截止/Grid场景截止/行情过期假设三失败，完整诊断保留；三项原样隔离37.555秒通过，登录/Grid具体负载原因未证实，未改其5/20秒边界或断言。行情明确按数据库而非宿主机等待过期，在原10秒内保留全部STALE/零价/有效租约断言。修正后再次完整verify及完整integration通过：持久化0.486/基础设施33.146/交易419.056秒；全部风险、28→29保留、32模型公共字段、旧资金/库存/租约/UNKNOWN/消息/独立进程与实际隔离schema迁移命令通过，12秒6成交smoke不算新30分钟。
- 浏览器：迁移29实际资金Grid启停22.03秒/包23.702秒；发布元数据8.09、双模板资金54.83（Maker25.40/Grid29.42）/包64.422秒通过。没有前端资产或依赖变更；163文档链接/差异检查通过，本地未重复其他四浏览器、镜像/持久开发/固定30分钟。
- 远程/范围：38e9a9c已推送main并核对SHA，其CI40822719700 cancelled，无成功结论；0a0ef4d的CI40820396271及4a29384的CI40817905370均success。本步检查后独立提交推送核对SHA，继续风险typed入口/页面、独立撤单及其余P07、P08–P10；完整A27/L1未完成。

## 2026-10-29：P07.30 typed 策略风险 gRPC 与 HTTP

- 改动：具体风险Proto、独立StrategyRiskQuery/StrategyRiskControl（37生成/32模型/schema29）；管理读取与显式控制分别注册，复用同一仓储/原连接，无新任务/资源或默认权限。HTTP GET/POST风险状态/历史收据、当前会话/独立CSRF/原键、严格2KiB字符串版本与确认字段；输出身份/版本/金额/来源/状态/请求绑定校验，错误成功503无部分业务数据。恢复不满足409 RECOVERY_DENIED，与版本409 CONFLICT区分，公开错误固定。见[风险合同](ADMIN_RISK.md)、[P07.30](validation/P07-30.md)。
- 验证：高位版本/ID/18位小数、精确证明、未知/重复/null/数字版本/尾随输入、缺少权限/CSRF、仅查询/全关闭路由及错误成功拒绝等单元/race通过；完整make verify/37生成/Hertz/fmt/lint 0 issues/规范正反例/22工具/全包单元/race/三实际命令构建通过。最终HTTP/race1.518、gRPC/race1.355秒。
- 原生：四组合15.581秒，新资金HTTP1.70/只读风险RPC0.81/原审计5.94/原只读管理5.57秒；真实operator/admin/viewer密码登录、关闭/重放、未确认取消保持200预留/200.2冻结、确认后admin凭新鲜所选行情恢复、旧历史不覆盖当前、viewer只读、Logout旧Cookie读/写401通过。只读进程不注册修改RPC，非API有效证书拒绝；实例全字段不变、核心仅原1单、最终1000USDT/零冻结/零预留、缓存许可关闭、2风险收据。164文档链接/差异通过，无schema/资产/依赖变更。
- 远程/范围：1475aa8已推送main并核对SHA，CI40828632829最近in_progress，未预报。本步检查后独立提交推送核对SHA；本地未重复完整交易/六浏览器/镜像/开发环境/固定30分钟。继续风险页面、独立撤单及其余P07、P08–P10，完整A27/L1未完成。

## 2026-10-29：P07.31 策略风险与额度页面

- 改动：策略详情显式打开风险快照、独立管理版本/额度预留/来源/配置/运行目标与UTC时间、审计链接；三角色/查询能力区分、关闭/恢复原因及确认、绑定目标、当前快照与历史收据分别展示、结果不明原UUID键/正文冻结重试。读取故障清空旧金额，401回登录，无浏览器存储；金额仅十进制文本校验/比较、版本BigInt，原因文本不执行。新增CI/e2e第七浏览器，无生产Go/Proto/schema/依赖变化，见[P07.31](validation/P07-31.md)。
- 验证：TypeScript/重复构建四资产SHA一致（JS B_ruSYPW/CSS QAzg1nNY），固定资产后完整make verify/37生成/Hertz/fmt/lint 0 issues/规范正反例/22工具/单元/race/命令构建通过，最终HTTP/race1.606秒。首轮浏览器关闭阶段已通过，后续夹具误把90秒场景截止用于单笔撤单而被一分钟合同拒绝，5.55秒失败诊断保留；改为独立两秒请求，原90秒场景及全部业务断言不变。
- 真实资金：新浏览器四阶段15.31秒/包16.986秒，实际API/strategy/market独立命令及原生Go核心/OMS。已提交丢响应后同键/正文重放仅1效果；未确认撤单仍200预留/200.2 USDT冻结，admin恢复409；确认核心终态后，真实独立暂停使旧恢复确认冲突，重新确认PAUSED目标/原配置/新鲜Binance恢复版本3。真实viewer只读、Logout旧Cookie401、原因HTML原文不执行；数字金额/403/高版本及76位边界金额注入仅作页面协议证明，复原实际读取通过。2风险收据/1明确暂停/原1核心单/0成交，最终1000USDT/10BTC、零冻结/预留及缓存许可关闭；除明确暂停字段外实例Proto全字段不变，三截图已查看、移动无溢出。
- 共享回归：同一最终资产原六组依次通过，登录7.36/CLI1.02、查询12.70、资金控制18.91、创建6.63、发布元数据11.31/双模板资金47.39（Maker22.87/Grid24.52）、审计4.50秒；七组均有最终资产通过记录，165文档链接/差异通过。没有重复本地全部交易/镜像/持久开发/固定30分钟。
- 远程/范围：abf7ac3已推送main并核对SHA，[CI40829752885](https://github.com/hmlax/muskdeer/actions/runs/40829752885)全部success，含Linux全部交易/六浏览器/持久开发/镜像；1475aa8的CI40828632829 cancelled，不报绿。本步测试后独立提交推送核对SHA，继续独立撤单、行情/订单成交持仓/风险事件及其余P07、P08–P10，完整A27/L1未完成。

## 2026-10-29：P07.32 真实订单及独立核心事实查询仓储

- 改动：AdminOrders当前viewer只读RR、明确策略范围的OMS列表/详情、ID游标1–100/状态/open筛选、订单自己的历史配置来源、OMS状态/成交/预留/最后确认及可关联核心的独立当前事实。批量生成Model查询，公开业务DTO不含提交键/追踪键/策略执行租约身份；缺核心不是无受理证明，读取不做确认/对账/撤单或预算释放。无新Proto/模型/schema/依赖/资产，37/32/29保持；尚未注册RPC/HTTP/页面，见[订单合同](ADMIN_ORDERS.md)、[P07.32](validation/P07-32.md)。
- 验证：domain/repository单元/race1.285/1.354秒；原生实际部分成交尚未OMS确认、未派发、UNKNOWN但核心已受理、历史Binance v1不被当前OKX v2覆盖、分页/精确状态/兄弟策略隔离、当前viewer/Logout/父取消及私有存储故障无部分页/游标通过。认证读取后另一事务真实成交不混入当前RR、下一次读取可见核心更新而OMS仍保留200预留。
- 撤单确认：真实取消PENDING保留200/200.2冻结；核心CANCELED释放冻结后读取仍保留OMS LIVE/PENDING与200额度，原取消恢复流程完成后才显示OMS终态/预留0和空open页，无查询审计或命令副作用。新测试复杂度33被lint拒绝，拆分阶段断言，未放宽检查；完整make verify/37生成/Hertz/fmt/lint 0 issues/规范正反例/22工具/单元/race/命令构建通过，最终四原生/race3.794秒（0.52/0.61/0.52/0.59）。167文档链接/差异通过。
- 远程/范围：5db062d已推送main并核对SHA，最近已确认完整CI为abf7ac3的40829752885 success，本步测试后独立提交推送核对SHA；没有重复本地全部交易/七浏览器/镜像/持久开发/30分钟。继续订单typed入口/页面、独立撤单及其余P07、P08–P10，完整A27/L1未完成。

## 2026-10-29：P07.33 typed 订单列表与详情 gRPC/HTTP

- 改动：StrategyOrderQuery与具体订单/可空核心观察（39生成/32模型/schema29）；管理查询开关、API-peer mTLS/原连接/当前viewer快照，明确历史来源、OMS与当前核心差异，读取不确认/对账/撤单或释放预留。严格分页/过滤/ID及成功响应字段边界、固定错误、无部分成功，见[订单合同](ADMIN_ORDERS.md)、[P07.33](validation/P07-33.md)。
- 验证：首次原生测试夹具误引用持久模型字段，typecheck拒绝；按实际MarketDataSource修正，全部断言保留。完整make verify/39生成/Hertz/fmt/lint 0 issues/规范正反例/22工具/单元/race/三命令构建通过，HTTP/race1.907、gRPC/race1.359秒。高位ID/18位小数/更高精度冻结额、未知/重复/矛盾输入、错身份/游标/金额/确认版本成功响应拒绝均通过。
- 原生：四组合12.495秒，新订单HTTP1.35/撤单差异RPC0.90/原审计3.56/原只读管理5.03秒。真实viewer登录、Binance v1不被当前OKX v2覆盖、部分成交尚未OMS确认、未派发core null、UNKNOWN但核心已受理、分页/过滤/兄弟策略、Logout恢复旧Cookie401及错证书拒绝通过；预算220/核心可用789.79与冻结110.11保持，实例全字段不变。核心撤单确认后仍返回OMS LIVE/PENDING/200预留，原恢复后才终态/零预留。168文档链接/差异通过。
- 远程/范围：767729e已推送main并核对SHA，其CI40835162304最近仍运行，不预报成功；5db062d的40833189132 cancelled，最近完整成功仍为abf7ac3的40829752885。无资产/依赖/schema变更，未重复本地全部交易/七浏览器/镜像/开发/30分钟；本步检查后独立提交推送核对SHA。继续订单页面、独立撤单及其余P07、P08–P10，完整A27/L1未完成。

## 2026-10-29：P07.34 订单列表、详情及独立核心事实页面

- 改动：策略详情显式打开订单、OMS状态/open义务筛选、1/20/50页长与ID分页、当前详情；历史配置来源、OMS成交/预留/确认版本及核心独立成交/冻结/费率/UTC时间分别呈现，缺核心不能证明未受理。精确string金额、BigInt身份、失败清旧数据/有界取消、401回登录、无存储或业务写。第八组订单浏览器进入CI/e2e，无依赖、生产Go、Proto/model/schema变化，39/32/29保持，见[P07.34](validation/P07-34.md)。
- 验证：TypeScript与重复构建通过；首次浏览器9.51秒在登录前因macOS沙箱MachPort Permission denied失败，空白探针复现，日志保留。按工具权限流程运行原测试后8.02秒/包9.792秒通过，未增期限或改业务断言；原策略查询16.77秒/包18.263、风险资金四阶段15.60秒/包16.977通过。截图发现复选框继承输入尺寸与标题按钮拉伸，仅改订单区域CSS；同一JS内容SHA，原查询/风险使用修正前CSS，未冒充最终CSS复验。
- 最终：四资产重复构建SHA一致（JS Bf3AlEGe/CSS b_u0aa1W）；最终资产后再次完整make verify/39生成/Hertz/fmt/lint 0 issues/规范正反例/22工具/全部单元/race/三命令构建通过，HTTP/race1.620秒。最终订单浏览器7.89秒/包9.527秒通过；真实viewer、三类订单、分页/过滤/空页、当前OKX请求与历史Binance订单、核心部分成交而OMS仍未确认、UNKNOWN实际已受理、Logout旧Cookie401，数字金额注入拒绝后真实重试通过。四范围预算220、USDT可用789.79/冻结110.11、OMS3/核心2/成交1/取消0、实例全字段不变；两张最终截图已查看、移动无整页溢出，资产SHA未变。
- 远程/范围：aefda21已推送main并核对SHA，其[CI40836781207](https://github.com/hmlax/muskdeer/actions/runs/40836781207)全部success（16分25秒），767729e的[CI40835162304](https://github.com/hmlax/muskdeer/actions/runs/40835162304)也全部success，含Linux全量交易/七浏览器/开发/镜像。本步检查后独立提交推送核对SHA；169文档链接/差异通过，未重复本地全量交易、其他五浏览器、最终CSS后的原查询/风险、镜像/开发或固定30分钟。继续独立撤单及其余P07、P08–P10，完整A27/L1未完成。

## 2026-10-30：P07.35 指定订单的持久管理撤单及控制器处理

- 改动：生成 AdminOrderCancellation/迁移30（40生成/33模型），当前 operator/admin、独立CSRF、订单revision确认、作用域/原因/幂等和不可变审计；viewer只读RR结果。实际strategy当前owner原子关联已有取消动作或创建固定键动作，沿原发送/查询/最终fencing恢复，不修改策略目标或风险开关；OMS终态和该单零预留才成功，明确失败保留义务。见[撤单合同](ADMIN_CANCELLATIONS.md)、[P07.35](validation/P07-35.md)。尚未接管理RPC/HTTP/页面。
- 红测与修复：控制器原样忽略管理端口、首次生效配置缺失时错误使用取消权限两项均先复现再修复。首次原生夹具延长原动作期限被数据库拒绝，改回原期限；未带隔离PG地址的调用在业务前失败，后续均使用独有资源。测试复杂度31被lint拒绝，只拆断言，未放宽门禁/期限。
- 验证：独立PG/race九场景9.469秒通过，含四并发原键、既有动作复用、兄弟策略、版本/当前角色/CSRF/Logout、审计/收据失败整体回滚、插入后会话到期拒绝及历史不可改删。实际200名义额订单核心已取消但OMS未恢复时总220预留和ACCEPTED保持，原恢复后才释放该单200；兄弟20保持。丢取消响应后真实控制器换owner仅1次取消发送/1动作/1取消请求，期间1 BTC成交保留，最终899.9 USDT可用、零冻结/该单预留，无新提交。
- 全量：最终完整make verify/40生成/Hertz/fmt/lint 0 issues/规范正反例/22工具/全包单元/race/三命令构建通过；完整make integration一次通过，持久化0.377/基础设施31.284/交易365.296秒，包含新九场景及原全部交易/独立业务进程/迁移与33模型公共字段。171 Markdown文件/377本地引用/差异检查通过，专属容器和卷已清理。
- 远程/范围：基线3d5b32f已核对main，其[CI40846455444](https://github.com/hmlax/muskdeer/actions/runs/40846455444)为cancelled，不报绿。本步检查后独立提交推送并核对远程SHA；没有前端资产/依赖变更，未重复浏览器、镜像、持久开发或固定30分钟。下一步撤单typed入口和页面，再继续其余P07及P08–P10；完整A27/L1与真实交易接入仍未完成。

## 2026-10-30：P07.36 指定订单撤单 typed gRPC/HTTP 与实际跨进程链路

- 改动：具体请求/收据/可空终态观察Proto、独立查询/控制RPC及相同Deer连接装配（42生成/33模型/schema30），POST单订单取消及202/200/Location、当前viewer收据GET。原管理读/写开关、API-peer mTLS、当前会话/独立CSRF/幂等/原期限、严格双字符串JSON/路径及无部分响应均保留；无新模型/迁移/依赖/资产或账户授权。见[撤单合同](ADMIN_CANCELLATIONS.md)、[P07.36](validation/P07-36.md)，页面继续后续。
- 复现与修复：HTTP红测先见404；收据校验复杂度32被lint拒绝，只拆终态校验保留断言。首轮原生资金链路8.73秒失败，其余三组通过；定位P07.35冒号决策键被用作执行RPC追踪ID，账户worker发送前因invalid_order退出。最小回归先复现，再仅派生strategy.action.<action_id>追踪，持久决策/幂等键、期限、不明结果恢复及RPC校验不变，未延长原等待。
- 验证：HTTP/gRPC/wire/business的单元/race及protobuf往返、字符串高位ID/订单确认、接受/终态presence、未知/重复/null/数字/缺身份输入、错配成功503、角色/错误分类通过。修复后四组真实进程/race一次10.828秒通过：新资金5.57、只读注册0.72、原订单1.40、原风险1.58秒。实际API/管理strategy/账户strategy/execution四命令由持久请求触发原取消动作；无market进程且预算一直关闭，仍可受控取消，期间模拟核心1 BTC成交保留，最终899.9USDT可用、零冻结/预留，无新单。
- 证明：真实operator/viewer登录、原键重放、旧版本/异正文409、兄弟范围404、有效非API证书读写拒绝、只读节点不注册写入、旧Cookie注销后读写401均通过。执行者启动前实例Proto全字段不变；最终订单/核心/动作/取消/收据各1，成交1，请求目标/来源/配置和关闭旗标保留。
- 全量：最终完整make verify/42生成/Hertz/fmt/lint 0 issues/规范正反例/22工具/全部单元/race/三命令构建，以及完整make integration一次通过；持久化0.726/基础设施27.916/交易369.281秒，包含新场景及原全部交易/独立进程/九项撤单与12秒smoke，自有容器/卷已清理。172 Markdown文件/383本地引用及差异检查通过，本轮未重复浏览器、镜像、持久开发或固定30分钟。
- 远程/范围：基线31acdd0已核对main，其CI41013356522为cancelled，不报绿。本步检查后独立提交推送并核对远程SHA；下一步P07.37撤单确认、原键重试和收据管理页面，继续其余P07及P08–P10，完整A27/L1与真实交易接入未完成。

## 2026-10-30：P07.37 指定订单撤单确认与持久收据页面

- 改动：订单详情确认原因/revision、明确单订单范围及受理非终态；不明结果冻结原键/正文和会清除请求的订单操作；viewer独立收据深链接/登录恢复/刷新、20秒有界查询、OMS状态/证明/审计链接，失败清旧结果/401回登录，无浏览器存储。HTML双ID规范/查询能力测试先红404后绿；第九组实际资金浏览器进入CI/e2e，无新依赖/Proto/model/schema，见[P07.37](validation/P07-37.md)。
- 验证：最终四资产重复构建SHA一致，完整make verify/42生成/Hertz/fmt/lint 0 issues/规范/工具/全部单元/race/三个命令构建通过。真实API/管理strategy/账户strategy/execution/mTLS/PG/Nacos/Chromium首次11.72秒，强化手机表单及保留请求断言后最终8.36秒通过：真实202后丢回包仍原键重试，待确认保留预留200/冻结200.2，期间成交1BTC/费用0.1，最终OMS CANCELED/SUCCEEDED、零预留/冻结、USDT899.9，原目标/来源/风险保持。只读深链接/刷新/数字版本拒绝/实际Logout旧Cookie401、HTML原文/无脚本或本地存储通过；最终桌面/390px确认表单和收据截图已查看。
- 共享回归：原八组均以最终资产通过，登录7.24秒/CLI0.82、查询15.03、资金启停22.80、创建7.59、发布元数据4.77/双模板资金42.21、审计8.96、风险15.07、订单9.18秒；最终make image-test非root/只读/禁网/readiness/JSON身份/SIGTERM通过，资源清理完成。173文档/387本地链接及差异检查通过；本地未重复默认全量交易/持久开发/固定30分钟。
- 远程/范围：基线11d975d已推送main并核对SHA，其[CI41019749207](https://github.com/hmlax/muskdeer/actions/runs/41019749207)全部success。本步相关检查后独立提交并立即推送核对SHA；继续行情监控及其余P07、P08–P10，完整A27/L1未完成，P11真实合同与测试环境仍待外部资料。

## 2026-10-30：P07.38 授权行情监控仓储

- 改动：AdminMarkets列表/详情、来源/交易对/明确LIVE或REPLAY隔离、有界feed游标；当前viewer只读RR、批量关联行与DB时间重检，缺快照保持nil，发布诊断与当前可用性区分，旧/失效发布者或过期行情清零当前价格与数量。无新权限/租约/连接/写入/依赖/Proto/model/schema；尚未接RPC/HTTP/页面，见[管理行情](ADMIN_MARKETS.md)及[P07.38](validation/P07-38.md)。
- 验证：新四场景及原目录/行情四回归真实PG/race共6.665秒通过；超过Number精度的序列/参考价和18位小数、分页/来源/模式/缺失、DB过期/接管/新发布恢复、并发发布与凭据修改一致快照、下一查询拒绝、真实Logout旧token、父取消/无部分数据、实际UPDATE以25006拒绝且交易对不变，原快照全字段及租约/订单/资金数量不变。最终完整make verify/Go1.27.3/42生成/Hertz/fmt/lint含govet 0 issues/规范/工具/全包单元/race/构建通过，逐文件审查无待修问题。
- 诊断：补充夹具误用不存在字段由native编译及verify typecheck拒绝，按生成status/常量修复；只读注入最初因GORM Session顺序复用了SELECT，被严格SQLSTATE断言拒绝，context后重置statement才实际执行UPDATE并获25006，未降断言或延时。过宽回归前缀误选无Nacos的原HTTP，最终精确全名选择八数据库场景通过，失败记录保留。175文档/392本地链接及差异检查通过；本地未重复全部交易/九浏览器/镜像/持久开发/固定30分钟，本步无进程装配或资产变化。
- 远程/范围：基线9be33284caa2555f37aeaa4cd918621fad3bb866已推送main并核对SHA；本步检查后独立提交并立即推送核对SHA，继续行情typed入口/页面、其余P07及P08–P10，完整A27/L1未完成，P11仍缺真实合同与环境。

## 2026-10-30：P07.39 行情监控typed gRPC与HTTP

- 改动：具体StrategyMarketQuery ListMarkets/GetMarket及feed/产品/当前读数/发布诊断/可空snapshot；management opt-in/API mTLS/current viewer/5秒边界，API沿原单连接/查询开关/唯一Cookie/3秒/no-store提供GET /api/v1/markets及/:feedID，必须明确LIVE/REPLAY、来源/产品/游标严格校验。完整嵌套、精确价格/政策、当前发布者/原发布有效期/数据时效/时钟/价差/中价及返回过滤绑定，错误成功503无部分结果，无新后台工作/权限/模型/schema/依赖/前端资产，44生成/33模型/schema30，见[管理行情](ADMIN_MARKETS.md)与[P07.39](validation/P07-39.md)。
- 验证：wire/strategygrpc/adminhttp/business单元/race1.453/1.454/1.662/1.475秒；大指数值先拒绝再String，18位小数/高位ID及序列、99.9999999999999999995中价、缺快照及假健康/未知字段/错配/错误游标/固定错误通过。最终实际PG/Nacos/mTLS/API/strategy五场景14.255秒通过，新行情2.20/原资金撤单3.80/仅查询3.12/订单1.56/风险1.98；真实viewer查询开启且控制关闭、两源/模式/产品/分页、DB过期及发布者释放零价、非API证书拒绝、Logout旧Cookie401、原发布全字段不变及零订单/风险/资金行通过。
- 诊断/检查：初lint复杂度31/32超30，拆小装配/健康证明后0 issues，无放宽；初实际新场景误复用要求控制开启的登录助手，夹具阶段失败，专用助手保留viewer/查询开/控制关断言后通过，原四回归两轮均过。最终完整make verify/Go1.27.3/44生成/Hertz/fmt/lint含govet/规范/工具/全部单元/race/三命令构建通过，逐文件审查无待修问题。176文档/398本地链接及差异检查通过，本地未重复全量交易/九浏览器/镜像/持久开发/固定30分钟。
- 远程/范围：de5a9a8已推送main并核对SHA，其CI41022985913最近仍in_progress，未预报；本步检查后独立提交并立即推送核对SHA，继续行情监控页面及其余P07、P08–P10，完整A27/L1未完成，P11真实合同/测试环境仍待资料。

## 2026-10-30：P07.40a CI拆分及全套结果门禁

- 诊断：980d736的CI41024620141在20分06秒被取消，GitHub annotation明确20m0s全局job上限；生成/单元/构建/全量integration及前五浏览器已success，审计中断，后四类及持久开发/镜像未运行。失败证据已保留，不将cancelled说成success。
- 改动：原串行检查拆为checks、integration、browsers三套独立job，每套仍20分钟及相同固定Go1.27.3/生成器/Actions SHA，浏览器独立安装锁定依赖；所有原命令/原九组、只读权限及业务期限/断言保持。保留verify名称作为always最终汇总，任何一套failure/cancelled/skipped均拒绝，详见[P07.40a](validation/P07-40-ci.md)。
- 验证：YAML解析、旧命令多重集合保留、工具链/Actions/权限/依赖顺序/无跳过与完整needs绑定通过；真实bash门禁10种成功/失败/取消/跳过组合全部符合退出码，文档/差异检查通过。仅CI编排/文档，未为此重复Go业务测试；新拓扑线上结果尚待实际CI完成，不预报。
- 交付：本独立修复先提交并立即推送main核对SHA，保留工作区尚未提交的P07.40行情页面/测试/生成资产，核对远程后继续页面交付；完整A27/L1及P08–P10未完成。

## 2026-10-30：P07.40 按源隔离行情监控页面

- 改动：三角色查询导航、明确LIVE/REPLAY/来源/交易对/页长及列表分页/详情/登录深链接；精确采样报价/序列、当前时效、历史发布诊断和政策分开。250ms单调时钟与页面恢复仅更新采样期限，不自动GET、不换源；到期显式提示，保留标明采样的价格。六秒取消/失败清旧数据/401登录，无写入口、浏览器存储或新依赖/Proto/model/schema，44生成/33模型/schema30，见[行情页面](ADMIN_MARKETS.md)及[P07.40](validation/P07-40.md)。
- 验证：路由/规范ID/能力测试先红404，曾因与web-build并发产生临时embed503，稳定构建后原测试0.432秒通过，未改断言。最终TypeScript、完整make verify/Go1.27.3/44生成/Hertz/fmt/lint 0 issues/规范/工具/全包单元/race/三命令构建通过；四资产重复构建SHA一致。实际API/strategy及两个独立market回放命令两阶段9.79秒通过：三绑定/两产品/精确过滤/页/深链接/刷新、LIVE空/未发布不造价、99.9999999999999999995中价/18位买一、到期无新GET、数字序列/403呈现清旧数据、停止Binance后原源无价格且OKX继续200、Logout旧Cookie401。只保留原3绑定/2租约/2快照，无订单/策略/交易账户/资金/风险/操作行；四桌面/390px截图已查看，导航与长数字无整页溢出。
- 共享回归：最终资产下原九组均有通过记录，未称首轮一次过：登录10.75秒/CLI0.79、查询13.38、资金启停35.05、创建10.63；发布元数据11.78/双模板43.20（Maker22.16/Grid21.04）、审计10.40、风险15.60、订单16.06、撤单17.41秒及最终镜像非root/只读/禁网/readiness/JSON/SIGTERM通过。首轮资金Maker在发布前失败，三业务worker同一时段deadline_exceeded退出；独立原样复验及资源/DB等待采样未重现具体触发原因，不冒称已修复，保留记录供P09/P10故障验证跟踪。178文档/405本地链接及差异检查通过，本地未重复默认全量交易/持久开发/固定30分钟。
- 远程/范围：先交付CI拆分9ba0b3972bb574003ef4c4c7e1bfde2a5c90b784并核对远程；第十组加入browsers，原命令/固定工具链/只读权限及汇总门禁保持，10种实际shell门禁组合再次通过。新拓扑线上结果仍待实际CI，不预报；本步独立提交并立即推送核对SHA后，继续成交/持仓等P07及P08–P10，完整A27/L1未完成，P11仍待真实合同与环境。

## 2026-10-30：P07.41 权威成交与持仓事实收录查询仓储

- 改动：AdminTrades当前viewer只读RR、明确策略及可选原OMS/BUY或SELL筛选、核心成交ID升序1–100分页/详情。批量原始提交命令、明确意图、历史配置、交易对与收录事实；费用按实际模拟政策校验，数据库记录时间不冒充交易所时间，收录存在不等于投影完整或已对账。无写入/新权限/工作器/Proto/model/schema/依赖/资产，44生成/33模型/schema30保持；尚未接RPC/HTTP/页面，见[成交合同](ADMIN_TRADES.md)、[P07.41](validation/P07-41.md)。
- 验证：缺仓储入口先红测，首次三个原生/race4.409秒；最终七场景真实PG/race6.412秒全过。Binance v1不被当前OKX v2覆盖、分页/原OMS/方向/兄弟范围、裸核心首次命令无intent_id不按同客户端ID补认、后序卖出已收录而持仓仍SOURCE_INCOMPLETE、并发收录不混入当前RR、Logout/取消/私有故障无部分结果、实际UPDATE以25006拒绝且交易对不变通过。
- 资金：99买/101卖各1BTC，费用0.099/0.101、USDT1001.8且OMS200预留保持；实际费率0.001234567890123456，两笔99.99各1BTC费用各0.12344444333344436544，USDT799.77311111333311126912/零冻结，OMS199.98预留保持，可空身份指针不共享。原始未关联成交不认领同客户端OMS100预留；读取不投递/重建/确认或开放风险。
- 检查：最终完整make verify/Go1.27.3/44生成/Hertz/fmt/lint含govet 0 issues/政策/22工具/全包单元/race/三个命令构建通过，repository race1.356秒；极端指数在算术前拒绝，180文档/412本地链接/差异及逐文件复审通过，独有PG容器/卷已清理。本地未重复默认全部交易/十浏览器/镜像/持久开发/固定30分钟。
- 远程/后续：fc2623b已推送main并核对SHA，其[CI41029358740](https://github.com/hmlax/muskdeer/actions/runs/41029358740)四job全部success（checks7分38秒/integration12分32秒/browsers9分46秒/汇总3秒），新拓扑及十浏览器获得线上证据；P07.40本地三worker同时deadline_exceeded的具体触发仍待P09/P10定位，不冒称已修复。本步检查后独立提交并立即推送核对SHA，继续成交typed入口及页面/持仓及P07–P10，完整A27/L1未完成，P11缺真实合同与环境。

## 2026-10-30：P07.42 成交typed gRPC/HTTP及实际跨进程读取

- 改动：具体StrategyTradeQuery ListStrategyTrades/GetStrategyTrade与独立收录presence，46生成/33模型/schema30；管理查询opt-in、API-peer mTLS/当前viewer/5秒/2KiB、API原唯一连接/3秒/no-store及严格只读路径/筛选。原始未关联order_id在ProtoJSON省略，未收录message为null，不制造0或对账许可；全字段/精确名义额/费用/过滤/游标/未知字段及详情身份绑定，错误成功整页503。无新权限/任务/持久化/依赖/资产，见[成交合同](ADMIN_TRADES.md)、[P07.42](validation/P07-42.md)。
- 诊断：两入口先编译红测；初HTTP错误要求optional标量null，按标准ProtoJSON严格验证缺席和message null；lint复杂度31拒绝后拆装配。首轮夹具先投递后序成交再发布配置被既有依赖检查正确拒绝，改为先发布再投递；第二轮临时runner在构建结束前调用旧.cache/bin/muskdeer，unknown service证据保留，最终构建后原四场景全部通过，无扩大期限或放宽业务断言。
- 验证：最终完整make verify/Go1.27.3/46生成/Hertz/fmt/lint含govet 0 issues/政策/22工具/全部单元/race/三个命令构建通过；HTTP/gRPC race1.661/1.327秒，高位ID/18位价格/超过18位手续费/Proto往返/极端指数/未知及错配成功拒绝通过，181文档/418本地链接/差异及逐文件审查通过。
- 原生：最终实际API/strategy/PG/Nacos/mTLS四场景/race9.541秒，新成交HTTP1.39/原订单1.31/行情2.09/仅查询注册3.32秒。当前viewer真实登录、非API有效证书拒绝、控制RPC未注册、历史Binance v1保持、两权威成交及可空收录/后序卖出未完整、分页/原OMS/方向/兄弟范围、Logout旧Cookie401通过。策略/持仓Proto全字段不变，200预留/USDT1001.8/零冻结及2核心单/2成交/1收录/1inbox保持，自有资源清理完成。
- 远程/范围：a221edbfe15810a67152bddeee212941e21681e4已推送main并核对SHA，其CI41032981260最近in_progress，未预报。本步独立提交并立即推送核对SHA后，继续成交页面/持仓等P07及P08–P10；本地未重复默认全量交易/十浏览器/镜像/持久开发/固定30分钟，完整A27/L1未完成，P11仍缺真实合同与环境。

## 2026-10-30：P07.43 成交列表、独立详情与记录时钟修复

- 改动：策略详情进入成交列表、原OMS/BUY或SELL/页长筛选及游标、独立详情/登录恢复/刷新；精确费用/历史来源/原始可空关联及持仓事实收录分别展示，不把收录当作已对账。string/BigInt校验，金额不经Number、六秒取消/失败清旧数据/401登录，无写入/存储或新依赖/模型/schema。第十一组浏览器进CI/e2e，46生成/33模型/schema30保持，见[成交合同](ADMIN_TRADES.md)、[P07.43](validation/P07-43.md)。
- 复现与修复：HTML先红404；初浏览器9.94秒在分页响应前读旧DOM，等真实第二行移除/新首行再断言，八秒上限保持。复审红测揭示P07.42将跨节点记录时间当作因果顺序，取消该比较，仍验证正时间、精确不可变事实/原始身份/金额/未知字段。实际慢一分钟的独立GORM记录时钟正常持久化收录，未修改不可变字段，仓储先断言时间确实较早；传输和页面随后正常展示，未制造对账或风险许可。
- 最终验证：完整make verify/Go1.27.3/46确定性生成/Hertz/fmt/lint含govet 0 issues/政策/22工具/全部单元/race/三个命令构建通过，HTTP/wire race1.688/1.414秒；TypeScript与四资产重复生成SHA相同（JS BKlfglCo/CSS DDx738OI）。同一最终资产四浏览器一次通过：新成交12.18秒/包13.782，原策略17.48/订单13.12/行情11.43秒；镜像非root/只读/禁网/readiness/JSON身份/SIGTERM通过。此前首版三回归/镜像有通过记录，未冒充时钟修复后的资产证据。
- 资金与页面：实际2核心单/2成交/1持仓事实/1inbox、USDT1001.8/零冻结/四范围200预留、原策略和不完整持仓Proto全字段保持；真实viewer、来源/筛选/分页/兄弟空页/深链接/刷新/慢时钟/Logout旧Cookie401均过，无业务写/JS错误/浏览器存储。高位ID/18位价格/更高精度费用/HTML原文/403为明确协议注入，非真实资金或权限故障声明；三张最终桌面/390px截图已查看、无整页溢出，自有资源清理完成。
- 交付/范围：861670f6d69003f17d57331e25226198b455335c已推送main并核对SHA，其[CI41033963743](https://github.com/hmlax/muskdeer/actions/runs/41033963743)及a221edb的CI41032981260均全部success，含当时全交易/十浏览器/开发/镜像。本步文档链接/差异/逐文件复审通过后独立提交并立即推送核对SHA；本地未重复最终资产其余七浏览器、默认全量交易、持久开发或固定30分钟。继续持仓及其余P07、P08–P10，完整A27/L1未完成，P11仍缺真实合同与环境；P07.40三worker同时超时的具体触发仍保留后续故障验证。

## 2026-10-30：P07.44 账户持仓及独立核心余额查询仓储

- 改动：AdminPortfolios当前viewer只读RR、初始化投影ID升序1–100分页/详情及精确账户/交易对过滤；存储投影/期初/成本/已实现损益/费用/生产者诊断，与核心成交/收录统计及可空核心余额分开。共享账户只一份、覆盖所有策略，统计不构成对账证明，缺资金不造零；不调用写入式Get/Rebuild/Apply，不加资产行锁，不改风险或预留。46生成/33模型/schema30保持，无新传输/页面/依赖/权限/资源或资产，见[账户持仓](ADMIN_PORTFOLIOS.md)、[P07.44](validation/P07-44.md)。
- 红测/检查：缺仓储入口先编译红测；初闭包作用域错误由编译拒绝后修正局部结果。首次六原生/race4.664秒通过，lint拒绝主测试复杂度38，只拆阶段助手/保留断言。最终完整make verify/Go1.27.3/46确定性生成/Hertz/fmt/lint含govet 0 issues/政策/22工具/全包单元/race/三个实际命令构建通过，repository race1.358秒；负损益/极端指数/负余额与输入边界单元通过，文档链接/差异/逐文件复审通过。
- 原生/资金：最终六新场景+七成交仓储+两投影投递/回滚回归共15项真实PG/race11.622秒通过。核心99买/101卖各1BTC后Source2/Collected0、投影1000/旧Reconciled=true与核心1001.8/零冻结分别保留；卖出先收录仍SOURCE_INCOMPLETE，明确买入投递才生产者完成/费用0.2。读取不补账，200预留保持。两策略共享一账户不重复；另禁用/未配核心资金账户nil余额、36位负损益无损，原账户2成交不串入。
- 快照/权限：并发真实部分成交不混入当前RR，下一读取Source1/冻结100.1但旧投影及LastSource0保持；私有余额故障无部分数据/游标，父取消、Logout旧token拒绝，实际UPDATE以25006拒绝/投影Proto全字段不变。源统计固定JOIN及收录分组限制精确账户/交易对，返回有界分组而非全部历史；自有PG容器/卷已清理。
- 交付/范围：1eaaa6e2f41bdc23d726b19bf393a604eb3390d2已推送main并核对SHA，其CI41036890086最近in_progress，未预报；P07.43提交后web-generated-check通过。本步独立提交并立即推送核对SHA后继续持仓typed接口/页面及其余P07、P08–P10；本地未重复默认全部交易/十一浏览器/镜像/持久开发/固定30分钟，完整A27/L1未完成，P11真实合同/环境仍待外部资料。

## 2026-10-30：P07.45 账户持仓typed gRPC/HTTP及真实管理读取

- 改动：StrategyPortfolioQuery及具体账户/存储投影/源与收录进度/可空核心余额（48生成/33模型/schema30），管理节点management开关/API-peer mTLS/当前viewer/5秒/2KiB；API原唯一连接/查询开关/3秒/no-store提供GET /api/v1/portfolios及/:portfolioID，精确账户/交易对/有界游标。所有小数与身份/版本/计数/时间JSON string；stored_reconciled明确为原生产者诊断，不对齐当前资金/统计或授权风险。无新权限/任务/消费者/迁移/依赖/资产，页面继续后续，见[持仓合同](ADMIN_PORTFOLIOS.md)、[P07.45](validation/P07-45.md)。
- 验证：两个入口先编译红测后实现；48确定性生成/Hertz/fmt/lint含govet 0 issues/政策/22工具/全包单元/race/三个实际命令构建的完整make verify通过，独立HTTP/gRPC/wire/business race1.523/1.450/1.439/1.444秒。高位ID/版本/源、36位负损益、旧诊断与新核心差异、缺余额、Proto往返及权限/截止/2KiB/未知字段/严格请求/错配成功整页拒绝通过；文档链接/差异/逐文件复审通过。
- 原生：最终构建后真实API/strategy/PG/Nacos/mTLS五场景/race11.410秒一次通过，新持仓1.31秒、原成交1.28/订单1.56/行情2.14/仅查询注册3.48。未投递时原Reconciled=true/现金1000与Source2/Collected0/核心1001.8分别保留；明确后序卖出投递才读取SOURCE_INCOMPLETE/收录1。另禁用账户无核心余额、36位负损益/分页/过滤/未知404、非API有效证书拒绝/无写RPC及Logout旧Cookie401通过。两个投影和策略Proto全字段保持，USDT1001.8/零冻结/四范围200预留及原2核心单/2余额/2投影/1收录/1inbox保持，自有资源清理完成。
- 交付/范围：42338915e4dc3e360f041fdb27d83e54c7930abc已推送main并核对SHA，其[CI41038372961](https://github.com/hmlax/muskdeer/actions/runs/41038372961)及P07.43的CI41036890086均全部success，含全量交易/十一浏览器/开发/镜像。本步独立提交并立即推送核对SHA后继续持仓页面、其余P07及P08–P10；本地未重复默认全量交易/十一浏览器/镜像/持久开发/固定30分钟，完整A27/L1未完成，P11仍缺真实合同与测试环境。

## 2026-10-31：P07.46 账户持仓列表、筛选与独立详情页面

- 改动：三角色查询导航、策略共享账户链接、精确账户/交易对及1/20/50页长/游标、筛选URL刷新恢复、独立详情/登录恢复/刷新。明确资产单位的期初/存储数量/现金/成本/已实现损益/费用、原生产者诊断、当前源/收录和独立核心可用/冻结/修订分别展示；不把缺失余额当零、不将诊断/计数当风险许可。金额/负损益无Number转换，string/BigInt身份、六秒取消/错误清旧数据/401登录，无写或存储。48生成/33模型/schema30不变，新第十二浏览器进CI/e2e，见[持仓页面](ADMIN_PORTFOLIOS.md)、[P07.46](validation/P07-46.md)。
- 验证：HTML先红404再规范ID/能力注册；TypeScript、四资产重复生成SHA一致（JS DUsDACo4/CSS Du16pnt4），完整make verify/Go1.27.3/48生成/Hertz/fmt/lint含govet 0 issues/政策/22工具/全包单元/race/三命令构建通过，HTTP/race1.482秒。实际API/strategy/PG/Nacos/mTLS/Chromium首次9.47秒/包10.991秒通过，未知产品/本地01无GET、已知未初始化空页/筛选刷新、分页/两策略共享单账户、真实36位负损益/缺余额/核心差异、Logout旧Cookie401通过。
- 资金/布局：原策略和两投影Proto全字段不变，USDT1001.8/零冻结/四范围200预留、原2核心单/2余额/2投影/2成交/1收录/1inbox保持。高位版本/源、76位余额/负损益、旧通过诊断与未完整收录、HTML原文/数字金额/403为明确协议注入；无业务写/JS错误/浏览器存储。三最终桌面/390px截图已查看、无整页溢出，自有资源清理完成。
- 回归/交付：同一最终资产登录9.29秒/CLI0.80、策略13.74、订单4.30、成交10.86、行情10.20及镜像非root/只读/禁网/readiness/JSON身份/SIGTERM均通过；原十一组CI命令/固定工具/只读权限/期限及三套job/always汇总保留，十种真实shell门禁、文档链接/差异/逐文件复审通过。5cd114accc21ea70d439d47063e84ed01fd25a94已推送main并核对SHA，其[CI41039920640](https://github.com/hmlax/muskdeer/actions/runs/41039920640)全部success，十一浏览器job8分03秒/integration10分17秒；未预报本步新CI。
- 范围/后续：本步独立提交并立即推送核对SHA后继续风险事件、运行概览及其余P07、P08–P10；本地未重复最终资产其余六组旧浏览器、默认全量交易、持久开发或固定30分钟，完整A27/L1未完成，P11仍缺真实合同与环境。风险快照目前原地覆盖，后续风险事件先补真实持久变更来源，不以当前快照伪造历史。


## 2026-10-31：P07.47 风险快照真实持久诊断历史

- 改动：Proto生成RiskSnapshotEvent、schema31及源表同事务AFTER捕获；初次记录BASELINE、实质变化CHANGED、移除REMOVED，普通序列/修订/观察/期限刷新不写重复历史。源逻辑退役/物理移除保留历史，历史不可改/软删/删；记录原始许可/对账组合，不推导当前风险许可。49生成/34模型，无RPC/HTTP/页面/任务/依赖/资产，见[风险历史](RISK_EVENTS.md)、[P07.47](validation/P07-47.md)。
- 行为：先红测42P01再实际PG/race；三新场景3.506秒、旧库升级/公共字段/原风险生产者/核心成交共九项7.936秒通过。真实1BTC部分成交产生同事务历史、重复成交不重复；USDT799.8/冻结100.1、OMS200预留保持。加强回滚断言在失败事务内已看到2条历史，回滚后仅原1条、0成交/原资金保持，同一trade_id可重试成功，原生/race2.186秒。旧schema30活动/退役源及高位序列/修订升级仅建BASELINE，源Proto不变、迁移重放不重复，无新订单或资金。
- 诊断：两次完整verify通过；首轮integration仅账本/CLI旧目标30失败，严格改31。第二轮未来迁移夹具仍用31造成测试主动INSERT唯一键冲突，已改32；不是并发迁移锁错误。策略测试累计submit=5也失败，原样定向连续10次通过（健康4提交、失效保持4且4撤单），具体触发尚未复现，不声称修复；保留逐阶段诊断/全部原断言及期限，继续P09/P10故障验证。定向并发迁移/策略PG-race4.419秒通过，全部失败证据保留于验证记录。
- 最终验证：完整make verify/integration通过（`.cache/p0747-complete.log`），Go1.27.3/49确定性生成/Hertz/fmt/lint含govet 0 issues/规范/22工具/全包单元/race/三命令构建通过；默认全量持久化0.392秒、基础设施27.421秒、交易393.690秒全部通过。策略诊断再次显示健康4提交、失效不新增且4撤单，但不以此次成功代替前述未复现故障定位；十次定向包26.181秒。188文档/456链接/差异及逐文件复审通过，自有容器/卷已清理。本地未重复十二浏览器/镜像/持久开发/固定30分钟，前端资产不变。
- 远程/范围：00ce29d9ec7810cbf6ba327ddc728d87388f698a已推送main并核对SHA，其[CI41161705859](https://github.com/hmlax/muskdeer/actions/runs/41161705859)四job全部success，含十二浏览器/全交易/开发/镜像；P07.46提交后web-generated-check通过。本步仅本地隔离迁移，无生产迁移/真实资金/部署；相关检查通过后独立提交并立即推送核对SHA，继续只读历史查询、typed接口/页面及运行概览/P07–P10。完整A27/L1未完成，P11仍缺真实合同与测试环境，P07.40三worker同时超时的具体触发继续保留。


## 2026-10-31：P07.48 当前viewer只读风险诊断历史仓储

- 改动：AdminRiskEvents复用当前viewer只读RR，按事件ID升序1–100分页/详情及精确策略/账户/交易对/来源/类型/原因组合筛选；直接保留源快照/配置/来源/可空绑定/发布者代次/高位序列/政策及原始许可/对账/原因/观察和期限。源移除、退役策略或缺核心账户不阻止历史读取；未知历史范围空页。查询不重评估/刷新/补账、不开风险、不初始化资源；非法记录/私有故障整页拒绝，无新传输/页面/任务/依赖/资产，49生成/34模型/schema31，见[风险历史](RISK_EVENTS.md)、[P07.48](validation/P07-48.md)。
- 红测/诊断：缺仓储入口先编译失败；初两实际PG/race2.671秒通过。首次11项中未开通账户夹具把策略和配置分两次提交，被既有fk_strategy_requested_config以23503正确拒绝；改为同事务创建，不改外键/业务断言。最终七新查询场景及四来源回归共11项真实PG/race7.134秒全部通过，自有容器/卷已清理。
- 行为：实际READY→核心1BTC部分成交失效→源物理移除，四历史与原始allow=true/reconciled=false/旧READY组合无损，查询不制造当前许可；USDT799.8/冻结100.1、OMS200保持。四记录逐项游标/共享账户/两来源/不同产品及组合筛选、退役且未开通账户/空feed/9007199254740993序列通过。当前会话读取后独立提交源变化仍不混入原RR，下次读取正常可见；真实只读UPDATE得25006且源Proto/历史不变，父取消/Logout旧token/私有故障/未知原因响应损坏均无部分数据或游标。
- 检查：完整make verify通过，Go1.27.3/49确定性生成/Hertz/fmt/lint含govet 0 issues/政策/22工具/全包单元/race/三命令构建；domain/repository race1.282/1.396秒。高位值/合法旧过期记录/指针拷贝及非法字段纯值边界通过，189文档/461链接/差异及逐文件审查通过。本地未重复默认全量交易/十二浏览器/镜像/持久开发/固定30分钟，前端资产不变。
- 交付/后续：374f832058a0d45f20d30070f37792bbadb6ea1a已推送main并核对SHA，其[CI41174879227](https://github.com/hmlax/muskdeer/actions/runs/41174879227)最近in_progress，未预报。本步检查后独立提交并立即推送核对SHA，继续typed风险历史接口/页面及运行概览/其余P07–P10。完整A27/L1未完成，P11仍缺真实合同和环境；P07.47第五次提交未复现及P07.40三worker同时超时仍保留P09/P10故障验证跟踪，不冒称已修复。


## 2026-10-31：P07.49 风险诊断历史typed gRPC/HTTP

- 改动：具体StrategyRiskEventQuery与RiskDiagnosticEvent（51生成/34模型/schema31），管理开关/API-peer mTLS/当前viewer/5秒/2KiB；API原唯一连接/查询开关/三秒/no-store提供GET /api/v1/risk-events及/:eventID，六种精确筛选/有界游标。记录许可/对账、原因/历史期限及配置/来源/可空绑定原样传输，JSON int64均string，空feed按标准ProtoJSON省略。非法/未知/错配成功整页503，无新模型/迁移/任务/权限/连接/依赖/前端资产，见[风险历史](RISK_EVENTS.md)、[P07.49](validation/P07-49.md)。
- 红测/诊断：新RPC入口先编译失败；初HTTP/RPC0.643/0.567秒通过。首次verify被newStrategy复杂度31（上限30）拒绝，提取同顺序/错误传播的管理装配函数，不屏蔽检查或变更资源所有权。最终完整make verify/Go1.27.3/51确定性生成/Hertz/fmt/lint含govet 0 issues/政策/22工具/全部单元/race/三个命令构建通过，HTTP/gRPC/wire/business race1.459/1.324/1.322/1.393秒。
- 合同：高位ID/序列/修订、Proto往返、旧过期原许可true/对账false/READY与早记录时间保持；可空引用不共享指针，亚毫秒时间/时长拒绝。非API/无期限/2KiB/未知请求及嵌套会话、重复/非法URL/体/Cookie/写方法、错配六种筛选/详情/游标/未知成功字段拒绝，错误无部分数据。
- 原生：最终构建后八项实际API/strategy/PG/Nacos/mTLS/race12.727秒全部通过，新风险历史与原持仓/成交/订单/行情/风险控制及两个仅查询节点回归通过。真实viewer读取4条原始事件及源已物理移除记录，另退役且未开通账户历史/9007199254740993序列/空feed、分页/组合筛选/未知空页、404/400/405、有效非API证书拒绝、无写RPC与Logout恢复旧Cookie后列表/详情401通过。
- 状态/交付：原策略/投影/退役风险源Proto及四历史全字段不变；总5历史/1退役快照/1核心单/1部分成交/0投影成交保持，USDT799.8/冻结100.1、OMS四范围200预留不变，自有资源已清理。190文档/468链接、差异及逐文件复审通过；本地未重复默认全量交易/十二浏览器/镜像/持久开发/固定30分钟，前端资产保持。49239711841e9783d865b9bb1856075a6c26260c已推送main并核对SHA，其CI41175339130及P07.47的CI41174879227最近均in_progress，未预报；本步独立提交并立即推送核对SHA，继续风险历史页面、运行概览及其余P07–P10。完整A27/L1未完成，P11仍缺真实合同/环境，已记录未复现故障继续后续矩阵跟踪。


## 2026-10-31：P07.50 风险历史列表、精确筛选与独立详情页面

- 改动：当前查询导航/策略链接、六种精确筛选/1、20、50游标分页及URL刷新、详情深链接登录/刷新。分别显示记录起点/变化/移除、时间和源快照/配置/来源、原始许可/对账/原因/期限、可空行情引用；明确历史诊断/READY曾判就绪，不作当前许可。string/BigInt、超安全日期原文保留、六秒取消/错误清旧数据/401登录，无写/浏览器存储或新Proto/model/schema/任务/依赖。51生成/34模型/schema31，新增第十三浏览器进入CI/e2e，见[风险历史](RISK_EVENTS.md)、[P07.50](validation/P07-50.md)。
- 红测/诊断：HTML先红404；首verify因路由复杂度33（上限30）拒绝，提取等价路径身份校验，保留原规范ID/404及能力开关。初浏览器5.57秒在恢复短修订3时失败，包含匹配提前读旧长修订9007199254740993，改为整串等待及原断言，8秒期限保持。复审800px导航存在字拆行，完整标签换行/身份独立一行；未以无整页溢出代替视觉检查。
- 最终检查：TypeScript与四资产重复构建SHA相同，JS C4k2QE6P/SHA65620ac3ff4e1d90263d0bcb383fa9493c53a4b93ef0f7184546ba6b652d6f38，CSS BvonsElE/SHA34a1232165387cb8eb536ba08904a84c4aa10a00eba79752ddbf7d610a8fb8a4。最终完整make verify/Go1.27.3/51确定性生成/Hertz/fmt/lint含govet 0 issues/政策/22工具/全包单元/race/三命令构建通过；最终镜像初被沙盒buildx活动目录权限拒绝，正式提权重跑后非root/只读/禁网/readiness/JSON/SIGTERM通过。191文档/472链接/差异/逐文件审查及原CI命令保留/十种真实shell门禁通过。
- 原生：新浏览器15.72秒/包17.454秒通过；实际viewer/双源六记录、移除源/退役且未开通账户、真实高位序列/空feed、六筛选/分页/登录深链接/刷新、本地01无GET/404、Logout恢复旧Cookie401通过。原策略/投影/退役源及四历史Proto全字段保持，总6历史/2快照/1核心单/1部分成交/0持仓事实、USDT799.8/冻结100.1与OMS200预留不变，自有资源已清理。高位修订/极值序列和时间、数字/HTML原因损坏及403均为明确协议注入，两非法字段独立拒绝，非真实资金或权限故障声明；无业务写/JS错误/存储，四最终1440/800/390截图已查看，无整页溢出。
- 共享回归：同一最终资产原十二组全过：登录7.46秒/CLI0.87、策略14.24、资金启停24.07、创建15.31、发布元数据9.64/双模板46.32、审计6.58、风险10.26、订单8.87、撤单10.09、行情14.42、成交10.33、持仓10.13秒。全部原断言与期限保持，隔离资源清理完成；本步13组均有最终资产本地证据，本地未重复默认全量交易/持久开发/固定30分钟。
- 远程/后续：9d8ffa8c127d3702c06c9b90b41bd93a00cc671b已推送main并核对SHA，其[CI41175850059](https://github.com/hmlax/muskdeer/actions/runs/41175850059)completed success；P07.47/48的CI41174879227/41175339130四job也均已success。此成功不代表已记录未复现故障已定位。检查后本步独立提交并立即推送核对SHA，继续运行概览等P07及P08–P10；完整A27/L1未完成，P11仍缺真实合同及环境，无生产迁移/真实资金/部署。


## 2026-10-31：P07.51 运行概览的只读策略统计与租约观测

- 改动：AdminOverview当前viewer只读RR、数据库观测时间、精确账户/请求来源与1–100游标；全筛选范围统计不随游标缩窄，有界18个SQL分组，不读取全量实例。存储状态/请求运行数与每页可空租约分开，OwnerRecorded/UnexpiredAtObservation/RuntimeMatchesLease分别表示保留持有者、采样时未到期、与运行记录相等，不证明当前进程/依赖健康或下单许可。旧格式持有者、零代次槽和释放后保留epoch按真实DDL处理；不领取/续租/推进状态/刷新风险，51生成/34模型/schema31保持，无新传输/页面/依赖/连接/任务/资产，见[运行概览](ADMIN_OVERVIEW.md)、[P07.51](validation/P07-51.md)。
- 红测/诊断：缺仓储入口先编译失败，空库PG/race2.215秒通过。夹具直接更新RUNNING的运行owner被23514“先恢复再运行”拒绝，改用实际Lifecycle恢复/激活及新鲜所选行情。第二轮错误假定Release清空owner，断言失败；核对源码/DDL，修正含糊Held为OwnerRecorded，保留释放owner/epoch及期限0，并覆盖合法epoch0槽与旧格式记录；未改源约束或租约行为。
- 原生：最终五项实际PG/race4.062秒通过，自有资源清理。空库不初始化；RUNNING/CREATED两策略统计与请求目标分开，归档排除/账户请求来源精确、后页及空页保持全范围计数。匹配当前租约、实际过期/接管/释放及真实零代次槽分别展示，旧runtime/风险Proto保持、0订单/0预留；聚合后独立真实创建策略不混入当前RR，下次读取才可见。故障无时间/计数/部分页/私有文本，父取消/Logout旧token拒绝；同只读事务UPDATE账户得到25006，账户/策略原样保持。
- 检查：完整make verify/Go1.27.3/51确定性生成/Hertz/fmt/lint/govet 0 issues/政策/22工具/全包单元/race/三命令构建通过，repository/race1.320秒。纯值高位统计、错误分组/溢出、已释放旧持有者/零槽及非法租约通过；193文档/477链接/差异/逐文件复审通过。本地未重复默认全量交易/十三浏览器/镜像/持久开发/固定30分钟，前端资产不变。
- 交付/后续：64495de851df197529b7506ea88613207679bf0c已推送main并核对SHA，其[CI41178623145](https://github.com/hmlax/muskdeer/actions/runs/41178623145)completed success，含13浏览器及全套门禁。本步独立提交并立即推送核对SHA后，继续typed概览读取、服务实际观测及页面/其余P07–P10；完整A27/L1未完成，P11仍缺真实合同及环境，原未复现故障继续P09/P10矩阵跟踪，无生产迁移/真实资金/部署。
