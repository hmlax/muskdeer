# 管理页面与身份 HTTP

P07.3提供登录、当前会话和注销的实际页面/接口，以及显式本地账户开通命令；P07.7提供typed管理查询gRPC，P07.8接入只读策略查询HTTP。策略页面、配置/控制写操作、行情/订单/成交/持仓/风险/审计查询及异步操作仍待后续步骤，不计完整R08/A27通过。

## 开启入口

`admin_http`是显式可选配置，仅允许api角色、PostgreSQL及字面量loopback HTTP监听。启动借用Deer已有连接，只检查迁移历史，不迁移、不创建用户、不启用交易任务。`cmd/doctor`和实际入口执行同样的管理配置检查。

本地开发先`make dev-up`，加载其`.cache/dev/environment.sh`，将生成的`.cache/dev/api.json`复制为`.cache/dev/admin-api.json`，保留本工作区动态数据库地址。确认http.address为`127.0.0.1:59000`并加入：

```json
"admin_http": {
  "enabled": true,
  "public_origin": "http://127.0.0.1:59000",
  "allow_insecure_loopback": true
}
```

这是配置中的一个字段块，必须放在完整ServiceConfig对象内。本地HTTP只接受显式开关及字面量回环地址；HTTP的端口0仅用于与受拥有监听匹配的动态测试/开发端口。HTTPS外部入口配置固定、规范化的`public_origin`，关闭insecure开关，由本机可信TLS入口转发到回环监听，保留配置的Host；服务不信任X-Forwarded-*。HTTPS cookie使用`__Host-`前缀、Secure、Path=/、无Domain，HTTP开发使用独立cookie名。生产入口/部署另行验收。

```sh
make build
.cache/bin/migrate -apply -config .cache/dev/admin-api.json
.cache/bin/admin -config .cache/dev/admin-api.json -username local-admin -role admin -password-env MUSKDEER_ADMIN_PASSWORD
.cache/bin/muskdeer -role api -config .cache/dev/admin-api.json
```

执行开通命令前，在当前终端通过安全输入设置并export `MUSKDEER_ADMIN_PASSWORD`；命令不接受密码参数，不提供默认密码，不输出口令/哈希。每次明确指定viewer/operator/admin之一，数据库用户创建及审计同事务。用后unset口令环境变量。页面入口为`http://127.0.0.1:59000/login`；服务自身不执行TLS终止。

## 页面及协议

React页面通过同源fetch调用Hertz，初始检查真实会话；显示加载、坏凭据、依赖故障、当前用户/角色/固定到期时间和注销。`/`、`/login`、`/session`可直接刷新，JS/CSS来自嵌入的编译产物，无CDN、外部字体或内联脚本。页面不将bearer放入响应JSON、localStorage或sessionStorage。

| 方法/路径 | 结果 | 边界 |
|---|---|---|
| POST /api/v1/auth/login | 200，typed session，两个独立cookie | 固定Origin、application/json、严格双字符串字段，拒绝重复/未知/null/尾随数据 |
| GET /api/v1/auth/session | 200，typed session | 唯一canonical会话cookie，仓储重新检查当前权限/到期 |
| POST /api/v1/auth/logout | 204，撤销会话并清cookie | 固定Origin、唯一会话cookie、独立X-CSRF-Token |

会话JSON的user_id/session_id/expires_at_ms是字符串，role使用已定义枚举；不返回任何秘密。401/403/400/503使用固定错误code/message，内部错误不序列化；不存在的`/api/*`返回JSON 404，静态页面不吞掉接口错误，错误方法返回JSON 405。管理响应no-store、Vary Cookie、nosniff；CSP限制资源/表单同源，禁止frame嵌入和base修改。登录/会话/注销的持久约束见[认证基础](ADMIN_AUTH.md)。后续业务写必须在效果事务内重新授权，不能把此页面的角色展示当作授权。

## 策略查询 HTTP

