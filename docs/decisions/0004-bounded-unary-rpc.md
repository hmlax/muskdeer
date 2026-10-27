# ADR 0004：有界、实时发现的 unary RPC

日期：2026-10-27。状态：P02.7.4 本地验证通过，验收记录见验证文档；远程结果随独立提交核验。

每次业务 RPC 从 Nacos 读取新快照，按 Target.Service 和可选 InstanceID 选择端点。指定实例缺失时返回 Unavailable，不向其他 owner 发送；未指定实例只适用于可任意节点处理的无状态调用。查询失败不使用旧快照。命名成员资格与服务证书均不替代最终执行 epoch/fencing。

Factory 构造时加载有限服务许可的 mTLS 配置。调用从准入等待开始使用一个总期限，另有更短建连期限、硬并发上限及请求/响应字节上限。每次调用拥有一个 gRPC 连接，返回时关闭；Factory.Close 关闭准入、取消并 join 已准入工作，再由 Deer 关闭借用的 discovery。当前选择明确所有权和失联拒绝语义；尚未复用连接、缓存地址或增加 watch。连接与发现成本须在 P10 实测后再优化，不预先宣称容量。

锁定的 grpc-go v1.84.0 中 `WithDisableRetry` 仍允许透明重试，不能单独作为非幂等操作的安全边界。负数重试缓存参数在测试中触发 SDK 空指针，已撤销，不纳入实现。使用公开 NewStream 执行 unary 生命周期，在发送业务消息前调用 ClientStream.Context 固定已建立的尝试；该方法明确具有禁止后续客户端重试的效果。随后 SendMsg、CloseSend、接收一条响应并验证 EOF，拒绝多响应；不自行重试方法调用。

ClientStream 文档通常建议在 Header/RecvMsg 之后读取 Context，以保留重试能力。这里为满足禁止业务请求重放的要求有意提前固定尝试，丢弃返回的 peer context。前置 TCP/TLS/stream 建立阶段仍可能有 SDK 内部尝试，此时尚未发送业务消息。禁止业务重放不等于调用结果已知，超时/中断仍须由后续持久化意图、幂等键和查询恢复处理。

行为门禁使用真实 mTLS HTTP/2 server，在 HEADERS 后发送 REFUSED_STREAM，再以 PING ACK 证明客户端已经处理拒绝，才允许测试 codec 完成消息编码。保留固定操作时只有一次 HEADERS；删除该操作的负对照稳定观察到两次并使断言失败。该测试随 SDK 升级必须保留；不依赖 sleep 推测处理顺序。

当前 facade 明确只支持 unary，NewStream 返回 Unimplemented。后续有真实流合同再实现流的取消、期限和资源所有权，不把不受控的原始 ClientConn 暴露给业务。

来源：[grpc-go v1.84.0 重试选项](https://github.com/grpc/grpc-go/blob/v1.84.0/dialoptions.go)、[ClientStream Context 契约及实现](https://github.com/grpc/grpc-go/blob/v1.84.0/stream.go)、[原生 unary 生命周期](https://github.com/grpc/grpc-go/blob/v1.84.0/call.go)。
