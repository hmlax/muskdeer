# Proto 生成的 Go 代码

本目录保存由 deer/proto 生成的消息、带 GORM 标签的持久化模型、gRPC stub。子目录与 Proto 源文件的 common/服务归属对应，例如 deer/proto/common/config/v1/config.proto 生成到 deer/pb/common/config/v1/config.pb.go。

运行 make generate 生成并提交代码；make generate-check 在隔离目录重建并比较，不写回工作区。禁止手改 .pb.go，业务实现放在所属服务。各服务 Wire injector 仍在自身 cmd 下由 make wire 生成。
