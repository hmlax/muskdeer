# Himalaya 开发网关支持代码

本目录维护 Muskdeer 接入 Himalaya 开发撮合所需的原生支持代码。不是已部署网关或已启动策略的声明。

`maker_fence.rs` 是撮合执行边界的持久化所有权组件：执行账户和策略实例分别持有 epoch；进程内使用撮合端的单调时钟，重启后只恢复 epoch，不恢复旧进程租约。新授予必须先持久化；写入结果不确定时关闭所有权。调用方在撮合写入期间持有 EffectGuard，避免检查与效果之间被新 owner 接管。

该组件必须接入原生撮合的最终执行路径才能提供端到端 fencing，不能只在 Go 发送前调用它。当前验证入口为：

```sh
CARGO_TARGET_DIR=.cache/himalaya/native-target rustup run 1.94.0 cargo test \
  --locked --manifest-path scripts/native/himalaya/Cargo.toml
```

工具链与序列化依赖均锁定，Rust 参数遵循 Himalaya 的 1.94.0。原生服务集成后还需运行 Himalaya 规定的完整 workspace/all-features 检查。网关合同在 [gateway.proto](../../proto/rpc/himalaya/v1/gateway.proto)，所有价格、数量、资金及费率使用十进制字符串。

`matching/` 保存本轮新增的原生撮合 RPC、实际订单簿回归测试及增量补丁。补丁仅包含本轮修改，不包含 Himalaya 原有未提交业务。`maker_matching.proto` 中的既有撮合消息沿用原系统 tick 合同，面向 Muskdeer 的网关仍只接收十进制字符串。

`matching/preimages.json` 记录补丁基线各文件的 SHA-256。复用补丁前必须逐项验证前像；`integration.patch` 为零上下文增量，核验后在原生 matching-engine 目录使用 `git apply --check --unidiff-zero`，不得向变化的源码强行应用。新增模块与两份 Proto 分别复制到原生 `src`/`proto`；根 `gateway.proto` 对应原生的 `maker_gateway.proto`。当前工作区已应用，无需再应用。

原生绑定将两级 proof 和准入时的期限带入原有 FIFO，在持有每交易对执行锁后复核所有权，并持有 guard 直到订单簿效果与 outcome 写入结束。`MatchingAuthority` 使用原生 ServiceAuth；默认关闭。显式 `HIMALAYA_MAKER_DEVELOPMENT_TEST_FUNDS=true` 且有持久化 outbox 目录才启用，只接受测试账户 10001/BTCUSDT 和 10002/ETHUSDT。检查点失败后状态探针拒绝声明可用。

开发源码已绑定并通过原生完整回归，运行容器尚未升级。仍需订单服务的持久命令日志、旧入口接管保护和失联撤单；单独启用此撮合服务不能视为完整 Gateway，也不能据此启动报价。

`order/maker_journal.rs` 与 `order/001_maker_commands.sql` 是持久命令身份组件及显式 version 1 增量迁移。服务启动只校验 schema/唯一键，不能自动执行 DDL；原生订单服务绑定尚在实施。`make native-himalaya-test` 同时运行所有权和真实 MySQL 日志测试，复用 `infra-mysql` 并只创建/清理随机测试逻辑库。凭据只从已有容器配置读入子进程环境，不输出或写入仓库。缺少 infra 或凭据时命令失败，不跳过测试。

最终撮合点现已拒绝接管资金账户的普通旧下单。原生清算使用独立管理员认证的 SystemReduce IOC 通道，在原清算准备/冻结后发送，并在目标处撤销 producer 双 proof；普通 service 身份不能调用。`order/integration.patch` 与前像摘要保存该原生通道/资源借用增量，与 matching 补丁一样只允许哈希核验后使用 `--unidiff-zero`。服务启动标志、未接管账户及默认关闭行为保持；目标失联撤单和完整 Gateway 仍待后续完成。
