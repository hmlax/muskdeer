# 数据库时钟租约

策略实例、账户执行分片和行情来源分片分别使用 StrategyLease、ExecutionShardLease、MarketPublisherLease 生成模型。记录均保留自增 Id、非空创建/更新时间及 nullable 删除时间，分别按实例、账户或feed ID唯一；释放时保留记录与 epoch，禁止删除记录后从零重新授权。feed身份包含来源/产品/LIVE或REPLAY，详见 [行情绑定](MARKET_DATA.md#p055-持久来源绑定与租约)。

进程启动时通过 `ownerid.New(ctx, instance)` 生成带 128 位随机数的 incarnation identity，同一进程重试保留它，重启重新生成。Nacos 实例名及请求关联编号不能代替 owner。身份格式检查不承担认证。

Acquire/Renew/Release 必须有原生 context deadline。TTL 为整数毫秒，范围 100ms–30s。事务锁定资源行后，使用 PostgreSQL `clock_timestamp()` 判定过期；返回的 ExpiresAt 仅供展示，调用者改写时间不改变授权。有效租约仅原 owner 可获取，重复 Acquire 返回原 epoch 和原到期时间，续期须显式 Renew。过期后即使同一 owner 再获取也递增 epoch；Renew 不复活过期记录。释放保留 owner/epoch、将到期时间置零；旧 owner/epoch 不能续期或释放接管者记录。epoch 耗尽时拒绝获取。

仓储借用 Deer 的 GORM 连接，不拥有连接池或后台任务。所有数据访问显式绑定具体生成 Model；时钟投影从已锁定的 Model 行读取，不使用客户端时钟或浮点转换。数据库故障保留 cause，但公开错误不输出 SQL 或连接内容。

租约是协调基础；模拟最终交易网关已按 P03/P04 在目标事务中验证授权，见 [模拟器](SIMULATOR.md)。行情最终快照写入也已按P05.6在提交时检查owner/epoch/数据库期限；业务续租工作器继续接入。续期结果不确定、过期或失去租约时必须关闭新增风险入口，接管后先恢复并对账。最终产生交易效果的系统须原子验证当前 owner、epoch 与数据库时间；单独查询租约后发送 RPC 不能保证安全。真实交易网关能力继续由 P11 单独验证。

实际 PostgreSQL 并发竞争、释放接管、旧 owner 拒绝、过期及新连接恢复证据见 [P03.7](validation/P03-07.md)。
