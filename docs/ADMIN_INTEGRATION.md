# 同仓库管理后台

按用户 2026-11-20 要求，参考 Bullfrog 的顶层后台服务/前端及共享库布局，Muskdeer 保持一个 Go module、Go 1.27.3。来源为 hmlax/admin 的 codex/muskdeer-console 分支 022e626fe；框架 MIT 许可证保存在 deer/management/LICENSE。来源提交仅记录溯源，构建不需要另一份 admin 或 Bullfrog 工作区。

| 目录 | 职责 |
| --- | --- |
| admin | Hertz 管理 HTTP，JWT/会话、菜单/API/写操作权限和可信身份入口 |
| operator | Kratos 管理 RPC、独立管理数据库、账户商户绑定与 Console 客户端 |
| adminfrontend | 平台及商户两套 React 应用，共享 src，独立构建产物 |
| deer/management | 后台公共运行库、Nacos、安全、追踪、OpenAPI |
| deer/proto/operator/v1、deer/pb/operator/v1 | 管理 RPC 与原生 GORM 模型真源/生成物 |
| scripts/admin | Hertz/API/RPC 显式映射、模型及前端生成工具和回归 |

operator 直接使用 deer/pb/strategy/console/v1，原跨仓库 Console Proto、摘要和客户端副本移除。HTTP API 与 operator DTO 仍独立，经生成的显式字段映射适配。核心不依赖后台实现。

管理库与交易库继续独立；既有管理框架模型、租户身份、账户绑定、菜单权限、审计和 schema 不因路径调整而改变。商户管理实例/账户/挂单/成交/持仓，平台管理公共模板、品种和网关。operator 通过既有 mTLS/可信商户范围调用核心，保留独立管理 Nacos 配置；本次不统一 Nacos 文档或改动运行策略。

## 构建与验证

```sh
make tools admin-tools admin-deps
python3 -m pip install -r scripts/admin/test/requirements.txt
make generate                 # 核心及后台协议
make admin-generate           # 后台 Proto/GORM/Hertz/Wire/OpenAPI/TS
make generate-check admin-generate-check
make admin-test admin-check
make build                    # 全部 Go 入口，包括 admin/operator 与管理维护命令
make admin-build              # 管理后端及两套前端
make build-all                # 全部 Go 入口及两套前端
```

后台与核心统一使用 scripts/go.py 的固定工具链/缓存；Makefile 与 Docker 构建均注入 main.Name、main.Version、main.Build，ID 从运行主机获取。后台生成比较在临时目录运行，不改写工作区，失败也不删除已提交客户端。pnpm 使用 12.16.1 与已提交锁文件。

统一 `make verify` 也覆盖后台生成比较、Go 测试/静态检查/构建以及前端类型和权限回归。Python 解释器可通过 `make PYTHON=/path/to/python3 ...` 指定。对象存储单元测试使用自有进程内 S3 协议夹具，不探测或写入开发者已有对象存储；这不等同实际 MinIO 部署验收。

原后台使用 standard lint，与核心的注释/命名规则存在历史差异；部分方法名必须匹配已有 protobuf RPC。`tools/admin-style-baseline.json` 只记录逐条与来源提交核对过的样式问题，包含原文件摘要、规则、声明/语句摘要和出现次数。新增位置、增加次数、核心代码以及任何非样式问题均不能命中基线；静态分析、错误检查、浮点/SQL/迁移限制继续覆盖全部 Go 代码。修改命中的声明或增加例外时应实际修复问题，不扩大基线来放行新问题。原仓库另外创建基础服务的部署/测试脚本未迁入，当前验证继续复用已有基础设施。

前端公共构建参数在各应用 .env.development/.env.production。将 .env.example 复制为忽略的 .env.local，填写与已部署 Nacos 匹配的 VITE_AES_KEY；商户开发入口还需 VITE_MERCHANT_CODE。密钥及实际部署参数不提交。生产商户代码仍由部署 app-config.json 固定，代理覆盖浏览器传入的商户范围；两端本地存储隔离保持。

```sh
python3 scripts/go.py run ./operator/cmd/server
python3 scripts/go.py run ./admin
make admin-dev-platform
make admin-dev-merchant
```

后端环境只提供现有 NACOS_ADDR/NAMESPACE_ID/GROUP/CLUSTER/认证等连接参数；完整服务配置由 Nacos 提供，不使用本地 YAML 或业务环境变量回退。默认 HTTP 本地映射 17788，前端 15888/15889。容器入口见 compose.admin.yml，可与 compose.yml 合并为同一 muskdeer Compose 项目，复用已有 infra-network。合并源码不执行管理 bootstrap/migrate，不重启交易或其他项目；升级及部署使用现有配置显式进行。

例如在提供已有私有配置路径后，使用 `docker compose -f compose.yml -f compose.admin.yml --profile admin config` 检查完整项目。仅更新核心时，根 compose-up 不自动删除其他 profile 的容器。前端包缓存固定在忽略的 .cache/pnpm-store。独立本地 HTTP 若监听 7788，可在前端 .env.local 中显式配置 VITE_PROXY 指向该端口。

原框架文档保留在 docs/admin/framework，历史路径 backend/platform/frontend 分别对应根 admin/operator、deer/management 与 adminfrontend；当前路径以本页为准。
