# Himalaya 开发网关支持代码

本目录维护 Muskdeer 接入 Himalaya 开发撮合所需的原生支持代码。不是已部署网关或已启动策略的声明。

`maker_fence.rs` 是撮合执行边界的持久化所有权组件：执行账户和策略实例分别持有 epoch；进程内使用撮合端的单调时钟，重启后只恢复 epoch，不恢复旧进程租约。新授予必须先持久化；写入结果不确定时关闭所有权。调用方在撮合写入期间持有 EffectGuard，避免检查与效果之间被新 owner 接管。

该组件必须接入原生撮合的最终执行路径才能提供端到端 fencing，不能只在 Go 发送前调用它。当前验证入口为：

```sh
CARGO_TARGET_DIR=.cache/himalaya/native-target rustup run 1.94.0 cargo test \
  --locked --manifest-path scripts/native/himalaya/Cargo.toml
```

工具链与序列化依赖均锁定，Rust 参数遵循 Himalaya 的 1.94.0。原生服务集成后还需运行 Himalaya 规定的完整 workspace/all-features 检查。网关合同在 [gateway.proto](../../proto/rpc/himalaya/v1/gateway.proto)，所有价格、数量、资金及费率使用十进制字符串。
