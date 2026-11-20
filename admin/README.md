# Admin Hertz API

admin 使用 Hertz 原生启动、路由、中间件和优雅退出。HTTP 默认端口 7788，通过 gRPC 调用 operator。配置与服务发现使用 Nacos。

路由注册链：`main.go → router_gen.go → biz/router/register.go → biz/router/*_generated.go → biz/handler → operator`。

HTTP 路由、参数和响应独立定义在 `idl/proto/admin/v1/*.proto`（`admin.v1`）。生成模型在 `biz/model/admin/v1/`；业务 handler 与字段权限在 `biz/handler/`；认证、RBAC、商户入口校验与审计在 `biz/mw/`。

在 backend 执行：

```sh
make hz          # 官方 hz update + 类型转换和注册适配
make openapi ts  # OpenAPI 与双前端共享 TypeScript 客户端
make api         # RPC、模型、Hertz 与 Wire 的完整生成
```

`admin.http.rpc` 可以将 HTTP 方法映射到 operator 的 gRPC 方法。两端 protobuf 独立，按字段转换，请求不会使用 JSON 或 bytes 包装业务数据。禁止手改生成的路由、模型、转换器和客户端。

平台入口不使用商户编号；商户入口通过 `X-Tenant-Code` 固定商户，后端校验其与 JWT 一致。文件上传、下载和审计导出仍通过 HTTP，当前框架没有业务事件订阅监听。

服务配置与部署见 [服务指南](../../docs/service_split.md)。
