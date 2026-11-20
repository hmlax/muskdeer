# 做市管理部署

平台与商户端复用既有 Hertz/admin → Kratos/operator，operator 使用 mTLS 调用独立 Muskdeer strategy Console gRPC。管理数据库不复制业务表、不配置业务数据库 DSN；只新增商户与做市账户绑定。

## 配置和部署顺序

1. 备份管理库、业务库和 Nacos 配置。四核心升级使用对应工作分支，停止并排空受管理的挂单、等待租约过期后运行 `instanceupgrade` 升级独立实例 schema 到版本 3。维护命令仍只从 env 连接 Nacos；不重建账户、订单或开发批次。
2. 为 operator 配置同一 CA 下精确 `operator.grpc` SAN 的客户端证书。strategy 的 `grpc_security.allowed_peer_services` 显式加入 `operator.grpc` 后才注册 Console。保留核心间原来的身份认证和最终 fencing。
3. 首次使用空管理库运行 `go run ./operator/cmd/muskdeer-bootstrap`；已有框架库升级时运行 `go run ./operator/cmd/muskdeer-migrate`，从 env → Nacos 获取管理数据库配置，空库初始化使用生成模型处理框架外键依赖，并拒绝任何已有表的数据库；模块升级仅安装第 1 版绑定表，已存在时拒绝重复安装。运行服务不执行 DDL。
4. 在管理 Nacos `data.yaml` 的 `runtime.muskdeer` 配置 enabled、endpoint、server_name=strategy.grpc、root_cert_file、certificate_file、private_key_file、timeout_ms。样例见已有 data.yaml.example。env 只保留 Nacos 引导连接，证书只读挂载。
5. 部署 operator、admin，启动会校验模块表并幂等合并本模块菜单/权限/API。不会清空其他模块的权限、角色和资源。模板管理员获得可向下分配的本模块权限；商户管理员不获得账户绑定管理能力。
6. 构建并部署平台与商户前端。沿用 Nginx 模板，ADMIN_API_UPSTREAM 指向内部 admin；商户部署设置 MERCHANT_CODE，代理覆盖浏览器传入的租户头。前端 VITE_AES_KEY 与 Nacos runtime.crypto_key 配对。

## 业务行为

- 平台仅提供策略模板、交易品种及网关页面。网关展示核心当前加载的基础设施连接，不展示凭据；连接修改继续通过 Nacos 和显式重启生效。
- 商户端提供总览、实例新建/完整参数编辑/启停、交易账户、订单/撤单、撮合原生挂单和持仓。平台身份访问这些业务接口一律拒绝，即使持有旧的管理员或自定义权限。商户表单可只读选择公共模板和已绑定账户的品种，没有公共管理页面。
- 账户归属由管理库维护；原账户绑定维护 API 仅供受控的平台运维配置归属，已撤下业务菜单，两端均不打包该页面。不会允许商户自行认领其他商户或尚未授权的交易账户。
- 模板只读展示已注册实现及数据合同，策略类型仍为做市铺单、网格和开发成交；网格有固定区间及只做多百分比两套实现。实例创建保存为停止；参数编辑要求先停，保存后显式重启四核心加载。界面区分请求运行状态、实际装载修订和待重启状态。
- 启停与参数修改使用版本检查，不透明重试；超时或版本冲突先刷新。停止后执行服务受控撤销本实例管理的订单。单独撤单要求停止实例，结果需撮合快照确认。
- 商户身份只从可信 HTTP 网关注入，operator 每个请求查管理库绑定范围；请求 DTO 不接受 Actor 或 allowed_account_ids。没有绑定默认拒绝业务数据。绑定的账户/商户身份不可修改，禁用撤销后续访问，不能转交历史归属。
- WashTrade 是开发行情发布，0 次数/累计名义量为不限，不产生真实账户对刷成交。金额、数量、比例使用十进制字符串。
- 不提供实例删除和资金预算编辑，不以历史订单回执替代当前撮合快照。