显式加入 `admin_http.strategy_queries_enabled=true` 后注册以下只读路由。API需要同环境Nacos及Deer拥有的grpc_clients，允许 `strategy.grpc`，使用已签发的 `api.grpc` mTLS客户端身份、正数且最多5秒的RPC原生上限；实际handler上限3秒并继承更早的HTTP取消/期限。strategy需显式开启 `business.strategy.management_enabled`，见[业务查询合同](ADMIN_STRATEGIES.md)。未开启时仍返回API JSON 404，不查询本地业务表或回退其他服务。

| 方法/路径 | 返回typed合同 | 参数 |
|---|---|---|
| GET /api/v1/strategies | ListStrategiesReply | after_id默认0，limit默认50、范围1–100 |
| GET /api/v1/strategies/:strategyID | GetStrategyReply | 正规范int64实例ID，无query |
| GET /api/v1/strategies/:strategyID/versions | ListConfigVersionsReply | 同实例独立版本分页，after_id/limit同上 |

调用使用唯一现有会话Cookie，API自行构造AdminSession，业务服务在同一只读快照内重新验证viewer权利；没有将API身份结果当作业务授权。拒绝GET body、重复/未知参数、非规范整数、溢出/负游标。不存在资源404、会话无效401并清cookie、角色拒绝403、依赖/协议故障503；后端私有错误不返回。路由不开放POST等写操作，也不启动交易。

响应从具体Proto直接序列化为HTTP JSON，采用proto字段名；int64 ID/版本/毫秒和所有小数均为JSON string，grid_count等int32计数仍为整数。Maker/Grid是parameters中的具体oneof，schema0参数为空；requested_config与active_config分别表示请求与实际生效，未生效为null/presence缺失，不能据此显示为已运行。该序列化是实际传输边界，不通过JSON/map复制业务对象。登录与当前会话增加布尔 `session.strategy_queries_enabled` 能力标识，供后续页面只展示已启用入口；标识不授予业务权限。

协议、配置/边界及实际独立API→Nacos/mTLS→strategy→PG验证见[P07.8](validation/P07-08.md)。当前React仅身份页面，策略页面和独立OpenAPI仍未交付。

## 构建与浏览器验证

精确版本见[决策](DECISIONS.md)：Node24.22.1/npm11.19.1，React19.3.1、TypeScript7.0.2、Vite8.3.5、Playwright1.64.1；版本/完整性及各包许可证经package-lock记录（包括MIT/Apache-2.0）。Node来自官方归档，4个平台SHA256在`tools/web-versions.json`，下载/展开有界且校验后才执行，拒绝目录穿越/越界链接/特殊文件；工具链、npm cache和测试Chromium全部在.cache，不改全局Node。

```sh
make web-install web-check web-build
make web-generated-check
make verify
make admin-browser
```

构建产物`web/admin/dist`随源码提交并由Go embed嵌入，普通Go构建/运行无需Node；运行时React/ReactDOM/Scheduler的[许可文本](../web/admin/public/assets/third-party-notices.txt)一同嵌入。修改前端后必须重新构建并提交产物；CI从锁文件安装（ignore-scripts）、类型检查及重建差异检查，再执行实际浏览器场景。浏览器命令安装固定Playwright对应Chromium，Linux通过Playwright安装所需系统依赖；使用独有动态端口PG、实际admin/muskdeer命令，所有自建进程/数据库有界清理，不操作已有资源。截图保存在`.cache/admin-browser/run-*/`，fixture只有明确测试账户，失败日志只保留固定阶段和错误分类，避免Playwright call log回显输入或Cookie。

浏览器场景覆盖错误密码、登录、角色、HTTP-only cookie/独立CSRF、深路由刷新、字符串身份字段、接口404、注销后旧会话拒绝和390px移动布局。Secure cookie的TLS属性另由Hertz协议测试验证；本地浏览器场景使用显式loopback HTTP。完整管理业务验收随后续能力接入，不把本命令成功等同完整A27。
