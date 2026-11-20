# 服务、配置与部署

HTTP 由 `backend/admin` 的 Hertz 原生路由和中间件处理，调用 `backend/operator` 的 Kratos gRPC 服务。operator 负责持久化和业务逻辑，platform 提供公共包、RPC IDL 与工具。API IDL 和 RPC IDL 独立维护，生成的适配器按字段转换。

## 配置

环境变量只承载 Nacos 连接：`NACOS_ADDR`、`NACOS_GROUP`、`NACOS_NAMESPACE_ID`、`NACOS_USERNAME`、`NACOS_PASSWORD`。应用配置来自同组的 `server.yaml`、`data.yaml`。示例位于 `backend/platform/deploy/infra/nacos/`；复制成未跟踪的本地文件后填写数据库、Redis、JWT、OSS 与加密配置。

admin 默认 HTTP `:7788`，operator 默认 gRPC `:7790`。gRPC 仅供内部访问。admin 的 `runtime.operator_endpoint` 留空时使用 Nacos 发现，也可显式设置 operator 地址。浏览器仅连接 admin HTTP。

密码重置和联系方式绑定使用独立的验证码发送配置，不依赖业务通知模块：

```yaml
runtime:
  smtp:
    host: smtp.example.com
    port: 587
    username: sender@example.com
    password: replace-with-secret
    from: sender@example.com
    from_name: Go Admin
    tls_mode: START_TLS
  sms:
    provider: ALIYUN # 或 TENCENT
    access_key_id: replace-with-secret
    access_secret: replace-with-secret
    sign_name: configured-sign
    template_code: configured-template
    app_id: ''
    region: cn-hangzhou
```

发送邮件时配置 SMTP，发送手机验证码时配置 SMS。未配置时发送接口明确返回失败；登录账号密码不依赖这些外部发送服务。配置更新后重启 operator。

## 双后台与租户

平台请求不携带商户头。每份 merchant-admin 部署通过 `/app-config.json` 固定商户编号，所有 API 请求携带 `X-Tenant-Code`。反向代理覆盖该头，后端将编号解析为 tenant ID，再与 JWT 租户校验。租户停用、过期、越权或访问其他租户数据均受后端约束；菜单按角色授权，平台管理菜单不会下发给商户。

前端构建和 Docker 入口见 [前端部署](../frontend/README.md)。backend 可使用 `docker compose -f docker-compose.infra.yaml --env-file .env.infra up -d --build`；先按配置模板发布 Nacos。数据库结构变更应单独执行，服务启动不执行 DDL。

## 生成和验收

```sh
cd backend
make api openapi ts
make wire-check
go test ./...
go build ./admin ./operator/cmd/server
```

`make hz` 会执行官方 `hz update`；生成文件禁止手改。`make infra-verify` 在已运行环境验证登录、菜单、刷新和退出。`make infra-test-tenant` 验证租户创建、隔离、权限和会话，会保留验收租户及私有报告。

移除的业务模块不再注册接口或启动后台任务。升级不会自动删除已有数据库表、数据、私有验收报告或备份；无法加载的旧菜单不会显示。源码清理完成后需重新构建并部署应用。

升级时请同步配置模板：删除 `runtime` 中旧的审计归档、备份、通知和打印选项；这些字段已从严格配置解析中移除。验证码发送配置迁至 `runtime.smtp` / `runtime.sms`。

`runtime.crypto_key` 同时用于凭证传输解密，双前端的 `VITE_AES_KEY` 必须与之匹配。更新这一配置时同步构建前端，并重启 operator。