## 校验命令

backend 执行 `make api openapi ts wire-check`、`go test ./...`、`go test -race ./operator/internal/data ./operator/internal/service -run Muskdeer`，构建 admin、operator 和维护命令。共享 MinIO 测试使用 MINIO_TEST_* 提供现有基础设施凭据；仅使用各测试自建逻辑资源。

frontend 执行 `npm run typecheck`、`npm run build`、`npm run test:tenant-routes`。业务服务的真实接口接通与 UI 夹具/模拟结果分别验收，部署账号及凭据只保存在私有部署记录，不写入仓库。


独立部署使用 `docker-compose.muskdeer.yaml`，项目名 muskdeer-admin。四个管理容器使用独立 Nacos 命名空间、管理数据库、Redis 逻辑库和 JWT 密钥，保留同机其他 admin 项目。应用环境变量只提供 Nacos 连接；镜像标签、只读证书挂载和商户前端编号属于 Compose 基础设施设置。`MUSKDEER_ADMIN_IMAGE_TAG` 必须指定已验收的镜像标签。

本机默认平台入口端口 15888，商户入口端口 15889，管理 HTTP 调试端口 17788，均只绑定 loopback。新商户默认没有做市账户绑定，平台授权绑定后才能访问相应业务数据。账号和密码不写入文档或仓库。


2026-11-09 当前 WashTrade 改为行情订阅后直发 SDK，每笔无数据库读写。商户参数表单移除运行编号、累计次数和累计名义量输入，旧字段保留在原实例参数中作历史兼容；保留来源、模式、频率、数量、比例与 development_only。开发成交旧历史API保留，当前发布不再新增记录；2026-11-10已移除该前端页面及菜单。核心配置仍通过 gRPC 管理，后台不直连业务数据库。

商户前端0480432b已实际发布到独立muskdeer-admin部署，保留管理后端/平台端/原登录凭据。双端TypeScript、18文件79条回归、商户生产构建及镜像构建通过；实际商户前端代理登录读取38实例、19账户、SOL实时40笔挂单，四核心装载修订一致；两端入口HTTP 200。核心f0403af的19个开发品种均取得SDK成功回执，旧开发成交表保持159931条零增量；历史页提示与此行为一致，没有管理库或权限迁移。

2026-11-10 新增 `long_grid_v1` 的完整强类型参数，分别更新核心 Console 镜像协议、operator RPC、独立 admin HTTP IDL、显式字段映射及 OpenAPI/TypeScript 生成物。商户新建或编辑时按模板实现选择百分比网格表单，提交只包含对应算法；原固定区间网格保持原字段。复用既有商户实例查看、编辑和启停权限，没有新增菜单或跨端授权。

只做多百分比网格表单提供间距、止损比例、单笔目标及持仓名义上限；本次规则为启动首笔、跌1%买/涨1%卖对应批次、跌破持仓均价0.5%优先全平并停止。十进制值仍为字符串，10倍CROSS由执行合同约束。实际数量按撮合lot向下取整，可能小于目标。参数保存后须显式重启核心加载，创建保持停止状态。

本步官方 API/OpenAPI/TS/Wire 生成通过，后端全库测试、admin/operator入口构建、双端类型检查、19文件81条前端回归和商户生产构建通过。真实迁移、后台发布、新实例绑定与启动单独验收，源码和镜像准备不代表已上线。

2026-11-10 4d96bd3e9已实际发布独立operator/admin与商户前端，平台镜像及原登录凭据保留。管理库完整备份后，只新增两个网格基金的既有商户归属；原19绑定不变。经真实商户HTTP启用两个网格，SDK原生历史及持仓确认首笔成交、CROSS/10倍和lot取整后的100U目标上限。商户可读40实例/21账户、四核心装载版本一致及两新基金的新鲜持仓；平台可读4模板但访问实例仍403。原38实例配置/开关和既有业务历史核对保持，原19铺单品种验收时均有挂单、19开发品种均取得SDK成功回执。具体交易身份、成交和备份仅保存私有记录，不提交管理库或交易库数据。

