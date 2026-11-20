# 新增 CRUD 业务模块

> 持久化实现已于 2026-11-04 统一到 Kratos + GORM。本文保留原设计与历史实现记录；当前代码路径、模型生成和租户约束以 [后端 CRUD 指南](backend.md) 与 [租户隔离](../tenant_isolation.md) 为准。

本分支只维护 React。后端使用 Hertz admin API、Kratos operator gRPC 和公共 platform；目录与服务边界见 [服务拆分指南](../service_split.md)。已有资源加字段请看 [字段修改指南](../field_change_guide.md)。

开发前阅读 [后端手册](./backend.md)、[React 手册](./react.md) 和对应目录的 AGENTS.md。仓库约定以 AGENTS.md 为准。

## 开发顺序

1. 从需求与现有模块确认实体名、业务字段、所属业务模块，以及普通 CRUD、树、主从或租户资源的形态。
2. 在 `backend/deer/proto/operator/v1/` 定义 operator 的数据结构和 gRPC 方法。
3. 在 `backend/admin/idl/proto/admin/v1/` 独立定义 HTTP 路由、请求及响应。API 不能 import RPC 业务消息或使用 Go 类型别名；handler 通过显式字段转换调用 RPC。
4. 在 backend 执行 `make api openapi ts`，生成 RPC、API 模型、Hertz 路由、转换、OpenAPI 和 React 客户端。
5. 修改 operator 的 Ent schema 后执行 `make ent`；实现 Repository 和 Service。
6. 使用 `make register ENTITY=<entity>` 装配新模块，检查 `operator/cmd/server/wiring_ent.go` 和 `scripts/admin/tools/gateway-services.json`，构造函数有额外依赖时补齐。不要手改生成路由。
7. 在 React 添加 `apiClient` Service getter、Hooks、页面、路由及中英文词条。参考 [React 手册](./react.md)。

参考模块：`deer/proto/operator/v1/dict_type.proto`、`admin/idl/proto/admin/v1/dict_type.proto`、`operator/internal/data/dict_type_repo.go`、`operator/internal/service/dict_type_service.go`。

## React 约定

- 列表使用 ProTable，表单使用 DrawerForm + formRef，数据请求使用 TanStack Query。
- 用 `new PaginationQuery({ paging, formValues })` 创建分页参数，搜索使用 contains，ID 精确匹配。
- CRUD 请求体为 `{ data: {...} }`，自定义 RPC 的完整请求体绑定使用 `body: "*"`。
- Update 通过 Hooks 传递 `updateMask`，关联 ID 列表遵循后端手册的快照、字段掩码和同事务 Replace 模式。
- 写操作成功后 `queryClient.invalidateQueries({ queryKey: ['listXxx'] })` 刷新对应列表。
- 业务词条放 `src/locales/<lang>/_modules/`；菜单标题使用 `meta.title: 'routes:xxx'`，词条放 `_core/routes.json`。
- catch 必须记录原始错误对象或重新抛出。纯本地 best-effort 兜底需注释原因。
- 前端只导入 `src/api/generated/admin/v1/`，生成文件通过 `make ts` 更新。

## 验证与部署

在 backend 执行：

```bash
go test ./...
go build -o /tmp/go-admin-check ./admin
go build -o /tmp/go-operator-check ./operator/cmd/server
```

在 `frontend/admin/react/` 执行：

```bash
npm run typecheck
npm run build
```

启动后在 `http://localhost:7788/docs` 检查新接口，实测列表、搜索、分页、新建、编辑及删除；关联字段需覆盖新增、清空和重新添加。租户资源同时验证跨租户隔离与无权限访问。

已有部署需重新生成内嵌 OpenAPI、重启 operator，并在管理页执行「接口同步」；新增菜单使用「菜单同步」。默认种子只影响全新数据库。

需要列表打印时使用 React 的 `usePrintTemplateDialog()`；资源匹配、批量打印和 PDF 部署依赖见 [打印模板](../service_split.md)。
