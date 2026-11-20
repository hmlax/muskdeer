# 租户与权限流程验收

## 独立环境完整验收

此方式使用当前源码构建的两个进程、专用 PostgreSQL / Redis / MinIO / Nacos，从测试数据库结构和基础角色开始创建真实业务资源。不会修改正在运行的应用或现有基础设施数据。

```sh
cd backend
python3 -m pip install -r scripts/admin/test/requirements.txt
mkdir -p /private/tmp/go-admin-core-e2e/bin
go build -o /private/tmp/go-admin-core-e2e/bin/ ./admin ./operator/cmd/server
python3 scripts/admin/test/core_e2e.py --binary-dir /private/tmp/go-admin-core-e2e/bin
```

需要 Docker Compose，默认占用专用 Nacos 端口 18849 / 19849，其他服务使用临时端口；同一机器上串行运行。脚本仅允许 `go-admin-core-e2e` 前缀的专用 Compose 项目，结束后停止测试进程、删除专用配置并清理测试容器和卷。

覆盖实际创建租户及管理员、同名账号隔离、登录/刷新/退出、租户停用与到期、权限组/权限点及 API/菜单关联、租户管理员创建角色和普通用户、提权拒绝、字段隐藏、即时撤权、权限/角色/用户禁用恢复。使用非默认的部署 AES 密钥，避免默认值掩盖配置问题。

逐步结果、请求状态和私有进程日志写入 `platform/artifacts/core-framework-e2e/<run>/`。隔离环境清理后，记录中的测试账号不可继续登录；不覆盖其他验收目录的账号和最新报告。

## 对已运行环境验收

运行条件：数据库结构、Redis、MinIO、Nacos、admin 与 operator 已准备好，平台管理员可登录。默认通过 `http://localhost:15888` 访问；`E2E_BASE_URL` 可指定直连 admin 的地址。

```sh
cd backend
python3 -m pip install -r scripts/admin/test/requirements.txt
make infra-test-tenant
```

脚本先验证平台登录、创建租户及管理员、商户初始化、同名账号跨租户隔离、停用/到期门禁、刷新与令牌撤销。随后创建权限组、权限点、角色和普通管理员，验证按钮/API 授权、字段脱敏、权限变更与撤销。

新建验收资源会保留。报告和账号写入被忽略的 `platform/artifacts/tenant-e2e/`、`platform/artifacts/tenant-permissions/`，凭据文件权限为 0600；不要提交或公开这些文件。平台账号可用 `ADMIN_SMOKE_USERNAME` 和 `ADMIN_SMOKE_PASSWORD` 指定。验证码仅从测试环境 Redis 读取自己的挑战，接口认证和授权保持启用。

`test_tenant_lifecycle.py` 的单元测试验证 HTTP 失败、商户头和凭据报告逻辑，不需要运行中的项目服务。当前框架不包含套餐、配额或业务消息的验收步骤。
