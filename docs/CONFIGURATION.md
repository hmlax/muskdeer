# 配置加载与本地检查

服务配置以 [ServiceConfig Proto](../deer/proto/config/v1/service.proto) 为唯一结构来源，文件使用 protobuf JSON。`deer.LoadConfig(ctx, path)` 返回独立的生成配置类型，并执行公共生命周期及启用组件的结构/范围校验。

`monitoring` 可配置独立的本地 Hertz 健康监听，使用 address/request_timeout；当前 address 必须为字面量 loopback，示例绑定 59002/59003。它与 API 的 http listener 各有端口和 drain 生命周期，单独暴露 /livez、/readyz；详见 DEER。

文件须为普通文件，最多 1 MiB。解析拒绝未知字段、重复字段（包括 snake_case/lowerCamelCase 两种别名重复）、错误类型和尾随 JSON；深度最多 64。duration 使用 protobuf 字符串，例如 `"5s"`、`"0.5s"`。不做环境变量或 shell 文本插值；凭据使用 schema 中 password_env/token_env 引用。

日志 filename、各组件 root_cert_file，以及 gRPC certificate_file/private_key_file 的相对路径，均以传入配置文件所在目录解析为绝对路径。空路径保持为空，绝对路径保持原值。加载时不创建日志目录、不读取 PEM/凭据、不连接数据库或网络；组件构造时再验证实际凭据、文件、TLS 和依赖。配置文件的 HTTP 字段决定本地检查采用 API 结构校验；真正创建服务仍由 NewDeer/NewDeerApi 校验其职责。

配置读取/解码/校验错误返回安全的 ConfigError，默认文本只说明失败阶段；原始 cause 可通过 errors.Is/As 检查。调用方应记录安全错误，不能把未经脱敏的解码 cause 或配置正文写入日志。

## doctor

`make doctor` 核对实际 Go 1.27.3，并用 [服务](../configs/examples/service.json)/[API](../configs/examples/api.json) 示例及六份 local 角色配置执行本地校验。检查其他文件：

```sh
python3 scripts/go.py run ./cmd/doctor -config /path/to/service.json
```

成功明确输出 `configuration syntax and limits validated`。输入/校验失败退出 1，命令参数错误退出 2；无配置时不输出成功。此检查验证工具链、JSON 和参数范围；实际 PEM、环境变量、网络可达性及完整服务就绪在构造/运行阶段验证。六角色进程入口已交付，交易业务及生产部署仍按后续任务执行。

显式数据库升级使用同一配置中的 PostgreSQL，其他资源不装配；命令、校验和及事务边界见 [迁移说明](MIGRATIONS.md)。

make verify 会通过 doctor 检查这些实际示例，并继续生成、格式、lint、单元/race 与构建；隔离依赖实连通过 make integration 单独执行。
