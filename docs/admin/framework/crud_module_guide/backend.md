# 后端 CRUD 模块

所有命令在 `backend/` 执行。admin 使用 Hertz 提供 HTTP；operator 使用 Kratos 提供内部 gRPC，业务数据由 GORM 访问。前端只调用 admin。

## 协议与持久化

1. 在 `deer/proto/operator/v1/<resource>.proto` 定义 operator 的业务 DTO 和 RPC。
2. 在 `deer/proto/operator/v1/model_<resource>.proto` 定义持久化实体，参照 `model_dict_type.proto` 的 `(operator.v1.model)`、`(tagger.tags)`、字段类型、约束及关联声明。
3. 在 `admin/idl/proto/admin/v1/<resource>_types.proto` 定义 HTTP API 自己的消息，在对应 service proto 定义 HTTP 路由和绑定。HTTP 与 RPC 消息互相独立，不 import、别名或序列化中转。
4. 在 `scripts/admin/tools/gateway-services.json` 注册 RPC 服务；生成器维护 HTTP/RPC 的显式逐字段转换和传输适配。

持久化字段的唯一来源是 model proto。生成模型直接用于 GORM，不另写 PO、TableName 或物理表名选择器。模型元数据必须保留既有列名、类型、索引、默认值和表结构兼容性。租户模型保留 tenant_id；需要数据范围的模型设置 data_scope。

```bash
make api       # RPC、GORM 模型、类型化操作、HTTP API、官方 hz 路由和传输适配
make openapi   # 内嵌 HTTP 文档与策略资产
make ts        # frontend/src/api/generated/admin/v1
```

只修改持久化模型或模型生成器时运行 `make models`。只修改 HTTP 契约时运行 `make hz`。生成文件禁止手改；重复生成应稳定。

## 仓储与业务

- 在 `operator/internal/data/<resource>_repo.go` 实现仓储。参照同类现有模块，使用 `store.DBClient`、生成模型和类型化操作；查询条件是原生 GORM Scope。
- 需要通用列表、字段投影、分页及更新掩码时，使用 `store.Repository[DTO, Model]`。更新回调直接调用生成的字段 setter，保持 optional、零值及显式清空语义。
- 自定义 GORM 操作以生成模型为查询目标，不能通过字符串指定物理表。关联子查询从模型元数据解析关联。
- `store` 在查询、更新和删除边界检查 ViewerContext 与 tenant_id；创建时写入当前租户，禁止商户通过更新改变归属。业务层仍需验证引用资源属于当前商户。
- 跨仓储操作只使用同一个事务客户端；错误、取消或 panic 回滚，提交失败必须返回调用方。不要把事务中的一部分写回根连接池。
- 每次数据库调用传递 ctx；错误保留原始原因。搜索使用 contains，ID 使用精确查询。
- 在 `operator/internal/service/<resource>_service.go` 实现业务、配额和数据归属约束。HTTP JWT、会话吊销、RBAC 和入口检查由 admin 负责，operator 不重复挂 HTTP 中间件。
- 在 operator 的 `internal/data/providers.go`、`internal/service/providers.go` 登记构造函数。`make register ENTITY=<resource>` 自动登记 provider 与传输契约，并生成 Wire；额外依赖也由 Wire 解析。用 `make wire-check` 检查生成一致性，不手改 `cmd/server/wire_gen.go`。

普通 CRUD 请求体是 `{data: {...}}`，标记 `body: "*"` 的自定义 RPC 接受扁平请求。HTTP 路由和请求/返回类型只在 proto 中维护。

## 数据库与验收

运行期构造器不自动执行 DDL。已部署数据库的新增列和索引需要独立、可审阅的迁移；测试夹具可显式使用 `Schema.Create` 创建隔离数据库。

至少验证新增、查询、contains 搜索、分页、编辑、零值、清空、删除、记录不存在、关联归属、跨租户拒绝与事务回滚。

```bash
go test ./...
go build -o /tmp/go-admin-admin ./admin
go build -o /tmp/go-admin-operator ./operator/cmd/server
```

新增接口需要重建内嵌 OpenAPI、重启 operator，再在平台页执行「接口同步」；新增菜单执行「菜单同步」。前端和多租户端到端测试见 [React 指南](react.md) 与 [租户流程测试](../tenant_lifecycle_e2e.md)。
