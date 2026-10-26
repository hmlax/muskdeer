# 给本地 Codex 的启动指令

在 Codex 中打开 ~/go/src/gitlib/bitdatech/Muskdeer，将下面整段作为开发任务发送：

```text
请根据本仓库 AGENTS.md、README.md 及 docs 下的规范，持续开发 Muskdeer 至 L1 模拟 MVP 验收通过，并为 L2 真实测试环境接入做好适配。

先检查当前目录、就近 AGENTS.md、git 状态、现有代码和远程地址，保留我的已有修改。检查安装器跳过的同名文件是否已合并，不将文档中的预期功能当作已实现。
公开仓库固定为 https://github.com/hmlax/muskdeer.git，Go module 固定为 github.com/hmlax/muskdeer。核对 origin 和远程分支，保留已有历史；初始空仓库采用 main。

按 PROGRESS 从首个未完成任务继续；若 P01 已完成，复用其验证记录和固定版本。Go 由用户固定为 1.27.3，开发、生成、测试、CI 与构建保持一致，不重新选版。P01 的官方工具链、Hertz/Kratos/gRPC/Nacos SDK 及 Proto→GORM→PostgreSQL 兼容结果见 docs/validation。公共模块包名 deer，各 Kratos 业务服务分别使用 deer.NewDeer 构造的 Deer 封装；API 使用 deer.NewDeerApi 构造的 DeerApi，复用 Deer 并托管 Hertz，保留各业务服务独立进程及身份。按 docs/DEER.md 实现并通过 A02/A26。持久化类型只从 deer/proto 下的 Proto 生成，禁止手写第二份数据库 Model。

api 用作管理界面，用户已确认包含可操作管理前端页面和后端接口。按 R08/P07 实现登录、概览、策略/行情源、订单/成交/持仓、风险与审计及异步结果页面，经 api 的管理接口调用业务服务并通过 A27；页面权限和后端鉴权一致，前端小数保留字符串。前端源码规划 web/admin，构建产物由 api/Hertz 托管；框架和构建版本实施时锁定。现有 DeerApi 生命周期或 OpenAPI 文档不能代替管理页面交付。

每张表必须有 Id 自增主键、createAt NOT NULL、updateAt NOT NULL、deleteAt 可为 NULL，按 docs/PROTO_AND_MODEL.md 统一字段映射并落实到生成模型与迁移。所有 GORM 数据操作显式使用 db.Model(&model.X{})，事务中用 tx.Model(&model.X{})，并贯穿 context；禁止任何 DB 变量上的 .Table(...)，业务、公共组件、脚本和测试均须遵守。通过 A01/A22 后才算完成数据库规范验收。

所有接口的小数字段统一定义为 string，包括请求、响应、嵌套字段和事件；Proto 禁止 float/double，JSON 必须输出十进制字符串。Go 小数统一使用 decimal.Decimal 存储、计算和比较，禁止 float32/float64 及浮点中转；配置、适配器、数据库和测试同样遵守。P01 核验并锁定统一 decimal 库，按 A23 验证精度、舍入和边界转换。

外部行情必须分别接入 Binance、OKX；Bitget（原文 bitgit）、Bybit 为候选。做市、网格等每个策略实例通过 market_data_source 独立选择来源并持久化配置，所选源失效不隐式切换；换源走配置版本及报价保护流程。按 A24 验证隔离、选择和恢复，A25 单独记录两家真实公共行情接入证据，不能以模拟接入冒充实连。

依照 docs/IMPLEMENTATION_PLAN.md 逐项实现，每完成一个可独立验证的最小步骤就运行对应测试/检查、修复失败、更新 docs/PROGRESS.md，提交本步相关代码与文档并立即推送远程工作分支。确认远程分支 SHA 与本次提交一致后再继续下一步；每步报告测试结果、提交 SHA、分支及推送结果。不要攒到阶段末尾统一提交，也无需逐次询问提交或推送权限。不用空实现、假测试、TODO handler 或静默跳过依赖冒充完成。

优先实现确定性交易模拟器、订单/资金/持仓闭环，再行情和做市，再参数化网格、对冲与多节点恢复。真实撮合接口尚缺时完成模拟适配并记录合同缺口，继续不受影响的工作；不要臆造现有接口。

资金和订单必须支持 UNKNOWN 查询恢复、幂等、预算原子预留、成交去重、outbox/inbox、失联撤单以及最终执行端 fencing。多节点测试必须验证旧节点恢复不能继续下单。

可自主进行本地可逆实现、测试及测试通过后的逐步提交/推送，用户已授权同步到上述公开仓库；不要启用生产密钥、真实资金、生产部署或破坏性迁移。推送失败保留本地提交并解决原因，不强推、不声称已同步。遇到真正影响实现的选择集中提出具体问题，同时继续独立工作。

完成时给出实际验收证据、运行方式、未完成项和风险；L1 与 L2 分开报告。若会话中断，在 PROGRESS 写入可恢复的下一步，后续从该记录继续。
```

## 中断后继续
“请读取 AGENTS.md 和 docs/PROGRESS.md，核对实际代码及测试，从首个未完成且依赖满足的任务继续，不重复已验证工作。”

单次 Codex 会话受运行环境和上下文限制；这些文件支持恢复迭代，不构成无人值守永久运行或必然一次完成的保证。
