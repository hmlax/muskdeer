# 已有资源增加或调整字段

前端共享代码位于 `frontend/src/`，平台和商户应用分别位于 `frontend/platform-admin/`、`frontend/merchant-admin/`。后端命令在 `backend/` 执行。

## 先确认字段归属

- 只有页面展示或录入变化：修改对应列表、表单和翻译，使用 API 已有字段。
- 业务/RPC 字段变化：修改 `deer/proto/operator/v1/<resource>.proto`。
- HTTP 字段变化：独立修改 `admin/idl/proto/admin/v1/<resource>_types.proto`；API 不 import RPC 业务消息。
- 存储字段变化：修改 `deer/proto/operator/v1/model_<resource>.proto` 的字段与 GORM 标签。禁止手写另一份持久化 PO 或修改生成文件。

保持字段编号不变；新增字段使用未占用编号。明确 optional、零值、空字符串、空列表和 NULL 的业务语义。修改既有字段类型时先确定数据库迁移与旧数据兼容策略。

## 后端修改

1. 根据用途分别修改 model、RPC 和 API proto。
2. 在仓储新增及更新路径接上生成 setter；DTO/模型转换使用明确字段，不经 JSON/map 中转。
3. 检查 UpdateMask：普通局部编辑不得清空未修改字段；显式空值可清空对应列，false 和 0 需要原样落库。
4. 修改枚举时同步业务校验和枚举转换。不可变字段不接入更新路径。
5. 为新增列/索引编写独立数据库迁移。生产构造器不会自动增加列；仅隔离测试显式创建 schema。

```bash
make api       # 包含 make models 和 make hz
make openapi
make ts
```

仅持久化标签或模型生成器调整使用 `make models`；仅 HTTP 契约调整使用 `make hz`。生成后检查差异和重复生成稳定性。

## 前端与验证

在共享列表、表单、搜索项和翻译中接入 API 字段；平台专有功能不能进入商户端。保存掩码只包含真正修改的业务字段，表单关闭保护遵循 [关闭行为规范](form_drawer_close_guard.md)。

运行两套前端的 `npm run typecheck`、`npm run build`，后端 `go test ./...` 并构建 admin/operator。数据库验证覆盖旧记录读取、新字段创建、局部编辑、零值、清空、不可变字段以及跨租户访问。

新增资源或端点继续参照 [CRUD 模块指南](crud_module_guide/backend.md)。
