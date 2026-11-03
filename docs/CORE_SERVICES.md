# 独立核心服务

2026-11-02 用户后续明确：核心模块分别独立 Kratos 进程，管理后台相关功能不用。当前默认构建/Compose 只包含四个核心服务，不启动 admin/operator、HTTP 管理接口或 React；原有管理源码及历史测试保留。定期报表/归档尚未实现，不部署无业务的 cron。

| 服务 | 独立入口 | 本机 gRPC | Nacos 身份 | 实际工作 |
|---|---|---|---|---|
| market | market/cmd | 127.0.0.1:59002 | market.grpc | Binance/OKX 独立回放流、质量、行情发布/读取 |
| strategy | strategy/cmd | 127.0.0.1:59003 | strategy.grpc | 账户策略候选扫描、实例控制及执行/行情 RPC 客户端 |
| execution | execution/cmd | 127.0.0.1:59004 | execution.grpc | OMS gRPC、风险候选、模拟撤单恢复、outbox |
| portfolio | portfolio/cmd | 127.0.0.1:59005 | portfolio.grpc | 持久成交消费、幂等投影及执行恢复查询 |

每个进程使用一个 Deer/Kratos 生命周期和自己的 mTLS 身份，Nacos group 为 MUSKDEER_CORE、namespace 为 muskdeer-local。gRPC 是认证的内部接口；strategy/portfolio 当前主要提供后台工作循环及标准健康 RPC，不把管理接口当核心接口重新开启。

核心服务现在**无命令行参数**：直接运行 market/strategy/execution/portfolio。Nacos 连接从 env 获取，服务与业务配置从 Nacos 的对应 `<service>.json` 读取；不使用本地 JSON 或 flags。具体字段和维护流程见 [Nacos 配置](NACOS_CONFIG.md)。

## 独立装配位置

四个服务按各自目录组织，cmd 统一放 main、wire.go、wire_gen.go 和 injector 测试；装配 provider 在各自 internal/app，继续共用根 go.mod，其余 cmd 命令保持原样。每个项目可单独构建，例如 `python3 scripts/go.py build -o .cache/bin/market ./market/cmd`。生成及清理合同见 [Wire 装配](WIRING.md)。

各 internal/app 分别维护配置检查、工作计划、服务定义和 Deer 生命周期，由独立 ProviderSet 注入。market/execution 只注册自身业务 RPC，strategy/portfolio 只有后台工作循环。四个 main 从 env 引导 Nacos 配置，调用本服务生成的 wireApp 和 App.Run，没有命令行配置参数或公共启动分发。

| 服务装配 | 直接业务构造 | 工作计划实现 |
|---|---|---|
| [market](../market/internal/app/workplan.go) | business.NewMarket → *Market | [market.go](../internal/business/market.go) |
| [strategy](../strategy/internal/app/workplan.go) | business.NewStrategy → *Strategy | [strategy.go](../internal/business/strategy.go) |
| [execution](../execution/internal/app/workplan.go) | business.NewExecution → *Execution | [execution.go](../internal/business/execution.go) |
| [portfolio](../portfolio/internal/app/workplan.go) | business.NewPortfolio → *Portfolio | [portfolio.go](../internal/business/portfolio.go) |

公共 `coreentry.Run` 已移除，coreentry 只含 readiness 和 cancellation 小工具。通用 `business.New/Runtime` 留给历史组合入口/管理回归，四个核心进程不再调用它；业务工作器的算法、超时、注册发现与资金合同保持。

## 启动

需要已运行的 infra-postgres、infra-redis、infra-nacos、infra-nats 和 infra-network，以及 Docker Compose、Python 3.11+、OpenSSL。Go 精确为 1.27.3；核心 Docker 构建不读取前端资产，不需要 Node。

```sh
make compose-up
docker compose --env-file .cache/compose/compose.env ps
```

准备脚本复用既有基础服务，只创建独立逻辑资源：有本工作区所有权标记的 muskdeer_local PostgreSQL 数据库/角色、muskdeer-local Nacos 命名空间、本地服务证书。Nacos 凭据从忽略的 `.cache/infra/nacos-credentials.json`（username/password）读取，Redis 从既有容器读取本地认证配置。独立持久 NATS 用户 muskdeer-local 的凭据放 `.cache/infra/nats-local-credentials.json`；该账户必须已配置在现有 infra-nats，不能使用会被测试清理的 muskdeer-test。所有凭据/密钥仅存在忽略缓存，权限 0600；每个容器仅挂载自己的证书/私钥，不挂载 CA 私钥。

启动前显式用同源码镜像中的 `/migrate -apply` 迁移，再由 `/coreinit` 创建 16 MiB 有界持久流 MUSKDEER_LOCAL、portfolio_local durable、两个回放绑定和一个**禁用且未注资**的模拟账户。两工具默认也读取 Nacos；维护容器使用 tools profile，仅连接 infra 网络，不占核心静态 IP。主题前缀为 muskdeer.local.core。重复准备不覆盖既有数据库、预算、broker 合同或已有 Nacos 配置；遇到不兼容资源直接拒绝。服务本身不迁移、创建消息资源、开户或注资。资源 ID 在 `.cache/compose/resources.json`，业务配置在 Nacos，`.cache/compose/nacos-drafts` 仅为首次发布的本地草稿，不挂载到服务。

当前回放价格为 99.99/100.01，只是本地模拟输入，不代表 Binance/OKX 当前市价或实连。策略服务扫描专属未注资账户，初始没有策略实例和订单；账户与预算均 disabled，不允许新增风险。持仓服务已经绑定真实 infra JetStream consumer，等待本环境成交事件。

应用网络固定 172.31.240.0/24，四个实例分别 .10–.13，准备时拒绝网段冲突；依赖连接走已有 infra-network。主机只发布 loopback gRPC 端口，健康端口仅在容器内 loopback。容器非 root、只读、去除 capabilities，日志按大小轮转。

```sh
# 已准备且初始化后再次启动
docker compose --env-file .cache/compose/compose.env up -d --remove-orphans --wait
# 仅移除本项目应用，保留数据和既有 infra
make compose-down
# 真实独立核心命令与隔离资源回归
make core-test
```

本工作区保留尚未验收的 schema 38 平仓修改。2026-11-03 按用户要求重新构建并通过 Compose 重建四个应用容器，当前运行源码为已提交的 `9f31d64`（含 Wire/Deer 注入及 deer/third_party 迁移），使用 schema 37，导出在忽略的 `.cache/compose-restart/source`；构建来源见 `.cache/compose/compose.env`，不把未提交功能混入运行镜像。四容器 healthy，实际 mTLS 双源回放行情及 execution 查询通过；启动证据见 `.cache/compose-restart/{before,after,live-probe}.json`。普通干净检出默认构建仓库；明确改用当前工作区时执行 `python3 scripts/compose.py prepare --source .`，应先核对迁移版本。原管理容器已移除，其历史数据保留。
