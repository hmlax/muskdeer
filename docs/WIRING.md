# 四服务 Wire 装配

market、strategy、execution、portfolio 使用同一目录和装配约定，各自拥有独立 injector 和 ProviderSet：

```text
<service>/
  cmd/
    main.go          # env/Nacos、信号、退出状态
    wire.go          # wireinject 声明
    wire_gen.go      # Wire 生成的普通 Go 调用
    wire_test.go     # 身份/管理拒绝、构造回滚、未 Run 清理
  internal/app/
    config.go        # 验证并复制本服务的配置快照
    workplan.go      # Deer 基础设施就绪后的 typed 业务装配
    server.go        # 绑定本服务 RPC、workers、进程日志
    app.go           # 唯一 Deer 的创建、Run 和 Close
    providers.go     # 仅 wireinject 构建时加载的 ProviderSet
```

ProviderSet 的调用关系为 Config → WorkPlan / processlog.Lifecycle → ServiceSpec → App。injector 同时接收 `[]deer.Option`，Wire 将选项传给最终 `app.New`，再由它调用 `deer.NewDeer(ctx, cfg, spec, options...)`。Wire 检查类型并生成调用顺序，不按服务名选择进程。四个 cmd 都只调用自己的 wireApp；没有回退到旧 runService 的手工装配。

标准 main 的 options 为 nil，Deer 按 Nacos 配置创建并拥有依赖。已有客户端可通过 `WithDB`、`WithRedis`、`WithLogger` 等选项借给 Deer；不会创建重复连接池，调用方在 App.Close/Run 完成后关闭这些客户端。选项的类型、优先级、健康与所有权详见 [Deer 契约](DEER.md)。四个实际 injector 均测试默认装配、晚期回滚和借入 logger 的透传/清理后可用。

WorkPlan 的 provider 只描述工作，不启动 goroutine、不访问数据库，也不创建第二份公共依赖。Deer 先完成日志、PG、Redis、NATS、Nacos 和 gRPC 客户端等配置依赖，再同步调用本服务的工作计划回调、注册 RPC。该阶段调用对应 business.NewMarket/NewStrategy/NewExecution/NewPortfolio，借用唯一 Deer 的资源；启动/停止、租约、资金、幂等、fencing 及后台算法保持原合同。现有共享业务/仓储实现继续保留，其他 cmd 命令不变。

App 是最后一个可能失败的 provider。NewDeer 在构造失败时自行逆序回滚；成功后 App 持有 Deer 的 cleanup，Run 在返回前合并清理错误；尚未 Run 的应用可显式 Close。Deer.Cleanup 接收 context 并返回 error，不能为了匹配 Wire 的无返回 func() 而丢弃清理错误。测试用占用 monitoring 端口制造晚期失败，检查 gRPC 端口释放及 Kratos/Hertz 全局日志恢复，再验证成功构造后直接 Close。

## 生成与检查

```sh
make wire            # 生成四份 wire_gen.go
make wire-check      # Wire diff，发现漂移非零退出，不写回工作文件
make generate        # Proto/OpenAPI 和 Wire
make generate-check  # 三类生成物一致性；已被 verify/CI 调用
```

Wire 固定 v0.7.0，go.mod 的 require/tool 和 tools/versions.json 一致，x/tools 使用已选择的 v0.49.0，并已实测 Go 1.27.3 生成。命令通过 scripts/go.py 使用仓库工具链和缓存，带 readonly 模块约束，不依赖全局 wire 安装。正常构建使用提交的生成物，不在启动时运行 Wire，providers.go 和 wire.go 由 wireinject 标签隔离。

Wire 的 [providers/injectors 与生成代码约定](https://github.com/google/wire/blob/v0.7.0/docs/guide.md) 和 [cleanup 约定](https://github.com/google/wire/blob/v0.7.0/docs/guide.md#cleanup-functions) 是上游合同。Google Wire [上游仓库已归档](https://github.com/google/wire)，本项目固定工具版本并提交生成物，不使用浮动版本或自动升级。
