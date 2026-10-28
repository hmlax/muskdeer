# 管理页面与身份 HTTP

P07.3提供登录、当前会话和注销的实际页面/接口，以及显式本地账户开通命令；P07.7提供typed管理查询gRPC，P07.8接入只读策略查询HTTP，P07.9交付实际策略列表/详情/版本页面，P07.13接入显式控制/操作结果HTTP，P07.14交付运行控制及独立结果页面。创建/参数发布/换源、行情/订单/成交/持仓/风险/审计查询及完整管理范围仍待后续，不计完整R08/A27通过。

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

协议、配置/边界及实际独立API→Nacos/mTLS→strategy→PG验证见[P07.8](validation/P07-08.md)。独立OpenAPI仍待交付。

## 运行控制与操作结果 HTTP

额外`admin_http.strategy_controls_enabled=true`才注册写入及操作查询，同时要求strategy_queries_enabled及原有Nacos/API mTLS客户端完整。strategy端须另外开启control_enabled。API借用同一Deer客户端连接调用独立StrategyControl，不写本地策略/操作表，不缓存用户授权。

| 方法/路径 | 结果 | 边界 |
|---|---|---|
| POST /api/v1/strategies/:strategyID/control | 新受理202；已终态原键回放200；Location指向具体操作 | 固定Origin、唯一Cookie/独立X-CSRF-Token/Idempotency-Key、严格JSON、当前operator及revision |
| GET /api/v1/operations/:operationID | 200，typed StrategyOperation及可空持久观察 | 唯一会话Cookie、当前viewer；不隐式执行控制或写结果 |

POST JSON只接受action、expected_revision、reason三个字符串。动作使用strategy.start/resume/pause/stop；revision为正规范int64字符串，原因规范化后1–256 UTF-8字节，幂等键16–64个已允许ASCII字符。拒绝数字revision、重复/未知/null字段、尾随数据、超过2048字节body及任何query；会话/CSRF不能通过业务JSON伪造。操作ID同样要求正规范int64，无GET body/query。停止不等于平仓或取消流程完成，实际业务证明见[管理控制](ADMIN_CONTROL.md)。

受理响应为ACCEPTED且没有完成时间/观察事实；终态SUCCEEDED或SUPERSEDED保留实际见证。所有ID/revision/毫秒及小数维持JSON string。版本/同键内容冲突返回409/CONFLICT，未认证401并清Cookie，权限/CSRF/Origin403，非法400，不存在404，依赖/协议故障503；错误不返回内部cause。每次调用原生上限3秒，服从更早父期限，不透明重发写请求。跨字段回复身份不符拒绝，幂等键读取使用拥有独立字符串的GetAll，避免Hertz后续Peek读取覆盖。

登录/当前会话增加布尔strategy_controls_enabled，表示API配置能力；viewer仍不能写，实际事务重新判断权限。未启用时两个路由都返回API JSON 404，不因查询已开而隐式启用控制。实际链路与测试夹具边界见[P07.13](validation/P07-13.md)。

## 策略查询页面

开启查询后，登录页和会话导航显示真实能力入口；`/strategies`为分页列表，`/strategies/:strategyID`为详情与独立分页版本历史。深链接未登录时先显示登录，再返回请求页面；刷新直接由Hertz提供嵌入SPA。未启用时不注册策略页面/查询路由；非规范页面ID为404，不影响API错误语义。

页面展示真实账户/交易对身份、模板、回放/实时模式、实际状态与请求目标、请求和生效配置/来源、具体Maker/Grid参数与历史版本。未生效显示“未生效”，历史schema0不补默认参数。只读列表和版本各页20条；前后页使用服务器ID游标。金额、价格、比率和版本/ID由typed解析保持string，仅整数计数使用JS number，ID校验使用BigInt而不舍入。

查询拥有6秒浏览器等待上限并在切页/卸载时取消；显示加载、真实空列表、找不到、权限/依赖故障和重试。业务401会清除页面会话并返回登录。配置写按钮继续后续，运行控制另见下节；不能仅依赖能力或角色显示充当授权。移动端参数完整可见，宽表在本地滚动容器中展示，不扩大整页宽度。

`make admin-strategy-browser`构建原生API/strategy，启动独有PG/Nacos等测试依赖及固定Chromium，经真实接口覆盖21个实例/21个版本分页、两模板来源、18位小数、刷新/深链接、404、网络读取故障重试、实际注销后会话失效、390px布局及真正空数据库。数据为显式未注资隔离夹具，非业务响应mock，不代表创建/启停或完整A27。该命令已加入CI及make e2e，证据见[P07.9](validation/P07-09.md)。

## 运行控制与结果页面

控制已启用时，实例详情增加运行控制区域。operator/admin按实际状态选择启动、暂停、恢复、停止，填写1–256 UTF-8字节原因并明确确认；当前实际状态已达同一目标时禁用重复按钮，viewer仅展示只读提示。状态更新期间或查询失败时不能基于旧快照发起新操作；真正权限/版本仍由业务事务判断。

写请求只有在用户确认时发送，使用当前修订与新幂等键；同步提交锁防止重复事件。网络/期限不确定时保留原键、原修订和原因，按原请求重试，不生成新键，也不把未确认显示为成功。403明确未授权，409提示版本变化后刷新重选；离开页面/会话过期取消本地等待，不代表撤回服务端操作。新请求发出后移除旧结果，避免混淆两次操作。

最新收据显示操作ID、动作/原因、绑定配置/来源、请求目标及实际见证，分别呈现ACCEPTED/SUCCEEDED/SUPERSEDED。实际结果每秒有界查询、单次6秒上限、自动窗口20秒，仍未完成时可手动查询；卸载取消请求和定时器。请求受理与实际完成分别展示，停止/暂停须后台完成原订单义务证明。

`/operations/:operationID`提供独立持久结果页面，可深链接登录和刷新，保留长ID/revision为string与BigInt校验，拒绝无证明的运行成功。只有控制启用时Hertz才注册页面，非法ID为404；客户端不在local/sessionStorage保存凭据或请求。

`make admin-control-browser`在专属L1回放环境启动实际API/market/execution/strategy及模拟核心资金/预算，通过浏览器完成Grid启动→暂停→恢复→停止，每阶段由Go核对四档核心资金冻结或终态归零；真实提交后丢失响应仍按原键重试、独立结果刷新/深链接、viewer无写按钮及390px布局。新多阶段测试有单独90秒场景/进程上限，既有40秒进程和业务原生期限保持。该命令加入CI/make e2e；范围、视觉和核算证据见[P07.14](validation/P07-14.md)。

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
