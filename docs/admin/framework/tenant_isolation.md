# 租户隔离

admin 负责 HTTP 入口、JWT、Redis 会话吊销、RBAC 和字段权限；operator 使用 Kratos gRPC 接收可信网关身份，并在 GORM 数据层强制资源归属。operator 不发布公网端口。

## 入口与身份

平台入口与商户入口分别由独立前端代理固定，浏览器不能通过伪造网关头切换身份。admin 解析商户编号，解析成功的 Portal Scope 与 JWT 的 tenant_id 必须一致。两端刷新 Cookie 按入口分离。

`deer/management/rpcbridge` 只转发 admin 校验后的操作者和允许的请求元数据；operator 从这些可信元数据恢复 ViewerContext。内部系统任务显式使用 SystemContext；不应把普通商户调用提升为系统身份。

## GORM 行级约束

持久化模型由 `deer/proto/operator/v1/model_*.proto` 生成。`operator/internal/data/store/core.go` 是类型化仓储操作的隔离边界：

- 模型存在 tenant_id 时，缺少 ViewerContext 的查询和写入均拒绝。
- 商户查询、更新和删除自动加入当前 tenant_id 条件。
- 商户创建记录强制写入当前 tenant_id；更新不能修改或清空归属。
- 只有显式的平台/系统 Viewer 可以跨租户操作。平台调用方仍受 HTTP 权限与业务规则约束。
- 需要数据范围的模型，读操作再叠加 SELF、UNIT、USER 等范围；无范围或显式 NONE 拒绝访问。
- 动态谓词是原生 GORM Scope。AND/OR/NOT 只组合业务条件，不能覆盖外层 tenant_id 限制。
- 自定义直接使用 GORM 的代码必须从生成模型出发，并显式保留相同的租户和资源归属检查。

关联资源必须同属当前商户。用户角色、岗位、组织、消息收件人等绑定在业务层检查引用对象，并在同一个事务中写入。事务客户端从原 GORM 事务构造，不能回退到根连接池。

## 权限与入口状态

商户角色创建或编辑只能授予当前操作者持有的权限；不可授予平台专属权限。接口同步按 HTTP 方法与路由保留接口 ID，避免自定义角色授权错配。平台租户管理接口必须拒绝商户调用。

有效期与停用状态由 HTTP 入口和认证服务校验；HTTP 入口检查不能替代这些数据约束。字段权限在 admin 返回前过滤，成员的角色、账号状态和会话吊销应即时生效。

## 验证

使用两个隔离商户覆盖 List/Get/Create/Update/Delete、显式伪造 tenant_id、跨租户关联、事务失败回滚、字段权限、状态门禁与即时撤权。测试必须证明其他商户的数据和授权均未变化。

- `store/core_test.go`：数据库级隔离、归属、聚合与记录不存在。
- 仓储和服务 SQLite 测试：资源绑定、批量操作和业务约束。
- `scripts/admin/test/tenant_lifecycle.py`：真实 HTTP 商户生命周期。
- `scripts/admin/test/tenant_permissions.py`：只读角色、字段限制和撤权。
- `scripts/admin/test/merchant_functional.py`：商户功能与平台权限拒绝。

命令与凭据管理参见 [流程测试指南](tenant_lifecycle_e2e.md)。
