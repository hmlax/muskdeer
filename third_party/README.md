# Proto dependencies

tagger/tagger.proto 与 tagger/LICENSE 原样取自 github.com/srikrsna/protoc-gen-gotag v1.0.2（MIT），用于在 Proto 字段上声明 GORM tag。生成插件版本固定于 tools/versions.json；生成的 Go 代码引用同版本模块的 tagger 包。

上游：[protoc-gen-gotag](https://github.com/srikrsna/protoc-gen-gotag/tree/v1.0.2)。

hertz/api.proto 与 hertz/LICENSE 原样取自 github.com/cloudwego/hertz/cmd/hz v0.9.8（Apache-2.0）。生成时通过明确的 go_package 映射把 HTTP 选项生成到 deer/proto/hertz；hz 使用 --use 复用项目 API DTO，不再生成第二套接口结构。

## OpenAPI 验证依赖

openapi/schema-2022-10-07.json 原样来自固定的[OpenAPI 3.1 官方 JSON Schema](https://spec.openapis.org/oas/3.1/schema/2022-10-07)，33992字节，SHA256 da01ba28852cac0de53893797cb8d1942bc3b05084f526dcc216717dec314ed0。openapi/LICENSE 来自 [OAI/OpenAPI-Specification 3.1.0](https://github.com/OAI/OpenAPI-Specification/blob/3.1.0/LICENSE)（Apache-2.0）。测试校验原始字节，不访问网络或加载未知引用。

验证器使用 github.com/santhosh-tekuri/jsonschema/v6 v6.0.2（Apache-2.0），固定于go.mod/go.sum，仅由文档合同测试使用；其Draft 2020-12支持用于实际编译响应/请求schema与官方文档校验，业务运行路径不引入验证器。[固定版本上游](https://github.com/santhosh-tekuri/jsonschema/tree/v6.0.2)。
