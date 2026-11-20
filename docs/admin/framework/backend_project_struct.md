# 后端结构

`backend` 下仅有三个顶级目录。

```text
backend/
├── admin/
│   ├── main.go
│   ├── router.go / router_gen.go
│   ├── biz/handler/           # HTTP 服务和字段权限适配
│   ├── biz/router/            # hz update 生成的路由注册
│   ├── biz/model/             # 独立 HTTP proto 生成的消息
│   ├── biz/mw/                # 认证、入口、权限、审计中间件
│   ├── idl/proto/admin/v1/    # HTTP 路由、参数和响应定义
│   └── internal/             # Hertz 启动及传输适配
├── operator/
│   ├── cmd/server/            # Kratos 启动、Wire 依赖注入
│   └── internal/
│       ├── service/          # 实现 gRPC 方法
│       ├── server/           # gRPC 注册与 API/RPC 适配
│       └── data/             # GORM 仓储；store/ 由模型 proto 生成
└── platform/
    ├── proto/operator/v1/    # gRPC 合约、业务数据与持久化模型
    ├── pb/operator/          # 平铺的生成 Go 文件
    ├── pkg/                  # 认证、隔离、配置等公共依赖
    ├── tools/                # 协议、模型与路由适配生成器
    ├── deploy/               # 基础设施配置模板
    └── scripts/              # 环境、部署和验收脚本
```

共享 `backend/go.mod`。API 与服务 protobuf 独立，网关通过显式字段转换调用 gRPC。仓储直接使用从 model proto 生成的 GORM 模型；不要手写第二份数据库 PO。

在 `backend` 执行 `make api openapi ts`，依次生成 RPC、模型、Hertz 路由、适配器、Wire、OpenAPI 和 TypeScript。扩展方法见 [CRUD 指南](crud_module_guide/backend.md)。
