# Muskdeer

连接既有交易所的自营做市与策略交易项目。当前交付 market、strategy、execution、portfolio 四个独立交易核心服务，以及具备 gRPC 健康检查的独立 cron 基础进程，使用 gRPC 和 Nacos 通信，继续共用一个 Go module。

管理 HTTP、前端、管理开户命令及 OpenAPI/浏览器工具已按当前需求移除。核心服务的 mTLS、风控、幂等、状态机、租约与最终 fencing 保持；已存在的数据库迁移和历史验收记录保留。

公开仓库：[hmlax/muskdeer](https://github.com/hmlax/muskdeer)。Go module：`github.com/hmlax/muskdeer`。

## 核心服务

| 服务 | 独立入口 | 本机 gRPC | 主要职责 |
|---|---|---|---|
| market | market/cmd | 127.0.0.1:59002 | Binance/OKX 行情、质量与参考价 |
| strategy | strategy/cmd | 127.0.0.1:59003 | 策略实例、报价计划与执行/行情客户端 |
| execution | execution/cmd | 127.0.0.1:59004 | 意图、OMS、风险、撤单恢复与 outbox |
| portfolio | portfolio/cmd | 127.0.0.1:59005 | 成交消费、幂等投影与恢复查询 |
| cron | cron/cmd | 127.0.0.1:59006 | 独立生命周期与健康检查；尚无定期业务 |

五个进程均提供 `grpc.health.v1.Health/Check`、`Watch`、`List`，服务名为空或 `<service>.grpc`。cron 可独立构建，默认 Compose 仍只启动四个交易核心。

每个进程使用自己的 Wire injector、internal/app ProviderSet 和一个 `deer.Deer`。核心命令无参数，Nacos 连接从 env 读取，配置从 Nacos 的 data.yaml/service.yaml 强类型 YAML 配对读取，配置更新后显式重启。

公共配置、模型、事件分别位于 `deer/proto/{config,model,common}`；gRPC 契约和生成代码统一位于 `deer/proto/rpc`。仓库内维护的第三方源码、注解和许可证在 [deer/third_party](deer/third_party/README.md)。

各项目的 `business` 拥有自身业务装配；执行、策略、持仓用例分别在各自 `usecase`。行情订单簿、接入、质量和工作器归 `market`；报价与策略适配归 `strategy`；订单适配、outbox 和模拟执行归 `execution`。公共交易领域与跨服务事务仓储位于 `deer/trading`，迁移位于 `deer/migrate`；单份共用，不复制资金、幂等与 fencing 逻辑。顶层不再保留 `internal`，各项目自己的 `internal/app` 继续负责生命周期和 Wire 装配。详见 [项目目录与依赖边界](docs/PROJECT_LAYOUT.md)。

## 本地启动

复用已运行的 PostgreSQL、Redis、Nacos、NATS 及 infra-network。需要 Docker Compose、Python 3.11+、PyYAML（tools/python-requirements.txt 固定版本）和 OpenSSL；Go 工具链固定为 1.27.3，Wire 固定为 v0.7.0。

```sh
# 首次准备、显式初始化本地逻辑资源并启动
make compose-up
# 后续直接重建和启动
docker compose up --build -d --wait
docker compose ps
# 停止本项目应用
docker compose down
```

准备脚本创建忽略的根 `.env` 入口，自动读取 `.cache/compose/compose.env`。已有用户 `.env` 保持原样，此时使用显式 `--env-file .cache/compose/compose.env`。构建源码位置由该配置记录；工作区存在未提交业务修改时，使用已验证的隔离源码，避免混入运行镜像。详细配置与端口见 [核心运行说明](docs/CORE_SERVICES.md)。

默认本地场景使用双源 REPLAY 和禁用、未注资的模拟账户；回放价格不代表交易所实时行情。应用不自动迁移、注资或启用策略。真实接口和生产交易验收继续按 [进度](docs/PROGRESS.md) 与 [验收范围](docs/ACCEPTANCE.md) 单独记录。

## 开发与验证

```sh
make doctor
make tools
make generate-check
make wire-check
make verify
make core-test
python3 scripts/check-layout.py
make integration
```

Go 命令统一通过 `python3 scripts/go.py` 使用固定工具链；例如 `python3 scripts/go.py build -o .cache/bin/market ./market/cmd`。`make generate` 更新 Proto/Wire 生成物，禁止手改生成代码。`make verify` 检查格式、生成一致性、lint、规范正反样例、工具测试、单元/race 与核心构建。

`make core-test` 验证实际独立核心命令的成交投影、预算、接管、报价与停止；`make integration` 验证保留的基础设施、迁移和业务合同。测试只创建隔离逻辑资源，保留共享基础容器。`make dev-up/dev-down` 关联或解除本工作区的开发资源；`make image-test` 检查核心镜像生命周期。

金额、价格、数量和费率在协议中使用十进制 string，在 Go 中使用 decimal.Decimal；持久化结构唯一来源为 Proto，数据访问显式 WithContext/Model，使用版本化迁移。工程约定见 [AGENTS.md](AGENTS.md)。

## 文档

- [当前需求与历史范围](docs/PRD.md)
- [架构与状态归属](docs/ARCHITECTURE.md)
- [Deer 与依赖所有权](docs/DEER.md)
- [Wire 装配](docs/WIRING.md)
- [Proto 与模型规范](docs/PROTO_AND_MODEL.md)
- [Nacos 配置](docs/NACOS_CONFIG.md)
- [交易与接口合同](docs/TRADING_CONTRACT.md)
- [开发测试成交策略](docs/SYNTHETIC_TAPE.md)
- [实施计划](docs/IMPLEMENTATION_PLAN.md)
- [开发进度与实际验证](docs/PROGRESS.md)
