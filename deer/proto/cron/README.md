# Cron 协议

cron 当前只提供标准 grpc.health.v1.Health，由公共 Deer 注册，复用 gRPC 上游健康协议；没有自定义业务 Proto，也不创建占位消息。后续定期业务合同放到本目录的 v1 子目录。
