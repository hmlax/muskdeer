# Operator Service

## 依赖注入

使用 Wire 装配 Kratos 应用。各层 `internal/{data,service,server}/providers.go` 声明 provider 集合，`cmd/server/wire.go` 声明 `initApp`；官方 Wire 生成的 `cmd/server/wire_gen.go` 纳入版本管理，禁止手改。

在 backend 执行 `make wire` 重新生成、`make wire-check` 检查一致性，或在本目录执行同名目标。`make api` 和 `make register ENTITY=<resource>` 会同步更新注入器。Wire 工具固定为 go.mod 中的版本，通过 `go tool wire` 运行。provider 返回的 cleanup 由生成代码在失败和正常退出时逆序调用。
