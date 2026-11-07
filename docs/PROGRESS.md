# 当前进度

更新：2026-11-02。

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
| P07 | in_progress | 网格/持久控制循环、独立运行目标、授权目录/策略查询/创建/启停/发布换源/审计历史及策略风险与额度的typed gRPC/HTTP/页面、收据与待激活/预算门禁通过；双模板资金换源及风险资金关闭/恢复通过。真实OMS/核心订单及管理撤单/控制器确认/收据页面通过；行情监控双源独立进程页面、权威成交及账户持仓仓储/typed接口/页面通过，十二组浏览器线上通过；真实风险诊断历史、只读仓储及typed gRPC/HTTP通过，风险历史页面/十三组浏览器线上通过；运行概览的策略数据库观测仓储及typed gRPC/HTTP、服务实际readiness/成员读取器与typed HTTP/只读页面及14组浏览器通过；34操作OpenAPI/生成门禁、鉴权下载与原生26类GET合同通过；策略范围紧急撤单的原子风险关闭/受理、有界执行/接管、typed接口及确认/原键重试/历史/结果页面与十五组浏览器通过；独立受限平仓及完整A27待完成 |
| P12 | done | React、独立 admin/operator、API 集中鉴权及既有 infra 复用；隔离 make verify、297 项业务/迁移集成、真实两应用/React、开发入口及双镜像通过，见 validation/P12-02.md |
| P08–P10 | todo | 尚未实现和运行 |
| P11 | blocked | 缺真实接口合同及测试环境 |

## 下一步

P12 两应用架构调整及后续独立入口/既有 infra 要求已完成，继续 P07 平仓工作。现有未提交平仓修改保留，不作为 P12 交付提交。
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


## 2026-10-31：P07.52 策略概览typed gRPC/HTTP读取

