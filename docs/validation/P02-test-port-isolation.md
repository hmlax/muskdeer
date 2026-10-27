# 隔离测试使用 Docker 动态端口

日期：2026-10-27。

指标步骤的完整集成先后两次因 Redis 固定宿主机端口 55440 绑定失败退出，尚未运行 Go 集成场景。宿主机 lsof 与 Docker published-port 列表均未发现当前占用者，未确定底层原因；不停止用户进程或重启 Docker。

compose.test.yml 改为由 Docker 分配四个 loopback 端口。runner 在当前随机 project 下查询实际 binding，严格要求单个 127.0.0.1:1..65535，再通过本次子进程环境传入 PostgreSQL DSN/端口及 Redis/NATS/Nacos 地址。Nacos 管理 fixture 同样使用该实际地址；不保留固定端口 fallback。所有版本、认证、私有 namespace、隔离卷及 scoped cleanup 保持原有协议。

7 项 Python 工具测试通过，新增动态地址传递/项目范围及非 loopback/零值/越界/多绑定拒绝；启动失败原异常、日志和清理测试保留。make fmt、lint、integration tag vet 与完整 make integration 通过；实际四容器分配到 127.0.0.1 的不同端口，原持久化、四种基础设施、两节点与 API mTLS/健康链路全部通过，临时容器/网络清理完成。

此修复单独暂存/提交。指标生产代码与其测试仍保留工作区，验证证据包含本轮工作区；远程 CI 以本次独立提交的实际结果为准。
