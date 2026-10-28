# Muskdeer

基于既有撮合引擎的自营做市与可配置策略平台。
公开仓库：[hmlax/muskdeer](https://github.com/hmlax/muskdeer)，Go module：github.com/hmlax/muskdeer。

## 当前交付
P01–P06已完成公共运行层、模拟现货/OMS闭环、双来源行情及做市/失效保护：Binance/OKX公开实连、实际独立业务进程及30分钟808成交核算通过。管理身份、实际策略查询页面/HTTP/gRPC及网格控制循环已分步交付，配置/运行管理写操作、对冲、恢复和L1验收继续P07–P10。进度及限制见 docs/PROGRESS.md。

## 开始开发
项目 Go 版本固定为 1.27.3，开发、生成、测试、CI 和构建使用同一精确版本。
运行 make doctor 验证并按需获取固定工具链；Go 命令使用 python3 scripts/go.py，例如 python3 scripts/go.py version。make test-tooling 验证工具链入口的版本检查及失败处理。
运行 make tools 安装固定版本 protoc 与插件，make generate 生成模型及 RPC，make verify 执行生成一致性、hz 生成/编译、格式、lint、工具链测试、Go 单元/race 测试与构建。make integration 使用 Docker 启动并清理独立 PostgreSQL/Redis/NATS/Nacos，执行模型、客户端/池/超时/TTL/生命周期、JetStream 确认丢失、Nacos 认证与 namespace 隔离测试；仅绑定 127.0.0.1，由 Docker 动态分配端口并传给本次测试进程，本机需 Docker 可用。Nacos 使用临时数据和明确测试口令，首次镜像下载/Java 启动会增加耗时。

make doctor 同时验证精确工具链及服务/API 示例配置；其他文件使用 `python3 scripts/go.py run ./cmd/doctor -config /path/to/service.json`。严格 Proto JSON、相对路径与校验范围见 [配置说明](docs/CONFIGURATION.md)。
make build 生成 `.cache/bin/muskdeer`、迁移和本地账户开通命令；例如 `.cache/bin/muskdeer -role market -config configs/local/market.json` 启动独立 market 进程，SIGTERM/CTRL-C 有序停止。`configs/local` 提供六种无外部依赖的本地配置及独立 loopback 端口；运行与日志边界见 [运维说明](docs/OPERATIONS.md)。api可显式开启[管理登录/会话页面与身份接口](docs/ADMIN_HTTP.md)，策略管理及完整P07仍待后续。
`make dev-up` 启动本工作区专属的持久 PostgreSQL/Redis/NATS/Nacos，生成 `.cache/dev` 地址、样例环境变量和六份配置；`make dev-down` 保留数据卷。`make dev-test` 在独立临时工作区验证重复启动、配置、实际依赖就绪和数据保留，结束只清理该测试资源。详见 [开发环境](docs/OPERATIONS.md#开发资源)。

可选 `business` 配置启用实际 execution RPC/outbox、portfolio消费、market工作器/行情RPC和strategy账户控制循环；需要显式迁移、准备绑定或模拟账户/持仓起点/预算及消息资源，服务不自动创建或开放风险。`make integration` 包含实际成交双命令、双来源market及双strategy接管/报价/终态停止的 Nacos/mTLS 闭环，见 [业务运行](docs/BUSINESS_RUNTIME.md)。
`make soak-smoke` 检查12秒真实策略驱动；`make soak` 使用独有测试基础设施执行固定30分钟场景，保存资金/预算/成交投影采样及实际业务进程日志。短场景不能计为30分钟验收，见 [长运行说明](docs/STRATEGY_SOAK.md)。
`make web-install web-check web-build`使用项目内精确Node/npm构建React管理页面，产物提交后由Go embed/Hertz托管；普通Go构建无需Node。`make admin-browser`使用独有PG、实际本地账户/API命令和Chromium验证登录、刷新及注销，截图保存在.cache/admin-browser；见[管理入口](docs/ADMIN_HTTP.md)。
`make image` 使用固定 digest 的 Go 1.27.3 构建 `muskdeer:local`，runtime 使用非 root 的 scratch；`make image-test` 验证只读/禁网运行、实际 readiness、日志及 SIGTERM。`make e2e` 汇总当前integration、image-test及身份浏览器流程；完整交易/策略管理验收随业务接入，不将当前命令等同于完整业务验收。
数据库升级使用实际构建的 `.cache/bin/migrate -apply -config /path/to/service.json`，有版本/校验和、事务锁和回滚；服务不自动迁移。当前版本24含交易对/实例/配置来源身份、typed做市/网格参数、策略/执行/行情租约、模拟核心资金/订单/成交、OMS意图/预算/撤单/outbox、成交inbox/持仓、行情快照/定价基准、最终所选行情门禁、平台/交易对额度、持久动作/运行incarnation、管理身份/审计及独立请求运行目标，交易表继续增加，见[迁移说明](docs/MIGRATIONS.md)和[参数说明](docs/STRATEGY_PARAMETERS.md)。
`make contract-check` 检查 Proto/模型字段和 Go 小数/选表语法，`make policy-test` 用真实 linter 的正反样例验证禁止规则；两者已接入 verify，见 [自动规范检查](docs/POLICY_CHECKS.md)。
在 Codex 中打开此仓库，发送 docs/CODEX_START.md 中的启动指令。Codex 检查现有实现与 PROGRESS，从首个未完成任务持续推进至模拟 MVP 验收通过。真实接口缺失时记录依赖并继续独立工作。
每个最小步骤完成并通过相关测试/检查后，立即提交并推送到该远程仓库，确认同步后继续下一步。文档步骤验证文档与文件完整性，不宣称业务测试已通过。

## 文档导航
- [需求与默认范围](docs/PRD.md)
- [架构及状态归属](docs/ARCHITECTURE.md)
- [公共 Deer、DeerApi 与日志规范](docs/DEER.md)
- [Proto 与 ORM 生成规范](docs/PROTO_AND_MODEL.md)
- [交易所适配合同](docs/EXCHANGE_CONTRACT.md)
- [接口、状态与事件](docs/INTERFACES.md)
- [任务顺序](docs/IMPLEMENTATION_PLAN.md)
- [验收矩阵](docs/ACCEPTANCE.md)
- [运维与故障处理](docs/OPERATIONS.md)
- [开发进度](docs/PROGRESS.md)
- [默认决策与待确认](docs/DECISIONS.md)
- [Codex 启动指令](docs/CODEX_START.md)

## 安装范围
公共路径与包名采用 deer，各 Kratos 业务服务分别使用 deer.NewDeer 构造的 Deer 封装；api 用作平台管理界面，包含可操作的管理前端页面和后端接口，使用 deer.NewDeerApi 构造的 DeerApi，复用 Deer 并托管 Hertz 入口，见 docs/DEER.md。管理界面范围与交付验收见 docs/PRD.md 的 R08、docs/ACCEPTANCE.md 的 A27；当前身份页面/接口与完整策略管理范围分别记录于docs/ADMIN_HTTP.md及PROGRESS。