- 改动：StrategyOverviewQuery及明确请求来源筛选、统计/运行元数据/可空租约typed消息，53生成/34模型/schema31；仅管理查询/API-peer mTLS/当前viewer/5秒/2KiB，API既有strategy连接/查询开关/三秒/no-store的GET /api/v1/overview/strategies。所有int64 JSON为string，源/存储状态/请求目标/租约事实分开，查询不领取/续租/授权风险；无新模型/迁移/任务/连接/依赖/资产，见[运行概览](ADMIN_OVERVIEW.md)、[P07.52](validation/P07-52.md)。
- 红测/诊断：缺RPC/HTTP入口先各自编译失败。原生夹具把StrategyAuthority误传LeaseToken编译拒绝，改用实际Acquire/Renew返回token；首verify复杂度34被拦，提取游标校验并保留全部断言/门禁。复审补映射前负生效版本/来源对及请求标志校验，防止错误字段被省略；亚毫秒时间拒绝，不改旧管理映射。
- 检查：完整make verify/Go1.27.3/53确定性生成/Hertz/fmt/lint/govet 0 issues/政策/22工具/全包单元/race/三命令构建通过，HTTP/gRPC/wire race1.520/1.367/1.350秒；映射前补充后独立最终lint也0 issues。高位统计/ID/修订/代次及Proto往返、32种HTTP损坏成功/筛选错配/计数溢出/嵌套未知/游标/期限、请求权限和私有故障边界通过。
- 原生：13项实际基础设施/API/strategy/mTLS/race16.032秒全过，新概览1.19秒，原概览五仓储及行情/订单/持仓/风险历史/资金风险/成交/仅查询撤单回归通过。真实viewer两策略统计独立于分页和耗尽空页，请求OKX未激活Grid与未知范围正确；实际释放租约保留owner/epoch/期限0及运行匹配但未到期false，nil与显式创建零代次槽分别保留。非API/无写RPC/400/405及Logout恢复旧Cookie401通过；两策略/风险Proto保持，USDT1000/冻结0、0OMS预留/订单/成交，自有资源已清理。
- 交付/后续：194文档/482链接/差异及逐文件复审通过，前端资产保持，本地未重复默认全量交易/13浏览器/镜像/持久开发/固定30分钟。fb0708042132f9f378cd74e2330e30fbdc4850a3已推送main并核对SHA，其[CI41179842883](https://github.com/hmlax/muskdeer/actions/runs/41179842883)最近in_progress，未预报；本步独立提交并立即推送核对SHA后，继续服务实际观测、概览页面及其余P07–P10。完整A27/L1未完成，P11缺真实合同/环境，原未复现故障留待矩阵，无生产迁移/真实资金/部署。


## 2026-10-31：P07.53 有界服务readiness与成员观测读取器

- 改动：AdminServices与serviceoverview，六固定角色/每页1–4/ID游标及全范围成员数。当前API只读本进程唯一Deer.CheckReady；其他角色新鲜读取当前scope健康启用gRPC注册，并借用现有factory/指定实例/mTLS health Check，不自动扩大allowlist。READY/NOT_READY/UNKNOWN、失败/超时/拒绝/容量及未配置各自显示，未探测无时间；成员/声明版本/单次readiness不证明incarnation或交易所有权。3秒整体、400毫秒身份/发现、350毫秒probe，无新Proto/model/schema、HTTP/页面、后台任务/连接池/依赖/资产，53生成/34模型/schema31，见[运行概览](ADMIN_OVERVIEW.md)、[P07.53](validation/P07-53.md)。
- 红测/诊断：缺读取器先编译失败；首verify测试复杂度43/33被拦，提取相同分页断言helper，不放宽门禁。首原生业务断言执行完成但夹具cleanup报context canceled，整组失败保留。父测试defer取消早于t.Cleanup，改为独立有界Run，先cleanup/等待正常退出再取消；操作30秒/probe350毫秒/整体3秒/退出6秒及nil退出断言保持，未忽略取消错误。
- 检查：完整make verify/Go1.27.3/53确定性生成/Hertz/fmt/lint/govet 0 issues/政策/22工具/全包单元/race/三命令构建通过，读取器单元0.711/race1.658、domain/race1.277秒；夹具修正后最终lint也0 issues。指定目标/allowlist快照/不改借用列表/分页/未配置/空成员、非法协议/迟响应子期限/父取消/容量与权限故障、前后会话/角色改变与非法时钟均通过。
- 原生：最终七项实际基础设施/API/双独立strategy/Nacos/mTLS/race8.372秒全过，新服务1.93秒、概览HTTP2.58秒及五仓储回归。两节点真实health与声明版本分开；当前DeerApi测试工作器依赖Check失败时NOT_READY而生命周期Ready仍true（明确注入）。SIGTERM第二节点后成员变1；另持有旧成员快照，实际factory新鲜解析指定失踪实例而失败，不回退存活节点。策略/风险Proto保持，USDT1000/冻结0、0OMS预留/订单/成交，Logout旧token拒绝，正常退出及自有资源清理通过。
- 交付/后续：195文档/487链接/差异及逐文件复审通过，资产不变；本地未重复默认全量交易/13浏览器/镜像/持久开发/固定30分钟。3b2464289aaf87ba9044b1e52a4c86fe0f1a6b77已推送main且远程SHA相等，其[CI41180386616](https://github.com/hmlax/muskdeer/actions/runs/41180386616)最近in_progress；P07.51的CI41179842883已completed success，未预报基线结果。本步独立提交并立即推送核对SHA后，继续服务HTTP、概览页面及其余P07–P10。完整A27/L1未完成，P11仍缺真实合同/环境，原未复现故障继续矩阵，无生产迁移/真实资金/部署。


## 2026-10-31：P07.54 服务观测typed HTTP读取

- 改动：admin/v1/services.proto/一生成文件，54生成/34模型/schema31；GET /api/v1/overview/services沿用查询开关/Cookie/三秒/no-store，借用同一AdminAuth/Deer发现/RPC factory/CheckReady。固定六角色/1–4页长/实例游标，来源/声明版本/就绪与未配置分开，int64 JSON为string，未配置省略可选探测时间。非法/错配成功整次拒绝，不添业务RPC、配置开关、权限、连接、worker、依赖或资产，见[运行概览](ADMIN_OVERVIEW.md)、[P07.54](validation/P07-54.md)。
- 红测/合同：缺typed映射及HTTP入口分别先编译失败；HTTP/wire首轮0.444/0.531秒通过，高位时间JSON/Proto往返、可选时间指针独立、未配置不制造null/零时间、角色/来源/实例/状态/配置/时间/游标/亚毫秒损坏成功、重复/非法URL/Cookie/GET体/写方法及401/403/私有依赖错误均验证，无部分成员或私有地址。复用同一远程ID校验，读取器行为保持。
- 检查：完整make verify/Go1.27.3/54确定性生成/Hertz/fmt/lint/govet 0 issues/政策/22工具/全包单元/race/三命令构建通过，HTTP/读取器/wire race1.532/1.656/1.236秒；原生夹具编译通过。196文档/492链接/差异及逐文件复审通过，前端资产保持；本地未重复默认全量交易/13浏览器/镜像/持久开发/固定30分钟。
- 原生：最终15项实际基础设施/API/业务命令/Nacos/mTLS/race21.268秒全过，新服务HTTP2.31秒：真实viewer具名读双strategy health/分页/全成员计数，当前API独立LOCAL_API/total1/实际READY；无业务worker的标准market角色仅验证真实注册/未配置RPC不探测，不冒称外部行情或业务接入，未部署cron空成员。SIGTERM第二strategy后计数变1、首节点仍READY，400/405/Logout旧Cookie401通过。策略/风险Proto、0OMS预留/订单/成交保持；原服务读取器2.04秒及概览/行情/订单/持仓/风险历史/资金风险/成交/仅查询撤单回归全过，资源清理完成。
- 交付/后续：539f37e448f6b2b48350a06597cda7f4c1be6001已推送main且远程SHA相等，其[CI41181201250](https://github.com/hmlax/muskdeer/actions/runs/41181201250)最近in_progress，未预报；P07.52的CI41180386616已completed success。本步独立提交并立即推送核对SHA后，继续概览页面及其余P07–P10。完整A27/L1未完成，P11仍缺真实合同/环境，原未复现故障继续矩阵，无生产迁移/真实资金/部署。


## 2026-10-31：P07.55 运行概览、策略租约与服务readiness页面

- 改动：查询导航/两能力HTML入口、服务与策略两独立加载/筛选/游标/刷新及URL恢复/初始登录深链接。全范围统计/存储状态/请求目标、请求/生效来源及运行owner/epoch分开；无槽/零代次未领取/释放保留槽分别显示，采样未到期/运行匹配不作当前许可。服务当前API/筛选注册范围/声明版本/实际就绪与未配置不混淆。string/BigInt，日期超范围原值显示；六秒取消/错误清区/401清整个工作台，无业务写/存储/轮询。第十四浏览器接CI/e2e，无新依赖/模型/Proto/迁移，54生成/34模型/schema31，见[运行概览](ADMIN_OVERVIEW.md)、[P07.55](validation/P07-55.md)。
- 红测/复审：HTML先红404；首实际浏览器14.12秒通过，五截图复审后把未领取期限0文案改为“未设置”，释放槽仍“已清零”，补独立断言，未改业务断言/期限。复用策略解析仅导出同函数；新增入口也准备固定Chromium，完整make命令12.83秒/包14.416秒通过。
- 检查：TypeScript/Node语法/可重复构建及make web-generated-check、四资产SHA一致，JS jSsrvxaM/SHA7c3e7bf277e32072d844c883709b106499d455cf144cc236e44c922f0700fbf6，CSS B6NmxxCP/SHAe716ce202df91fe6191a12c25b662b7b01f043ed75de4f0b1338b00e8e249982。最终完整make verify/Go1.27.3/54确定性生成/Hertz/fmt/lint/govet 0 issues/政策/22工具/全包单元/race/三命令构建通过，HTTP0.417/race1.497秒。最终镜像非root/只读/禁网/readiness/JSON/SIGTERM通过；197文档/498链接/差异/逐文件复审及原CI命令保留/十种真实shell门禁通过。
- 原生：最终双阶段浏览器10.95秒/包12.570秒通过：真实viewer三策略/两状态/独立请求数、保留释放/零槽/无槽、双节点具名health及真实停机后只剩首节点；本API/未配置market注册/空cron分别显示。分页/筛选/未知空范围/刷新、精度/独立数值或HTML/租约/范围损坏协议注入及403清区、Logout旧Cookie401通过，无业务写/JS错/浏览器存储。三策略/风险/持仓Proto保持，USDT1000/冻结0、0OMS预留/订单/成交、2租约，资源清理，五最终1440/800/390/高位/停机截图已查看无整页溢出。
- 共享回归/故障：前五组通过后审计在页面启动前Nacos注册/lookup超时；服务端记录加入后移除，客户端既有1秒CommandTimeout，具体时延未定位，失败日志保留。未改客户端/期限/断言，原配置独立审计12.54秒通过；继续后七组。最终同资产原13组均有通过：登录12.17秒/CLI0.84、策略13.28、控制23.06、创建12.20、发布9.90/双模板44.23、审计12.54、风险16.68、订单8.65、撤单10.00、行情15.16、成交10.17、持仓7.56、风险历史12.29。没有将首批失败改为通过或称基础设施已修复，此启动故障与既有未复现事项留P09/P10矩阵。
- 交付/后续：926d8dd483e4b7fe3ecc47b0d7fab2e9d171e583已推送main且远程SHA相等，其[CI41181726835](https://github.com/hmlax/muskdeer/actions/runs/41181726835)及P07.53的CI41181201250已completed success。本步独立提交并立即推送核对SHA后继续OpenAPI及剩余P07/P08–P10。完整A27/L1未完成，本地未重复默认全量交易/持久开发/固定30分钟；P11仍缺真实合同/环境，无生产迁移/真实资金/部署。


## 2026-10-31：P07.55.1 EOF格式与提交门禁核验

- P07.55的39d9eb81092ca15dbc54723e4fff707a72889286已推送main且远程SHA相等。随后发现暂存检查报新文件EOF空行，但当时与统计串在同一shell命令，统计退出码0掩盖失败；普通差异检查也未包含未跟踪新文件，不能替代暂存检查。明确记录该执行错误，追加修正，不改写历史。
- 仅移除overview-api.ts末尾多余空行；make web-generated-check含TypeScript通过，四资产SHA完全保持（p0755-1-web-generated.log/p0755-assets.json），197文档/498链接及普通差异检查通过。Go/浏览器未重跑，格式改动不改变已经验收的服务资产。暂存检查单独运行且退出码0后才执行提交/推送；随后继续OpenAPI及剩余计划。


## 2026-10-31：P07.56 管理OpenAPI与确定性生成门禁

- 改动：[OpenAPI合同](OPENAPI.md)覆盖实际29路径/31管理操作/65schema；由公开Proto响应/Input生成，HTTP登录/会话/控制/风险输入显式定义。当前Cookie/Origin/CSRF/幂等/角色及能力开关、分页筛选、201元数据与202异步/历史结果语义均描述；int64及金融小数保持string，optional省略与message null分开，不暴露内部RPC会话包装。54 Proto生成/34模型/schema31及前端资产保持，在线入口随后交付。
- 红测/检查：缺生成器先编译失败；命令实际生成/检查，漂移时非零且不覆盖文件。固定官方3.1.0/2022-10-07 schema及SHA/Apache-2.0，固定验证器v6.0.2仅用于测试，拒绝未知外部引用。首合同/命令0.334/0.295秒通过；高位和有符号边界、坏类型/溢出/未知字段、模板/小数精度及风险前置字段分别验证，不用零值传输测试冒充业务合法回复。
- 验证：完整make verify通过，Go1.27.3/54确定性Proto及OpenAPI/Hertz/fmt/lint/govet 0 issues/政策/22工具/全包单元/race/构建，新schema race2.290、命令race1.792秒；相关记录见[P07.56](validation/P07-56.md)。四资产SHA与P07.55相同；本地未重复原生HTTP、14浏览器、容器集成、镜像、持久开发或固定30分钟，后续补在线文档及原生载荷合同验证。
- 交付/后续：d04aa7a50d26d5f325239e39db22240962872334已推送main并核对SHA，基线[CI41186229418](https://github.com/hmlax/muskdeer/actions/runs/41186229418)最近in_progress。文档/链接/逐文件复审及独立暂存检查通过后，本步独立提交立即推送核对SHA，再继续P07与P08–P10；完整A27/L1未完成，P11仍缺真实合同/环境，无生产迁移、真实资金或部署。


## 2026-10-31：P07.57 鉴权OpenAPI下载与原生HTTP合同核验

- 改动：GET /openapi.json随查询能力开放，每次通过同一AdminAuth检查当前viewer，三秒/父取消、唯一Cookie/无query/body/no-store及固定Host，失败不泄露部分文档。构造时一次生成285945字节，无文件/外网/新连接/后台任务；54 Proto/34模型/schema31、31操作/65schema及四前端资产保持。测试专用离线验证器未进入业务进程依赖图，见[OpenAPI](OPENAPI.md)、[P07.57](validation/P07-57.md)。
- 红测/检查：入口先红404；API覆盖明确仍是31个/api/v1操作，文档路径有独立delivery覆盖。首lint要求测试分支用tagged switch，修正而不降低门禁。完整make verify通过：Go1.27.3/doctor/contract/生成/Hertz/fmt/lint/govet 0 issues/政策/22工具/全包单元/race/构建，HTTP0.514/race4.152秒、schema race2.119秒；拒绝歧义/禁用/私有依赖/坏身份/拒绝及撤销会话均通过。
- 原生：16项实际基础设施/独立API和strategy/Nacos/mTLS/race22.950秒全过，新合同1.65秒。下载字节与生成物相同；24类GET分别为19类200和5类按缺资源/关闭controls的404，登录/会话/无体Logout及参数400也验证。真实两模板/两源、审计、持仓、行情、概览与风险符合下载schema；修改实际载荷副本的数值类型分别被拒绝，非真实服务故障。撤销后恢复旧Cookie下载401且不泄露文档；两策略/风险Proto、USDT1000/冻结0、0OMS预留/订单/成交保持，自有资源清理。
- 交付/后续：e0c01438f419ae42ddab69f2fdef2680d7f1b608已推送main并核对SHA，基线[CI41186979697](https://github.com/hmlax/muskdeer/actions/runs/41186979697)最近in_progress。文档/链接/逐文件复审及独立暂存检查通过后立即提交推送核对SHA，再继续紧急撤单范围、独立受限平仓及其余P07/P08–P10。完整A27/L1未完成；本地未重复默认全量交易/14浏览器/镜像/持久开发/固定30分钟，P11仍缺真实合同/环境，无生产迁移、真实资金或部署。


## 2026-10-31：P07.58 策略紧急撤单领域请求与进度

- 改动：应用domain-modeling，明确strategy范围及独立风险修订确认、目标收录和pending_submissions；Normalize拒绝缺省/账户/全局扩展，State区分未收录完、无OMS身份提交、待确认目标、已归结失败与全终态。UNKNOWN不计成功/失败，FILLED不冒称CANCELED或平仓，历史结果不授予风险；见[范围合同](ADMIN_EMERGENCY_CANCELLATIONS.md)、[P07.58](validation/P07-58.md)。仅领域组件，无持久化/接口/执行能力，54生成/34模型/schema31和OpenAPI/前端资产保持。
- 红测/验证：缺领域定义先编译失败；选择性单元0.276秒、字段复审改为涵盖全部提交的PendingSubmissions后完整domain单元0.236秒通过。高位身份/风险修订、原因UTF-8字节、明确范围及计数一致性/最大边界/溢出测试通过。完整make verify通过：Go1.27.3/doctor/contract/生成/Hertz/fmt/lint/govet 0 issues/政策/22工具/全包单元/race/构建，domain race1.345秒；改名后另做最终lint，四资产SHA不变。
- 交付/后续：f535c1b5f374e18c5b1e381f3abcd3d2f9ef105d已推送main并核对SHA，[CI41187483413](https://github.com/hmlax/muskdeer/actions/runs/41187483413)最近in_progress，未预报。文档/链接/逐文件复审及独立暂存检查通过后单独提交推送并核对SHA，继续原子关闭风险与批次持久化、范围执行和独立受限平仓。原生/浏览器/镜像/全量integration/持久开发/固定30分钟本步未运行，P07/A27/L1未完成；P08–P10继续，P11缺真实合同/环境，无生产迁移或真实资金。


## 2026-10-31：P07.59 紧急撤单不可变受理结构与schema32

- 改动：Proto唯一生成AdminEmergencyCancellation及版本32，55生成/35模型/schema32；显式strategy范围、风险独立修订、唯一风险关闭和独立范围审计，提交时重新检查当前operator/admin/会话、同一策略/操作者/原因和实际关闭；历史不可改删，不回填请求或修改旧资金/风险/目标。受理证据与未来可变分页进度分开；仍无受理仓储/接口/执行，见[范围合同](ADMIN_EMERGENCY_CANCELLATIONS.md)、[P07.59](validation/P07-59.md)。
- 红测/诊断：缺生成模型先编译失败。首PostgreSQL整批失败，CLI旧二进制输出31而断言32，临时入口遗漏预构建；失败13.556秒及日志保留。完整verify/构建后在全新PG重跑，未把断言改回旧版本或放宽期限；未来版本/回滚探针同步为33，其他升级起点和资金断言保持。
- 检查：完整make verify通过，Go1.27.3/doctor/contract/55生成及OpenAPI/Hertz/fmt/lint/govet 0 issues/政策/22工具/全包单元/race/构建，migrate0.272/race1.431秒；OpenAPI/四资产不变。最终23项实际PG/race15.023秒全过，新结构/旧31升级/撤销提交回滚/重开风险及viewer拒绝分别0.56/0.43/0.46/0.50秒，指定约束验证；35模型真实公共列、迁移CLI两次32、并发/锁/DDL回滚/旧升级及资金/激活/UNKNOWN义务均通过，自有容器网络清理。
- 交付/后续：f127b43c051670c83e1284b20ada52577e85a2bb已推送main并核对SHA，[CI41187757436](https://github.com/hmlax/muskdeer/actions/runs/41187757436)最近in_progress，未预报。文档/链接/逐文件复审及独立暂存检查通过后单独提交立即推送核对SHA，继续分页进度、原子关闭风险与受理、fencing执行及独立受限平仓。只有隔离测试schema迁移，生产/真实资金/部署未执行；业务HTTP/14浏览器/镜像/默认全量integration/持久开发/固定30分钟本地未重复，P07/A27/L1及P08–P10未完成，P11缺真实合同/环境。


## 2026-10-31：P07.60 原子范围受理与未归结恢复门禁

- 改动：内部仓储同一授权/金融事务关闭风险、失效旧缓存、写风险/范围审计和不可变受理；复用原锁顺序，策略互斥后重读历史以支持同键并发。当前operator/CSRF受理重放、当前viewer scoped读取；新键旧风险确认/同键异内容冲突，历史不重算或重执行。schema33仓储和最终恢复谓词一致，所有无完成证明范围保持待处理；55生成/35模型及OpenAPI/四资产保持，无HTTP/gRPC/页面/执行任务，见[范围合同](ADMIN_EMERGENCY_CANCELLATIONS.md)、[P07.60](validation/P07-60.md)。
- 红测/检查：缺类型/仓储先编译失败，预留字段名修正为实际ReservedQuoteNotional，金额断言保持。完整make verify通过：Go1.27.3/doctor/contract/生成/Hertz/fmt/lint/govet 0 issues/政策/22工具/全包单元/race/构建，domain0.291/race1.267、repository0.274/race1.334、migrate0.239/race1.285秒。迁移最新33/未来探针34，旧业务断言保持；先构建全部新二进制再开始PG，未重复旧CLI前置遗漏。
- 原生：36项实际PG/race27.168秒全过。真实LIVE/INTENT/实际提交丢回复UNKNOWN及全部余额/预留、兄弟策略/预算与目标保持；两写点故障回滚全部风险/缓存/历史；四同键只有一个效果，后来风险修订不改历史。当前viewer/CSRF/真实注销、零订单/动作仍待处理、绕过仓储恢复被最终约束拒绝并回滚通过；原正常恢复/未知义务/共享资格/库存/市场/目标、并发/回滚及旧升级/CLI33回归通过，自有资源清理。
- 交付/后续：a5690b3b4de9886b2d70d81b7db4af74c73c6631已推送main并核对SHA，[CI41188444895](https://github.com/hmlax/muskdeer/actions/runs/41188444895)最近in_progress。文档/链接/逐文件复审及独立暂存检查通过后单独提交立即推送核对SHA，继续分页/目标/fencing执行与持久完成证明，然后管理入口及独立受限平仓。当前没有执行完成/恢复能力，不把受理冒充已撤；P07/A27/L1和P08–P10未完成，P11缺真实合同/环境；生产/真实资金/部署未执行，其余HTTP/14浏览器/镜像/全量integration/持久开发/固定30分钟本地未重复。


## 2026-11-02：P07.61 有界范围收录与持久完成证明

- 改动：生成Task及单笔收据可空parent、schema34，56生成/36模型；每次一个范围/最多16目标、已有256待处理上限、不明提交屏障/单调游标/实际义务不可跳过。原接受身份及历史审计仅委托同范围取消，注销不丢持久工作，不恢复旧bearer；普通单笔当前权限保持。复用旧最终fencing/UNKNOWN/资金链路，显式存储进度/完成才可移除范围恢复屏障，见[合同](ADMIN_EMERGENCY_CANCELLATIONS.md)、[P07.61](validation/P07-61.md)。未装配实际周期、管理RPC/HTTP或页面，组件由测试显式驱动。
- 红测/检查：缺Model/端口先编译失败，不存在helper及clear遮蔽builtin由编译/lint拒绝后明确修正，未降低门禁。完整make verify通过，Go1.27.3/doctor/contract/56生成及OpenAPI/Hertz/fmt/lint/govet 0 issues/政策/22工具/全包单元/race/构建，domain0.356/race1.558、repository0.365/race1.416、migrate0.357/race1.380秒；最新34/未来探针35，旧断言和资产保持。
- 原生：最终52项PG/race38.303秒全过：空范围显式完成0.48、17真实预留两页/注销后限范围0.93、实际取消丢回复/owner接管/部分成交0.83、未发动作屏障0.46、写失败原子回滚0.48、跳目标/前缀拒绝0.49、旧33只补待处理0.40秒。物理取消1/新增提交0，核心确认后USDT899.9/冻结0/预留0；原单笔权限/故障/未知/并发/资金、36模型和升级/CLI34均过。收尾独立失败证明两个子场景1.40/包3.192秒：真正NOT_ACCEPTED不解决未知原提交，已知LIVE可FAILED但冻结200.2/预留200保持；最终lint0 issues，未称同批执行，自有资源清理。
- 交付/后续：9400cc8395334978fd4186dc39da3d28a188e8d3已推送main并核对SHA，[CI41189557498](https://github.com/hmlax/muskdeer/actions/runs/41189557498)已completed success；P07.56–P07.59的CI也均已success。文档/链接/逐文件复审及独立暂存检查后独立提交立即推送核对SHA，再接实际策略周期/原生进程及管理入口/页面。P07/A27/L1及P08–P10未完成，P11缺真实合同/环境；仅隔离schema测试，生产/真实资金/部署未执行，HTTP/14浏览器/镜像/全量integration/持久开发/固定30分钟本地未重复。


## 2026-11-02：P07.62 紧急撤单接入策略周期与独立进程

- 改动：借用每进程唯一Deer数据库装配范围端口，现有account worker成功取得/续租并恢复原动作后，每周期最多一个范围，再走原单笔取消/最终fencing。游标耗尽回起点、备用不收录、未激活配置也能处理空范围；无新循环/资源/Proto/model/schema或管理入口，56生成/36模型/schema34和OpenAPI/四资产保持，见[合同](ADMIN_EMERGENCY_CANCELLATIONS.md)、[P07.62](validation/P07-62.md)。
- 红测/检查：缺依赖/游标/stage先编译失败；集成夹具误向仓储调用Submit由编译拒绝，改用既有IntentService。完整make verify通过，Go1.27.3/doctor/contract/生成/Hertz/fmt/lint/govet 0 issues/政策/22工具/全包单元/race/三命令构建，business0.446/race1.402、usecase0.269/race1.447秒；补测试后最终fmt/lint0 issues、usecase/race1.664秒通过。未使用旧二进制证明装配。
- 原生诊断：首23项63.293秒两项失败，旧HTTP遇Nacos一秒查询超时，新夹具漏执行账户所有权worker导致路由lease_lost；补齐既有账户配置，不改fencing/期限。第二批44.633秒完成链路后因夹具numeric文本比较失败，改用typed仓储/decimal同值比较；保留全部证据。Nacos旧故障仍无根因结论，继续P09/P10，复测通过不宣称修复。
- 最终原生：相同23项实际PG/Nacos/NATS/独立业务进程/race44.336秒全过，自有资源清理。新范围6.40秒自动处理注销后的已授权工作与未激活空实例；双策略PID、原owner退出/更高epoch接管、原取消请求仍1，确认前USDT799.8/冻结200.2/预留200。核心竞态成交1后确认撤单，自动持久成功、CANCELED/成交1/预留0、USDT899.9/冻结0；OMS/核心/收据/动作/取消请求/成交各1，无额外提交，风险仍关闭。旧HTTP4.16、单笔接管0.88、暂停恢复停止9.37、普通双进程策略9.16秒及原范围权限/回滚/未知/分页/DDL/升级均过。
- 交付/后续：679bf449e9ab9870b9df1518d842a2efea41dc17已推送main并核对SHA，[CI41373329806](https://github.com/hmlax/muskdeer/actions/runs/41373329806)最近in_progress。206文档/543本地链接/逐文件审查、独立暂存检查后单独提交立即推送核对SHA，再继续viewer进度读取和typed管理入口/页面及独立受限平仓。P07/A27/L1与P08–P10未完成，P11缺真实合同/环境；生产/真实资金/部署未执行，14浏览器/镜像/默认全量integration/持久开发/固定30分钟本地未重复。


## 2026-11-02：P07.63 当前viewer范围历史与只读进度

- 改动：writer/reader窄端口，当前viewer的明确策略/1–100游标历史与单范围观测；原只读RR内统一身份/受理/任务/计数/数据库时间，有界任务批量查询。读取不Prepare/推进/发送，缺任务/不一致证明不可用；存储结果与当前计数分开，历史FAILED后来成交不改写结果。56生成/36模型/schema34、OpenAPI/四资产保持，无公开传输/页面/新资源，见[合同](ADMIN_EMERGENCY_CANCELLATIONS.md)、[P07.63](validation/P07-63.md)。
- 红测/工程：缺Observation/History先编译失败；完整make verify通过，Go1.27.3/doctor/contract/生成/Hertz/fmt/lint/govet0 issues/政策/22工具/全包单元/race/三命令构建，domain0.559/race1.596、repository0.116/race1.412秒。补测试后lint拒绝复杂度34，提取原断言helper、不改门禁，最终fmt/lint0 issues；高位身份/修订、非法/缺失证明单元通过。最终生产代码在原生前已构建，verify后只补测试。
- 原生：22项实际PG/race16.479秒全过，自有资源清理。新空范围读取2.12秒保持Task，仅显式worker后见完成；历史分页0.61秒隔离共享账户兄弟，观测/历史跨事务变化0.51/0.47秒保留同授权快照，后续才见新任务/目标及注销拒绝。两个读取路径真实UPDATE账户均获25006（0.97秒），零部分结果/时间、账户/Task保持。原失败用例1.32秒保留NOT_ACCEPTED/冻结/预留断言，后来真实全成交+OMS Recover使当前terminal1/failed0，存储FAILED及修订/完成时间保持；原受理/权限/并发/回滚/UNKNOWN/最终约束/升级通过。
- 交付/后续：4f08f51718845926ea17d435158a887ffcdde28e已推送main并核对SHA，[CI41374338063](https://github.com/hmlax/muskdeer/actions/runs/41374338063)最近in_progress。文档/链接/逐文件复审及独立暂存检查后单独提交立即推送核对SHA，继续typed管理RPC/HTTP和确认页面、受限平仓。P07/A27与P08–P10/L1未完成，P11缺真实合同/环境；旧Nacos等故障留待矩阵，本步不宣称修复。生产/真实资金/部署、独立业务HTTP/14浏览器/镜像/默认全量integration/持久开发/固定30分钟本地未执行。


## 2026-11-02：P07.64 范围紧急撤单typed管理gRPC

- 改动：明确scope/策略/独立风险修订/原因/键的Request只返不可变受理；Query读存储Record/当前Progress/数据库时间及有界历史。API-peer mTLS/当前权限/CSRF、5秒/2KiB/未知字段/单次调用保留，Management查询与Control写服务分开注册，借用同一Deer数据库；无HTTP/页面/新模型/迁移/资源。58生成/36模型/schema34，OpenAPI31操作/65schema及四资产保持，见[合同](ADMIN_EMERGENCY_CANCELLATIONS.md)、[P07.64](validation/P07-64.md)。纠正INTERFACES将已有OpenAPI列为未交付的过时概述，概念接口仍不视作实现清单。
- 红测/检查：缺typed消息/入口先编译失败；实际生成新增两个生成物，首lint拒绝copy遮蔽builtin后改名。最终完整make verify通过：Go1.27.3/doctor/contract/58生成/OpenAPI/Hertz/fmt/lint/govet0 issues/政策/22工具/全包单元/race/三命令构建，strategygrpc0.654/race1.914、strategywire/race1.891秒。高位身份/修订、Proto往返、16种嵌套/范围/计数损坏、历史结果与当前计数、分页/精确毫秒、期限/权限/私有故障通过。
- 原生：最终13项实际PG/Nacos/NATS/独立业务进程/race29.950秒全过，自有资源清理。新实际管理RPC受理/同键、独立风险1→2、异内容/旧修订冲突、viewer/CSRF/非API证书拒绝及真实注销通过；Get/List不推进任务，LIVE/策略Proto及预留200保持，USDT799.8/冻结200.2，受理/风险各1、动作/取消请求0。只读进程不注册写服务，空范围不伪造成功；旧HTTP撤单、控制/查询管理、范围双进程接管及只读查询/历史失败后来成交全过，未变期限/断言。
- 交付/后续：6ff7cd6b397902eb52ecf1818df44cdae4659abe已推送main并核对SHA，[CI41374888508](https://github.com/hmlax/muskdeer/actions/runs/41374888508)最近in_progress。文档/链接/逐文件审查及独立暂存检查后单独提交立即推送核对SHA，继续Hertz HTTP/确认页面及受限平仓。完整P07/A27和P08–P10/L1未完成，P11缺真实合同/环境；生产/真实资金/部署、14浏览器/镜像/默认全量integration/持久开发/固定30分钟本地未重复。旧Nacos等故障继续矩阵，本次通过不宣称根因已修复。


## 2026-11-02：P07.65 范围紧急撤单Hertz HTTP与OpenAPI

- 改动：DeerApi原连接上的POST范围受理、GET观测及1–100历史，明确scope/字符串风险修订/原因，固定Host/Origin/唯一会话/CSRF/键及2KiB，严格拒绝歧义；首次与完成后重放都202加Location，当前任务另GET。queries/controls分开，现有GET同路径禁写405、未配置404。58生成/36模型/schema34/四资产保持，无新资源/页面；OpenAPI31路径/34操作/71schema、312400字节，见[合同](ADMIN_EMERGENCY_CANCELLATIONS.md)、[P07.65](validation/P07-65.md)。
- 红测/工程：缺装配字段先编译失败，首次OpenAPI比较发现生成物未刷新，实际生成后通过。完整make verify通过，Go1.27.3/doctor/contract/58生成及新OpenAPI/Hertz/fmt/lint/govet0 issues/政策/22工具/全包单元/race/三命令构建，HTTP/race4.135、OpenAPI/race1.836秒。高位字符串、11种歧义、独立证明、错误成功/固定故障、分页及202-only schema通过，最终生产二进制在原生前构建。
- 原生诊断：首9项19.156秒两个新夹具失败，其余7过；误将无体204交给JSON检查器，以及仅查询登录误用controls=true的helper。改用原严格Logout204/恢复旧Cookie，并明确断言query=true/controls=false/operator，原期限和生产实现不变；最终fmt/lint0 issues。
- 最终原生：相同9项实际PG/Nacos/NATS/独立API及业务进程/race22.771秒全过，自有资源清理。新闭环4.30秒完成真实受理/原键202、异内容/旧修订409、viewer/CSRF403、注销恢复旧Cookie401；已受理工作随后由实际worker执行，确认前USDT799.8/冻结200.2/预留200，竞态成交1再确认后存储成功、预留0、USDT899.9/冻结0。最终OMS/核心/子收据/动作/取消请求/成交各1，范围/风险各1且风险仍关闭；重登录后已完成原键仍同一受理202，历史/空页及旧viewer拒绝均过，实际载荷按下载合同验证。仅查询1.29秒保持空Task并拒POST405；旧单笔、范围接管、RPC、全26类GET合同及资金风险HTTP全过。
- 交付/后续：dae621739cdc840eec8a2d50433c38b10b3a526e已推送main并核对SHA，[CI41375523473](https://github.com/hmlax/muskdeer/actions/runs/41375523473)最近in_progress。文档/链接/逐文件审查及独立暂存检查后单独提交立即推送核对SHA，继续范围浏览器确认/结果页面及独立受限平仓。P07/A27及P08–P10/L1未完成，P11缺真实合同/环境；生产/真实资金/部署、14浏览器/镜像/默认全量integration/持久开发/固定30分钟本地未重复，旧Nacos等故障留待矩阵。


## 2026-11-02：P07.66 范围撤单浏览器确认、历史与结果

- 改动：明确策略/账户/交易对/独立风险修订的人工确认、当前权限及原键重试；历史游标和结果深链接、存储任务与当前计数分开展示，有界读取/轮询，错误清旧数据，BigInt与原文展示。Hertz页面随queries开放，真实浏览器入口接入CI/e2e第十五组；58生成/36模型/schema34、OpenAPI34操作/71schema保持，见[合同](ADMIN_EMERGENCY_CANCELLATIONS.md)、[P07.66](validation/P07-66.md)。
- 工程/诊断：HTML先红404后0.516秒通过；首次浏览器14.51秒因协议注入循环复用旧错误提示而重复处理route失败，逐例恢复并等待真实成功后通过，不改生产行为或放宽期限。手机布局按截图修正后冻结四资产、构建，再做最终检查。完整make verify通过，精确Go1.27.3/doctor/contract/58生成/OpenAPI/Hertz/fmt/lint/govet0 issues/政策/26工具/全包单元/race/三命令构建，资产两次构建相同。
- 浏览器/资金：最终三阶段14.81秒/包16.501秒全部通过。实际两个挂单提交后丢202回复，原键原体重试只关闭风险/受理各1；未启动owner前策略Proto保持。两个取消待确认时USDT749.75/冻结250.25/预留250，仍ACCEPTED；真实竞态成交1并确认两个取消后持久成功、terminal2、预留0、USDT899.9/冻结0，风险仍关闭。OMS/核心/子收据/动作/取消各2、成交1，无额外新单；viewer历史/刷新/注销恢复旧Cookie和独立协议损坏/高位注入通过，1440/800/390及手机确认截图已查看。
- 回归：相同最终资产的原十四组/十五项实际浏览器全过，包142.802秒；make image-test的非root/只读/禁网readiness、JSON日志/SIGTERM通过，自有资源清理。没有重复固定30分钟或持久开发。
- CI诊断：基线[CI41376416311](https://github.com/hmlax/muskdeer/actions/runs/41376416311)的checks/browsers成功，integration迁移包累计600.095秒触发默认整包10分钟，当时soak smoke只运行11秒、无前置断言失败。改为实际完整Go清单两个顺序批次，保留race/count=1/各10分钟，空/重复/坏清单或任何失败立即退出并保留自有诊断。4项工具红测缺实现后11项执行器单元通过，实际270项分135/135；旧方式本地迁移包480.867秒一项服务概览注册等待Nacos一秒超时、其余通过，保留诊断且未放宽期限；最终make integration退出0，persistenceprobe0.542/infrastructureprobe-race32.125秒；迁移两批230.364/228.400秒，270项RUN/PASS与原清单逐批相等，无遗漏/重复/跳过，soak smoke实际12秒/6成交，全量断言保持，自有资源清理。旧Nacos等独立故障不因本次诊断宣称修复。
- 交付/后续：基线138ebd205bc47e444aa03e75a135cbf8c47b9e7b已推送main并核对SHA；本步文档/链接、逐文件复审和独立暂存检查后单独提交立即推送核对SHA，再推进独立受限平仓。完整P07/A27及P08–P10/L1未完成，P11仍缺真实合同/环境；无生产迁移、真实资金或部署。


## 2026-11-02：P07.67 受限平仓领域确认与候选门禁

- 改动：明确account_instrument范围及唯一策略关联的模拟现货账户，当前admin候选、原范围持久成功/独立风险仍关闭、持仓及核心余额修订/数量确认；全部义务与未知提交为零、投影对账/来源完整才可提出精确SELL/reduce_only/GTC。共享账户、裁剪或非零尘埃拒绝，NO_ORDER不是持久完成。仅领域纯函数，无持久化/接口/执行授权/页面；58生成/36模型/schema34、OpenAPI34操作/71schema/四资产保持，见[合同](ADMIN_FLATTENINGS.md)、[P07.67](validation/P07-67.md)。
- 红测/精度：先因缺领域定义编译失败；首绿0.319秒，补当前时间/坏证明/资产门禁后0.297秒。新增ledger尾零红测0.245秒失败，改为只有截到18位后数值完全相等才可转订单精度，非零余数继续拒绝；七项最终0.247秒通过。高位ID/不增风险MaxInt64、角色/修订/全量义务、共享/真实场所、空余额UNKNOWN、坏/未来证明、tick/lot/金额上下界与123.46913480236913477891精确乘积通过。
- 检查：最终完整make verify通过，精确Go1.27.3/doctor/contract/58生成/OpenAPI/Hertz/fmt/lint/govet0 issues/政策/26工具/全包单元/race/三命令构建；domain最终沿用相同源码通过缓存，前一完整单元0.057/race1.368秒。资产SHA相同；此步无数据库/进程/浏览器/镜像/固定30分钟验证，未借用上一提交结果冒称重跑。
- 交付/后续：基线8b7a61f10e0198402b1a4e10ae91b25cf34e95cf已推送main且SHA相同，[CI41380886998](https://github.com/hmlax/muskdeer/actions/runs/41380886998)最近in_progress。212文档/578本地链接检查后，独立暂存复审、单独提交并立即推送核对SHA，再继续持久请求、互斥及专用最终fencing。完整P07/A27及P08–P10/L1未完成，P11缺真实合同/环境，旧Nacos等故障保持待定位；无生产迁移、真实资金或部署。


## 2026-11-02：P07.68 Proto平仓确认结构与schema35

- 改动：Proto唯一生成AdminAccountFlattening及schema35，59生成/37模型；确认账户/产品/唯一策略、原范围记录、独立风险/投影/核心余额修订、完整数量/限价及当前admin/独立审计。COMMIT重核当前权限、范围/风险/余额/源统计/已有义务与精度限额，历史不可改删、审计及操作者键分别唯一。仅结构，无受理仓储、工作进度、互斥或交易授权/接口/页面；OpenAPI34操作/71schema和四资产保持，见[合同](ADMIN_FLATTENINGS.md)、[P07.68](validation/P07-68.md)。
- 红测/检查：缺生成模型先编译失败；make generate实际生成59文件，未手改生成物。最新断言35/未来及回滚探针36，旧升级起点和业务断言保持；生产二进制在原生前构建。完整make verify通过：Go1.27.3/doctor/contract/生成/OpenAPI/Hertz/fmt/lint/govet0 issues/政策/26工具/全包单元/race/三命令构建，domain0.225/race1.352、migrate0.258/race1.366秒。补独立唯一性测试后最终fmt/lint0 issues。
- 原生：21项实际隔离PostgreSQL/race15.807秒全过，资源清理。新结构1.02秒验证12类坏确认/缺账户FK、10及10.01精确往返、独立业务键/审计唯一、两个自增ID和历史约束；策略Proto、BTC10/USDT1000/冻结0/预留0/核心单0保持。提交复核0.86秒拒绝同事务注销或余额/修订变化并完整回滚，viewer/operator拒绝；新增共享关联后旧确认拒绝0.63秒。34→35/重放0.49秒不改旧风险或补造请求，其余17项覆盖37模型公共列、旧升级/UNKNOWN/并发/期限/DDL回滚/未来历史及CLI两次35，全部断言保持。
- 交付/后续：基线a5fc0c25d9272101af1b9db350fb0155402424b0已推送main且核对SHA；[P07.66 CI41380886998](https://github.com/hmlax/muskdeer/actions/runs/41380886998)所有jobs均success，含十五组浏览器和修正后的完整分批集成。文档/链接与独立暂存复审后单独提交立即推送核对SHA，再继续受理仓储、账户互斥和最终fencing。P07/A27/平仓执行及P08–P10/L1仍未完成，P11缺真实合同/环境；本步未重复默认全量integration/浏览器/镜像/持久开发/固定30分钟，无生产迁移、真实资金或部署；旧Nacos偶发故障根因保持待定位。


## 2026-11-02：P07.69 待处理平仓账户/配置/风险屏障

- 改动：schema36在既有金融互斥及延迟约束下保护账户，未有完成证明前所有确认含零数量均待处理，阻止管理员恢复、预算/缓存新增风险、共享策略关联及参数/来源版本/激活变化；正常运行/租约字段保持各自语义，不授权交易。新建/发布确切约束映射CONFLICT、恢复沿用RECOVERY_DENIED，不隐藏其他存储错误。59生成/37模型/OpenAPI34操作/71schema/四资产保持，无受理仓储/工作执行/新入口，见[合同](ADMIN_FLATTENINGS.md)、[P07.69](validation/P07-69.md)。
- 红测/检查：分类单元先失败0.598秒；三项实际PG红测包2.891秒，原实现确实允许恢复/新共享策略及两种顺序的配置+确认提交。分类0.450秒绿；完整make verify通过，Go1.27.3/doctor/contract/59生成/OpenAPI/Hertz/fmt/lint/govet0 issues/政策/26工具/全包单元/race/三命令构建，repository0.158/race1.391、migrate0.284/race1.358秒。最新断言36/未来及回滚探针37，旧升级起点、资金断言保持，原生前已构建最终二进制。
- 原生：53项独立PG/race44.827秒全过，资源清理。新恢复/直接预算/缓存拒绝0.60秒保留关闭修订/预留0/核心单0；创建/发布冲突0.57秒无策略/配置/目标/收据部分效果；两种事务顺序0.58秒均提交拒绝且完整回滚。原平仓结构、创建/发布/风险并发和故障/当前权限/资格/库存/来源、历史、37模型公共列及旧升级/UNKNOWN/并发/锁期限/DDL回滚/未来版本/实际CLI两次36全过，无断言改宽或跳过。资产SHA一致。
- 交付/后续：基线50db6cb7ec267f1d51a70abbfae50e1de515a10d已推送main且SHA相等，[P07.68 CI41382609803](https://github.com/hmlax/muskdeer/actions/runs/41382609803)所有jobs success。文档/链接和独立暂存复审后单独提交立即推送核对SHA，再继续受理仓储、活动工作唯一性及专用最终fencing；完整平仓/P07/A27及P08–P10/L1未完成，P11缺真实合同/环境。默认全量integration/浏览器/镜像/持久开发/固定30分钟本步未重复，无生产迁移、真实资金或部署，旧Nacos偶发根因仍待定位。


## 2026-11-02：P07.70 管理员平仓受理、原键重放与只读确认

- 改动：当前admin/会话/独立CSRF在原键读取前校验，既有金融锁下重读唯一账户关联、范围证明、独立风险/投影/核心余额修订、全量余额/对账/义务及精度限额；原子写独立审计和不可变确认，原键保留历史，新键待处理冲突。当前viewer只读RR取确认，不补账或推进工作。只借用数据库，无新资源/模型/迁移/执行授权/公开入口，59生成/37模型/schema36/OpenAPI34操作/71schema/四资产保持，见[合同](ADMIN_FLATTENINGS.md)、[P07.70](validation/P07-70.md)。
- 红测/诊断：缺类型/构造/编码器先编译失败，首domain/repository0.550/0.359秒绿。首verify被两处回调AddError未处理返回拒绝，显式写回事务错误；首五项PG/race6.380秒四过一失败，撤销夹具复用创建SQL造成重复audit键（单例诊断1.179秒）。对照固定GORM源码，先WithContext再NewDB重置语句且保留事务，强化恰好更新一个会话；单例0.69/包2.391秒通过，未改业务权限/期限或放宽断言。
- 检查：最终完整make verify通过，Go1.27.3/doctor/contract/59生成/OpenAPI/Hertz/fmt/lint/govet0 issues/政策/26工具/全包单元/race/三命令构建，repository0.603/race2.082、domain单元同源缓存/race1.275秒，最终二进制在原生前构建。
- 原生：59项独立PG/race51.492秒全过，资源清理。权限/CSRF/修订/数量及真实注销重登录2.77秒；两写点回滚1.02秒及提交撤销回滚0.53秒；共享/未对账/未完成范围拒绝1.58秒；历史/只读0.61秒保留原确认10，明确后来余额故障注入9后不补账；四同键0.59秒确认/独立平仓审计各1、异内容和新键冲突，无新核心单/预留/策略变更。原平仓及屏障、创建/发布/风险故障/并发/资格/历史、37模型和升级/UNKNOWN/锁/回滚/未来历史及CLI36全过。四资产SHA相同。
- 交付/后续：基线164bad124ec93e28579b86c32fc95fa89ded879d已推送main并核对SHA，[P07.69 CI41389674759](https://github.com/hmlax/muskdeer/actions/runs/41389674759)四项jobs success。文档/链接和独立暂存复审后单独提交立即推送核对SHA，再继续活动工作、唯一执行和专用最终授权；全部确认仍待处理，完整平仓/P07/A27及P08–P10/L1未完成，P11缺真实合同/环境。默认全量integration/浏览器/镜像/持久开发/固定30分钟本步未重复，无生产迁移、真实资金或部署；旧Nacos偶发根因保持待定位。


## 2026-11-02：P07.71 平仓持久任务及账户活动唯一性

- 改动：Proto生成AdminAccountFlatteningTask/schema37，60生成/38模型；新确认同事务自动QUEUED，旧确认升级只补待处理标记，当前viewer只读RR取存储进度。账户只允许一个ACTIVE，身份/范围/修订不可回退；提交重核实际租约/运行身份及数据库时钟，高epoch接管保留原任务。QUEUED/ACTIVE/缺行/零数量仍待处理，无终态、认领仓储、执行许可或公开入口；OpenAPI34操作/71schema/312400字节与四资产保持，见[合同](ADMIN_FLATTENINGS.md)、[P07.71](validation/P07-71.md)。
- 红测/诊断：先缺模型/状态/codec编译失败，domain/repository0.260/0.607秒绿；四项PG缺表红测2.923秒。首schema轮2.901秒一过三失败，改真实受理事务内SQL初始化后故障注入并断言确有任务、按既有RECOVERING准备运行身份；不放宽生产状态/权限/期限。下一轮4.069秒四过一失败，升级改比数据库前后完整Proto而非Go入参与numeric尾零，六项5.011秒过。
- 继续诊断：首72项58.455秒71过/一项有效重试被owner拒绝；单例2.424秒及20次12.489秒均过，未复现根因。夹具改认领/更新时间同取PG时钟、明确未来时间拒绝，20次15.100秒过；不据此宣称原偶发根因确定。随后72项60.429秒全过。审查另发现租约先于策略的实际锁反序；PID只读投影修正后确定性红测0.54/包1.140秒得到55P03，调整为既有策略→租约顺序，同例0.95/包5.703秒绿，原期限/权限和资金断言保持。
- 检查：最终完整make verify通过，Go1.27.3/doctor/contract/60生成/OpenAPI/Hertz/fmt/lint/govet0 issues/政策/26工具/全包单元/race/三命令构建；migrate0.333/race1.278秒，domain/repository同源缓存，repository此前0.295/race1.366秒。最终二进制在原生前构建。
- 最终原生：73项独立PG/race58.293秒全过，RUN/PASS与精确选择清单相等，无遗漏/重复/失败，自有资源清理。七项新任务0.54/0.49/0.51/0.49/0.52/0.51/0.59秒覆盖原子回滚、当前viewer/只读、初态/历史、账户活动并发、到期/未来时间、真实epoch接管、确定性锁顺序及36→37重放，无意图/订单/预留。其余66项原受理/屏障、创建/发布/风险、实际核心资金/费用/STP与双最终fencing、38模型及升级/UNKNOWN/锁/回滚/未来38历史/实际CLI两次37全过，四资产SHA一致。
- 交付/后续：基线a05734ba345ad44e394b4d7c7778597739c736f6已推送main且远程SHA一致，[P07.70 CI41392531983](https://github.com/hmlax/muskdeer/actions/runs/41392531983)四项jobs均success。文档/链接和独立暂存复审后单独提交立即推送核对SHA，继续持久认领、专用最终授权与实际执行；完整平仓/P07/A27及P08–P10/L1未完成，P11缺真实合同/环境。默认全量integration/浏览器/镜像/持久开发/固定30分钟本步未运行，无生产迁移、真实资金或部署；原认领偶发及旧Nacos等诊断证据保留，不将未复现当作已确认根因修复。


## 2026-11-02：P07.72 当前所有者认领平仓任务

- 改动：借用数据库的AdminFlattenWork.Claim，在ReadCommitted按既有策略→租约→任务/金融锁顺序，重核实际租约/运行身份、原确认的独立风险/投影/余额修订、范围完成/唯一账户/全量数量/精度/对账/义务。相同owner重查资格但不改行，高epoch接管保留原ID；时间同取PG，写后/提交失权零返回并完整回滚，两个历史确认竞争返回明确账户冲突。无新资源/迁移/工作循环/意图/订单/预留或最终交易授权，60生成/38模型/schema37/OpenAPI34操作/71schema/312400字节/四资产保持，见[合同](ADMIN_FLATTENINGS.md)、[P07.72](validation/P07-72.md)。
- 红测/检查：构造/取消先缺工厂编译失败，repository0.353秒绿/domain同源缓存；首三项PG/race6.109秒过，加入实际写后/提交故障四项7.807秒过。完整make verify通过，Go1.27.3/doctor/contract/60生成/OpenAPI/Hertz/fmt/lint/govet0 issues/政策/26工具/全包单元/race/三命令构建；repository单元同源缓存/race1.357秒。补历史竞争测试后最终fmt及make lint/govet0 issues，最终二进制在原生前构建。
- 最终原生：78项独立PG/race65.453秒全过，RUN/PASS与精确选择清单相等，无遗漏/重复/失败，资源清理。新五项0.57/3.12/0.57/1.01/0.51秒：管理员实际注销后由当前owner认领历史确认、同owner不改行；六类事实/权限变化拒绝；四同owner仅一次认领/真实高epoch接管保留ID；两个实际写后故障完整回滚并可恢复；历史确认竞争仅一个ACTIVE，另一个保持QUEUED。策略Proto和BTC10/USDT1000/冻结0/预留0保持，无动作/意图/核心单。原73项全部资金/风险/权限/并发/历史/最终fencing、38模型及迁移/CLI37断言保持，资产SHA相同。
- 交付/后续：基线b0d04dec15d2be4118027bc53267f3cfddc9aaeb已推送main且远程SHA一致，[P07.71 CI41396854480](https://github.com/hmlax/muskdeer/actions/runs/41396854480)最近in_progress。文档/链接和独立暂存检查后单独提交立即推送核对SHA，再继续唯一OMS意图关联与专用最终授权/实际执行。完整平仓/P07/A27及P08–P10/L1未完成，P11缺真实合同/环境；默认全量integration/浏览器/镜像/持久开发/固定30分钟本步未运行，无生产迁移、真实资金或部署。原认领间歇失败及旧Nacos等证据保持，不将未复现当作已确认根因修复。


## 2026-11-02：P12.1 两应用架构约定
- 根据用户最新要求确定 React + admin（Hertz/HTTP）+ operator（Kratos/gRPC），替代六角色及独立 cron 的部署要求；单仓库、单 go.mod、Deer 及业务正确性保持。
- 明确会话/角色/CSRF 统一在 admin 校验，operator 接收审计身份，业务风控、幂等和最终 fencing 继续执行。
- 验证：git diff --check 及本步文档内容/引用检查；本步只变更规范，业务测试未运行，P12.2/P12.3 尚未完成。
- 原有未提交平仓代码及生成物保持，未纳入本步提交。工作分支：codex/two-apps。


## 2026-11-02：P12.2–P12.4 独立 admin/operator 与既有 infra

- 按用户最新要求保留 React，后端仅 admin（Hertz/HTTP）和 operator（Kratos/gRPC）两个应用，分别独立 cmd 入口；删除共用 cmd/role 分发，旧模块也使用独立测试夹具。单仓库、单 go.mod、Deer 装配及租约/资金/最终 fencing 保持。
- admin 校验会话、角色和 CSRF，operator 只接收认证 admin 传入的审计身份；真实 HTTP→gRPC 操作、权限拒绝、原键重试和注销通过。React 概览展示两种实际服务及就绪；前端及 OpenAPI 生成物同步。
- 测试/开发接入改为复用现有 Docker infra，独立逻辑资源结束清理，Redis 不执行全局暂停，认证拒绝用临时账号。经授权设置独立 NATS 测试账户及 9 GiB 总额度；没有新增基础服务部署。
- 最终隔离验收：make verify 全部通过，lint/govet 0 issues；make integration 的依赖合同及 297 项迁移/业务入口（149+148、race）通过；make two-apps-test 的实际资金/停止闭环、独立 admin/operator 和真实 React/Chromium 通过；make dev-test 与两个非 root/只读/禁网镜像生命周期通过。24 工具测试、前端检查/构建及文档/差异检查通过。详见 [P12.2–P12.4 验证](validation/P12-02.md)。
- 诊断保留：旧认证失败夹具、独立入口的取消/超时诊断及 Nacos 异步用户缓存均已修复并重验。宿主磁盘耗尽导致共享 Docker 只读，清理约 39 GB 本项目 Go 缓存，经授权重启 Docker Desktop，确认原容器恢复后重验通过。真实凭据及运行证据仅在忽略的本地缓存。
- 原有未提交平仓代码、生成物与相关事件测试失败保持，不纳入本步提交；固定三十分钟、全部十五组历史浏览器及生产环境未运行。P07/A27 及后续 MVP 层级仍未完成；CI 集成/浏览器任务需已有 infra 的自托管 runner，不冒充远程 CI 通过。
- 文档基线 6df6cea 已推送 codex/two-apps 并核验；本步独立暂存、提交后立即推送同一分支，提交 SHA 与远程核验见交付报告及 Git 历史。

## 2026-11-02：P12.5 Docker Compose 复用 infra 并实际启动

- 根据用户要求增加应用专用 compose.yml，仅启动 admin/operator；连接既有 infra-network 和 PostgreSQL/Redis/Nacos，不部署基础服务。创建有所有权标记的独立数据库/角色及 Nacos 命名空间，配置/凭据/本地 mTLS 仅保存于忽略缓存。显式用同源码镜像中的 migrate/adminuser 完成 schema 37 和一次管理员初始化，重复运行不重置密码。
- 管理服务新增显式本地 Docker 监听选项，仍固定 loopback 浏览器来源且宿主仅发布 127.0.0.1:59000；operator 经 Nacos/mTLS 通信，无宿主端口。当前仅管理模块，无账户/资金/行情执行工作和消息流，不产生交易；消息工作后续继续复用 infra-nats 独立开发账户。
- 验证：独立源码的 fmt/生成检查/lint/govet（0 issues）/全包单元通过，相关 race 与 28 脚本测试通过；最终两镜像构建、显式迁移重放、Compose up --wait、两容器 healthy、真实 HTTP 登录/Origin 拒绝/Nacos→operator 查询及真实 React/Chromium 概览通过。既有四个 infra 容器 ID 与启动时间保持。详见 [P12.5 验证](validation/P12-05.md)。
- 诊断：ProtoJSON 持续时间 500ms 改为 0.5s 后实际启动通过；监听校验复杂度拆分后静态检查通过。Buildx/Chromium 沙箱限制经工具审批重验。既有 NATS unhealthy 不属于本步修复，管理模式不连接 NATS。保留原有平仓未提交修改；运行镜像使用独立 schema 37 源码，未借本步提交该功能。完整集成/历史浏览器/三十分钟/生产本步未运行。
- 地址 http://127.0.0.1:59000，用户名 admin，密码见忽略的 .cache/compose/credentials.json。基线 2b69495 已同步 codex/two-apps；本步验证后立即独立提交、推送并核验远程 SHA，交付报告记录结果。

## 2026-11-02：P13 独立核心 Kratos 服务并启动

- 用户后续明确管理后台均不用、核心模块分别独立进程，替代 P12 两应用部署边界。新增 cmd/market、strategy、execution、portfolio，各有参数解析、入口、镜像、gRPC 端口和 Nacos 身份；独立 coreentry 只装配 Deer/业务工作循环，拒绝管理 HTTP/RPC 开启标志。默认构建及 Compose 移除 admin/operator 和 React 依赖，原管理容器已移除，历史源码及原有未提交平仓修改保留。未实现报表/归档不部署空 cron。
- 继续复用既有 infra；经工具审批为 infra-nats 热加载独立持久 MUSKDEER_LOCAL 账户，原账户流/消费者保持，无基础容器部署或重启。显式迁移同源码 schema 37，由 coreinit 准备 16 MiB 流、durable、双源 REPLAY 绑定和禁用/未注资账户，不创建管理员、不注资或开策略。重复准备拒绝不兼容 broker、保留预算与资源 ID。
- 验证：独立源码 fmt/60 Proto 与 OpenAPI 生成检查/lint/govet（0 issues）/全包单元及核心构建通过；28 脚本测试通过；coreentry/business 相关 race 通过；make core-test 四项实际 infra/race 14.335 秒全过，包含真正新核心命令的成交投影/预算闭环、策略双节点接管及终态停止、资源幂等及 broker 冲突不覆盖。
- 运行：最终四镜像构建及非 root/只读/禁网 readiness、JSON 日志和 SIGTERM 通过；Compose up --wait 四容器全部 healthy，四个独立 Nacos 健康身份。实际 grpcurl/mTLS 双源行情均 HEALTHY、99.99/100.01 精确返回，execution 订单列表为空。端口分别 59002–59005，仅本机发布。详见 [核心运行说明](CORE_SERVICES.md) 和 [P13 验证](validation/P13-01.md)。
- 默认 doctor/dev 配置、e2e 和 CI 同步为核心服务；管理资产/浏览器退出默认 CI，历史管理回归仅显式 legacy-admin-e2e。dev-test 用独立临时目录验证实际 market 进程连接既有 PG/Redis、重复关联和解除后共享容器保持；未声称远程 CI 已通过。
- 诊断：首次消息前缀不满足三级合同已修正；真实 PG 测试复现嵌套 GORM 事务选表错误，初始化改为单事务显式生成 Model 操作后通过，未放宽资金/幂等断言。首测试夹具补实际迁移与有界 context，静态检查问题修复后重验。完整历史集成/浏览器/三十分钟及生产本步未运行，运行数据为回放和禁用模拟账户，不宣称外部实连或实盘。工作分支 codex/two-apps，验证后独立提交、立即推送并核验 SHA。

## 2026-11-02：P13.2 各服务独立启动装配

- 按用户针对共享 Run 方法的后续要求，移除 coreentry.Run 及 name 分发；market/strategy/execution/portfolio 各自 cmd/service.go 独立维护配置快照、Deer 生命周期、初始化/清理及工作计划。直接调用 NewMarket/NewStrategy/NewExecution/NewPortfolio，返回各自业务类型；行情/执行只注册自身 RPC，策略/投影只装配自己的工作循环。coreentry 只保留健康与取消工具。
- 业务装配按服务分文件；公共 runtimeData 只借用 Deer 依赖并检查迁移，不选择或启动服务。通用 New/Runtime 仅为历史组合入口保留适配，核心启动不再调用；原工作算法、事务、预算、fencing、超时及清理合同保持，未增加基础服务、迁移或资金。
- 验证：独立 schema 37 源码的 fmt/60 Proto 与 OpenAPI 生成检查/lint/govet（0 issues）/全包单元/四核心构建通过；六个相关包 race 通过；make core-test 四项实际 infra/race 13.796 秒全过，成交投影/预算闭环 2.31 秒、幂等资源 0.43 秒、broker 冲突 0.02 秒、策略接管/报价/终态停止 9.45 秒。新增四服务身份/管理拒绝测试先因缺本地 runService 编译失败，拆分后通过；导入分组问题修正后最终静态检查通过。
- 运行：四个最终镜像重新构建并重建应用容器，Compose up --wait 全部 healthy；四镜像非 root/只读/禁网的 readiness、JSON 身份及 SIGTERM 通过。实际四 Nacos 健康身份、两路未过期 HEALTHY 回放与 99.99/100.01 精确价格、execution 空订单查询经 mTLS 通过。原 49 个未提交平仓路径保留，运行构建来源为忽略的 .cache/service-entry/source；旧管理原生/浏览器、完整历史集成、三十分钟及生产本步未运行。详见 [P13.2 验证](validation/P13-02.md)。
- 工作分支 codex/two-apps，基线 1dbfd50 已同步；本步验证后单独提交、立即推送并核验远程 SHA，见交付报告与 Git 历史。

## 2026-11-02：P13.3 核心服务无参数、Nacos 业务配置

- 根据用户后续要求，四个核心命令移除 flag/-config/-check-ready 和本地 JSON 路径；保持各自独立 runService，env 提供 Nacos 连接/认证/范围/TLS/期限，启动读取对应 Data ID 的严格 Proto JSON。拒绝错误身份、未知内容及远程连接覆盖，不回退本地文件。启动快照变更需显式重启，账本/持久版本仍由 PostgreSQL 管理。
- 新独立无参数 readycheck；维护工具默认读取 Nacos，migrate 保留 -apply，历史维护文件选项兼容。Compose 只挂自身证书，通过 env 启动；显式首次准备只发布缺失项，不覆盖控制台业务配置。tools profile 避免维护与运行服务争用固定 IP，显式 schema 37 迁移和 coreinit 重放通过，不注资、不启用交易。
- 验证：相关包 race 通过；独立 schema 37 源码的 fmt/60 Proto 与 OpenAPI 生成检查/lint/govet（0 issues）/全包单元/核心构建通过；core-test 四项实际 infra/Nacos/race 15.033 秒通过，包含真实独立命令成交投影/预算、幂等初始化、broker 冲突与策略接管/终态停止。dev-test 真实 Nacos 启动及重复关联/退出通过；最终 30 项工具测试、四个最终镜像非 root/只读/无参数/无 JSON 挂载的 readiness/日志/SIGTERM 通过。
- 运行：四个最终镜像 Compose up --wait 全部 healthy，实际核实 Nacos 四配置/四健康身份、双源未过期 HEALTHY 回放及精确 99.99/100.01、execution 空订单；没有新增或重启共享基础容器。原 49 个未提交平仓路径保持，构建来源 .cache/nacos-config/source。详见 [配置说明](NACOS_CONFIG.md) 和 [P13.3 验证](validation/P13-03.md)。旧全量集成/管理浏览器/固定三十分钟及生产本步未运行。
- 工作分支 codex/two-apps，基线 84fd86c 已同步；本步验证后独立提交、立即推送并核验 SHA，结果见交付报告和 Git 历史。

## 2026-11-02：P13.4 移除运行时 Go 版本判断

- 按用户后续要求，移除所有应用命令中的 runtime.Version() 精确比较，包括四核心入口、readycheck、migrate、doctor 及保留的管理命令；删除无用 runtime 导入和 doctor 硬编码版本输出。版本继续在 go.mod 声明（当前已为 1.27.3），不新增运行时版本门禁；参数/配置/取消逻辑保持。工程约定同步记录此项对原要求的调整。
- 验证：独立 schema 37 源码的 fmt-check、lint/govet（0 issues）与十个受影响命令包测试/编译通过；源码扫描确认 cmd 不再包含 runtime.Version()。本次仅移除启动检查，没有改并发/交易算法，未新写重复测试；全量单元/race、生成及核心交易回归本步未重跑。
- 四个新镜像构建并 Compose up --wait 首次全部 healthy；随后宿主磁盘耗尽，Docker 日志明确记录 no space left on device 并自动关闭引擎。清理本项目 Go 构建缓存后恢复约 4 GB，正常重启卡住，经过工具审批仅终止已核实的 Desktop 进程并恢复引擎，显式启动原 infra-nats；共享基础容器 ID/卷保持，启动时间因恢复而改变。配置/NATS 未就绪时核心进程安全失败，依赖恢复后重验通过，未屏蔽错误。
- 最终 Compose up --wait 四服务全部 healthy；核实最终镜像 ID、无参数/JSON 挂载、四份 Nacos 配置和四健康身份，实际 mTLS 双源行情均未过期 HEALTHY、价格 99.99/100.01，execution 订单为空。证据 .cache/runtime-version/live-probe.json。原 49 个未提交平仓路径保留，运行源码仍为 .cache/nacos-config/source；既有 NATS 的旧 stream 健康告警不在本步修复范围，不宣称全部共享 infra 健康。不涉及生产、资金或迁移。
- 工作分支 codex/two-apps，基线 035e402 已同步；本步验证后独立提交、立即推送并核验远程 SHA，见交付报告和 Git 历史。

## 2026-11-03：P13.5 核心服务按项目目录组织

- 按用户要求，将 cmd 下需要的四个服务分别迁移为 market/cmd、strategy/cmd、execution/cmd、portfolio/cmd；各自 main.go、service.go 和 service_test.go 共 12 个文件原样移动，内容逐字节保持。继续使用单一根 go.mod，其他 cmd 命令原样保留；不改服务身份、Nacos/env 配置、运行算法或二进制名。
- 同步 Makefile 的独立构建路径、Dockerfile 源码复制/构建/许可证依赖路径及 .dockerignore 白名单，更新当前架构和启动文档链接；历史验证记录保留原路径。核心构建不依赖管理入口。
- 验证：独立 schema 37 源码的 fmt-check、60 Proto/OpenAPI 生成检查、lint/govet（0 issues）、make build 及四个新路径命令包测试通过；make core-test 四项实际 infra/race 共 15.428 秒通过，成交投影/预算 3.01 秒、资源幂等 0.40 秒、broker 冲突 0.02 秒、策略接管/报价/停止 10.40 秒。本步未新写重复测试；全量单元/race、旧管理/浏览器和固定三十分钟未重跑。
- 四个新路径镜像构建并 Compose up --wait 全部 healthy；核实最终镜像 ID、无参数/JSON 挂载、四份 Nacos 配置与健康身份，实际 mTLS 双源行情未过期 HEALTHY、价格 99.99/100.01，execution 订单为空。仅重建本项目四个应用容器，不部署或启停共享基础设施；证据 .cache/service-layout/live-probe.json。原 49 个未提交平仓路径保留，验证/运行源码仍为 .cache/nacos-config/source，不混入 schema 38；未涉及资金、生产或迁移。
- 工作分支 codex/two-apps，基线 89f0cf0 已同步；本步验证后独立提交、立即推送并核验远程 SHA，见交付报告和 Git 历史。

## 2026-11-03：P13.6 Wire 装配与 Deer With 注入

- 按用户要求完成四服务独立 Wire injector 和 internal/app ProviderSet，固定 Wire v0.7.0，生成物入库并接入 generate/verify；main 仍为无参数 env/Nacos 启动，各进程只有一个 Deer。原有空 market/cmd/service.go 已随旧装配移除，其他 cmd 命令保持。
- Deer 保存 Kratos App、日志及公共客户端；NewDeer/NewDeerApi 兼容原调用，增加 WithDB、WithRedis、WithLogger、WithJetStream、WithDiscovery、WithGRPCClients。注入依赖借用且优先于配置创建，不另开池；构造/启动/运行健康检查保持。未注入资源仍由 Deer 创建并逆序清理；调用方在 Deer 清理后关闭借入资源，Wire 透传选项且不吞清理错误。
- 验证：隔离 schema 37 源码的格式、60 Proto/OpenAPI/Wire 一致性、lint/govet（0 issues）、全包单元与核心构建通过；deer 全部子包、processlog、business、四服务相关 race 通过。真实 infra 全部客户端合同含新 DB/Redis 注入所有权/readiness 测试通过，25.807 秒；四项真实核心进程 integration/race 原时限重跑全部通过，13.533 秒。30 项 Python 3.13.2 工具测试、文档链接和差异检查通过。详见 [P13.6 验证](validation/P13-06.md)。
- 诊断：构造函数复杂度超限通过拆分初始化修复；日志测试按本次精确增量校验；旧 Python 工具错误改用现有 3.13.2 后通过。首次组合回归出现 Nacos 配置 fetch 失败及观察等待超时，具体远端原因未确定，未放宽断言/时限；独立重跑原四项通过。原有平仓未提交修改保留，完整单元/实际进程使用 .cache/deer-options/source 排除这些修改；未运行镜像/Compose 重建、完整历史迁移/浏览器、固定三十分钟或生产，未操作共享基础容器生命周期、资金或生产迁移。
- 工作分支 codex/two-apps，基线 3b94abe 已同步；本步检查通过后独立提交、立即推送并核验远程 SHA，见交付报告与 Git 历史。

## 2026-11-03：P13.7 第三方目录归入 deer

- 按用户要求将根 third_party 的 13 个文件整体移至 deer/third_party，涵盖 Hertz/tagger Proto 注解、OpenAPI schema 与 lumberjack 维护源码/测试/许可；移动前后逐文件 SHA-256 一致，继续共用根 go.mod。
- 同步日志包导入、Proto 生成/合同检查/Hertz include、OpenAPI 离线测试夹具、文档链接与 Docker 许可复制路径；Docker 直接通过 COPY deer 纳入该目录，移除根 third_party 的独立 COPY/白名单。合同扫描继续递归 deer，覆盖移动后的 Go 文件；历史验收记录中的当时目录文字保留。
- 验证：格式、60 Proto/OpenAPI/Wire 生成一致性、两次 Hertz 生成确定性及编译通过；lint/govet 为 0 issues，policy-test 正反样例通过。日志/lumberjack/OpenAPI/policy 相关 race 与核心构建通过；隔离 schema 37 源码的 deer 全部包、OpenAPI、合同命令及四核心入口单元通过。未新增镜像实现行为的重复测试，原有平仓修改保留。
- Docker 同源码 build 阶段完成四核心及三维护命令编译，runtime-market 导出通过，核验运行文件与 lumberjack LICENSE 原始哈希；没有替换当前运行容器。Buildx 初次写 ~/.docker 活动记录被沙箱拒绝，按工具审批重试后构建通过。Docker 验证使用 .cache/thirdparty-move/source 排除原有平仓修改，产物在 .cache/thirdparty-move/runtime。
- 修改文档链接及 git diff --check 通过。完整全包单元/race、基础设施/交易集成、Compose 重建、浏览器及生产本步未运行；本步仅改路径，没有业务算法、协议或数据迁移。
- 工作分支 codex/two-apps，基线 784cc3c；本步检查通过后独立提交、立即推送并核验远程 SHA，见交付报告与 Git 历史。

## 2026-11-03：通过 Docker Compose 重建并启动最新核心服务

- 按用户明确要求，从已提交的 9f31d64 导出 schema 37 源码至 .cache/compose-restart/source，包含 Wire/Deer With 注入及 deer/third_party 迁移；原有未提交平仓修改保持，未混入镜像。prepare --source 更新持久构建来源，复用已有配置、证书及逻辑资源。
- Docker Compose build market strategy execution portfolio 通过；up -d --force-recreate --no-build --wait --wait-timeout 90 重建四个应用容器并全部 healthy。核对新旧容器及镜像 ID 均不同、新容器对应最终 infra-local 标签、启动重启计数均为 0，日志为正确服务身份的 JSON 且无 ERROR。容器仍为无参数、单证书目录只读挂载。
- 实际 mTLS 验证四份 Nacos 配置及四个独立健康注册、Binance/OKX 两路未过期 HEALTHY 回放及精确 99.99/100.01、execution 空订单查询全部通过。基础 PostgreSQL/Redis/Nacos/NATS 的容器 ID/启动时间以及四份 Nacos 配置哈希与启动前一致，未操作共享基础设施生命周期。
- 构建版本与前后状态/RPC 证据保存于忽略的 .cache/compose-restart；未执行迁移、重新注资、启用账户、生产部署或外部实盘。此步只进行实际构建/启动/运行探针及文档检查，未重复上一轮已通过的 Go 单元/race 和交易集成。
- 构建源码提交 9f31d64 已在 origin/codex/two-apps；本步运行记录经文档链接和 git diff --check 验证后单独提交、立即推送并核验远程 SHA，见交付报告与 Git 历史。

## 2026-11-03：P13.8 普通 Docker Compose 命令默认配置入口

- 用户贴出的普通 docker compose down/up --build 因未传 --env-file 而缺少 MUSKDEER_RUNTIME_DIR/UID；复现清空 shell 引导变量后 config --services 返回 1。根因是准备脚本仅写 .cache/compose/compose.env，未建立 Compose 默认读取的根 .env。
- prepare 现在为不存在的根 .env 创建指向已准备 compose.env 的相对链接；重复准备即使原子替换设置文件，默认入口也立即读取新内容。已有用户普通 .env 或其他链接保持原样并提示显式 --env-file；根入口加入 gitignore，未提交任何机器配置或凭据。README 和核心运行说明同步普通命令用法。
- 新增三项回归先失败、实现后通过，覆盖默认读取/重复更新、保留用户文件和外部悬空链接；Python 3.13.2 全部 33 项工具测试通过。实际 prepare 后，在完全清空 shell 引导变量时，普通 config 与显式 --env-file 的完整解析结果一致且无警告，构建源仍为已验证的 schema 37 快照。
- 实际执行不带 --env-file 的 docker compose down 和 docker compose up --build -d --wait --wait-timeout 90 成功，四个新应用容器全部 healthy、启动重启数为 0；随后普通 ps、四份 Nacos 健康身份、mTLS 双源未过期 HEALTHY 回放及 99.99/100.01 精确价格、execution 空订单查询通过。共享 PostgreSQL/Redis/Nacos/NATS 容器 ID/启动时间及四份 Nacos 配置哈希与操作前一致。
- 运行证据在忽略的 .cache/compose-env/{before,after,live-probe}.json；原有平仓修改保留，镜像运行源码仍为 9f31d64，未混入 schema 38。本步只改 Python 准备及文档，Go 单元/race、生成和完整交易集成本步未重跑；没有迁移、注资或生产操作。文档链接及 git diff --check 通过。
- 工作分支 codex/two-apps，基线 4a32ca1；本步修复/测试/文档独立提交后立即推送并核验远程 SHA，见交付报告与 Git 历史。

## 2026-11-03：P14.1 核心 gRPC 协议迁出根 api

- 用户确认移除根 api/web 和管理 HTTP，保留核心 gRPC。先将服务 Proto 与生成代码迁至 deer/proto/rpc，同步 Go 导入、Proto import/go_package、生成器/检查、Hertz 验证及 Docker 路径；根 api 移除，下一步继续删除 web 和管理 HTTP 应用。
- 生成代码全部由工具重新生成；迁移前后 47 个 descriptor 逐项对比，只规范化文件/import/Go 包路径后完全相同，Proto package、字段编号、消息类型、RPC 名称及选项保持。
- 原有 49 个未提交文件和完整 binary diff 备份在 .cache/apiweb-removal。确认其中 48 个文件除 RPC 导入和 gofmt 外内容保持，执行接口生成代码从原有 Proto 重新生成；独立暂存已提交基线的迁移版本，不把平仓业务修改混入本步提交，原有修改在新路径继续保留。
- 验证：隔离 schema 37 源码生成一致性（60 Proto/OpenAPI/Wire）、Hertz 确定性与编译、格式、lint/govet（0 issues）、全包单元及核心构建通过；工作区生成检查通过。真实四核心进程 integration/race 最终四项全过，14.431 秒，覆盖成交/投影/预算、资源幂等、broker 冲突和策略接管/报价/停止。
- 回归期间两次 execution 启动 fetch 失败；独立回读实验明确复现 Nacos 发布已确认但立即 GET 为 404。测试进程夹具现在在原 command_timeout 内等待该新配置可读，仅对 404 等待，其他错误或内容不符立即拒绝；保留原交易/启动超时和断言，失败清理已发布的测试配置。三个针对性单元/race 检查覆盖可见性、异常/内容拒绝及取消；真实四项随后全过。生产启动逻辑不变。
- 不涉及迁移、注资或生产；当前运行容器暂未重建，待管理 HTTP/Web 清理完成统一验证。工作分支 codex/two-apps，基线 a1b06f6；本独立步骤检查通过后提交、立即推送并核验远程 SHA，见交付报告与 Git 历史。

## 2026-11-03：P14.2 删除管理 HTTP/Web 并更新核心容器

- 删除根 web、管理 HTTP 包、admin/adminuser/openapi 命令、API 进程夹具、前端构建资产与 Node/浏览器工具、管理 HTTP/OpenAPI 测试和配置；清理默认及历史管理 Makefile 入口、Docker 白名单、离线 schema 与 jsonschema/v6 依赖。根 api 已由 1da8847 迁至 deer/proto/rpc；四核心 gRPC、后端领域/仓储和历史迁移保持。
- 退休应用在测试进程工厂中明确拒绝，doctor 拒绝 HTTP 配置；共享测试夹具移入后端成交测试，原资金、精度和作用域断言保留。README/当前架构和范围文档按四核心服务重写，旧管理文档标注历史，删除源码的链接指向固定历史版本。
- 隔离 schema 37 源码完整 make verify、build-fixtures 通过：60 Proto/Wire、Hertz 生成/编译、lint/govet 0 issues、规范正反样例、29 项工具测试、全部剩余 Go 单元/race 和核心构建。完整基础设施 integration/race/goleak 26.941 秒通过；五项后端/实际核心进程 integration/race 15.650 秒通过，含接管/报价/终态停止和迁移后共享夹具的成交来源/精度检查。
- 工作区生成检查、29 项工具及相关包 race 通过；原有未提交平仓代码的 eventwire TestPublicationRejectsChangedEnvelopeOrDecimalShape 失败保持，完整 verify 的通过范围为本次隔离提交源码，未冒充工作区全部业务已验收。49 个原有修改及已删除目录的本地依赖均有忽略备份，原业务差异未混入提交。
- Docker Compose 已使用 .cache/apiweb-removal/rpc-source 重建四镜像及容器，全部 healthy，实际 mTLS 双源未过期 HEALTHY 回放/精确价格和 execution 查询通过。最初探针重启数为 0，后续检查发现 market 重启 1 次、execution 2 次，日志为 worker deadline_exceeded，PostgreSQL 同时记录行情租约行锁等待取消；无 OOM，当前无阻塞会话，再次 mTLS 探针通过。该运行稳定性问题尚未修复，未宣称零重启或长期稳定。四份 Nacos 配置哈希及共享 infra 的容器 ID/启动时间保持，没有迁移、注资、启用账户或生产操作。详见 [P14 验证](validation/P14-02.md)，运行/集成证据在 .cache/apiweb-removal。
- 未运行剩余全部历史迁移集成、固定三十分钟或真实资金/生产。工作分支 codex/two-apps，P14.1 提交 1da8847 已同步；本步验证后独立提交、立即推送并核验远端 SHA，见交付报告及 Git 历史。

## 2026-11-03：P15.1 顶层 internal 按项目与公共职责迁移

- 用户确认服务专属实现下沉，公共领域、租约和跨服务事务仓储继续共用，保持单 go.mod 与既有事务合同。38 个原有包迁至所属项目或 deer，顶层 internal 移除：market 拥有订单簿/行情/质量/适配，strategy 拥有报价/策略适配，execution 拥有订单适配/outbox/模拟器；公共 domain/repository/配置合同在 deer/trading，迁移在 deer/migrate，进程与测试支撑在 deer。集中 business/usecase 本步暂置公共区，下一步拆为项目业务装配与用例，不把临时位置冒充最终边界。
- 同步 Go 导入、Wire 生成、Docker COPY/白名单、迁移 SQL 的精确 lint 豁免、工具/回归路径及文档引用。日志 caller 过滤同步真实包路径；历史组合测试入口包改为 testentry，与路径一致。镜像继续排除测试、testdata、证书、私钥和环境文件。
- 512 个搬迁的已提交 Go 文件逐文件核对，除声明的导入/包名/caller 路径及 gofmt 外内容一致，没有业务算法、SQL、协议或超时调整。49 个原有修改在忽略的 .cache/internal-split/uncommitted 备份；全部逐文件核验保留，只随目录调整导入/格式，独立暂存干净基线的迁移版本，不提交原有平仓业务。
- 隔离 schema 37 源码全包单元通过；公共交易包、四项目、日志/testentry 的 race 通过，integration 标签下 migrate/infrastructureprobe/persistenceprobe 编译通过。60 Proto/Wire 生成一致性、Hertz 确定性/编译、格式、lint/govet 0 issues、规范正反样例、29 项工具测试及核心/历史夹具构建通过；606 个本地文档链接有效，git diff --check 通过。
- 本步未运行实际基础设施/交易集成、镜像构建或 Compose 重建，留待业务装配拆分后验证；当前容器仍为上一轮源码，既有 worker 超时重启未作修复。原有工作区 eventwire 测试失败仍未纳入本次隔离验收，不声明所有未提交功能通过。
- 基线 0ac9e96，工作分支 codex/two-apps；本独立步骤验证后提交、立即推送并核验远端 SHA，见交付报告及 Git 历史。

## 2026-11-03：P15.2 各项目拥有业务装配与用例

- 四个 typed 业务构造、RPC/worker 工作计划归各项目 business；执行、策略与持仓用例分别在所属项目 usecase。各 internal/app 直接引用本项目 business。公共配置/资源借用移至 runtimeconfig，轮询移至 runloop；历史组合仅留 deer/testbusiness 并委托项目实现，默认核心不依赖历史装配或其他项目 business。
- 保持共用 domain/repository 的事务边界；单份订单终态判断归 OrderState.Terminal，回放值配置归 domain.MarketReplayConfig，保留原 marketworker.ReplayConfig 类型别名，避免公共校验拉入行情工作器。未改业务算法、SQL、Proto/RPC、超时或资金/幂等/fencing 合同。新增项目目录说明，更新 Wire/架构/运行文档。
- 隔离 schema 37 提交源码完整 make verify、build-fixtures 通过：60 Proto/Wire、Hertz 确定性/编译、格式、lint/govet 0 issues、规范正反样例、29 项工具、全包单元/race 和构建；全部迁移/探针 integration 标签编译通过。四核心生产依赖图逐项验证，只用自身 business，无历史夹具及公共配置对行情工作器的反向依赖。
- 真实既有 infra 的完整 infrastructureprobe integration/race/goleak 32.530 秒通过；五项实际四核心命令 integration/race 23.195 秒通过；三项历史组合/模块实际进程回归 18.047 秒通过，覆盖部分成交、费用、资金冻结链路及停止；outbox/inbox/ACK 重投顺序 6.347 秒通过。未降低断言或扩大时限，均为隔离模拟场景，非外部实盘或生产。
- Docker build 阶段完成四核心及三维护命令编译/许可证收集，验证镜像 muskdeer-build:layout-check；未重建 Compose 或修改共享基础容器生命周期。现有 worker 超时重启问题保留，未在目录调整中改恢复策略。
- 49 个原有文件在两步迁移后再次逐项核验，除导入/格式外完整保持；暂存使用隔离已验证源码，原有业务不混入提交。根工作区四核心构建、生成/Wire/格式通过；原有 eventwire TestPublicationRejectsChangedEnvelopeOrDecimalShape 仍失败，未将未提交功能宣称通过。证据与限制见 [P15 验证](validation/P15-02.md)。
- P15.1 的 9363fe3 已同步；本步验证后独立提交，立即推送 codex/two-apps 并核验远端 SHA，见交付报告和 Git 历史。剩余全部历史迁移集成、固定三十分钟及生产本步未运行。

## 2026-11-03：P16 集成边界纠正，撤回原生改造

- 用户明确只使用 Himalaya 现有 Go SDK/接口，不修改撮合。此前 27abd74、5551e2e、3fa13a5、01d2d73、67a804c 的自定义原生合同路线撤销，以新提交保留更正历史，不改写远端。
- 原生 matching-engine/order-service 的 20 个受影响路径均先核验本轮增量的完整哈希，再恢复原内容或仅移除我的新增文件；恢复后逐字节验证。原有未提交业务保持。原生运行容器从未升级，未操作资金、旧挂单或仓位，没有应用迁移到运行库。
- 移除 Muskdeer 中未部署的自定义网关 Proto/客户端、Rust 支持和迁移工具；SDK 模块、README、Place/Cancel/ForFund 查询及认证实现已只读核验。后续只在 Muskdeer 执行、行情、策略及持仓消费侧适配现有 SDK，不虚构目标 epoch 或幂等支持；BTC/ETH 做市仍未启动。
- 验证：撤回后的隔离提交源码与 9f34646 相比仅有四份边界文档差异；格式、60 Proto/Wire 生成一致性、contract-check、四核心及维护命令构建通过。原 Go SDK 使用项目固定 Go 1.27.3 执行 `go test -mod=readonly ./...` 通过，未修改 SDK。Muskdeer 原有 49 个文件再次逐字节核验保持；本步没有运行策略或宣称接管完成。

## 2026-11-03：P16.2.1 消费既有 Go SDK

- 将用户提供的 lukla-go 16 个依赖文件原样固定在 deer/third_party，以逐文件 SHA-256 清单验证；保留 SDK 模块声明，通过根 go.mod replace 引用。SDK 源码及生成文件均未修改，应用工具链仍为 Go 1.27.3。make sdk-check 已纳入 verify。
- execution/himalaya 使用 SDK 的既有 Place 和 ForFund 查询；下单采用精确 decimal、POST_ONLY/CROSS、显式账户及持久身份，禁用配置重试。没有最终 Muskdeer Admission 的客户端只读；发送后错误标记 UNKNOWN，保留身份且不补发。此步定义 Admission 接口，尚未接入持久化守卫或服务工作循环，不代表目标撮合支持 epoch 或 client_id 唯一去重。
- SDK 原测试及六项适配行为测试 race 通过；隔离提交源码的格式、lint/govet（0 issues）、contract-check、60 Proto/Wire 一致性、四核心及维护命令构建通过。首次静态检查发现三处错误信息大小写问题，修正后重跑通过。未运行全量单元/race、数据库集成或镜像构建；原有 49 个业务修改未混入验证源码。
- 真实只读 SDK 探针已成功连接原地址，BTC/ETH 账户与市场查询通过，分别仍有 40/39 张旧挂单，各有一条仓位，市场健康。之前的握手失败目前未再出现，未将原因归结为已证实的网关问题；没有重启共享基础设施、下单、撤单、注资或平仓。做市接管尚未完成。
- 基线 53b2135，工作分支 codex/two-apps；本独立步骤验证后提交并立即推送，核对远端 SHA 后继续持久化和生命周期接入。

## 2026-11-03：P16.2.2 SDK 持久意图及最终发送校验

- Proto 定义 SDK 扩展迁移记录、账户预算及意图，由现有 protoc/gotag 生成；仓储直接使用生成 Model。扩展 0001 具有独立校验和账本，复用核心执行租约，保持核心迁移历史不变，也不占用用户未提交的 0038；仅提供显式 Apply 与只读 Check，没有启动自动迁移。
- Prepare 在同一事务内锁定账户租约、检查新鲜且启用的共享预算、原子预留总名义金额并保存请求摘要和精确数值。发送时先提交 DISPATCHED，再在最终 SDK 回调前锁定并复核 epoch、风险、不可变请求；请求受租约与风险有效期共同约束。崩溃、回复丢失和未记录回执均保留预留并阻止新增风险，不补发。成功回执持久化订单号，同样不释放预留。
- 复用既有 PostgreSQL、只创建并清理隔离测试数据库；七项集成/race 18.459 秒通过，覆盖并发抢同一预算、旧 epoch、已提交发送标记、SDK 测试网关回复丢失不重发、请求变化、风险过期/降额、回执及扩展历史。适配包 race、61 Proto/Wire 一致性、格式、lint/govet（0 issues）、合同门禁、四核心与维护命令构建通过。最初测试身份未使用 incarnation 格式、订单标识的 numeric 类型不符合结构门禁，分别修正测试身份和 varchar 标识后原检查通过；没有降低断言。
- 本步尚未将仓储接入四服务工作循环，也未提供撤单/终态预留释放及完整对账。目标接口仍无原生 epoch/去重保证：本步仅在 Muskdeer SDK 出口校验，不能撤回目标已接收的请求；未知结果无确定证据时保持阻断，不能宣称完整自动接管。实际开发账户未执行 DDL、下单、撤单或资金操作。
- 用户已选定沿用原脚本每侧 20 档、盘口数量 10%、最高 10 倍杠杆；真实限额和运行配置仍待工作循环接入，测试预算不是开发账户的已配置额度。49 个原有文件再次逐字节核验保持。完整全包单元/race、全部历史迁移、镜像及真实做市本步未运行。
- SDK 基础提交 075e0bb 已推送并核验远端；本步通过检查后独立提交并推送 codex/two-apps。

## 2026-11-03：P16.2.3 禁止 SDK 业务请求透明重放

- 核查固定 gRPC 版本实现，WithDisableRetry 不关闭透明重试；用 HTTP/2 REFUSED_STREAM 与 PING ACK 同步屏障复现一次 SDK Place 出现两次请求，回归先失败。随后仅通过 SDK 公开 DialOptions 扩展点，在 Muskdeer 中先提交 stream 再发送业务体，沿用现有 rpcclient 的单次发送合同；限制消息大小、禁止等待重连后重放，不修改 SDK 实现或目标协议。
- SDK 原有 unary/stream 认证拦截器继续提供 token 与协议版本，适配中消除由 unary 转 stream 的重复头；原协议认证断言保持。七项适配 race 通过，拒绝流从两次变为一次；七项隔离 PostgreSQL 集成/race 7.532 秒通过。隔离提交源码的格式、lint/govet（0 issues）、61 Proto/Wire 一致性、合同门禁及核心/维护构建通过；SDK 16 文件原始哈希保持，实际 BTC/ETH 只读查询再次通过。
- 原有业务修改保持；不启停共享基础设施、不执行开发账户迁移或订单/资金写入。尚未部署该适配，四服务行情/策略/执行/投影生命周期与真实做市接管仍未完成，完整全包测试及镜像本步未运行。
- 基线 05f1b0a 已推送并核验远端；本步通过验证后独立提交并推送 codex/two-apps。

## 2026-11-03：P16.3.1 实时二十档与原生单位边界

- 增加 Muskdeer 内部的行情深度、账户/仓位/订单观察和目标订单 typed Proto；原始 SDK 契约保持。Nacos Proto 增加显式 SDK 运行配置及账户/数量/额度参数，此步尚未开放业务分支。所有服务间小数字段为 string。
- market/usdm 使用固定 Binance USDm public TLS 地址接收完整二十档，校验来源、时间、顺序、十进制字符串、两侧价格及序列，缺侧/过期/交叉数据拒绝；会话取消关闭并加入 socket 回调。SDK 数据在边界按 effective precision 转 decimal，净仓位有明确方向，quote_min_size 按原生 1e8 单位转换；非正常/非 ONE_WAY/跨账户或其他产品数据拒绝。
- SDK 先读全账户订单，再读仓位，保守保留读取期间的潜在成交义务；多次查询仍不宣称原子快照。实际只读探针同时验证 BTC/ETH 原生账户、价格/数量单位和每侧二十档实时流，未调用交易写接口；敏感观察保存在忽略目录。
- 适配及深度 race/goleak、精度/来源/过期等行为检查通过；隔离提交源码格式、lint/govet 0 issues、64 Proto/Wire 一致性、合同门禁和核心/维护构建通过。首次复杂度检查促使按市场/仓位职责拆分函数，原检查重跑通过。SDK 及原有 49 文件保持。未运行全量交易测试、镜像或做市，本步通过后独立提交推送 codex/two-apps，继续撤单/恢复及四服务工作循环。

## 2026-11-03：P16.3.2 实时授权、保证金与撤单恢复

- SDK 扩展追加不可变 0002，增加意图期限、策略/来源授权、资金观察、撤单记录及 typed 可重建投影结构；扩展迁移按版本校验并升级，0001 与核心历史不变。模型继续 Proto/gotag 生成。
- Live 意图在准备和最终 SDK 发送处检查当前策略、行情发布 epoch、期限及可用保证金/费用；持有数据库执行/策略/来源授权行直到有界发送返回。价格、数量、费用及风险均使用 decimal；新增风险与未知撤单互斥。SDK Cancel 先持久化请求、只发一次，允许来源失效时受控撤单；原生唯一订单号可通过已有历史游标查询，不假定 client_id 去重。
- 终态观察只释放未成交部分，成交名义金额仍保守保留；重复回报不重复释放，缺失结果不视为终态，未发送且过期的 PREPARED 可原子回收。已有义务观察不自动调低，既有仓位不转入模拟现货账本，额度显式维护接口不由启动启用。
- 十项隔离 PostgreSQL 集成/race 12.039 秒通过，覆盖保证金、未发送过期、三种旧授权/期限、撤单回执与终态、原有共享预算/未知回复等；SDK/仓储 race、精确 ID 游标与 Cancel SDK 协议验证通过。修复 numeric(76,36) 数据库结果不能按 18 位输入规则解析的问题，保留原断言；格式、lint/govet 0 issues、64 Proto/Wire 一致性、合同门禁及核心/维护构建通过。
- 实际 SDK/二十档只读探针再次通过；没有执行开发账户 DDL、撤单、下单或资金操作，四服务运行接入仍在下一步。原 SDK、原 49 个修改保持，完整全包测试及镜像未运行。本步验证后独立提交并推送 codex/two-apps。

## 2026-11-03：P16.3.3 四个独立进程 SDK 工作循环

- 四项目 typed 构造分别接入本项目原生业务，不引入共用 role 工厂，Wire/单 Deer/资源借用合同保持。market 拥有真实二十档和发布授权，strategy 通过 gRPC 获取来源/投影并提交目标，execution 独占 SDK 生命周期与最终执行、行情保护/停止撤单，portfolio 保存权威 typed 观察。仅 execution 读取 SDK token。
- Nacos 显式配置固定额度、比例、档位、杠杆、预期网格；固定开发目标与来源校验，无静默切源或改产品。新增无本地配置的显式 himalayainit -enable，服务仅检查扩展历史。Docker 构建先复制本地 SDK 依赖再下载模块，并包含维护命令；Compose 仅 execution 注入 SDK 环境引用。
- 真实 mTLS 传输、既有 PostgreSQL 隔离库及 SDK 测试对端组合验证策略→执行下单→持仓观察，来源 epoch 失效后撤单通过；全部十一项 SDK/实时合同 integration/race 串行重跑 32.873 秒通过。首次组合检查有一项旧核心迁移夹具失败，增加底层诊断后保留原时限串行重跑全过，未确认底层原因，不将其掩盖为确定已修复。
- 四项目相关单元、行情/策略/SDK/持仓及配置 race 通过；格式、lint/govet 0 issues、64 Proto/Wire、合同门禁及四核心/维护构建通过。补充 [开发启动与限制](HIMALAYA.md)，未声称原生 epoch、client_id 去重、原子快照、完整成交账本或生产验收。
- SDK 源码与原 49 文件保持，运行配置/有限额度已在忽略目录准备，但本步尚未发布配置、迁移开发库或调用真实交易写接口。代码验证后独立提交推送 codex/two-apps，再进行实际 Compose 部署与 BTC/ETH 接管；完整全包测试与镜像验收不属于本步通过声明。

## 2026-11-03：P16.3.4 有界历史恢复进度

- 已从已推送的 2a87ce2 隔离源码构建镜像，显式发布四服务 Nacos 开发配置、初始化独立 SDK 扩展和固定预算，并通过 Compose 启动四核心进程；容器均 healthy、自动重启为零。原生 SDK 只读观察确认旧报价从 BTC/ETH 40/39 降为 0/0，二十档来源和 typed 投影读通，但尚未形成持续新报价。
- 实际接管中观察到恢复调用超时；代码核查及慢查询回归复现了另一项恢复缺陷：逐笔历史查询总耗时超过单次期限时，原流程等待全部结果才持久化，每轮重复相同前缀。改为每笔权威事实独立持久化，下轮只继续剩余记录；不因缺席确认终态，不扩大超时，不释放未知风险，不更改撮合或 SDK。
- 新增 40 笔慢历史与真实数据库/mTLS/策略/持仓工作循环回归，修复前稳定失败，修复后恢复报价及来源失效撤单通过。首次 race 验证暴露测试在 SDK 返回与确认持久化之间提前观察，改为原时限内等待两项条件，保持原断言；最终全部十二项 Himalaya integration/race 串行通过（22.718 秒）。相关单元、vet、lint 0 issues、合同及 64 Proto/Wire 检查通过。原 49 文件逐字节保持，SDK 16 文件校验通过。
- 本步完成恢复修复验证并独立提交推送，随后仅重建、更新执行服务继续实际报价验收；不将容器健康或模拟对端通过描述为真实做市已完成。

## 2026-11-03：P16.3.5 开发部署与外部对账阻塞

- 四个独立 Compose 核心进程已实际启动，market/strategy/portfolio 镜像来源为 2a87ce2，execution 已更新为修复提交 f429b85；四者均 healthy、自动重启次数零。Nacos 读回核验、显式 SDK 扩展迁移及本项目预算启用完成，凭据和实际配置仅保存在忽略目录。
- 实际 SDK/mTLS 探针确认 BTC/ETH Binance USDm 各侧二十档、投影新鲜、原生各一笔仓位可见；旧 mm 报价当前为 0/0，新 deer 报价也是 0/0，尚未通过真实持续做市验收。Muskdeer 未发送新单，未充值、划转或平仓。
- 环境期间发生独立外部更新：对比部署前快照，目标 order-service 和 matching-engine 的容器 ID 均已改变；用户确认另一会话正在更新或清理。四项共用 infra 容器 ID/启动时间保持。当前 SDK 历史游标行为可核验，但逐一查找 BTC 40 笔、ETH 39 笔已记录撤单 ID，全部缺失；本项目 79 笔撤单仍保持 DISPATCHED，不把没有活动挂单视为终态证明。历史清理服务也在运行，缺失原因需由环境维护方确认，不能仅凭代码猜定。
- 持续新报价因此被对账门禁阻止；保留四服务观察和原有审计，不清空状态或绕过风险控制。本步部署动作完成，真实报价验收标记 blocked，待外部更新完成且终态证据恢复后继续。不能将有界恢复回归修复描述为本次部署全部故障的唯一已确认原因。
- 修复 f429b85 已推送并验证 codex/two-apps 远程同 SHA；本记录仅更新部署事实，未重复运行全包测试。此前十二项相关 integration/race、单元、vet、lint、Proto/Wire 及 SDK 清单检查通过；原 49 文件字节保持。

- 用户随后确认外部“更新完成”。更新后再次逐笔 SDK 查询结果仍为 BTC 0/40、ETH 0/39 笔终态命中；四服务继续 healthy，新报价仍为零。已在忽略目录导出缺失原生订单 ID 清单供环境维护方核对；未通过改写撤单状态、重建预算或猜测成功放行。本项目重启不构成恢复历史证据的方法。

## 2026-11-03：P17.1 有限开发测试成交合同

- 用户明确选择既有 SDK WashTrade 的 market-data-only 语义。新增 synthetic_tape 配置（默认缺省关闭），限制显式开发标志、固定 run_id、100ms..1m 间隔、最多 1000 次及每品种数量/总名义金额；按选择来源的顶档交替方向并精确对齐原生网格，过期行情拒绝。
- 增加 typed 内部意图/结果与 Proto 生成独立发送槽模型；SDK 扩展追加不可变 0003，不修改旧扩展或核心历史。数据库唯一键为 run_id/symbol/sequence，JSON 存储为具体消息，不进入真实成交/持仓账本。此步仅合同、定价和模型；内部发送 RPC 的真实处理和工作循环在 P17.2 完成，尚不启用或调用实际 WashTrade。
- 定价回归先失败后通过；相关单元及 race 通过，既有 PostgreSQL 隔离库的 typed JSON/四字段/重复槽/核心历史集成 race 通过（3.754 秒）。首次夹具调用遗漏 bounded context，修正后原断言通过；lint 的导出常量注释问题已修正，最终 0 issues。66 Proto/Wire 一致性、合同及四核心/维护命令构建通过。SDK 和原 49 文件保持；完整全包测试未运行。

## 2026-11-03：P17.2 测试成交策略与 SDK 发送闭环

- strategy/native 新增独立 synthetic 工作循环，通过当前来源和 mTLS 向 execution 发送 typed 意图；execution 重新取价，检查原生市场/精度/数量/最小金额，仅调用既有 SDK WashTrade。SDK 仍由同一 execution 生命周期拥有，不在 provider 启动连接或新池；四核心/Wire/单 Deer 装配保持。该路径不传账户、不下单、不修改原生资金或仓位，不解除旧做市撤单对账。
- 每个 run/symbol/sequence 在 SDK 前提交 UNKNOWN 发送槽及政策摘要；总次数、含 UNKNOWN 的名义金额、数据库时间间隔及政策不可变跨重启保留。发送时再次锁定执行、策略、来源授权，SDK 请求透明重放关闭。独立连接 NOWAIT 验证发送期间三类行锁均保留，回复丢失/并发重试只发一次；缓存回执标记 replayed，日志不冒充新发布。
- 新增 schema-only 维护模式，仅迁移 SDK 扩展，不启用资金预算；与 enable 互斥，参数检查通过。文档给出默认关闭的 Nacos 样例及有限轮次语义，实际环境本轮未迁移、未发布配置或合成成交。用户仅要求新增策略，未将模拟对端结果描述为真实行情已发布。
- 原有十二项 Himalaya 加四项 synthetic 集成/race 串行全过（27.817 秒），覆盖 typed 审计、唯一槽、政策/额度/间隔、旧 epoch、并发未知回复、真实 mTLS 到 SDK、非 strategy 拒绝、方向交替、两次发送且零订单、重启不重置。另将测试 TLS 服务 Stop 设为等待 handler，相关三个工作循环集成/race 再过（8.526 秒），保证测试资源退出被加入。
- 迭代中修正测试对端协议头名称（沿用原 SDK x-orderbook-protocol），以及在数据库准备后才发布夹具行情，保持既有 1.5 秒新鲜窗口。一次组合回归的旧 DROP SCHEMA 清理超时，原时限串行重跑全过；未确认原超时底层原因，不降低断言或扩大期限。
- 66 Proto/Wire、合同、lint 0 issues、四核心及维护构建、相关单元/race、拒绝流不重放和只读客户端拒绝 synthetic 均通过；SDK 16 文件原始哈希及原有 49 文件保持，文档链接通过。完整全包测试及真实开发行情消费验收未运行。P17.1 d3af00a 已推送并核验远端；本步完成后独立提交推送 codex/two-apps。

## 2026-11-03：P17.3 BTC/ETH 测试成交实际启用

- 用户要求同时启动 BTC/ETH 做市与 WashTrade 测试成交。从已验证提交 f2f5afd 隔离源码构建四镜像，显式 schema-only 升级 SDK 扩展 0003；既有资金预算未启用或修改。Nacos 四服务配置逐一读回核验，strategy/execution 使用相同的有限政策，Compose 四服务均 healthy。实际凭据仅 execution 私有环境注入。
- 首轮 dev-20261103-01 为每品种 60 槽、1 秒间隔，BTC 数量 0.001、ETH 数量 0.01，固定名义金额上限 12000/6000。实际 SDK 审计为 BTC/ETH 各 60 笔 SYNTHETIC_PUBLISHED，零 synthetic UNKNOWN；该有限轮已完成，重启不重置，不代表真实账户成交或行情消费已验收。
- 做市仍受旧 BTC 40/ETH 39 笔缺失终态阻止。用户明确选择一次性按当前 SDK 账本建立开发基线，要求保留旧未知审计及风险额度、不重发旧单、不平仓；下一独立步骤实现、验证并显式应用，禁止直接把旧撤单改成 CONFIRMED 或清空账本。本记录只声明测试成交启动回执验收，做市未完成。

## 2026-11-04：P17.4 明确批准的一次性开发基线

- 用户明确批准按当前 SDK 账本建立一次性开发基线，保留旧未知审计及风险额度，不重发旧单、不平仓。新增独立 Proto 审计模型与不可变 SDK 扩展 0004；默认预览的 himalayabaseline 维护命令只使用只读 SDK，要求配置账户、两次稳定且无挂单的观察、当前执行租约、完整 legacy ID 匹配、没有非终态本项目意图及现有限额可容纳保留风险。无自动应用或启动迁移。
- 原撤单状态保持 DISPATCHED，仅明确批准的旧 ID 退出被删除历史查询；独立记录保存原始订单、当前观察及双摘要。每账户只可建立一次，精确重复无副作用。完整原数量按原价/当前 mark 较大者保守保留；后续原生义务同步持续加上历史保留量，不降低上界、原限额、预留、开关或杠杆。新 UNKNOWN 撤单/意图继续阻止新增风险，风险下调及摘要变化失败关闭；最终 SDK 出口再次检查。
- 有意义回归先失败再实现；六项基线集成/race（含五种拒绝条件）通过，覆盖新意图可在剩余额度内发送且未知不重发、原审计不变、一次性/幂等、风险持续保留、降低风险拒绝及提交声明后最终出口重检。其余十二项 Himalaya 与四项 synthetic 集成/race重跑通过，共二十二项。最终出口故障注入最初复用了 GORM 语句，修正 NewDB/Context 会话次序并先验证实际数值已改变后，SDK 调用数为零；未降低断言。
- 并发 synthetic 回执重放回归暴露旧发布授权过期影响纯回执读取：缓存槽不再取得来源/策略发布行锁，首次发送及每个新槽仍做完整授权并持锁到 SDK 返回；旧来源 epoch 下的新槽保持拒绝。两次旧迁移夹具遇到 PostgreSQL statement timeout，保留原时限按组重跑通过，未确认底层原因，不声称已修复共享环境。另修正新夹具缺少 fee 字段，保留原断言。
- 68 Proto/Wire、合同、lint 0 issues、核心/维护构建、相关单元/race及 SDK 清单通过；原 49 文件逐字节保持。维护命令参数检查及文档链接通过；完整全包测试未运行。首轮真实测试成交进一步在 market-data GetRecentTrades 中按 SDK 成交 ID 逐一匹配 BTC/ETH 各 60/60。
- 四核心目前按计划暂停，旧 execution 清理因旧终态缺失以非零退出，未视为撤单终态证明；共享 infra/撮合未启停。本步代码验证后提交推送，再显式迁移 0004、预览/应用已批准的私有清单并恢复四服务，实际做市启动仍待下一步验收。

## 2026-11-04：P17.5 开发基线实际应用及启动核验（做市仍 blocked）

- f132982 已独立提交、推送并核验 origin/codex/two-apps 同 SHA。四镜像仅使用该提交的隔离源码构建，未包含用户原有 49 个修改文件；Compose 显式执行 schema-only 0004，再用只读 SDK 连续两次确认无挂单及稳定账本，预览后应用已批准的 BTC 40/ETH 39 笔原始清单。新增两条一次性审计；旧 79 笔撤单仍为 DISPATCHED。应用前后预算差异仅 observed_notional/update_at，未改变原有限额、预留、开关及杠杆。
- 四服务 Nacos 配置逐一保护旧摘要、显式发布并读回核验，版本 himalaya-sdk-f132982；Compose 重建启动四核心进程，最终均 healthy、自动重启为零。共享 PostgreSQL、Redis、Nacos、NATS、order-service、matching-engine 六个容器 ID/启动时间均保持。未改撮合或 SDK，未充值、划转或自动平仓。
- BTC/ETH 首轮真实 SDK 下单均返回 RUNNING、placed=40（每侧二十档），随后观察到撤改单对账等待和风险拒绝；不能据首轮成功或容器健康声明持续做市已完成。最终探针来源各侧二十档及投影新鲜，旧 mm 挂单为零；采样时新 deer 挂单也为零。BTC 日志出现 RISK_LIMIT；未扩大现有限额或降低已保留风险。
- ETH 新增一笔 DISPATCHED 意图缺少原生订单号；现有 SDK 活动订单及最多十页、每页二百条的终态历史扫描均未找到其 client_id（游标到末尾）。该本次新意图不在用户批准的旧 79 笔基线内，继续保留审计/预留并阻止新增风险，不重发、不清空、不将缺席当作拒单/撤单终态。缺少该笔权威终态/明确拒绝证据，持续双币做市验收仍 blocked；未确认本次未决的具体底层原因。实际 client_id 和查询证据仅保存在忽略的私有目录。
- WashTrade 新有限轮 dev-20261104-02 已实际启动：每品种最多 1000 槽、间隔 1 秒，BTC 数量 0.001/合成名义上限 120000、ETH 数量 0.01/合成名义上限 60000；这些合成上限不改变资金预算。采样时各 217 笔 SYNTHETIC_PUBLISHED、零 synthetic UNKNOWN，仍在发布；market-data GetRecentTrades 各二百条窗口内分别精确匹配 182 个本轮 SDK trade_id。旧六十槽轮次及其回执保持，重启不重置任何轮次。测试成交仅生成行情，不代表账户真实成交。
- 本步为实际部署与只读验收记录；相关二十二项集成/race、生成、lint、构建结果见 P17.4，未重复全包测试。SDK 十六文件哈希与用户原四十九文件字节再次核验不变，文档链接及 git diff 检查通过。四服务留在开发观察状态，测试成交可独立继续，不能把本步标为 BTC/ETH 做市全部完成。

## 2026-11-04：P17.6 按明确启动指令隔离历史阻塞及修复未发送意图

- 用户进一步明确历史订单不再阻止启动 BTC/ETH 做市及开发测试成交。本次新增有限的历史意图批准合同、Proto 生成审计字段和不可变 SDK 扩展 0005；维护命令默认预览，应用时持有执行租约并读取两次无挂单且账户/仓位稳定的 SDK 观察。只标注完整清单内既有 DISPATCHED 意图的批准与观察，原状态、身份、全额预留、额度及原 legacy 基线保持；未来未知、未批准撤单和旧单重发仍被阻止。
- 复现并修复最终出口检查在 SDK 调用前失败仍永久留下 DISPATCHED 的问题。只有成功声明该次发送、且本次调用明确没有进入 SDK 的路径可结束本地未发送意图并释放该笔预留；已发送回复丢失及崩溃继续保留未知。使用有界、同步清理，在租约已变化时仍按账户锁顺序完成可证明未发送的本地清理，不再调用交易接口。
- 两项行为回归先失败：历史批准后新意图仍被阻止；零 SDK 调用的出口失败仍留 DISPATCHED/100 预留。修复后历史批准、七类拒绝/原子回滚、原状态与风险保留、预览无写入、新未知及旧单不重放、未发送清理和原有合同共 25 项顶层 PostgreSQL integration/race 通过（45.152 秒）。一次初始组合回归发现宿主/VM 时钟相差 1ms，观察新鲜度沿用 Normalize 的调用方时钟，租约仍用数据库时钟；保留原 2 秒窗口。
- 69 Proto/Wire 一致性、合同、格式、lint/govet 0 issues、四核心及维护构建、相关单元/race 通过。SDK 16 文件及用户原 49 个文件字节保持；全包测试未运行。代码通过后独立提交推送，再从已提交的隔离源码构建镜像、显式迁移并实际启动验收，不将本步描述为持续做市已完成。
- 运行准备期间已显式发布并重启有限开发测试成交新轮 dev-20261104-03，保持每币最多 1000 槽、1 秒间隔和原数量/合成金额上限；首次实际审计 BTC/ETH 各 61 笔 SYNTHETIC_PUBLISHED。历史原轮次不重置。只读报价核验显示 BTC 40 笔目标合计约 129872 USDT，超过既有剩余额度；后续启动收紧单笔额度，保留原总预算、20 档和 200ms 刷新。

## 2026-11-04：P17.7 双币做市及测试成交实际启动

- a0879d8 已推送并核验 origin/codex/two-apps 同 SHA；四镜像从该提交的隔离源码重建。仅暂停本项目四服务，显式迁移 SDK 扩展 0005、预览并应用 BTC/ETH 各一笔现有未知意图的本次清单；原两笔仍 DISPATCHED，批准及当前 SDK 观察附加保存，原 79 笔 legacy 审计与两条基线不变。总额度仍为 BTC 335000 / ETH 683000；单笔上限收紧到各 500，不注资、划转或平仓。
- Nacos 四配置发布及读回核验，版本 himalaya-a0879d8；四个新容器均 healthy、自动重启为零。03:28:07 / 03:28:10 UTC 分别实际返回 BTC/ETH RUNNING、placed=40；后续仍能完成新轮重报价。所选 Binance USDm 双边二十档与持仓投影新鲜，两个原仓位仍可见；没有新增永久未知意图。
- 实际观察同时发现撤完旧报价再等待终态、最后重建整组报价的路径会出现短暂无报价，不能据单次 40 笔完成声明连续双边报价验收。继续处理重报价连续性；新撤单的终态门禁及全额风险保留不绕过。
- 部署同时显式启动有限测试成交新轮 dev-20261104-04，仍每币最多 1000 槽、间隔 1 秒，BTC 0.001 / 上限 120000、ETH 0.01 / 上限 60000；首次审计两币各 91 笔 SYNTHETIC_PUBLISHED，零本轮 UNKNOWN。旧轮次不重置，合成成交不计入账户成交或仓位。
- 共享 PostgreSQL、Redis、Nacos、NATS、order-service、matching-engine 六容器 ID/启动时间逐一核验未改变。原 49 个用户文件及 SDK 保持；本步为部署与只读验收记录，相关生成/静态检查及 25 项集成/race 见 P17.6，未重复全包测试。

## 2026-11-04：P17.8 重报价保留双边及交替提交

- 实际重报价空档已在 SDK/mTLS/数据库工作循环中复现：终态查询被确定性屏障暂停时，原流程买卖两侧均为零。改为每侧缺少新目标时最多保留一笔本项目、未穿越新报价且不超过单笔额度的旧报价；新目标存在后再撤该旧报价。legacy、穿越价格和过大旧单不作为桥接报价，全部义务继续计入预算。
- 有效策略/来源及当前账户观察可维持既有报价的有效期；缺失终态仅返回当前观察和 UNKNOWN，不释放预留，不因缺席放行新单。新提交仍需所有撤单有正向终态并通过最终风控/epoch 检查。买卖目标改为交替提交，避免批量期限中断后只形成单边；行情失效/停止仍受控撤掉全部挂单。
- 双边桥接、交替提交的行为回归及上述运行层屏障回归均先失败后通过；新增穿越/legacy/额度拒绝覆盖。26 项 PostgreSQL integration/race 通过（45.652 秒），源失效全撤单、有界历史恢复、历史批准、未来未知门禁及有限测试成交均保持。相关单元/race、69 Proto/Wire、合同、格式、lint/govet 0 issues、核心/维护构建和 SDK 清单通过，用户原 49 文件保持；未运行全包测试。
- 本步代码尚未替换运行镜像。开发观察另外出现一笔新增未知下单，后续按 SDK 正向事实诊断恢复；不把它自动加入此前两笔历史批准，不以本步测试通过声明实际连续报价已全部验收。

## 2026-11-04：P17.9 实际发送前预留有效窗口

- da9b012 已推送并核验远端；只重建和更新 execution/strategy，market/portfolio 与共享基础设施保持。运行核验发现 BTC/ETH 各一笔在更新前已生成、无原生 ID 的未知意图，SDK 当前及完整有界历史均没有该 client_id；原意图的声明发送时间距离操作到期仅余 205/179ms。保留 UNKNOWN 和全部预留，不以缺席推定拒单；没有取得原 SDK 调用的完整错误，因此不将具体超时成因写成已证实。
- 新增 live 路径发送余量检查：声明发送和最终 SDK 出口均至少保留 500ms 的操作/来源/预算有效窗口，源有效期不扩大。过晚请求在 SDK 前拒绝，已发出且回复未知仍保持原合同；补充低基数上游状态码诊断，不记录原始敏感请求。已有通用传输测试入口不受 live 窗口政策影响。
- 新回归先复现余下 200ms 仍进入 SDK，修复后零 SDK 调用且没有 DISPATCHED。27 项 PostgreSQL integration/race 通过（48.241 秒），双边保留、源失效全撤、有界恢复及未知不重发保持；69 Proto/Wire、合同、格式、lint/govet 0 issues、核心/维护构建和相关单元/race 通过。SDK 16 文件及用户原 49 文件保持；未运行全包测试。
- 用户要求不再追既有历史后继续启动；已准备另一份固定私有清单，仅含上述两笔目前既有未知。后续在暂停写入、双次稳定 SDK 观察和保留全额风险的独立维护步骤中应用，不部署任何自动豁免未来未知的循环；随后更新执行镜像及 2 秒业务操作配置，继续实际挂单验收。

## 2026-11-04：P17.10 发送窗口修复部署及双币运行采样

- 2bc8fbf 已推送并核验远端，从该提交隔离源码重建执行/策略镜像。仅暂停并更新这两服务，显式预览/应用另外两笔既有未知的固定清单，原四笔仍 DISPATCHED、原身份/全额预留及批准观察保持；未来未知无自动豁免。业务操作期限改为 2 秒，来源仍 1.5 秒，SDK 最终发送仍至少保留 500ms；单笔 500、原总额度不变。
- 新轮 dev-20261104-06 两币各 90 笔 SYNTHETIC_PUBLISHED、零本轮 UNKNOWN，数量、1 秒间隔、1000 槽及原合成金额上限保持；此前各轮保留且不重置。四服务 healthy、自动重启为零；六个共享容器 ID/启动时间、原用户 49 文件及 SDK 16 文件保持。
- 04:28:00–04:28:55 UTC 十二次 SDK 采样中，两币均反复达到双边报价，BTC 和 ETH 均观察到 20 买/20 卖；来源和持仓投影新鲜，两个原仓位仍可见，未批准且超过 5 秒的 DISPATCHED 意图始终为零。
- 采样也保留了启动初期单边及重报价空档：当旧桥接价格已穿越新参考价，当前流程仍先撤旧单再等终态，后段样本分别出现 BTC 一次、ETH 两次双侧为零。没有把本步描述为无空档连续验收；继续在既有预算/未知门禁内调整交接顺序。相关 27 项 integration/race、生成/静态及构建验证见 P17.9，本部署步骤未重复全包测试。

## 2026-11-04：P17.11 价格跳变时先建立安全接续报价

- 将实际跳价场景加入确定性 SDK/mTLS/数据库回归：旧价带外移后，在撤单终态屏障处原实现 buy=1/sell=0，先失败。修复在没有既有未决撤单、完整预算/授权允许时，先为缺失方向提交最多一笔不穿越原生当前对侧挂单的目标，再撤旧单；常规补单跳过已成功预补的目标。
- 预补和常规提交共用同一持久意图、金额预留、500ms 窗口和 SDK 出口检查，旧单仍需正向终态，不放行既有 UNKNOWN。预补失败仍尝试受控撤除失效旧单，并保留两类错误；无额外连接、池或 goroutine，账户级 gate 与唯一执行者保持。
- 上涨/下跌方向与原生挂单不穿越的单元/race、跳价终态屏障双边接续、源失效全撤单等 28 项 PostgreSQL integration/race 通过（49.103 秒）。69 Proto/Wire、合同、格式、lint/govet 0 issues、核心/维护构建通过；新增单元补充后再次 unit/race、格式和最终 lint 通过。SDK 16 文件和用户原 49 文件保持，未运行全包测试。
- 本步提交推送后仅更新 execution，strategy/market/portfolio 和当前 dev-20261104-06 轮次继续运行，保留计数、额度、历史批准和所有预留，再采样实际双边挂单；本步不提前声明实际无空档验收完成。

## 2026-11-04：P17.12 报价交接部署与回复丢失诊断

- b3838ca 已推送并核验远端，只更新 execution，其他三服务继续运行。双币反复完成 40 个目标更新；04:45 的十二次采样中 BTC 九次、ETH 十次同时存在买卖挂单，仍各出现一次双侧为零，所有样本均无新增超过 5 秒的未批准 DISPATCHED。此为实际运行与保留的空档证据，不是全时连续验收。
- dev-20261104-06 随后达到 BTC/ETH 各 1000 笔 SYNTHETIC_PUBLISHED、零该轮 UNKNOWN；显式启动有限新轮 dev-20261104-07，首次采样各 56 笔，政策/数量/额度保持。重启核验发现新增未确认请求，其中 ETH 已能在 SDK 终态历史按 client_id 对应到 CANCELED，但现有自动恢复仅查询已知原生 ID，因此仍阻止后续新增风险。另有一笔 BTC 声明发送紧邻重启且尚无正向终态，继续保留全部预留，不据缺席判定失败。
- 四服务 healthy、自动重启为零，六共享容器及用户原 49 文件、SDK 16 文件均保持。本步部署未修改资金或基线；继续补充从正向历史找回原生身份及撤单前保存已查询到的身份，不重复提交旧请求，不把容器健康描述为双币报价已最终完成。相关 28 项集成/race 与生成/静态结果见 P17.11。

## 2026-11-04：P17.13 回复丢失后恢复原生身份及可证明未发送的清理

- 为没有原生 ID 的未批准 DISPATCHED 意图增加同账户、同产品 client_id 终态历史关联，最多十页、每页二百条；只提交查到的正向事实，身份冲突或跨账户记录拒绝，缺席继续保留未知和全部风险，不重发原请求。受控清理在撤单前保存当前活动订单身份；常规刷新沿用末尾观察，不增加每轮重复观察写入。
- SDK 传输记录是否进入请求体发送；只有确认 NewStream 失败且未进入 SendMsg 的本次声明可走既有本地未发送清理。一旦进入 SendMsg，任何错误仍按未知保留；SDK 源码及透明重放禁用保持。原生下单已生效但回包丢失、随后终态的回归，以及连接建流前失败留下永久未知的回归均先失败再通过。
- 相关三十项 PostgreSQL integration/race 最终按原期限分两组通过：五项原生工作循环 20.493 秒，其余二十五项 124.694 秒。包含回复丢失仅恢复一笔原订单且不重发、没有请求体时结束本地意图、活动/终态风险、源失效全撤、价格跳变交接、历史批准及有限合成槽。此前组合和分组运行曾遇到测试建表/认证超时及工作循环未按原期限收敛，保留失败记录；暂时停止本项目 strategy/execution/portfolio 后复核，未证明共享环境延迟的具体根因，未扩大期限或降低断言。
- 69 Proto/Wire、合同、最终格式、lint/govet 0 issues、四核心及维护构建、相关单元/race 通过；SDK 16 文件及用户原 49 文件字节保持，完整全包测试未运行。此步代码尚未部署；下一步从已提交源码更新执行/策略镜像并恢复三个暂停服务，market 及共享基础设施继续运行。
- 停止期间只读 SDK 确认双币无活动挂单，两个原仓位保留；当前仅余一笔 BTC 无正向终态的未批准历史意图，已生成精确的单笔私有清单，按用户本轮忽略既有历史的指令在维护租约及双次稳定观察下处理，保留全部预留。07 轮已经用完每币一千槽：BTC 1000 发布，ETH 997 发布及 3 未知；这些审计不重写、不重发或重置。恢复时显式启用同额度的有限 08 轮。

## 2026-11-04：P17.14 双币做市与第 08 轮开发合成成交恢复运行

- 0d4293f 已提交、推送并核验 origin/codex/two-apps 同 SHA；从该提交的隔离源码构建 execution/strategy 镜像。两服务 Nacos 版本 recovery-0d4293f，显式启用 dev-20261104-08 并逐一读回核验。维护命令预览及应用成功，仅批准已列明的现有单笔 BTC 意图；原状态与全部风险保留，历史批准总数为五，不启用任何自动批准未来未知的循环。
- 恢复 execution、portfolio、strategy 后四服务均 healthy。执行/策略/持仓自动重启为零；market 在此前检查期间有一次 deadline_exceeded 退出并自动恢复，当前健康，未确认该期限超时的具体底层原因。共享六容器 ID/启动时间保持；SDK 扩展仍为五条，原两条开发基线逐字节保持，没有新迁移、注资、划转或平仓。
- SDK 实际观察两币都有买卖挂单，且都完成四十个报价目标；例如 ETH 05:22:45 UTC placed=38/retained=2、BTC 05:23:05 UTC placed=29/retained=11。05:22:47–05:23:49 UTC 十二次观察中 BTC 八次、ETH 十次双边存在；BTC 两次、ETH 一次双侧为零，不能声称无间断满二十档。所有样本均无新增超过五秒的未批准 DISPATCHED；两个原仓位仍可见，最终来源各侧二十档与持仓投影均新鲜。
- 第 08 轮合成回执从各 22 增至各 117 笔 SYNTHETIC_PUBLISHED，该次审计零本轮 UNKNOWN。保持每币最多一千槽、最小一秒间隔、BTC 0.001/合成名义上限 120000、ETH 0.01/上限 60000；旧轮次及未知槽不重置。它仅生成开发行情，不是账户成交。服务继续运行，有限轮次达到额度后自动停止发布。
- 静态预算策略前后逐字段一致：BTC 总名义上限 335000、ETH 683000，各单笔 500，原开关、杠杆及费率不变；运行观察值与新订单预留继续正常变化。相关三十项 integration/race、生成、静态检查和构建见 P17.13，本部署记录未重复全包测试；SDK 原始十六文件和用户原四十九文件保持，文档链接及 diff 检查通过。

## 2026-11-04：P17.15 参考 market-maker 的滚动深度维护

- 按用户指定参考 Himalaya tools/market-maker，核对逐档差分、小批撤补、缺价优先、稳定最小量和 aggTrade 合成成交来源。原实现数量同时刷新时会把二十档撤到每侧一档；单元与真实 SDK/mTLS/数据库工作循环均先复现该结果。改为每侧每轮只撤补向上取整十分之一的安全待改订单，其余继续保留，二十档证明等待点实测买卖各十八档。
- 同价待改数量占据当前价位，缺少价格优先补齐；当前原生对侧和本轮新报价均纳入不交叉检查，每侧最多比目标多一个交接订单。前一批未确认时不继续抽走下一批，也不续订旧计划期限；穿越新对侧目标、过大或 legacy 订单继续受控清理。延后改量、旧价和重复价位清理均保留 RECOVERING，全部目标完成后才报告 RUNNING。重复单尚在时误报完成的单元回归先失败再修复。
- 最终三十一项 integration/race 分组全过：六项原生工作循环 21.996 秒，二十五项合同 44.136 秒。新四十档案例在原有十五秒生命周期内验证十批改量收敛（本次 5.351 秒）、证明屏障十八档和源失效全撤；原单档五秒等待检查保持。新夹具保留原 900 义务并使用独立 2000 预算容纳四十档，原场景仍为 1000，不涉及运行预算。夹具行情发布者改用真实租约 Renew，修复固定十秒不续期导致较长场景失效；仍以 epoch 变化验证失效，不伪造有效期。
- 回复丢失回归按有原生 ID 的正向终态断言，允许原本合法的本地未发送终态记录，SDK 原生订单数仍严格为三，重发断言保持。迭代期间共享 PostgreSQL 测试建表发生原有两秒 statement timeout，失败证据保留，原期限单项复核及最终两组均通过，未确认共享延迟根因。69 Proto/Wire、合同、格式、lint/govet 0 issues、四核心/维护构建及相关单元/race 通过；SDK 十六文件与原用户四十九文件字节保持，全包测试未运行。
- 本步只实现铺单维护的参考效果；合成成交当前仍为有限定时/交替方向，参考工具的 aggTrade 价格、方向和数量镜像差异已在 SYNTHETIC_TAPE.md 明确，未声称接入新的成交源。代码提交推送后仅更新执行服务，按策略先停、SDK 撤净、执行切换、原策略恢复的顺序操作；保持第 08 轮政策、计数及现有全部风险，实际运行采样另记。

## 2026-11-04：P17.16 滚动铺单部署与实际观察

- 3c42ed8 已推送并核验 origin/codex/two-apps 同 SHA，从该提交隔离源码构建镜像。先停 strategy，SDK 确认双币活动挂单为零且没有新增未决下单，再停并更新 execution，Nacos 仅改执行版本为 rolling-3c42ed8；恢复原 strategy 容器，market/portfolio 保持运行。未新增历史豁免或重置审计。
- 四核心均 healthy；execution/strategy/portfolio 自动重启为零，market 保留此前一次已恢复的重启。两条开发基线、五条 SDK 迁移记录和全部静态预算策略前后逐字段一致，BTC 总额度 335000 / ETH 683000、单笔各 500 保持。六共享容器 ID/启动时间保持，两个原仓位仍可见。
- 06:05:48–06:06:49 UTC 十二次 SDK 观察中 ETH 十二次、BTC 十次同时有买卖挂单；BTC 仍有两次双侧为空，不能声明完整连续二十档或与参考工具等效。各次新增超过五秒的未批准 DISPATCHED 均为零；来源双侧二十档及持仓投影新鲜。实际完成记录包括 BTC placed=21/retained=19、RUNNING；ETH placed=39/retained=2、RECOVERING，后者如实保留待交接状态。
- 第 08 轮在更新前后均已用满一千槽，BTC 999 发布/1 未知、ETH 998 发布/2 未知；本次保持政策、计数与回执，不补发未知、不自动创建下一轮。当前仍是定时合成成交，没有宣称已镜像参考工具的 aggTrade 来源。
- 本步仅为部署与运行记录；相关三十一项 integration/race、生成、静态及构建见 P17.15，未重复全包测试。文档链接、diff、SDK 十六文件及原用户四十九文件保持核验通过。服务继续运行，仍保留实际观察到的报价空档作为后续诊断事实。

## 2026-11-04：P17.17 最新合成成交跟随聚合成交流

- 按用户调整最新成交的指令，新增显式 AGG_TRADE 模式。market 独立连接 Binance USDm `/market/ws/<symbol>@aggTrade`，保留来源 ID、精确价格/数量、主动方向和交易/接收时间，通过 mTLS typed GetTrade 提供；深度路径独立，断流清除成交缓存，无深度或换源回退。共享 WebSocket 生命周期补充取消后不再发布已缓冲帧的检查，真实公网只读探针在 BTC/ETH 各三条后准确退出。
- 策略显式携带来源意图，执行端重新读取当前权威成交；价格按原生 tick 对齐，数量按已有 quantity_fraction 缩放、裁剪到单次上限并向下取整，低于最小量/金额跳过。原始观察保存在 Plan/Result 的 typed source_trade，兼容旧 BOOK 模式；新模式拒绝缺少来源的旧 wire，不默认为交替方向。新增字段沿用现有 JSONB，无新增持久化 Model 或 SDK 扩展迁移。
- 发送槽声明持有执行租约行锁，逐槽验证来源 ID 严格递增；回复未知、并发、下一序号或重启均不能再次消费同一来源。最终发送同时锁定执行、策略、深度及成交发布授权，并使用来源与租约最短有效期。旧 wire 的错误放行先复现后修复；隔离副本移除来源去重后确实产生第二次 SDK 调用并使负向控制失败。合成成交仍只发布开发行情，不创建账户订单或资金变更。
- 三十四项 PostgreSQL integration/race 最终分组全部通过：六项原生工作循环 22.448 秒，其余二十八项 44.668 秒，保留原期限与断言。此前组合运行遇到建表/认证超时和旧双边保留场景未收敛，保留失败证据；暂时停下本项目策略、执行与持仓并撤净 SDK 挂单后复核，未证明共享环境延迟根因。新增来源中断、旧 wire 拒绝、来源审计、重复未知、并发和最终发布租约锁覆盖通过。
- 69 Proto/Wire 生成比较、合同、格式、lint/govet 0 issues、核心/维护构建及相关单元/race 通过；SDK 十六文件及原用户四十九文件字节保持，全包测试未运行。本步代码尚未部署，market 与共享基础设施继续运行；提交推送后从隔离源码配套更新 market/strategy/execution，恢复持仓并启用同额度有限第 09 轮，再记录实际发布与消费验收。

## 2026-11-04：P17.18 聚合成交模式部署与最新成交消费验收

- 7723a02 已推送并核验 origin/codex/two-apps 同 SHA，从该提交的完整隔离归档构建 market/execution/strategy 镜像，未混入用户原四十九个待提交文件。三份 Nacos 配置显式选择 AGG_TRADE、版本 aggtrade-7723a02 并逐份读回；配套更新三服务并恢复 portfolio。四核心均 healthy，本次容器自动重启为零；没有新增迁移、历史批准、注资、划转或平仓。
- 有限新轮 dev-20261104-09 从 BTC/ETH 各 4/5 增至 22/36，最终审计为 52/85 笔 SYNTHETIC_PUBLISHED，零本轮 UNKNOWN。共 137 个来源 ID 在各自币种内严格递增且唯一，Plan/Result 来源一致，声明时来源新鲜，价格/方向/数量均符合原始来源与缩放规则。BTC 因原上限和步长均为 0.001，成交量仍为 0.001；ETH 已观察到 0.001 至 0.01 的多个数量，不扩大原单次额度。
- 实际行情消费端 market-data.GetRecentTrades 三次核对分别匹配 9、58、137 个本轮 SDK 回执，最后 BTC 52/52、ETH 85/85 的成交 ID、价格、数量及主动方向全部一致。mTLS GetTrade 实际返回 Binance/USDm 双币的新鲜事件，价格与方向随来源变化。每币仍最多一千槽、最小一秒间隔，数量比例 0.1、BTC/ETH 单次上限 0.001/0.01、合成总名义上限 120000/60000 保持；它是有限开发行情，不是账户成交。
- 原两条开发基线及五条 SDK 扩展迁移逐字段保持；静态资金策略保持（BTC 335000、ETH 683000、单笔各 500）。可用观察金额、实时预留和快照时间正常变化，未将这些动态值错误当成静态配置。六共享容器 ID/启动时间、SDK 十六文件和原用户四十九文件保持。
- 原生做市继续工作，执行日志分别记录 BTC placed=25/retained=15、ETH placed=7/retained=33 的 RUNNING。只读采样曾同时有双币双边挂单，后续仍分别观察到 BTC 或 ETH 重报价时双侧为零；不声明连续满二十档。各次未批准且超过五秒的 DISPATCHED 均为零，两个原仓位保持。一次持仓快照探针因 stale 被正确拒绝，随后同一探针确认双币二十档来源与持仓均新鲜，失败证据保留，不把容器健康等同于每次业务观察有效。
- 本步为部署与只读验收，代码相关三十四项 integration/race、生成、格式、静态及构建见 P17.17，未重复全包测试。文档链接与 diff 检查通过；服务继续运行，达到本轮有限额度后停止合成发布，不自动重置审计或创建下一轮。

## 2026-11-04：P18.1 Himalaya 账户及市场业务参数移入数据库

- 用户最新要求替代此前 Nacos profiles 的业务归属：账户、基金、交易对、总额/单笔限额、杠杆、行情源与 tick/step 统一由生成的 HimalayaBudget Model 保存，复用原资金策略，不另建重复额度表。移除 config Proto 的 HimalayaProfile，保留 profiles 的编号 3 和名称；四核心、基线维护命令均只读数据库形成启动快照，缺失/非法绑定拒绝，无本地/Nacos 回退、启动写入或热切换。
- 扩展迁移 0006 仅追加 symbol、market_data_source、price_tick、quantity_step 和有效交易对唯一索引；旧行保留空绑定/零网格，必须显式导入，不能猜产品。公共 HimalayaPolicy/ProfilePolicy 是 typed 业务与审计快照，不是配置或第二套 Model；保持原字段编号、规范化数据库 numeric 填充，旧配置编码的金样验证合成政策指纹不因存储迁移变化，动态余额/时间不进入指纹。
- himalayainit 增加独立 -profiles FILE 模式，严格 Proto JSON 输入、每批最多八项且原子落库；既有启用状态、身份、创建时间、观察值、全部风险预留与历史记录保持，新预算默认禁用，不操作原生资金/订单。维护须等待执行、策略、行情、成交及持仓租约到期；额度缩减不能低于保留风险，已删除记录不能复活。-enable 改为读取已完整定义的数据库策略，-schema-only 不导入数据。
- Nacos 仍接受业务 profiles、维护在活跃执行者下仍能改变额度的两个行为均先复现失败，再修复。新增 PostgreSQL 覆盖整批后项风险拒绝回滚前项、启用状态及余额/预留保持、新预算禁用、行情 owner 拒绝维护、无启动写入、显式重启才更新快照、0005 升级和重复迁移保留旧 UNKNOWN 审计；四个新增集成案例单组通过 6.842 秒。
- 三十八项 integration/race 用例最终均有通过记录：六项原生工作循环整组 23.940 秒，另外三十二项合同中一项并发回执测试发生原有两秒 statement timeout，其余通过；该项按原五秒 context 单独复核通过（案例 0.76 秒、包 2.210 秒）。此前共享 PostgreSQL 建表/迁移超时及二十档案例未按期限收敛均保留证据，未证明底层延迟根因，未扩大期限或降低断言。70 Proto/Wire、合同、格式、lint/govet 0 issues、核心/维护构建及相关单元/race 通过；全包测试未运行。
- SDK 十六文件及用户原四十九文件字节保持。已先停止本项目策略、实际 SDK 撤净双币挂单且无新未决，再暂停其他三核心；共享基础容器保持运行。当前开发参数已私下逐项备份，四份 Nacos profiles 完全一致，原两条基线/五条 SDK 历史已采样；本步代码尚未部署。提交推送后显式迁移/导入原值、移除四份 Nacos profiles 并配套更新四镜像，保留第 09 轮现有 BTC 246 发布/1 未知、ETH 387 发布/2 未知及全部限额，不创建新轮或批准未来未知。

## 2026-11-04：P18.2 数据库业务参数部署与恢复核验

- 4937838 已推送并核验 origin/codex/two-apps 同 SHA，从该提交的隔离归档构建四核心镜像。四份 Nacos 配置均移除 profiles、版本更新为 dbprofiles-4937838 并逐份读回一致；显式运行 himalayainit -profiles，扩展 0006 与原值导入成功。恢复 market、execution、portfolio、strategy 后四核心均 healthy、本次自动重启为零，业务不依赖遗留配置字段。
- 数据库实际九项业务字段与导入前的原 profiles 逐项一致：账户/基金、交易对、总额/单笔限额、杠杆、行情源和 tick/step 均由 HimalayaBudget 提供。原预算静态字段、Id/createAt、启用状态及两条开发基线保持；旧五条扩展历史逐项保持，仅追加迁移 6。实时观察金额、预留与快照时间继续由工作循环更新，没有资金划转、注资、平仓或新增历史批准。
- 第 09 轮实际 636 个持久槽逐条核验，新数据库快照计算的执行政策指纹与全部旧记录一致，来源单调唯一及 Plan/Result 审计保持。恢复首轮审计仍为 BTC 246 发布/1 未知、ETH 387 发布/2 未知，不重置次数、不重发未知、不创建新轮；继续沿用既有重启读取回执的路径，未将这次不变计数描述为新增发布。
- SDK 采样同时存在 BTC 买5/卖3、ETH 买1/卖6，后续观察为 BTC 买6/卖8、ETH 买5/卖4；双币各一笔原仓位仍可见，未批准且超过五秒的新 DISPATCHED 为零。后续 mTLS 探针确认双币二十档来源和持仓新鲜，GetTrade 返回新鲜 Binance/USDm 观察。首个持仓探针曾因 stale 正确拒绝，证据保留；不将健康容器等同于每次业务快照有效，也不声明解决既有重报价空档。
- 六共享容器 ID/启动时间、SDK 十六文件和用户原四十九文件保持。本步仅部署与只读验收；相关三十八项 integration/race、生成、格式、静态及构建见 P18.1，全包测试未运行。文档链接及 diff 检查通过，四服务继续运行。

## 2026-11-04：P19.1 data.yaml / service.yaml 强类型配置拆分

- 按用户最新要求替代单份 Proto JSON：DataConfig/data.proto 定义 PostgreSQL、Redis、JetStream 与业务运行控制，ServiceSettings/service.proto 定义身份、gRPC/HTTP/监控监听、TLS、日志、RPC 客户端及生命周期；既有 Proto 业务字段编号保持。ServiceConfig 仅为 Deer/既有接口的内存装配结果，核心启动不再消费该聚合配置。六个本地/历史示例均拆为各自目录下 data.yaml、service.yaml，账户业务参数仍在数据库。
- Nacos 每服务读取两项 `<service>.data.yaml`、`<service>.service.yaml`；新 env 为 NACOS_DATA_CONFIG_ID/NACOS_SERVICE_CONFIG_ID，通用工具必须明确提供，旧 NACOS_DATA_ID 拒绝。两文档同名/同版本，service 的 data_sha256 绑定 data 原始字节，缺项、错误分区、未知字段或半次发布失败。连接认证仍来自 env，内容只能提供注册参数，不覆盖 bootstrap；发布先 data 后 service，启动后无热更新/本地回退。
- YAML 输入使用 Node 转换真实传输语法后交给 Proto 严格解码，无 typed Go→map 桥接或浮点中间值；重复键、多文档、anchor/alias/merge、自定义 tag、非十进制整数和未加引号的小数拒绝。完整精度字符串、类型装配往返、错配/篡改、安全错误与 context 取消覆盖通过，最初双 YAML 启动回归先因旧加载器未实现而失败，再通过。维护脚本使用现有 PyYAML 6.0.2 并固定到 requirements，Go YAML 依赖沿用 3.0.1。
- Compose、开发准备、镜像 smoke 与 testprocess 都改成成对发布/读取/删除；真实核心进程无参数、无配置挂载，历史显式 JSON 工具/测试入口兼容保留。相关单元/race、31 项 Python 工具测试、71 Proto/Wire 生成比较、格式、lint/govet 0 issues 及核心/维护构建通过。四项真实进程 integration/race 均有通过记录，覆盖成交发布/消费/对账、CoreLocal 不注资、broker 合同及策略接管报价停止；一次复核在新风险门禁处拒绝，未确认具体延迟原因，该用例按原 25 秒 context 单独通过（案例 3.92 秒、包 5.344 秒），未扩大期限或降低断言。完整全包测试未运行。
- 保留原用户四十九文件；期间另出现 portfolio_facts.go 的无关换行整理，单独备份并排除提交/构建，最终隔离源码使用其 HEAD 版本。SDK 十六文件保持。为真实进程复核已停止本项目策略、撤净原生挂单后暂停其他三核心，基础容器保持；当前四份运行配置与资金/合成审计已备份，本步尚未部署。提交推送后按原业务参数发布八份 YAML 并恢复四核心，保留第 09 轮 BTC 519 发布/1 未知、ETH 847 发布/2 未知及全部限额，不新建轮次或批准未来未知。

## 2026-11-04：P19.2 双 YAML 部署与运行状态核验

- 7bd490a 已推送并核验 origin/codex/two-apps 同 SHA，从仅含已提交源码的隔离归档构建四镜像。Nacos 发布四服务共八份 YAML，版本 yaml-7bd490a；配对读回与原运行参数逐字段一致，仅版本改变。恢复四核心并确认全部 healthy 后，删除已备份的四份旧 JSON；删除确认曾先读到 Nacos 旧缓存，随后有界等待读回全部缺失，未重启或回滚已健康服务。
- 容器实际仅使用 NACOS_DATA_CONFIG_ID/NACOS_SERVICE_CONFIG_ID 两项，未继承旧 NACOS_DATA_ID；四核心健康、本次自动重启为零。两条数据库资金策略静态字段、两条开发基线及六条 SDK 迁移记录保持，没有数据库迁移、注资、划转或平仓；六共享容器 ID/启动时间、SDK 十六文件及全部五十个无关待提交文件保持。
- 第 09 轮 1369 个既有发送槽逐条核验，新 YAML 装配的执行政策指纹与全部旧记录一致，来源单调唯一及审计保持；计数仍为 BTC 519 发布/1 未知、ETH 847 发布/2 未知，不重置、不补发未知、不创建新轮。执行日志的 replayed 回执不视为新增成交发布。
- 运行观察如实保留业务阻塞：双币各一笔原仓位存在，新未批准且超过五秒的 DISPATCHED 为零，当前两币活动挂单均为零。执行处于 OUTCOME_UNKNOWN/RECOVERING，持仓探针因观察未完成而拒绝。两币各抽查最新五笔待确认撤单，SDK 按原生 ID 查询终态历史均无对应记录；缺席不构成终态证明，原预留及恢复门禁保持，未自动扩展历史批准。本步仅完成配置拆分部署，不宣称重新恢复持续做市或解除上游终态回执缺失。
- 相关单元/race、31 项脚本测试、四项真实进程回归、71 Proto/Wire、格式、静态及构建见 P19.1；本步为配置/只读运行核验，未运行完整全包测试。文档链接与 diff 检查通过，四服务和恢复循环继续运行。

## 2026-11-04：P20 服务归属、旧入口清理与 gRPC 健康

- 按最新用户要求替代保留 DeerApi/组合回归入口的约定：删除 cmd/operator、operator 本地示例、deer/testbusiness/testentry/testservice 旧角色分发及 operator 客户端重定向；各交易服务的回归命令迁入自己的 testservice，策略管理回归装配迁入 strategy/testbusiness。公共 testprocess 只接收所属项目的 typed builder，不导入业务实现。四核心仍直接装配各自 business，不启用管理 RPC。
- 新增 cron/cmd 与 cron/internal/app 的独立 Wire 图、日志和 Deer 生命周期，无参数、env/Nacos 双 YAML 启动。五个入口均要求 gRPC，并通过实际生成图调用标准 Health/Check 验证 named SERVING/未知服务 NotFound；健康 Watch/List 继续由公共 Deer 提供。cron 尚无定期业务，不创建空工作器或加入默认四核心 Compose。其真实独立命令读取隔离 Nacos 配对配置、提供健康 RPC 并正常停止的 integration/race 通过。
- 删除 DeerApi/NewDeerApi/APISpec 和无运行调用方的 API 服务观测实现、合同及 Proto。Hertz 仅保留公共 loopback 监控，原传输排空、请求关联、依赖回滚/借用测试保留为私有监控夹具或 gRPC 注册失败测试；业务 HTTP 指标移除。历史数据库迁移、共享资金/幂等/fencing 事务仓储及用户未提交平仓功能保持。
- 将 outbox claim/错误/subject 合同提至公共 outboxcontract，实际发布工作器仍归 execution；模拟行情默认策略归公共 domain，marketquality 复用同一来源。共享仓储/corelocal 不再反向导入服务实现。新增 check-layout 门禁纳入 lint，检查 Deer 无服务实现依赖、五进程拥有自己的装配图且不装配其他项目 business/回归夹具。doctor 同步支持 YAML 目录，五服务本地示例检查通过。
- 以 HEAD 加本轮改动构造隔离源码，全部五十个既有待提交文件逐字节保持并排除构建。隔离源码全包非 integration 的 race 测试、相关单元/race、35 项 Python、70 Proto 生成比较、五份 Wire diff、合同/规范正反样例、格式、lint 0 issues 及五服务/维护/迁移后夹具构建通过。cron Linux 镜像构建通过，首次遗漏 Docker 上下文白名单已补齐。工作区直接全包测试仍有原未提交 eventwire/validate_test.go 的断言失败，本轮未改该文件；不将隔离源码通过宣称为这部分用户功能完成。
- 验证中的失败保留：一次隔离目录刷新与并行检查重叠读到将删除的 api.go，固定源码后顺序复核全包 race 通过；最初核心进程组出现准入拒绝、测试库迁移失败，单独复核成交投影及 outbox 存储/ACK 用例按原 25 秒 context 通过。策略接管复核曾由 execution.risk 的原一秒 deadline 失败而停止，历史夹具成交投影亦曾因新风险门禁拒绝；没有扩大期限、降低断言或修改交易逻辑。策略控制回归按原期限通过；其他最终复核结果补充如下。

- 进一步隔离验证：SDK 先确认两币活动挂单/新未决均为零，短暂停止本项目四应用后复核；用例已完成首实例接管，但新实例双边报价未在原 25 秒内收敛，故未标该回归通过。随后恢复同 ID 的原四容器并全部 healthy，六共享容器 ID/启动时间、两币原仓位及原未知/撤单计数保持。改动前 HEAD 源码逐文件核对一致，以独立目录构建其 market/execution/strategy 二进制后，在相同现有 infra 中该用例通过（19.67 秒）；该对照只证明旧版此次可通过，未据此断言候选故障根因。继续按原期限复核候选，未修改交易代码或测试断言。

- 最终同轮对照在同一套隔离逻辑资源上顺序执行：原 HEAD 接管用例 12.03 秒通过，候选 9.91 秒通过，均保留原 25 秒 context、原一秒业务 deadline 和全部资金/接管/停止断言。迁移后的独立成交夹具与策略控制回归也按原期限通过；相关核心和 outbox 用例均有通过记录。初期间歇性失败未被证明已修复，不把一次通过宣称为负载稳定性保证。完整 make verify、全量 integration/e2e/soak 未运行。
- 本轮目录、清理和健康接口验收通过。cron 镜像还以禁网运行验证无参数合同，传入配置 flags 明确退出 2；未用该负向启动冒充健康检查，真正健康由 Nacos 独立进程用例验证。四原开发容器已恢复 healthy，未部署候选交易镜像、未迁移或改动资金/订单处置政策；BTC/ETH 原未知恢复阻塞保持。最终提交与远程结果见本轮报告。


## 2026-11-04：P21 Proto 以服务名组织

- 按最新用户要求移除 deer/proto 顶层 config/model/rpc/hertz 分类，第一层仅 common、market、strategy、execution、portfolio、cron。各服务 RPC、持久化模型、专属配置归其 v1、model/v1、config/v1；SDK 内部 Market/Execution/Portfolio RPC 分别归所属服务 himalaya/v1。公共信封、基础连接、跨服务资金/品种/身份/通用租约、事件及框架探针/注解归 common。cron 沿用标准健康协议，无占位业务 Proto。完整定义归属见 deer/proto/README.md。
- 保留全部 Proto package、消息与服务全名、字段号/类型/GORM tag、RPC 方法名，HimalayaAuthority 所在源文件逐字节保持。Go 源码使用方改用新导入路径，项目调用点与五份 Wire 已更新；生成器按实际服务声明生成 gRPC，Hertz 注解/探针路径同步。目录门禁限制第一层归属和 go_package，模型四字段门禁覆盖各服务与 common/model。未新增数据库迁移，也未改变共用 repository 的事务边界。
- 在独立的 clean/user-overlay 两套源码中迁移并重新运行 protoc/gotag/Wire，没有手改生成字段。两套迁移前后各 241 项完整消息/枚举/服务描述逐项一致，46 个生成模型的 GORM 表名/列/原始标签/主键元数据逐项一致；710 个既有 Go 文件及原有 41 个待提交 Go 文件在归一化 Proto 包别名后语法树一致，排除本轮有意更新的规范门禁。原有五十处未提交修改按新路径保留，独立于目录整理的索引版本，原 0038 迁移和未完成业务不纳入本轮提交。
- clean 源码全包单元与 race、39 项 Python 工具、85 Proto/五 Wire 生成比较、Hertz 确定性生成及编译、合同和规范正反样例、格式、lint/govet 0 issues、五服务及维护/回归夹具构建通过；实际工作区含原业务修改的全 integration 标签代码也编译通过，生成比较与 lint 通过。生成模型 PostgreSQL 读写/NULL/精确小数、迁移账本、CoreLocal 幂等不注资、outbox claim/fencing 集成/race 通过。跨进程发布/消费/对账首轮在新风险准入处拒绝，保留证据；同用例按原 25 秒 context 单独复核通过（2.60 秒），没有扩大期限、降低断言或改变交易逻辑。cron 实际独立命令从隔离 Nacos 双 YAML 启动并提供健康 RPC 的 integration/race 通过（2.83 秒）。
- 本轮仅代码/协议目录调整；没有重建或重启现有交易容器，没有发布 Nacos、改写资金策略或处置未知订单。原有未提交 eventwire 测试失败不计作本轮修复，完整工作区业务验收、全量 integration/e2e/soak 和候选镜像部署未运行；数据库迁移文件字节保持。目录与协议兼容验收完成，提交及远程结果见本轮报告。


## 2026-11-04：P22 所有配置共用一个 Proto

- 按用户进一步要求，将原公共 data/service 与 market/strategy/execution 的五份配置定义统一到 deer/proto/common/config/v1/config.proto，生成同一 configv1 包；移除独立服务 config 目录和旧分散生成物，更新 26 个配置使用方的导入/包限定符及相关文档。目录门禁同时约束 config 路径和 muskdeer.config.v1 命名空间只能使用该文件。
- DataConfig、ServiceSettings、ServiceConfig、全部业务运行设置与原字段保持；Nacos 继续提供 data.yaml/service.yaml 两份配对配置，原身份/版本/哈希及严格 YAML 校验不变。HimalayaAuthority 和数据库业务参数不改，不涉及数据库迁移或运行配置发布。
- clean/user-overlay 两套迁移前后各 241 项完整描述符一致；712 个已有 Go 文件及原有 41 个未提交 Go 文件在归一化 Proto 包别名后业务语法树一致。原有 50 处未提交修改单独保留，未纳入配置整理提交。所有生成物由 protoc/gotag 重新生成，原手工生成代码没有补丁修改。
- 隔离源码全包 race、41 项 Python、81 Proto 生成比较、五 Wire diff、合同/格式检查、lint/govet 0 issues、工作区全 integration 标签编译通过。五服务本地双 YAML 的 doctor 校验通过；实际 cron 独立命令从隔离 Nacos 双配置启动、提供健康 RPC 并正常退出的 integration/race 通过（2.60 秒）。完整工作区业务测试、全量 integration/e2e/soak 未运行，原有未提交 eventwire 断言失败不计入本次修复；未重启或部署交易服务。


## 2026-11-04：P23 Proto 生成代码独立到 deer/pb

- 按用户要求将全部 81 个项目 Proto 生成 Go 文件移至 deer/pb，保留 common/服务子目录；包文档一并迁移，deer/proto 只保存 60 份协议源文件及说明。go_package、所有 Go 消费者、Hertz 注解映射及工具夹具同步调整；五个 Wire injector 由固定版本工具重新生成，仍归各自 cmd。配置继续共用 common/config/v1/config.proto，data.yaml/service.yaml 不变。
- 生成器隔离输出和比较覆盖 deer/pb，目录门禁拒绝源目录残留 Go 文件、生成目录混入 Proto 及旧 go_package。新增三项目录规范回归，44 项 Python 工具测试通过。SDK/第三方原始代码和全部数据库迁移保持，未复制模型或改变仓储事务边界。
- clean/user-overlay 两套迁移前后各 65 份完整文件描述符除 go_package 外逐项一致，包括消息、字段、枚举、服务、RPC、注解和保留编号；两套各 46 个生成模型的 GORM 表名、列和元数据逐项一致。715 个已有手写 Go 文件仅更新导入路径和相应 gofmt；原有 50 处待提交业务修改按新路径独立保留，不纳入本次提交。
- 隔离源码全包 race、81 Proto 生成比较、五 Wire diff、Hertz 确定性生成/共享 DTO 编译、doctor 双 YAML、合同/规范正反样例、格式、服务依赖图及 lint/govet（0 issues）通过；实际工作区含原有修改的全 integration 标签编译通过。五服务及维护命令构建通过，新构建 cron 独立进程从现有 Nacos 的隔离双 YAML 启动、健康 RPC 和正常退出的 integration/race 通过（1.29 秒）。
- 初次 race/Hertz 编译因磁盘空间不足中断，保留失败记录；仅清理本项目可重建的 Go 编译缓存，恢复约 23 GiB 空间后复核通过，没有改动测试断言。完整工作区业务测试、全量 integration/e2e/soak 和镜像部署未运行；未重启交易服务、未发布业务配置或变更订单策略。本轮目录与兼容验收完成，提交及远程结果见本轮报告。


## 2026-11-04：P24 移除 Hertz Proto 注解

- 按用户要求移除 deer/pb/common/hertz 生成包和其唯一使用方算术探针的 HTTP 路由/参数注解，删除 deer/third_party/hertz 的 api.proto/许可证。算术探针的消息字段、编号及 gRPC 方法保持，query tag 继续由 tagger 生成，供现有手工 HTTP 绑定测试使用。Hertz 运行时仍为公共监控提供 HTTP 服务。
- 删除 scripts/verify-hz.py、make hz-check、verify 中的对应依赖及 tools/versions.json 的 hz 插件；生成器不再传入 Hertz Proto 或导入映射。重新生成受影响的 probe Go 文件，源码和生成目录均无 Hertz 注解引用，历史验收记录保留。
- make generate-check（80 个 Proto Go 文件、五 Wire）、fmt-check、服务布局、lint/govet（0 issues）、44 项 Python 工具测试及全包 go build 通过；Deer、frameworkprobe、JetStream 和五服务入口的相关 race 回归通过，覆盖 gRPC、HTTP 十进制 query 绑定、监控生命周期及健康检查。
- 原有 50 处待提交修改逐字节保持，与本次 20 个文件清理独立。完整工作区业务测试、全量 integration/e2e/soak 未运行；未重启交易服务、发布配置或改动数据库。提交与远程结果见本轮报告。


## 2026-11-04：P25 移除无业务用途的模型样例

- 按用户要求核查 46 个持久化模型的维护代码引用；仅 StorageProbe 没有业务/迁移实现引用，是 P01 技术验证实体。删除 common/model/v1/storage_probe.proto、对应生成 Go 文件及两套专属建表夹具。保留有业务、租约、身份审计或迁移账本用途的其余 45 个模型；本轮没有合并或重划已有 OMS/资金事务。
- 生成契约、PostgreSQL 持久化与基础设施回归改用真实 Instrument 和 AccountBudget；隔离夹具复用迁移 0002/0007 的表定义及约束。保留主键、13 列模型元数据、numeric(38,18)、联合唯一键、时间自动更新、NULL/显式零删除时间、事务回滚等断言。额度模型验证 false/零预留写回和保留 revision，并验证显式整数零实际送达数据库后按业务约束返回 23514；没有放宽真实业务约束。lint 正例也改用 Instrument。
- 相关 persistenceprobe/policy 单元 race、79 个 Proto Go 文件与五 Wire 生成比较、格式、依赖图、lint/govet（0 issues）、规范正反样例、44 项 Python 和全包构建通过。复用现有 infra-postgres 的独立临时数据库，PostgresContract 的五组及 PostgresInfrastructure 的六组 integration/race 均通过，覆盖精确小数、约束/NULL、更新/回滚、连接池、超时、readiness 和认证错误；测试逻辑资源已按原流程清理，未新增/停止共享基础容器。
- 原有 50 处未提交修改逐字节保持，实际业务模型和所有数据库版本化迁移未修改。完整工作区业务测试、全量 integration/e2e/soak 未运行；未重启交易服务或发布运行配置。本轮清理与相关验收完成，提交和远程结果见本轮报告。


## 2026-11-04：P26.1 通道业务模型与存量升级

- 按最新要求移除六个 Himalaya 命名的持久化类型，改由 TradingAccountPolicy、VenueOrderIntent、VenueCancellation、VenueAccountSnapshot、ExecutionRecoveryBaseline 和 VenueSchemaMigration 承担业务记录；定义与生成代码按 common/execution/portfolio 归属。字段、精度、请求摘要和既有状态值保持，未将 UNKNOWN 当作失败释放额度。Himalaya 适配层的进一步职责拆分属于下一步 P26.2，本记录不宣称整项完成。
- 显式迁移模块归 deer/migrate/venue，历史六份 SQL 逐字节保持；新增 0007 原子升级既有表名，保留主键、数据和约束。新代码启动只读检查完整版本，升级只由维护命令显式调用。旧版升级回归额外写入 UNKNOWN 下单、未决撤单、持仓观察和恢复基线，逐项验证升级后完整消息一致；目的表冲突时验证 ledger/额度回滚，随后两次升级验证幂等。
- clean/user-overlay 两套 integration 标签全包编译通过；clean 全包 race（复核采用 -p 1）、79 Proto 和五 Wire 比较、格式/合同/布局及 lint/govet 0 issues、44 项工具测试和全包构建通过。现有 PostgreSQL 的隔离库中相关 SDK 准入/金额/配置维护/历史升级、并发预留、未知不重发及合成成交额度/重放的 integration/race 通过。
- 初次并行验证保留两处失败：Deer 短期限 RPC 用例在建连阶段 deadline 到期、尝试数为零；并发额度用例在核心迁移夹具处触发原 statement_timeout。未改期限或断言，顺序复核全包 race 和相关 PostgreSQL 组通过；本轮不宣称已修复长期负载稳定性。原有 50 处用户修改逐字节保持；未升级当前交易数据库或重启交易容器。P26.2 继续收敛适配层，提交与远程核验见当前工作记录。


## 2026-11-04：P26.2 Himalaya 仅作为第三方适配器

- SDK/gRPC 连接、认证、禁止透明重发、请求摘要、单位与响应转换集中到 execution/adapters/himalaya；通过 execution/venue/gateway 的强类型端口由 execution/business 显式注入。持久化、共享额度、最终 fencing、撤单/报价/UNKNOWN 恢复和 worker 生命周期归 execution/venue；Store 只依赖无网络 Codec。数据库业务策略读取迁至 deer/trading/venueconfig，共用 native 值解析/状态分类不再依赖 GORM 或持久化模型。真实 Go 依赖图验证适配器的直接及传递依赖无 ORM、生成模型、交易仓储或业务 runtime。
- 三个服务的内部 RPC 源文件/生成文件目录从 himalaya/v1 改为 venue/v1；Proto package、字段、方法全名及旧配置键保持。HimalayaAuthority 按此前明确要求保留；这些兼容消息不作为 ORM 实体。新增门禁拒绝模型目录中的 Himalaya 命名实体，并禁止适配器外的生产代码直接导入上游 SDK；现有 16 份 SDK 源文件哈希保持。
- 新增行为回归：下单和撤单摘要与拆分前持久化身份完全一致；行情/账户未就绪时拒绝新增风险观察，但仍可读取订单并受控撤单，实际 Place 调用为零；测试成交 BUY/SELL 显式映射、未知方向在进入 SDK 前拒绝。原准入、预算、所有权、恢复、滚动报价与有限测试成交断言保留。
- 隔离本次提交源码的全包 race（-p 1）、integration 标签全包编译、全包构建及核心/维护/模块夹具构建通过。叠加原有用户修改后的 integration 标签全包编译通过；工作区格式、合同/布局、lint/govet 0 issues、规范正反样例、47 项 Python、79 Proto 与五个 Wire 生成比较通过。
- 复用现有 infra-postgres 的隔离逻辑库，SDK/原生四服务/滚动报价/回复丢失/UNKNOWN/测试成交/业务策略和旧模型升级共 32 项 integration/race 中 31 项首次通过；一项在核心迁移夹具触发原 statement_timeout，保持期限及断言单独复核三个变体全部通过（5.799 秒）。首次工具检查误用系统 Python 缺少 PyYAML、规范负例与 lint 并发被工具锁拒绝，改用已有 Python 3.13 并顺序运行后通过；未更改测试合同或屏蔽错误，不宣称已修复基础设施长期负载稳定性。
- 原有 50 处用户未提交文件逐字节保持，暂存内容与隔离提交源码逐项核对；未运行完整 integration/e2e/soak，未升级当前交易数据库、发布 Nacos 或重启交易容器。通道 0007 升级及新旧版本部署顺序已记录，实际运行环境保持原状。


## 2026-11-04：P27 通道接口全面改名，不保留旧接口兼容

- 按最新明确要求替代 P26 的旧线协议保留约定：全部 Himalaya* 公共消息/配置类型改为 Venue*，common/v1 源文件与生成文件改为 venue.proto/venue_policy.proto；execution、market、portfolio 通道 RPC 分别采用 muskdeer.<service>.venue.v1。旧类型不注册、旧路由没有别名或转发，所有服务端、客户端、测试对端及共享授权传递一起更新。VenueAuthority 仍表达业务最终授权，不作为第三方 SDK Model。
- 配置仅使用 business.venue 与 execution_venue=gateway；旧 himalaya 配置键和 himalaya_sdk 执行值拒绝。维护源码、用法、Makefile 和 Docker 构建/复制路径统一为 venueinit、venuebaseline，原命令目录移除；新旧服务/客户端与 Nacos 配置必须配套发布。更新当前接口文档，历史验收记录不改写。
- 新增回归先验证旧版会接受旧配置、注册旧消息与旧服务名称，按原断言在新实现通过。实际 bufconn gRPC 验证两个旧有副作用方法返回 Unimplemented、处理次数为零，新客户端各调用一次；验证新服务全名与旧类型注册缺失，JSON/YAML 旧配置拒绝、旧执行值在业务启用/关闭时均拒绝。最后审查先复现关闭状态误接受旧值，再收紧通用执行值白名单；复核全包 race、包含原修改的编译、lint 和全包构建通过。Proto 门禁新增拒绝 provider 专属业务消息/旧命名空间，并要求通道 RPC 命名空间随服务归属。
- 隔离本次提交源码的全包 race（-p 1）、clean/user-overlay 两套 integration 标签全包编译、五服务/维护命令及四模块夹具构建通过；工作区格式/依赖布局、合同、lint/govet 0 issues、规范正反样例、49 项 Python、79 Proto 和五个 Wire 比较通过。变更 Markdown 本地链接与生产 Go 旧类型/旧路由/旧命令残留审查通过；原有 50 处用户修改逐字节保持且未纳入暂存。
- 复用现有 infra-postgres 的隔离逻辑库，32 项相关 integration/race 全部一次通过（72.453 秒），覆盖新 RPC 下的四服务接管、二十档滚动保留、行情失效撤单、未知/回复丢失不重发、最终授权、预算、配置维护、历史升级及有限测试成交。对端为测试 SDK/gRPC，不冒充真实网关交易验收。
- 第三方 SDK 16 文件保持原始哈希。此次仅切换内部合同，数据库记录身份、实际 SDK 请求摘要、索引和历史迁移保持；未清理既有资金/订单记录，未升级当前数据库、发布 Nacos 或重启交易容器。完整 integration/e2e/soak 与镜像构建本轮未运行。


## 2026-11-04：P28.1 数据库业务执行参数基础

- 新增 Proto/GORM 唯一来源 ServiceRuntimePolicy，按核心服务保存强类型 BusinessRuntime、schema 版本、revision 和确定性参数摘要；RuntimePolicyMigration 管理独立显式配置结构升级，保留根迁移及原有用户 0038 修改。迁移不生成默认参数、不启用业务、不改订单/资金记录，启动仅允许只读校验。
- 数据库加载拒绝缺失、删除、摘要不符及非法参数，返回独立副本，调用方提供的内存参数不会成为缺失回退。离线导入按稳定服务顺序加锁、乐观版本校验和整批事务写入；精确重复保持原记录身份/时间/版本，活跃交易租约拒绝维护。该接口要求先停止所有交易节点，不提供在线热切换或跨进程自动配置切换保证。
- 新增五项真实 PostgreSQL integration/race：精确十进制/持久化元数据/副本隔离与幂等、并发版本竞争及批量回滚、活跃所有权和非法输入、true→false 写回与软删除拒绝、DDL 冲突整批回滚及校验和篡改拒绝。复用既有 infra-postgres 的隔离逻辑库，5.589 秒通过，资源按夹具清理；新模块纳入 make integration。
- 定向 runtimeconfig race、80 Proto 与五 Wire 比较、格式/布局/合同、lint/govet 0 issues、规范正反样例、49 项 Python、全包构建及 integration 标签全包编译通过。初始外部测试包依赖安排及三处 lint 风格问题已修正；未降低断言或放宽现有交易门禁。
- 本步仅完成数据库基础 API，尚未切换核心启动及删除 Nacos business 合同，继续 P28.2；不将部分基础设施完成宣称为整项完成。原有 50 处未提交业务修改保持，未修改当前运行数据库、Nacos 或交易容器。本步不重复运行完整全包 race/integration/e2e/soak。

## 2026-11-06：新增十七品种启动准备——有界多品种容量

- 用户要求启动 SOL、1000PEPE、ADA、AAVE、BNB、DOT、ARB、AVAX、DOGE、ETC、ETHFI、HYPE、OP、ONDO、SUI、SUN、WLD 的开发铺单及合成行情，并指定参考 Himalaya 的 `scripts/market_makers.py`（实际文件使用下划线）。只读 SDK 核验十七个 USDT 交易对均已注册、交易状态开放且当次健康正常；运行数据库目前仅配置 BTC/ETH，现有运行镜像仍是旧配置合同，尚未向新币发单。
- 将离线导入、数据库启动读取和有限合成运行的容量统一为 `native.MaxProfiles=32`，覆盖现有双币加十七币；启动最多读取三十三条以检测超限，超过上限拒绝而不截断。此常量是工作器容量边界，不替代数据库里的账户、额度、数量、频率及轮次参数。
- 新增 19/32/33 品种的 CLI 与合成合同回归；真实 PostgreSQL 验证三十二项整批导入、每项完整加载、新预算禁用、启动无写入、超限整批不写入及数据库超限拒绝。修改前 CLI/合成及实际导入均先失败；修改后四个相关包 race 通过，四项 PostgreSQL integration/race 通过（包 5.915 秒）。
- 八十份 Proto 和五个 Wire 比较、合同、格式、布局、lint/govet 0 issues，以及四核心和 venueinit 构建通过。隔离归档的格式脚本因无 Git 文件清单拒绝，随后在工作区运行原格式/静态检查通过；没有降低门禁。原有五十五份未提交文件哈希保持。
- 本步只交付容量基础；运行数据库、Nacos、原 SDK、账户资金及交易容器均未变。继续按参考工具读取账户和运行参数、完成数据库策略装配与配套部署，再记录真实挂单和合成发布；未运行完整 integration/e2e/soak，不将容量测试视为十七品种已启动。

## 2026-11-06：P28.2 数据库策略快照与实际存量升级准备

- Nacos 两份 YAML 全面移除 business，旧键即使关闭也拒绝；只保留连接、认证和进程配置。四核心在 Deer 的唯一数据库池就绪后同步读取独立启动快照，不回退 Nacos、本地文件或内存默认值。纯 venue_connection 选择通道模式；模拟器运行调度沿用 ServiceRuntimePolicy。
- 铺单和 WashTrade 复用 StrategyInstance/StrategyConfigVersion：每品种一个实例，schema=2 的 maker_parameters.venue_runtime 保存强类型参数，四核心共读同一组请求版本；账户、产品和财务限额只由 TradingAccountPolicy 提供，不复制资金参数。公共工作器控制必须一致，币种和合成数量/金额上限逐项合并，缺项、冲突、超限均拒绝。滚动替换比例和每侧交接容量同样由数据库提供。
- venueinit -strategies 显式执行整批离线维护，资料、不可变版本和启用在同一事务提交，任一品种修订冲突连同资金策略一起回滚；不调用 SDK、注资或改原生订单/仓位。整批真实回归先暴露嵌套事务继承错误 GORM 表作用域，改为保留事务连接的独立查询 Session 后通过。已有参数版本不可改写或删除，更新只能追加；维护拒绝活跃 owner，数据库修改不热切换现有快照。
- 标准通道路径显式追加 0008 的参数形状及历史保护约束，直接复用已有 maker_parameters JSONB，不改变核心迁移和列结构。旧库兼容回归先证明新增列会破坏未升级通道的普通策略，随后改为强类型 JSON 变体；原 V1 策略升级和有限成交回复丢失仍通过，未改用户未提交的 0038。
- 部署前实际读取发现运行库另有已执行的无审计 0007/0008，分别与现有隔离镜像中的原始 SQL 校验和一致；这两个旧升级此前已经删除订单、撤单、基线及合成槽表。本步保存精确历史定义，只有识别该已存在分支才追加 0009 的未来审计结构及 0010 的参数约束；不会重新执行 DROP、改写前八条历史或伪造已丢失审计。实际 PostgreSQL 回归验证预算/风险、ID/时间、持仓观察和历史 grace 值保持，未来审计为空且可重复升级（2.336 秒）。
- 最终全包 race 通过；末次事务/迁移调整后相关包 race 补跑通过。80 Proto、五 Wire 比较、格式/合同/布局、lint/govet 0 issues、50 Python、全包及核心/维护/模块夹具构建、含原用户修改的 integration 全包编译通过。SDK 16 文件保持。五项运行参数 PG/race 4.247 秒，六项整批/启动/财务维护 PG/race 5.767 秒，旧策略/回复丢失/版本维护四项 6.565 秒通过；已知历史路径单元验证任何待执行路径均不重放表删除。
- 通道相关目标用例均有通过记录；两次组合运行分别出现已有迁移 statement_timeout，按原期限及断言单独复核通过（6.303 秒），未声称已修复共享基础设施长期延迟。四项真实核心进程回归中三项首轮通过，一项首次风险许可拒绝、原样独立复核 2.50 秒通过；失败诊断保留。完整 integration/e2e/soak 未运行。
- 用户指定参考脚本的十七个独立账户均只读确认已注资，现存 26–40 笔报价均为对应 mm_ 前缀，未发现其他来源或运行中的 Rust 做市进程。部署输入私下保存，沿用 20 档、200ms、0.1 比例和 10 倍杠杆；开发合成继续每币千槽/秒级采样的有限边界。本步尚未改运行库、发布 Nacos 或重启容器；提交推送后再配套部署并记录真实结果。原五十五份用户文件除 README 合并双方独立修改外保持字节一致，暂存只纳入本步内容。


## 2026-11-06：P28.3 实际多币部署的进程工作器容量

- 显式维护已完成实际存量通道的 9/10 升级和十七个版本化策略整批导入；旧前八条迁移和 BTC/ETH 资金策略逐字段一致，原有共享容器未停止。首批小数带尾零与存储规范形式不一致导致整批回滚，改用同值规范十进制字符串后成功；无 SDK 注资或下单。
- 新 market 启动准确拒绝 34 个工作器，暴露 Deer 仍有旧的 32 工作器进程边界；在尚未启动新执行器时停止该容器，保留数据库审计和租约约束。上限明确调整为 64，满足最多 32 品种的盘口与聚合成交通道，超过 64 仍整批拒绝，不改业务额度或频率。
- 34/64/65 工作器回归先在原实现复现两个容量拒绝；修改后 Deer 全包 race 通过（2.249 秒），覆盖全量退出和资源释放、超限无部分启动。四核心构建、格式、布局、合同及 lint/govet 0 issues 通过。未重复无关 Proto/Wire 生成和全量交易集成测试。
- 本步提交推送后重建四核心并继续真实挂单及合成发布核验；本条不将镜像健康或参数导入视为十七币交易启动完成。用户原五十五份未提交内容保留。


## 2026-11-06：P28.4 十七币开发运行已启动与实际边界

- 从已推送的 851aa8d 隔离归档构建四核心镜像，未包含用户未提交业务修改。先停止旧策略与行情，让原执行器依现有保护撤完 BTC/ETH，再停止其余进程并等待所有租约过期；旧 BTC/ETH 挂单为零，旧资金策略含 ID、时间、风险观察、预留及 grace 逐字段保持。共享容器没有执行停止/重建；前后 ID/启动时间核对仅原已重启循环的 Pulsar 自行变化，PostgreSQL/Redis/Nacos/NATS 和所有交易所组件保持。
- 已显式升级实际通道历史分支至 10，保留前八条迁移原记录；17 个数据库实例均 requested_config_version=1/revision=1，选择 SOL、1000PEPE、ADA、AAVE、BNB、DOT、ARB、AVAX、DOGE、ETC、ETHFI、HYPE、OP、ONDO、SUI、SUN、WLD 的 USDT 市场。Nacos 八份基础设施 YAML 已配套发布并回读，无 business；账户绑定、20 档、200ms、0.1 比例、10 倍杠杆和有限合成参数由数据库启动快照提供，没有 SDK 注资。
- market/execution/portfolio/strategy 依次启动，最后检查四容器 healthy、重启次数均零。每个币均有真实 SDK 原生 ID 的 BUY/SELL 意图，以及下游 GetRecentTrades 与 SDK 回执 trade_id 匹配的合成行情；旧 mm_ 报价已清空。合成成交是开发行情发布，不产生账户撮合或仓位成交。
- 约 08:30 UTC 核验十七币合成发布总计 1045 条，ONDO 一条合成槽结果未知，保留消耗且不重发；每币仍以 dev-20261106-17 的最多 1000 槽、至少 1 秒间隔为限。金融 DISPATCHED 和未确认撤单超过五秒的数量均零，没有新增未知审批、释放未知预留或重置运行槽。
- 实际铺单尚未稳定达到每侧二十档：抽样每币为 0–6 笔，存在行情/计划失效和撤单确认期间的保护窗口；SUN 行情间歇恢复，但已有双边原生下单及合成回执。不将进程健康、历史下单成功或配置深度宣称为持续满档，未绕过新鲜度、最终 fencing 或未知恢复合同。此轮完成启动；持续深度和长期稳定性尚未验收。
- 相关全包/目标 race、PG 行为回归、生成比较、构建和静态检查见 P28.1–P28.3，本记录为真实运行与只读对账证据；未运行完整 e2e/soak。运行私有输入、备份及日志仅存忽略目录，公开提交不包含凭据和资金账户资料；原五十五处用户修改保留，README 仅自身两条链接仍未暂存。


## 2026-11-06：P29.1 用户纠正启动范围，先修浅盘口撤补循环

- 用户指出 BTC/ETH 不应随新增十七币停用，且其余盘口缺深度。上一轮把“其他可以不启动”扩大成停掉已运行策略，并以存在下单/合成回执报告启动完成，验收范围不够；本轮按原账户及资金限额恢复 BTC/ETH，不增加资金或继续展开参数架构改造。
- 逐笔 SDK 下单实测约 200–400ms，定位到滚动刷新在每侧只有 2/6/17/18/19 档时仍先撤固定批次，深度无法积累。新增这些浅盘口回归在原实现均复现丢失有效档位；仅调整现有报价保留函数，按唯一有效价位计算可花费的深度，先补到原配置的保留水位再滚动刷新。重复、交叉、超额和 legacy 订单不作为安全深度保留；预算、来源时效、最终 fencing 和 UNKNOWN 合同保持。
- execution/venue 全包 race（1.453 秒）、执行器及维护构建、格式/合同、lint 0 issues 通过。六个原生工作循环场景中四项首次通过；二十档刷新首次源失效撤单观察未收敛、回复丢失夹具遇迁移 statement_timeout，按原期限和断言独立复核两项通过（13.659 秒），保留首次失败日志，不把共享环境负载波动说成已解决。另一次首个夹具迁移超时同样保留记录，未改测试合同。
- 二十档检查仍验证取消证明屏障期间至少 18/18，随后收敛并在源所有权丢失后全撤。部署尚待本步提交推送后进行，以真实连续深度复核；BNB/ETC 新出现的各一笔 DISPATCHED 仍保留全额风险，SDK 活动及终态历史未找到，不套用此前旧未知清单的审批。


## 2026-11-06：P29.2 BTC/ETH 恢复，持续深度仍未验收

- 已部署 e6b40b0 的浅盘口修复；配套停止生产者并由原执行器受控撤完十七币，等待租约过期后，只追加 BTC/ETH 两个数据库策略实例。原十九个账户的绑定、单笔/总额度、杠杆、tick/step 和其他资金政策逐字段核对保持；没有注资、平仓、清零预留或覆盖原十七个参数版本。BTC/ETH 复制原合成单次上限 0.001/0.01 与原频率，加入当前 dev-20261106-17 的每币千槽有限运行，不重置其他币的已消耗槽。
- market、execution、portfolio、strategy 已重建配套镜像并恢复，四容器健康。BTC/ETH 均恢复双边原生订单及合成发布；末次样本 BTC=19/19、ETH=19/8，瞬时满档不视为持续满档。多币浅盘口修复后出现 18–20 档，仍有短边和全撤恢复窗口，持续十九币深度验收未完成。
- 采样证实执行器原八个数据库连接均被活跃或跨 SDK 调用事务占用，共享 PostgreSQL 同时存在 WALWrite/WALSync 等待。在全局 max_connections=100 且留足余量前提下，仅调整 execution 的基础连接池为 32/8 并显式重启，其他进程保留。两个短观察窗口对未阻断品种的短边中位数为 12/13，样本环境变化，不能证明单独扩大池已解决性能问题；没有关闭 fsync、同步提交或持久化审计。
- 用户明确选择不隔离 BNB #8555 与 ETC #6737；保持两者 DISPATCHED、原 client_id 和全部 152.0288/106.27227 USDT 名义金额预留，审批引用仍为空，未重发。另有 1000PEPE #27869、SOL #29584、ONDO #29585 在此次连接池调整之前运行时进入新未知，当前也按既有门禁阻断；未套用旧审批或清单，也未将这些币报告为正常补单。
- 实际双边、合成回执与当前原生深度仅作为局部证据，不再把“参数已配置”“四容器 healthy”或“出现过订单”写成全部盘口达标。仍需解决真实链路在新鲜度/发送窗口及未知恢复方面的运行问题，此轮没有新增框架或重划资金事务。
- 私有配置/SDK 观察和逐轮深度样本留在忽略目录；除此前已自行重启的 Pulsar 外，所有共享容器的 ID/启动时间保持。原五十五处用户修改保持（非 README 五十四份逐字节校验，README 自身两条新增链接未暂存）。本记录只运行文档差异检查；代码 race、构建、静态及 PostgreSQL 回归结果见 P29.1，没有执行完整 e2e/soak。


## 2026-11-06：P30.1 按用户要求取消固定实例数量限制

- 用户明确改为 strategy_instances 每行保存类型、数据源、实现标识和完整参数，不设策略模板表；本步先完成可独立验证的容量修正，后续继续三类型拆分和数据迁移，不将本步作为整体迁移完成。
- 删除每进程 64 工作器、通道 32 配置、每账户 16 策略/风险候选、128 库存策略、16 运行账户和行情依赖等固定数量门槛，数据库读取全部结果而不截断。保留唯一性/参数合法性、风险金额、有限开发轮次、所有权、网络消息大小和连接并发合同；这些不再按实例数量拒绝启动。
- 33/65/257 规模的 CLI、合成配置和完整工作器生命周期回归先在原边界失败；修改后七个相关包 race 通过，四核心及维护命令构建、格式、合同和 lint 0 issues 通过。真实 PostgreSQL 验证 257 个品种整批导入/全量加载及新增第 258 个，原资金状态、启动只读及非法尾项整批拒绝保持（9.23 秒）；单账户 257 个实例包含停止请求、隔离其他账户且未产生执行/租约副作用（3.442 秒）。
- 大规模测试最初逐项提交导致夹具 15 秒超时，改为同一测试事务批量准备，原期限与断言保持。嵌套准备又暴露既有 GORM 表作用域继承问题，改为无嵌套的强类型批量实体写入后通过；首次失败日志保留，没有降低断言。原五十五处用户修改未纳入提交，非 README 文件字节校验保持。
- 当前运行库完整私有备份 14,321,448 字节已保存到忽略目录；此步未停当前服务或执行数据清理。后续新结构和迁移将保存订单、资金预留、未知审计和既有合成槽身份，再切换运行库。

## 2026-11-07：P30.2 策略模板、独立实例与可核验迁移

- 用户最新明确要求建立策略模板，替代此前“不建模板表”的选择。新增 Proto/GORM `StrategyTemplate`，显式写入铺单、网格、WashTrade 三行，保存实现标识、所需输入、具体 Proto 参数字段合同、启用和修订；实例以 template_id/template_revision 关联，并独立保存完整具体参数。模板没有隐式运行参数覆盖，当前参数不从历史版本或 Nacos 回退。三种算法实际注册，网格共用精确算术价格逻辑并补充期货双向库存/总预算提交检查；没有凭空创建或启动缺少有效实参的网格实例。
- 四核心分别从同一借用数据库池读取只读启动快照，按实际实例和模板依赖装配；移除此前公共参数必须完全一致的合并。执行器共用一个 SDK 会话、每账户预算和门禁，各实例维护独立报价有效期和订单前缀；最终 SDK 事务检查实例、模板修订/启用、账户、产品和所选来源，保留原执行/策略/行情 fencing。其他实例订单纳入资金和交叉检查，但不撤销；受控旧做市切换仍能撤销原 legacy 报价。
- 新增显式 `instancemigrate` 和 `venueinit -instances`。新基线只安装十三张有用表，旧三个迁移账本按原 ID/时间/版本/校验和归入不可变 MigrationHistory；只为确实执行的新基线记录当前迁移。已有 StrategyConfigVersion 只做审计，原始迁移 SQL 和用户的未提交 0038 均保留。旧全局初始化工具拒绝向新工作库补建已清理的运行配置表。
- 迁移拒绝活跃源库、未知表或未支持表的非空数据；目标 DDL、复制、实例转换和核验一个事务提交。生成模型逐批比较资金、订单、撤单、恢复基线及合成槽的全部字段，保留序列高水位。原铺单身份保持，嵌入的开发 WashTrade 拆分为独立实例；现有运行摘要绑定转换参数，从包括 UNKNOWN 在内的全部已消耗槽继续，不重发旧槽、不批准未知、不释放预留。
- 六项真实 PostgreSQL integration/race 通过（干净发布候选 26.301 秒）：未知状态/全额预留/历史/有限槽保持，活跃及非空未支持数据整批拒绝，过期实例最终 SDK 调用零次，260 个不同参数实例覆盖三算法且精确重复/冲突批次保持，数据库模板及修订绑定和缺失模板拒绝，序列高水位保持。工作区模板版本回归亦通过（10.920 秒）；初始小数填充精度、旧表新默认字段 RETURNING 及测试结构问题已修正，未改断言或放宽期限。
- 发布候选全包 race、全量构建、83 Proto/五 Wire 比较、格式/布局/合同、规范正反例和 lint 0 issues 通过；最终撤单所有权补充另跑相关 race。原工作区全包 race 曾命中用户未提交的 FlatteningId 事件校验断言，该原有测试及业务修改保持；部署候选仅包含本步内容，在其原基线和全部本步改动上全包通过。SDK 源码不变，完整 integration/e2e/soak 未运行。
- 本步尚未停止运行服务或切换实际数据库；提交推送后执行最终停机备份、迁移和真实重启核验，后续单独记录。源库预备完整备份保留。原五十五份用户修改及另两份文档链接未纳入提交，非 README 字节校验保持。结构与维护说明见 [策略模板与实例](STRATEGY_INSTANCES.md)。

## 2026-11-07：P30.2.1 保持执行安全观察节奏

- 部署前核对恢复路径，保持原有 500ms 执行安全观察周期，策略计算仍使用各实例自己的数据库轮询参数；恢复警告仅在进入失败状态时记录，避免多实例按 200ms 计算频率重复查询和持续刷日志。没有调整资金、未知处理或业务频率。
- execution/venue race、执行器构建、格式与 lint 0 issues 通过。本条修改在实际切换前完成；源库仍有十七个未批准未知账户，本轮不会批准或清零它们。

## 2026-11-07：P30.3 实际模板入库、工作库迁移和重启完成

- 已推送并从隔离归档构建 0365e29/de345c7 的四核心镜像。旧镜像的 Docker 内容索引缺失，按 ID 备份及 commit 均失败；在尚未停机时导出当前容器可运行文件系统，并导入带原入口的本地回滚镜像，未捕获运行环境中的口令。四份镜像与文件系统归档保存后才停止策略/行情，由原执行器受控撤单，挂单从 61 收敛到零，再停执行/持仓并等待全部租约过期。最终完整 PostgreSQL 备份 24,519,556 字节已校验目录，私有保存。
- 首次真实迁移按设计拒绝旧库的非空停用表，目标事务回滚为空；此前行数估计没有反映这些实际种子记录。逐表实际计数查明八类旧记录共十八行：禁用且预留为零的旧预算、无余额/订单/成交/命令的模拟账户、过期行情租约及缓存，以及已移除管理入口的用户/会话/审计。全部原记录留在只读旧库和完整备份中；仅在本轮隔离逻辑副本恢复时，通过明确 TOC 清单移出这些旧记录，原生资金/订单/未知/合成槽逐项核对与原库一致，再通过未放宽的严格迁移门禁。自建临时逻辑库已在切换验证后删除。
- 当前工作库为 muskdeer_instances_20261107，只含十三张工作表，strategy_templates 已保存铺单、网格、WashTrade 三种完整实现/数据依赖/参数合同；strategy_instances 为十九个铺单与十九个独立开发 WashTrade，共三十八个启用实例。网格模板可见且实现已注册，没有伪造价格区间或新增网格运行。原实例 ID、资金绑定和额度保持；迁移前 129,110 条订单意图、83,133 条撤单、8,536 个合成槽、二十条产品资料及十九个持仓观察逐项核对保持，零条恢复基线也保留所需表。三个旧迁移账本完整归档，不重放旧 DROP 或重置身份。
- 原 muskdeer_local 已设默认只读，防止旧连接继续写入；四服务 Nacos 仅切换基础数据库名和版本，八份 YAML 配对校验及有界回读通过，无 business 参数回流。立即回读曾碰到 Nacos 缓存旧值，保持服务停机并等待配对内容收敛后才启动。market、execution、portfolio、strategy 已依次恢复，末次四容器 healthy、重启计数均零，三种模板和三十八实例均能从当前库核对。
- 启动后的观察已出现 329 条新原生意图，末次全部取得确认或终态；开发合成槽在原 8,536 条基础上继续，观察增加 760 条，保持原 run_id、每币最多一千槽和未知槽计入消耗，不重放旧槽。中间一笔新 DISPATCHED 随后得到正常原生确认，末次仍有十六个账户被原有未批准未知阻断；没有隔离、批准或重发。BNB #8555 与 ETC #6737 仍为 DISPATCHED、审批引用为空，完整 152.0288/106.27227 USDT 名义预留保持。
- 持续盘口深度仍未验收：末次 HYPE 样本为 5/19，其余多数为空；服务健康、实例装配及出现过下单不作为满档证明。十六个未知账户保持业务阻断，未绕过资金/来源/最终 fencing。合成发布只进入开发行情，不冒充账户撮合成交。
- 共享 PostgreSQL、Redis、Nacos、NATS 和交易所组件的容器身份/启动时间保持；仅此前已有重启循环的 Pulsar 自行继续重启。本轮未停止或删除共享基础设施。原五十五处用户业务修改及另外两份文档链接保留，非 README 内容再次逐字节验证。代码/PG/race/生成/构建/静态验证见 P30.2/P30.2.1；本条是实际迁移与运行核验记录，仅运行文档差异检查，没有将完整 integration/e2e/soak 写成通过。

## 2026-11-07：P30.4 全部已配置做市启动检查

- 按用户要求对四核心执行幂等启动；原容器已在运行，未停止、重建或重启。四项 readycheck 均通过，四容器 healthy、重启计数零，Nacos 四服务连接均指向新工作库。十九个做市铺单实例全部 desired_running、关联账户 enabled，十九个策略执行租约均有效。
- 末次观察仍有十六个账户受未批准 DISPATCHED 结果阻断，观察期间瞬时为十七个；这与启停开关不同，不因重复启动消除。BNB/ETC 原两笔未知和全额预留再次核对保持，没有隔离、审批或重发。末次 HYPE 活动挂单样本为六买四卖，其余十八币样本为空，不能宣称全部正常铺单或深度验收通过。
- 本轮仅做运行核验，完整业务参数及有限开发合成运行身份、配额保持。私有证据保存在忽略目录；仅文档变更运行 git diff --check，代码测试未运行。

## 2026-11-07：P30.5 按用户要求撤销挂单并核对未知结果

- 用户要求全部撤销。先停止 strategy 进程防止补单，由仍在运行的 execution 通过原有持久化撤单、所有权校验及独立终态观察处理挂单。十九个配置账户先后两次原生活动订单查询均为零；没有平仓或修改余额。market/execution/portfolio 继续 healthy，strategy 保持暂停，数据库实例启停配置未被清空。
- 十六笔未批准下单意图均没有原生 order_id。逐账户历史查询扫描到游标末尾（BTC/ETH 各四页，其余各一页），没有找到对应 client_id；当前 SDK CancelOrder 只接受原生 order_id，不能为这些缺失身份的意图构造有效撤单。另十七条未确认撤单按已知 order_id 定向查历史亦无匹配；没有将活动列表为空或撤单已发送视为终态证明。
- 十六笔未知的原 ID、DISPATCHED、空审批引用和完整预留逐项核对不变，含 BNB/ETC；没有隔离、审批、重发或手改终态。当前可确认的是活动挂单为零，历史未知仍待通道提供接单/拒单/终态证据，不能宣称全部未知已撤销。原始查询证据私有保存；本轮仅文档差异检查，代码测试未运行。

## 2026-11-07：P31.1 做市按当前通道状态运行

- 用户最新要求以撮合侧为准，不等待本地历史对账，全部做市恢复。当前独立实例报价只读取通道当前订单/持仓/可用资金，不在报价路径查询历史证明，不因旧 DISPATCHED 或缺失撤单回执冻结整个账户。该要求取代此前 BNB/ETC 整账户阻断约定，AGENTS 与实例文档已明确优先级；原未知状态、身份、审批引用及名义预留审计继续保留，没有伪造终态或重发。
- 当前敞口随新鲜通道观察更新；入场同时覆盖完整准备中/未确认请求，以及观察开始后刚确认的请求。已确认旧请求由当前订单/持仓体现，不再用累计历史审计二次阻止报价。基金行锁序列化总预算；单笔/总额、保证金/费用、来源和实例最终 fencing 保留。重复活动观察不改接单确认时间，避免把旧接单不断误计为新请求。旧运输层测试缝继续验证原历史合同，不用于当前服务装配。
- 新增真实 PostgreSQL/race 回归，先复现报价错误依赖历史查询，再验证历史服务不可用仍报价、旧未知和撤单保留、累计审计不冻结当前额度、当前挂单/持仓全部计入敞口、未观察请求仍占预算、重复发送零次、过期观察拒绝。检查结果与实际部署另记；本步不改数据库结构、策略模板或具体参数。
- 干净候选全包 race、格式/布局/合同、lint 0 issues、83 Proto/五 Wire 比较、规范正反例及全部构建通过。七项当前实例/迁移 PostgreSQL/race 通过；另十四项旧 SDK/费用/预算并发/最终所有权/历史审批边界 PostgreSQL/race 通过（55.254 秒），原历史断言保持。测试夹具初次暴露缺少延迟外键配对，改用正式实例维护入口构造；测试拆分降低复杂度后重新编译核验，没有降低业务断言。原用户未提交平仓代码未进入候选；完整 integration/e2e/soak 未运行。

## 2026-11-07：P31.2 全部铺单及有限开发行情恢复

- 00fbf4b 已推送并从提交归档构建新的 execution 镜像，旧执行器镜像单独备份。只更新执行器二进制，另外三服务沿用原已验证镜像。四服务 Nacos 配置逐份哈希相同，继续使用现工作库，没有新增迁移或重建业务表。
- 十九个开发合成实例中十五个已达到原批次一千槽上限。按本次全部启动要求，先保持策略暂停并确认十九账户活动挂单为零，再短暂停止另外三服务、等待全部租约过期，保存 27,145,508 字节完整备份。通过正式离线实例维护入口，为十九个开发实例建立新批次 dev-1107-121223，每实例仅运行标识改变，沿用原数量、频率、数据源、累计量和一千槽上限；追加十九份版本历史，不重置或重放旧批次。十九铺单实例的参数和修订完全不变。维护前后资金、原未知、撤单及旧合成槽逐项一致。
- market/execution/portfolio/strategy 依次恢复，四项 readycheck 通过，四容器 healthy、重启计数零。十九铺单及十九合成策略租约均有效；末次观察十九币全部有新增挂单确认，合计 6,414 条确认，十九合成品种均取得发布回执，新批次已确认发布 602 条。BNB/ETC 已恢复报价，原十六笔未知的 ID、DISPATCHED、空审批引用和完整名义预留仍保持；没有伪造撤单终态、审批或重发旧请求。
- 深度持续满档仍未验收：多次抽查中十七币出现过双边挂单，BTC 曾为 20/20；末次 DOT 为 4/0、SUN 为 0/0。两币均有新增确认，日志仍可见 LEASE_LOST 与撤补波动，持续深度原因尚未完整定位；不再因旧未知记录形成账户停机。未把启动、出现过挂单或有限测试行情等同于连续二十档或账户真实成交。
- 共享基础设施和交易所容器身份/启动时间保持，原有 Pulsar 自行重启除外。旧只读工作库未修改；原五十四份非 README 用户业务文件及另外两份用户文档链接逐字节保持，README 用户修改保留。原始运行、备份、配置哈希和逐币观察保存在私有忽略目录；本条仅补充运行记录，文档差异检查通过。

## 2026-11-07：P31.3 修复发布后重启的实例校验错误

- 用户反馈 SOL 没有运行。现场确认其活动挂单为零、近五分钟无新意图，四核心均 unhealthy 且各已累计重启一百三十四次，启动报实例配置非法。停止四个重启循环后只读核验：十九个开发批次均连续、单摘要、未超一千槽；十九份已存摘要全部与执行器附加连接身份后的策略精确匹配，却与缺少连接字段的启动计算不匹配。首次运行时尚无槽可校验，所以先前启动和短期观察通过；此前测试未覆盖非空发布后的实际连接身份。
- 原因是执行器在发布前附加 Target/TokenEnv，而 resumeTape 使用未附加连接的 Settings 计算摘要。修复 Load/Import 显式借用同一强类型 VenueConnection，四核心公共装配及正式维护命令均透传。只对齐已有摘要校验输入，不改数据库结构、参数来源、未知订单策略、旧摘要、运行标识或已消耗次数。
- 新增真实 PostgreSQL/race 回归：按执行器连接形成三条已消耗 UNKNOWN 槽，连续三次加载都从第三槽之后继续；错误连接仍拒绝，原参数重复维护不追加审计，同批次修改参数原子拒绝，全部原槽逐字段不变。现场只读探针已复现旧失败并验证新加载逻辑；最终检查和部署结果另记。
- 干净候选全包 race、八项当前实例/迁移 PostgreSQL/race、格式/布局/合同、lint 0 issues、83 Proto/五 Wire 比较、规范正反例及全部构建通过。原五十四份非 README 用户文件和另两份用户文档逐字节保持，未将未提交平仓修改纳入候选。四镜像和当前数据库已备份，完整备份 32,803,209 字节；部署后将继续验证原批次重启与实际 SOL 挂单，不以构建或健康检查代替持续运行观察。

## 2026-11-07：P31.4 SOL 实际恢复及发布后重启复验

- 已推送 3bf1816，并从提交归档构建、替换四核心镜像。四服务实际连接身份已逐份比较一致；沿用当前数据库及三十八个实例的完整原参数、修订和运行标识，没有清空订单、改旧摘要或开新测试批次。market/execution/portfolio/strategy 依次启动，四项 readycheck 通过。
- 带已有发布记录恢复后，再对 strategy 执行一次人工重启；重新加载原批次成功。随后八十三秒内三次独立 SOL 活动订单观察分别为买卖 5/4、19/19、19/19，恢复后的 SOL 下单确认从 112 增至 358，末次新意图距离观察时间约 319ms。SOL 合成槽从原 241 继续至 297，序号连续、摘要唯一、旧槽继续计入一千次上限。三十八个策略租约有效，四服务各次 healthy、自动重启计数零；这是有限恢复观察，不冒充长期满档验收。
- 维护前四十一笔未确认意图的名义预留和审批引用保持；其中六笔在收到匹配的原生订单观察后正常进入 ACKNOWLEDGED，没有手改终态或批准隔离。原十六笔历史未知不因本次重启被清理，也不重新形成整账户停机。四服务参数快照逐项保持，源只读库及共享基础设施/交易所容器未修改，已有 Pulsar 自行重启除外。
- 造成重复启动失败的摘要输入差异已现场复现并修复；最初触发服务重启的因素尚未完整定位。运行日志、完整备份和逐次挂单/槽号观察私有保存；代码验证见 P31.3，本条仅补充实际部署及重启复验，文档差异检查通过。

## 2026-11-07：P32.1 修复报价维护主动造成的深度断档

- 用户反馈盘口仍断档。现场四服务 healthy、无自动重启，SOL 仍有新下单；连续四十五个活动订单/行情样本中行情查询全部成功，帧剩余有效期 1,308–1,486ms，但买单从二十档降至七档，末段源最优价不变且有连续撤单。代码将上一帧 1.5 秒有效期当作所有挂单的清空期限，而一次维护最多运行两秒；维护还在补单前批量撤旧，导致健康源下的深度反复下降。
- 当前实例保留挂单的许可改为已接受策略所有权、源身份及配置租约期限；保护循环逐次检查当前新鲜源、双方所有权及报价是否仍安全，旧帧过期不再等于整批撤单。策略/源失权、当前行情不可用或账户禁用仍受控撤单，新 SDK 提交仍保持原有行情期限、预算及最终 fencing。保护扫描复用一次账户观察，移除重复 SDK 查询。
- 报价维护使用已有交接档位先补，重新读取实际活动订单后才退同价旧单或安全多余价格档；回执、本地 admitted 或未观察目标不算可见替代。每次退单按全部已计划撤单成功的最坏情况计算剩余价格档，不为维护主动降到目标深度以下；无交接容量继续遵守显式替换比例。源交叉/超限旧单及实际成交不以维持档数为由保留。
- 回归在旧期限/旧撤单选择下分别复现：四十笔安全挂单全部被撤，或无可见替代时先撤四笔。修复后相关 race 与真实 PostgreSQL 回归通过，包含跨过旧帧时限仍保留四十笔、每次实际撤单都维持双边二十个唯一价格档、只撤新增不安全单、失去策略所有权后撤全部。完整检查和部署观察另记；不修改数据库、模板、铺单具体参数或合成批次。
- 干净候选全包 race、当前执行/实例迁移 PostgreSQL/race、三项旧源失权/滚动换量/最终 SDK fencing PostgreSQL/race 均通过；旧滚动测试仍保留双边至少十八档断言。格式/布局/合同、lint 0 issues、83 Proto/五 Wire 比较、规范正反例及全部构建通过。首次 lint 报测试条件链可改 switch，按建议整理后完整复验通过；没有降低深度、安全或期限断言。原五十四份业务文件及两份用户文档逐字节保持，未提交平仓工作未进入候选。完整 integration/e2e/soak 未运行，实际持续深度待部署对比。

## 2026-11-07：P32.2 执行器更新、连续采样及尚存深度原因

- 6b1703a 已推送，执行器由提交归档独立构建；旧镜像已保留。先停 strategy，由旧执行器将当前 265 笔报价撤净至零，再停 execution、等待报价租约过期并完整备份 64,225,581 字节；只替换 execution，market/portfolio 未停，strategy 恢复原镜像。第一次紧接 start 的策略探针尚未就绪，随后四项 readycheck 与容器健康均通过，自动重启计数零。
- 三十八个实例全部字段、四服务 Nacos 配对内容哈希、账户业务额度与配置不变；账户的活动敞口/可用额/观察期限随真实挂单正常更新。维护前一百二十六笔未确认记录的身份、全额名义预留及审批引用保持；允许之后的正向原生观察推进状态，没有伪造终态或清零。全部旧开发批次、消耗和摘要保持；已到原一千槽上限的实例自然结束，其余继续原轮次，不建立新批次。十九个铺单租约有效。
- 首轮 SOL 一百八十次约七十九秒采样显示冷启动从 1/1 逐步补至 20/20，之后多数为 20–21 笔；不把冷启动浅档计为运行稳定。第二轮一百八十次约七十八秒采样按唯一价格计为买侧 13–21、卖侧 12–21，行情查询无错误、没有空边；快速源价格跨越旧报价时仍有受控撤单和补充延迟。因此持续双边二十档未验收，不能用这段改善宣称全部品种已满档。
- 现场还观察到 market 多品种在同一时段断开并重新获得行情，随后多个盘口再次变浅；四进程没有退出或重启。SUN 单独源探针明确收到十八帧后超过两秒无新帧，现有读期限触发 i/o timeout，旧帧也已超过 1.5 秒业务新鲜度；同批 SOL/DOT 探针在三十五秒主动截止前分别收到二百八十七/一百七十七个合法帧。未放宽行情期限、保留失效报价或虚构外部更新来掩盖断档，连接中断原因仍需进一步定位。
- DOT 的浅卖盘有独立原生证据：一次最近三十笔历史全部为 ORDER_CANCEL_TYPE_POST_ONLY，后续三十笔仍二十五笔为该原因，均没有本地撤单记录；这些请求获传输确认但未成为活动挂单。不能把 ACK 或不断新建意图等同于盘口有深度；此步未修改撮合、关闭 PostOnly、改变外源定价或撤其他账户订单。SUN 最近三十笔为应用受控撤单，与源失效观察一致。
- 源工作库继续只读；共享基础设施和交易所容器身份/启动时间保持，既有 Pulsar 自行重启除外。原五十四份非 README 用户业务文件和两份文档逐字节保持，README 用户改动保留。配置/订单/逐次价格档样本、源探针、备份及审计私有保存；本条仅为部署记录，代码回归见 P32.1，持续全品种深度尚未完成。

## 2026-11-07：P33.1 开发 WashTrade 零额度表示不限制

- 用户明确要求做市刷单无需额度限制，有限制设零。本步将 `maximum_events=0`、`maximum_notional="0"` 分别定义为无累计次数/名义量上限，正数仍生效；取消校验、策略循环和执行端历史扫描的一千次固定截止。单次数量、比例、频率、数据源、development_only、不可重发及最终 fencing 保持，不新增真实账户对刷，也不修改铺单资金预算。
- 运行序号由 uint32 扩为 uint64，数据库继续使用原 bigint，转换前检查存储范围，避免长期循环回绕。原 run/symbol/sequence 唯一键保持；执行端用有界分页遍历全部已消耗记录，验证连贯序号、摘要、金额、原来源及未知审计，没有以分页大小截断总次数。
- 已消耗实例可以通过正式离线维护仅把两个额度改成零，保留原批次和历史策略摘要并绑定完整新参数，追加实例修订及参数审计；重复维护幂等，其他同批次参数变更仍拒绝。已有 UNKNOWN 继续占用序号，不改旧槽或重发；四核心公共加载从原序号之后恢复。
- 新增当前工作库版本二和旧通道升级 SQL，仅将序号 CHECK 从 0–999 改为非负；原版本一内容与校验和不变，没有 DROP TABLE 或删除历史。新安装显式安装完整版本；旧工作库启动只读拒绝缺升级。`venueinit -instances` 在同一维护事务执行升级及参数导入，有有效所有权拒绝升级。
- 零限制原测试先失败后通过。干净候选全包 race、真实 PostgreSQL/race 的一千零一条旧未知之后继续发布/旧槽不重发、九项实例迁移/维护/重启/最终版本检查，以及当前执行完整相关套件通过；版本一升级保留原账本和旧槽，有活跃所有权拒绝、重复升级幂等，负序号仍拒绝。格式/布局/合同、lint 0 issues、83 Proto/五 Wire 比较、规范正反例和全部构建通过。
- 初次数据库回归暴露了原 CHECK 限制及新增嵌套维护的 GORM 表作用域继承问题，已通过版本化 DDL和独立 Model 会话修正；测试夹具的奇偶方向与 PostgreSQL numeric 格式按正式构造和持久化快照比较修正，没有降低历史保留或去重断言。共享 PostgreSQL CPU 曾超过两核，旧迁移夹具在并行检查期间触及原两秒 statement_timeout；静态检查结束后隔离重跑通过，期限不变。原五十四份非 README 用户业务文件及两份文档保持，未提交平仓工作未进入候选；完整 integration/e2e/soak 未运行。实际十九实例零额度维护及发布后续另记。

## 2026-11-07：P33.2 十九开发实例零额度实际生效

- 3bd8b00 已推送并从干净提交归档构建四核心镜像，原四镜像分别备份。暂停 strategy 后由原 execution 将 645 笔活动报价撤至零，再停止其余三服务、等待全部租约过期，保存 83,461,348 字节完整备份；未操作共享基础设施或交易所进程。
- 通过新镜像的正式 `venueinit -instances`，在同一事务将当前库从原版本一显式升级为版本二，并为十九个开发 WashTrade 实例去除次数和累计名义量上限。沿用原批次 dev-1107-121223、全部账户绑定、来源、数量、比例、频率和 development_only，仅这两个累计额度为零并各追加一份修订审计。原版本一账本所有字段保持，当前仍为十三张表；旧运行库保持只读。
- 维护期间全部财务事实、旧未知与撤单、合成槽逐项相同，十九个铺单实例全部字段不变；没有清零配额、新建批次或重发未知。四服务 Nacos 配对内容哈希不变，业务参数仍由当前数据库提供。维护前二百二十八笔未确认意图的完整名义预留及审批引用在恢复后的两次核验保持。
- market/execution/portfolio/strategy 依次更新并通过 readycheck，四容器 healthy、自动重启计数零，十九铺单及十九开发策略租约均有效。恢复后十九品种均取得新的合成行情发布回执；首次逐币新槽观察已有 148 条确认发布，包括 SUN。两次完整核验间隔 56.708 秒，SOL 从维护前一千个已消耗槽继续至 1,006、1,028，最新序号 1,027；其余十八品种也均在原消耗基础上增长，原摘要仍唯一、序号连续，没有在旧一千次处再次停下。
- 原五十四份非 README 用户业务文件及两份文档逐字节保持，README 修改保留；共享容器身份与启动时间保持，既有 Pulsar 自行重启除外。完整备份、配置哈希、维护清单、逐次审计与新发布回执私有保存。本条仅补充实际操作，代码和数据库回归结果见 P33.1；没有把开发行情发布视为真实账户成交或持续满档盘口验收。

## 2026-11-07 独立管理后台接入：核心 Console

- 按用户要求复用独立 admin 框架，并同时支持平台与商户；不恢复核心旧 admin/operator 进程或管理用户表。
- 新增策略 Console 强类型 gRPC：模板/实例/参数/启停、品种、账户策略、订单和撤单审计、开发发布记录、撮合原生快照及停止实例撤单。只在 strategy mTLS 允许 operator.grpc 时注册；商户范围由独立 operator 提供，空范围拒绝。
- 当前参数与控制版本分别校验；配置/控制历史继续写入原不可变审计表。加载停止实例，策略/执行循环检查当前 desired/revision；暂停开发发布不消耗序号。新增维护迁移 0003：开发 run 身份唯一，配置和控制版本分离；现有服务/数据尚未升级。
- 验证：全库单元测试通过；相关包 race 通过；真实隔离 PostgreSQL 的范围隔离/并发 CAS/停后装配/审计回归通过；生成一致性、格式及全库 lint 通过。独立 admin 页面与权限链路仍在开发，未部署新控制入口，未更改实际做市运行状态。

## 2026-11-07 管理入口部署打包

- 核心 Console 的显式 instanceupgrade 已加入 Docker 构建、运行镜像和许可证依赖清单，避免仅本机构建有维护命令而部署镜像缺失。四核心使用同一业务代码版本；Docker runtime-strategy 构建通过，其余目标复用同一构建层。
- 用户已要求部署并交付登录账号。当前进入备份、Nacos/mTLS 接入和双端实际部署验收阶段，不把源码/测试通过冒充运行环境已接通。
