# Go Admin 双前端

`platform-admin` 是平台管理后台，`merchant-admin` 是独立部署的商户管理后台。两端使用 React、Ant Design、React Query 和共享 API 客户端，各有入口、环境配置、构建产物和 Vite 缓存。

## 开发与构建

```sh
cd frontend
pnpm install --frozen-lockfile
npm run dev:platform # http://localhost:15888
cp merchant-admin/.env.development.local.example merchant-admin/.env.development.local
# 填入已有且启用的 VITE_MERCHANT_CODE
npm run dev:merchant # http://localhost:15889
npm run typecheck
npm run test:tenant-routes
npm run build
```

两套应用只读取各自目录的 `.env*`。`VITE_APP_TITLE` 是标题，`VITE_SERVER_PORT` 是端口，`VITE_API_URL` 默认 `/`，`VITE_PROXY` 将 `/admin` 转发至 `http://127.0.0.1:7788/`。`VITE_AES_KEY` 与 Nacos 的 `runtime.crypto_key` 一致。

平台不使用商户编号。商户入口在加载 React 前请求 `/app-config.json`，读取 `{ "merchantCode": "your-code" }`；开发与 preview 由 `VITE_MERCHANT_CODE` 提供此配置。缺失或非法配置会停止启动。登录表单只收账号、密码和验证码。

商户 API 请求携带 `X-Tenant-Code`，代理将其固定为部署编号；平台代理移除此头。JWT、角色授权、字段权限和租户数据隔离由后端执行。商户菜单来自服务端角色授权；本应用未包含的页面会被过滤。平台产物包含租户、菜单/API/权限点和全局设置页，商户产物保留管理员、角色、组织、审计、文件及个人设置。

认证、偏好、标签、锁屏、语言和搜索缓存按应用与商户隔离，刷新 Cookie 使用当前入口的 `refresh_exp` 或 `refresh_exp_<code>`。首页为通用欢迎页。

## Docker

```sh
# 从配套 Nacos data.yaml 的 runtime.crypto_key 注入 VITE_AES_KEY。
# FRONTEND_CONFIG_REVISION 使用配套配置修订号；密钥变更时必须同时更新，避免复用旧构建缓存。
export FRONTEND_CONFIG_REVISION=your-config-revision
MERCHANT_CODE=your-code docker compose up --build -d
# 或分别构建
docker build --secret id=frontend_aes_key,env=VITE_AES_KEY --build-arg FRONTEND_CONFIG_REVISION --build-arg ADMIN_APP=platform -t go-admin/platform-admin:local .
docker build --secret id=frontend_aes_key,env=VITE_AES_KEY --build-arg FRONTEND_CONFIG_REVISION --build-arg ADMIN_APP=merchant -t go-admin/merchant-admin:local .
```

构建必须提供配套 `VITE_AES_KEY` 和配置修订号，缺失会失败；不使用仓库 `.env` 示例值构建部署镜像。BuildKit secret 只用于构建时向 Vite 传值，不写入镜像环境变量或构建参数。浏览器必须使用配套构建产物才能正确提交加密口令，接口脚本使用后台密钥登录成功不代表浏览器产物已配对。

镜像构建时固定应用类型，运行 merchant 镜像时必须设置 `MERCHANT_CODE`。`ADMIN_API_UPSTREAM` 默认为 `http://host.docker.internal:7788`；Linux 的独立 docker run 需加 `--add-host=host.docker.internal:host-gateway`，Compose 已配置。

nginx 转发 `/admin/` 并覆盖商户头，配置和 HTML 禁止缓存，TLS 由部署入口处理。两端输出分别为 `platform-admin/dist`、`merchant-admin/dist`。

## 扩展

共享页面在 `src/pages/app/`，每端通过 `pages.ts` 决定可打包页面。HTTP 客户端来自 `backend/admin/idl/proto/admin/v1/`，在 backend 执行 `make ts` 更新；禁止手改 `src/api/generated/`。新增功能见 [CRUD 指南](../docs/crud_module_guide/react.md)。
