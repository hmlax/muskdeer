# 行情协议与独立订单簿

协议核对日期：2026-10-27。首批只选择 binance、okx，候选仍不启用；外部 symbol 由调用方显式配置，Frame 的 source/instrument/symbol 必须与目标 Book 完全一致。不匹配事件不会写入另一个来源的订单簿。

## P05.1 序列与深度

Binance 现货使用 REST 快照锚点与 `U/u` 增量范围：快照安装后为 SYNCHRONIZING，不输出价格；忽略已覆盖的增量，下一有效范围必须覆盖当前序列+1，再进入 READY。缺口使当前簿 INVALID，必须重新获取快照并衔接，不能继续使用旧价格。依据 [官方 Spot Streams](https://github.com/binance/binance-spot-api-docs/blob/master/web-socket-streams.md#how-to-manage-a-local-order-book-correctly)。

OKX `books` 快照以 prevSeqId=-1 建立完整簿，更新的 prevSeqId 匹配上一 seqId。空更新允许相同序列；内容相同的最后一个重复事件不再次应用或刷新数据时间。维护重置仅在前序仍匹配时接受 seqId 回退，同时增加本地 generation；其他乱序/缺口失效。依据 [官方 Sequence ID 说明](https://tr.okx.com/docs-v5/en/#order-book-trading-market-data-ws-order-book-channel)。

OKX 的 JSON books checksum 已于2026-06-23停用，字段固定为0，不能再用于完整性判定；本实现使用序列链，网络接入必须使用 TLS。依据 [官方弃用公告](https://www.okx.com/en-sg/help/okx-order-book-channels-checksum-field-deprecation)。

深度价格/数量使用 decimal，更新数量0删除对应档位；同一侧重复的规范化价格、零快照数量、空一侧、交叉簿和超过容量都拒绝。每侧最多5000档，超过界限不静默截断；使用新的完整快照恢复。本地 generation 标识簿重建/序列重置，不能作为跨进程所有权或执行 fencing。

Book 属于单个 actor，调用者串行操作；输入切片不保留，内部深度 map 不外露。Top 只返回同步且形态有效的 BBO、来源、时间与序列，mid 使用显式 QuoRem 保留半个最小价格单位；仍须经过后续新鲜度/离群质量与风险校验才能驱动报价。

## P05.2 Typed 行情 JSON

`internal/marketwire` 解码 Binance REST depth、原始 spot depthUpdate 及 OKX books 的具体结构，绑定明确 instrument ID/外部 symbol 和本地 receive time。REST 响应本身没有 symbol/event time，沿原 HTTP 请求身份关联，不伪造交易所时间；增量及 OKX 使用实际协议时间。

价格/数量只接受 JSON string，直接按20位整数/18位小数解析为 decimal。Binance 档位必须2个字段，OKX必须4个字段且订单数为规范非负整数；空增量与0数量有效，快照空侧/0数量拒绝。必需数组和序列的缺失/NULL 与合法0序列明确区分；OKX checksum 只作可选整数元数据接收，不判断其数值有效性。

消息最多1MiB、数组最多5000项、结构深度最多64。先用 UseNumber 的 token 扫描核对具体 Go wire struct 的准确字段/schema，拒绝重复键、大小写别名、未知字段、错误类型和尾随数据，再解码回同一 typed struct；schema map 只保存字段类型，不承载或转换业务值。所有业务 JSON 始终使用显式字段，不经 map/any 转换桥或浮点。

decoder 保留取消 cause，错误文本不输出原始 payload；交易对或 channel/action 不匹配明确拒绝。

## P05.3 TLS 网络会话

`internal/marketfeed` 每个 Run 绑定一家来源/产品；构造不联网。使用固定公开 TLS 地址、原生 HTTP/握手/读写期限、无环境代理/重定向，无账户或交易权限。Gorilla WebSocket 固定 v1.5.3；[Go 漏洞记录](https://pkg.go.dev/vuln/GO-2026-6278)标明弱掩码随机数问题影响此版本之前版本，所选版本使用 crypto/rand。

Binance 使用[官方无鉴权 market-only 地址](https://github.com/binance/binance-spot-api-docs/blob/master/faqs/market_data_only.md)：`data-stream.binance.vision:443` 的原始现货 depth@100ms，以及 `data-api.binance.vision` 的 depth。先读取首条增量再请求 REST，读任务同时保留最多256条/8MiB缓冲，单消息1MiB；落后快照最多刷新3次。只在范围衔接后输出价格，已覆盖增量不刷新时效。服务器 PING 的 PONG 复制原 payload；默认23小时50分重建会话，早于24小时限制。

OKX 使用 `wss://ws.okx.com:8443/ws/v5/public`，显式 typed `books` 订阅；准确检查对应产品的确认消息后接受快照/增量。控制事件、pong、重复更新不制造新价格。空闲20秒发送文本 ping，再一段相同期限未响应则重建，见[官方连接与心跳约定](https://app.okx.com/docs-v5/en/)。error/维护通知、坏消息、序列缺口、缓冲溢出或连接失败先输出无价格 INVALID，再按配置的有限间隔重连同一来源，完全重新同步；无跨源 fallback。

单 actor 负责订单簿和应用写入；独立读任务负责读期限和协议控制回复，连接关闭可并发。Run 取消会终止 HTTP、关闭 WS，并等待 reader 与关闭回调退出。observer 串行且有期限，持久化/下游 observer 失败会终止 Run，由 Deer 上层处理；不在失败后悄悄跳过。process-local Connection/Generation 仅为诊断，不能充当跨进程 fencing。

`python3 scripts/go.py run ./cmd/marketprobe -source binance -symbol BTCUSDT -duration 30s -reconnect-after 5s`（OKX 改 source=okx、symbol=BTC-USDT）只读输出 JSONL，小数仍为 string。每个连接至少2次完整簿观察、至少2个连接后成功退出；人为限制会话寿命验证实际重连恢复。两家本轮实连证据见 [P05.3](validation/P05-03.md)。

## 当前边界

协议、网络回放和两家公共行情实连已通过；持久质量、分布式发布授权、market 服务装配和策略按源消费继续后续步骤。当前 READY 只表示协议同步；仍须检查时间/离群等质量与风险，不能直接驱动订单。A05/A24 的策略保护以及完整 P05/L1 尚未完成。
