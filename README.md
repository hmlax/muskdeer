# Muskdeer

基于既有撮合引擎的自营做市与可配置策略平台。
公开仓库：[hmlax/muskdeer](https://github.com/hmlax/muskdeer)，Go module：github.com/hmlax/muskdeer。

## 当前交付
P01 已完成：固定 Go 工具链、Proto/GORM/gRPC 生成、Hertz/Kratos/Nacos SDK 契约及真实隔离 PostgreSQL 兼容验证通过。当前尚无交易业务服务或可运行交易部署；进度及限制见 docs/PROGRESS.md。

## 开始开发
项目 Go 版本固定为 1.27.3，开发、生成、测试、CI 和构建使用同一精确版本。
运行 make doctor 验证并按需获取固定工具链；Go 命令使用 python3 scripts/go.py，例如 python3 scripts/go.py version。make test-tooling 验证工具链入口的版本检查及失败处理。
运行 make tools 安装固定版本 protoc 与插件，make generate 生成模型及 RPC，make verify 执行生成一致性、hz 生成/编译、格式、lint、工具链测试、Go 单元/race 测试与构建。make integration 使用 Docker 启动并清理独立 PostgreSQL 执行真实契约测试，仅绑定 127.0.0.1:55439；本机需 Docker 可用且该端口空闲。
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
公共路径与包名采用 deer，各 Kratos 业务服务分别使用 deer.NewDeer 构造的 Deer 封装；API 使用 deer.NewDeerApi 构造的 DeerApi，复用 Deer 并托管 Hertz 入口，见 docs/DEER.md。API 管理界面第一版只交付管理 API 和 OpenAPI 文档，不默认追加完整前端。
