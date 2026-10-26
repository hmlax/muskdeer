# 决策、默认值与待确认

## 用户已明确
项目 Muskdeer；Go 版本固定为 1.27.3；Hertz、Kratos、gRPC、Nacos、PostgreSQL、GORM、Redis、优先 NATS、Prometheus/Grafana；公共组件及 config/model Proto 集中；生成 ORM，不额外手写数据库 Model；独立 cron，其他服务支持多节点；Codex 持续推进 MVP。
每张表必须有 Id 自增主键、createAt NOT NULL、updateAt NOT NULL、deleteAt 可为 NULL。所有 GORM 数据操作显式使用 Model，禁止 Table("xxx") 及其封装绕过；业务、公共组件、脚本和测试一致执行。
所有接口的小数字段统一 string；Go 小数值、计算和比较统一 decimal.Decimal，禁止 float32/float64 及浮点中间转换，覆盖价格、数量、金额、费率、比例、百分比等全部小数。
外部行情必须接入 Binance、OKX 两家，Bitget、Bybit 为候选；每个策略实例可独立指定所用来源。用户原文 bitgit 暂按 Bitget 规范化命名。
公共目录与包名为 deer，构造函数为 deer.NewDeer；各 Kratos 业务服务分别使用统一的 Deer 封装，并增加 DeerApi。用户已确认各业务服务继续独立运行，通过统一 Deer 类型封装。
公开仓库为 https://github.com/hmlax/muskdeer.git，Go module 固定 github.com/hmlax/muskdeer；每个最小步骤完成且相关测试/检查通过后都须提交并立即推送远程，无需逐次确认，不能累计到阶段末尾才提交。

## 本准备包采用的可逆默认值
| 项目 | 默认 | 理由/变更影响 |
|---|---|---|
| API 封装 | DeerApi 组合复用 Deer，构造函数 deer.NewDeerApi | 落实用户增加 DeerApi 的要求，结合已有 Hertz/Kratos 架构采用此装配方式；具体参数/返回类型由 P02 实现 |
| 仓库结构 | 单 go.mod | 统一使用已确定的 module github.com/hmlax/muskdeer |
| 产品 | 内部自营现货 | 合约/客户策略改变风险模型，需另行明确 |
| 策略 | 做市 + 网格 | 先两模板验平台化，未声称用户已选择网格 |
| 界面 | 管理 API + OpenAPI | 用户只明确 Go API，未明确前端范围 |
| 所有权 | PostgreSQL lease + epoch | 提供可审计 CAS；最终网关仍需 fencing |
| 实例行情选择 | market_data_source 指定单一来源，无隐式换源 | 落实用户按实例选来源；多源聚合/自动主备不是此次默认范围 |

## 不阻止模拟开发的缺口
- 现有撮合/账户/清算接口及测试环境，见 EXCHANGE_CONTRACT。
- 外部对冲交易所选择、私有交易测试账户和 API 能力；行情首批 Binance/OKX 已明确。
- 实际交易对、资金预算、容量及报价延迟目标。

## 技术验证后可能需要决定
如果“Proto 生成结构直接作为 GORM 类型”不能正确处理精确金额/NULL 等要求，给出实测失败和两个可行路线，请用户决定；不可擅自手写 Model 规避。
如果最终交易网关无 fencing、幂等查询或失联撤单能力，明确能力缺口和改造建议；模拟开发继续，但真实接管保证及生产准备不能宣称完成。

## 变更记录
2026-10-26：创建准备基线；尚未核验本地仓库与任何现有接口。新决策须记录来源、日期、状态及受影响验收，助手建议不标用户批准。
2026-10-26：按用户本轮明确指令，将四个必备数据库字段与 Model 数据访问规则设为强制规范，覆盖 AGENTS、Proto/ORM、deer、启动指令、实施计划及 A01/A22；Proto/数据库采用 snake_case 字段映射属于本次实现约定。已检查当前工作目录，仅有准备包，无业务代码或实际数据库可供验证。
2026-10-26：按用户后续明确指令，将接口小数 string 与 Go decimal 规则扩展到所有小数字段，替换此前仅金额/数量或权威数值的有限表述；同步工程规则、Proto/ORM、接口、需求、启动指令、实施计划与 A23。decimal 库具体选型及版本待 P01 核验，当前仅更新规范。
2026-10-26：按用户要求明确首批 Binance/OKX、候选 Bitget/Bybit 及策略实例独立指定行情源，更新 R01/R02/R04、架构、接口、模型、P05/P07/P11 与 A04/A05/A24/A25。bitgit 按 Bitget 理解；首版单源字段、显式换源及无隐式主备为本次实现约定，未宣称用户另行要求多源聚合。
2026-10-26：前一轮公共组件命名记录由下列最终公共模块及服务封装约定替代。
2026-10-26：用户最终明确公共模块 deer、构造函数 deer.NewDeer、统一 Kratos 服务封装 Deer 及新增 DeerApi；经本轮澄清，各业务服务分别使用统一 Deer 封装。公共规范更名 DEER.md，所有 Proto 路径统一 deer/proto；API 组合 Deer、采用 NewDeerApi 为本次配套装配约定，更新 P02、A02/A26。
2026-10-26：用户明确选用 Go 1.27.3，将其设为固定版本并移出可逆默认值；同步工程约定、启动指令、P01/P02、生成规范与验收环境要求。官方工具链获取及依赖兼容仍由 P01 实测，不将版本选择当作已安装或验证通过。
2026-10-26：用户指定公开 GitHub 仓库 hmlax/muskdeer，并明确每步测试通过后提交及推送；据此固定 module，替换此前限制远程推送的约定。已匿名核验远程可访问且尚无分支；按授权安装项目规范到根目录，准备首次文档基线提交，业务开发仍从 P01 开始。
