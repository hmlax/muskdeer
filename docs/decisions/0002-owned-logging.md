# ADR 0002：实例拥有的 JSON 日志与同步 lumberjack 维护

日期：2026-10-26。状态：P02.3 实现约定。关联 A02/A03/A26；进程入口的框架日志桥接仍待集成。

## 原因

[lumberjack v2.2.1](https://github.com/natefinch/lumberjack/blob/v2.2.1/lumberjack.go) 的 mill 在首次打开文件时启动后台 goroutine，Close 只关闭当前文件，没有结束维护协程的入口。实例反复构造/关闭不能据此满足 Deer 的资源所有权约定。原实现的毫秒备份名还可能在快速轮转时覆盖此前文件。

未修改的源码在本机行为测试中复现：关闭后再次写入成功、Rotate 返回时 gzip 尚未完成、保留清理尚未完成、12 次快速轮转少于 12 个备份；单独运行并发写入测试时，测试本身通过，但 goleak 检出仍等待的 millRun。

## 决策

保留 lumberjack 的大小轮转、UTC 命名、保留和 gzip 路线，使用固定 v2.2.1 源码的仓库内维护版本 third_party/lumberjack。保留 MIT LICENSE、上游 commit、模块校验和和原文件 SHA-256，具体改动见 [来源记录](../../third_party/lumberjack/README.md)。保持单 go.mod，不替换其他 SDK 的传递依赖。

维护版在打开/轮转时同步执行保留和压缩，返回错误，不创建后台协程；Close 为终态并可重复调用；备份命名避开已存在的原始/gzip 文件。移除可变全局测试 hook，文件关闭和维护错误向调用者保留。继续运行项目完整 lint/race/goleak，不为第三方来源文件新增忽略规则。

deer/logging 使用标准库 slog JSONHandler，提供 LogAttrs 的即时错误返回、Slog 适配及 Kratos Logger 接口。仅保存首个输出错误，Close 返回该错误及资源关闭错误，避免错误不断累积占用内存。stdout 为借用输出，关闭实例时不关闭进程 stdout。

日志基路径转换为 `<目录>/<service>/<instance>/<文件名>`。Linux/macOS 使用独占、非阻塞文件锁拒绝同一日志文件的第二个写入者；锁随文件句柄关闭释放，保留空的 .lock 协调文件以避免 unlink/inode 竞争。身份路径只接受有界 ASCII 字母、数字、点、下划线和连字符。其他系统的文件日志明确报不支持，stdout 日志仍可用。

日志初始化作为 Deer 的首个资源，其他依赖逆序释放后最后关闭日志；DeerApi 直接复用所组合 Deer 的 logger。构造函数不改变 slog/Kratos/Hertz 的进程全局 logger。实际业务进程入口尚未提供，因此框架默认日志尚未完成统一桥接，不能将整个 A03 或 P02 标完成。

## 字段与边界

每条 JSON 根记录保留 service/instance 与标准 time/level/msg，调用者字段（包括内联 group）不能覆盖这些值。结构化敏感 key/group（凭据、token、签名、cookie、完整 body/payload 等）被替换；decimal.Decimal 输出十进制字符串；任意结构体/map/slice 不直接序列化为日志值，未知 Any 值输出占位标识。错误记录采用文本表示，避免错误类型自带的 MarshalJSON 绕过策略。

脱敏不承诺从任意自然语言中识别秘密；调用者必须使用固定消息和受控属性，不把密钥、DSN、签名或敏感请求拼进消息/错误文本。request_id/strategy_id/operation/error_code 由调用边界以类型明确的属性提供，自动请求上下文注入仍待 HTTP/gRPC 中间件实现。

保留的根字段按大小写不敏感比较；同名根 group 放到 attributes 命名空间下，保持内层 group 名称并避免与实例身份冲突。

## 代价与验证

轮转、gzip 与清理带来同步文件 I/O 延迟。测试以 1 MiB 小阈值检查真实轮转、压缩及记录完整性，不声明高频日志吞吐或硬实时停止保证。后续若测量显示瓶颈，应设计有容量、退出和故障传播约定的日志队列，不能恢复无所有者后台 worker。

验证命令和实际结果见 [P02.3](../validation/P02-03.md)。
