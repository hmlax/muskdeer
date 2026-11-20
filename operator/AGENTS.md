# 管理业务服务

operator 是独立 Kratos gRPC 进程，与全项目共用根 go.mod，不提供浏览器 HTTP。业务、GORM 和管理库在 operator/internal，权威 RPC/持久化 Proto 在 deer/proto/operator/v1，生成代码在 deer/pb/operator/v1，Wire 在 operator/cmd/server。通过本仓库 deer/pb/strategy/console/v1 的 mTLS 客户端调用核心，禁止业务库直连、协议副本和反向导入其他服务 internal。

保留商户范围、RBAC、模型真源及独立管理库现有 schema；源码合并不自动迁移或重置数据。新字段/生成依赖从 Proto 和 provider 修改后运行根 make admin-generate；检查 make admin-test、make admin-check、make admin-build。环境只提供 Nacos 连接，不引入本地配置回退。