同日修复商户镜像漏传配套AES参数导致正确密码在浏览器中被拒绝的问题。前端构建必须显式传入BuildKit secret和配置修订号，见frontend/README.md；修订号参与缓存键。5b01e9f54商户镜像已替换，检查实际浏览器资源及原账号HTTP登录/授权查询均通过。只替换商户前端，独立后台、平台及四核心容器不重启；账户凭据和数据库配置保持。


2026-11-10 账户页增加商户专用添加入口及muskdeer:account:create权限。仅接入已有撮合基金：用户必须已获本商户有效授权，且其所有历史账户绑定不得属于其他商户；新基金须通过核心gRPC核验实际撮合身份、精度和参数。未完成登记保持不可用，超时使用同参数重试；正常禁用绑定不能通过添加恢复。接入只保存账户配置，策略另行配置，不开户或划转资金。

同日已发布190e5c6bd3d7及配套核心f6a0a506767b。账户/订单/撤单/快照时间接受毫秒数字字符串，缺失或非法值显示占位符；开发成交前端页面删除，旧菜单同步停用且导航过滤旧授权，历史API和WashTrade运行保留。实际双端登录、商户21账户/40实例、添加权限与403/409拒绝路径通过，原参数/开关/账户归属及历史保持；管理库备份私有保存，没有数据库迁移。


## 商户默认首页

商户首页 `/dashboard/analytics` 展示业务概览的模板、实例、请求运行、账户、订单记录、已发布开发成交和未知开发成交数量，支持原来的定时与手动刷新。只有商户构建加载该业务组件，平台首页沿用框架组件。

原 `/muskdeer/overview` 页面已删除，对应菜单在 operator 启动的幂等目录同步中置 OFF。原 `muskdeer:overview:view` 权限和 API 保持，通过 Dashboard / Analytics 菜单关联提供新入口，不增加跨商户或平台业务访问权。部署 operator 和商户前端后，刷新页面以重新读取授权导航。


Jaeger tracing is configured in Nacos `server.yaml` under `runtime.tracing`: `enabled`, collector `endpoint` (host:port), `insecure`, optional `root_cert_file`, integer `sampling_per_million`, and `export_timeout` (for example `3s`). Environment variables continue to bootstrap Nacos only; no `OTEL_*` fallback is used. The process service names are `muskdeer-admin` and `muskdeer-operator`, separate from other deployments sharing Jaeger.

The HTTP entry creates a W3C trace before authentication and returns `X-Trace-ID`. HTTP → operator → core gRPC preserves parent spans, including the existing core single-dispatch client. HTTP 4xx/5xx and gRPC/domain failures are marked as errors. Enable the existing `data.database.enable_trace` option to include operator database timings; SQL text/parameters and exception text are not exported. Redis timings include command names only; existing framework logging automatically correlates the active trace.

Each process owns its provider and bounded asynchronous export queue. Shutdown drains services and flushes spans within a fixed deadline; collector failure does not become a business readiness dependency. Sampling retains complete sampled request chains, and completed synchronous operations are searchable independently of long-lived market subscription spans. No frontend rebuild, menu or permission change is required for tracing.


2026-11-11 已配套发布后台46fa0e135f3f和核心1dc8daf687eb。实际HTTP链路串起六个服务（138个span），并验证无追踪头时的服务端Trace ID、404错误标记、数据库/Redis耗时及策略→订单服务→撮合引擎传播。后台全量，高频核心根trace采样10%；已采样的父决策向下保持。原21账户/40实例配置、启停、账户归属、schema与历史核对保留，既有前端容器、AES配套和原登录凭据不变。Jaeger沿用现有共享内存存储，重启会丢失内存中的历史trace；本次没有变更共享存储或其他应用。
