# Muskdeer

基于既有撮合引擎的自营做市与可配置策略平台。
公开仓库：[hmlax/muskdeer](https://github.com/hmlax/muskdeer)，Go module：github.com/hmlax/muskdeer。

## 当前交付
P01/P02 公共运行层已完成，含 Deer/DeerApi、配置、独立进程启停与回滚、日志/轮转、PostgreSQL/Redis/JetStream、Nacos、mTLS gRPC 工厂、健康/指标及固定 Go 镜像；本机 arm64 和 Linux CI amd64 验证通过。P03 已开始业务值对象与持久化基础，交易业务及管理页面按后续任务实现。进度及限制见 docs/PROGRESS.md。

## 开始开发
项目 Go 版本固定为 1.27.3，开发、生成、测试、CI 和构建使用同一精确版本。
运行 make doctor 验证并按需获取固定工具链；Go 命令使用 python3 scripts/go.py，例如 python3 scripts/go.py version。make test-tooling 验证工具链入口的版本检查及失败处理。
运行 make tools 安装固定版本 protoc 与插件，make generate 生成模型及 RPC，make verify 执行生成一致性、hz 生成/编译、格式、lint、工具链测试、Go 单元/race 测试与构建。make integration 使用 Docker 启动并清理独立 PostgreSQL/Redis/NATS/Nacos，执行模型、客户端/池/超时/TTL/生命周期、JetStream 确认丢失、Nacos 认证与 namespace 隔离测试；仅绑定 127.0.0.1，由 Docker 动态分配端口并传给本次测试进程，本机需 Docker 可用。Nacos 使用临时数据和明确测试口令，首次镜像下载/Java 启动会增加耗时。

make doctor 同时验证精确工具链及服务/API 示例配置；其他文件使用 `python3 scripts/go.py run ./cmd/doctor -config /path/to/service.json`。严格 Proto JSON、相对路径与校验范围见 [配置说明](docs/CONFIGURATION.md)。
make build 生成 `.cache/bin/muskdeer`；例如 `.cache/bin/muskdeer -role market -config configs/local/market.json` 启动独立 market 进程，SIGTERM/CTRL-C 有序停止。`configs/local` 提供六种无外部依赖的本地配置及独立 loopback 端口；运行与日志边界见 [运维说明](docs/OPERATIONS.md)。api 的管理页面和业务路由仍待 P07。
`make dev-up` 启动本工作区专属的持久 PostgreSQL/Redis/NATS/Nacos，生成 `.cache/dev` 地址、样例环境变量和六份配置；`make dev-down` 保留数据卷。`make dev-test` 在独立临时工作区验证重复启动、配置、实际依赖就绪和数据保留，结束只清理该测试资源。详见 [开发环境](docs/OPERATIONS.md#开发资源)。
`make image` 使用固定 digest 的 Go 1.27.3 构建 `muskdeer:local`，runtime 使用非 root 的 scratch；`make image-test` 验证只读/禁网运行、实际 readiness、日志及 SIGTERM。`make e2e` 汇总当前运行层 integration 与 image-test；后续交易和管理浏览器 E2E 将随业务接入，不将当前命令等同于完整业务验收。
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
公共路径与包名采用 deer，各 Kratos 业务服务分别使用 deer.NewDeer 构造的 Deer 封装；api 用作平台管理界面，包含可操作的管理前端页面和后端接口，使用 deer.NewDeerApi 构造的 DeerApi，复用 Deer 并托管 Hertz 入口，见 docs/DEER.md。管理界面范围与交付验收见 docs/PRD.md 的 R08、docs/ACCEPTANCE.md 的 A27；目前尚未实现管理页面及业务接口。
