# 实例指标

配置 monitoring 后，本地 Hertz 端口提供 `/metrics`。响应为 Prometheus text 0.0.4（UTF-8、逐行文本），由每个 Deer 独立的 Registry 产生；DeerApi 的业务 HTTP 与监控 listener 复用同一个 Registry。无全局注册器、push 连接、后台采样或出口队列。

| 指标 | 类型 | 含义 |
|---|---|---|
| deer_requests_total | counter | 完成的 middleware/interceptor 请求，按 transport/code 分类 |
| deer_request_errors_total | counter | HTTP/probe 4xx/5xx/未知状态及非 OK gRPC |
| deer_request_duration_seconds | histogram | handler 持续时间，可跨节点聚合分位数 |
| deer_live | gauge | Run 是否仍拥有进程 |
| deer_lifecycle_ready | gauge | 启动完成且停止尚未开始的生命周期标记 |

transport 固定为 http、grpc、probe。HTTP/probe code 仅为 1xx–5xx/other；gRPC 为 17 个标准状态，未知值归 Unknown。路径、方法参数、用户/订单/请求/实例 ID、错误文本都不进入标签；总 series 数固定。scrape 不记录自身，探针与业务 HTTP 延迟分开。

延迟以 time.Duration 整数纳秒输入，直接转换 decimal.Decimal 秒并精确累加，输出 decimal 字符串到标准文本协议；没有 Go 浮点转换或中间计算。固定 bucket 为 1/5/10/25/50/100/250/500ms、1/5/10s 及 +Inf，+Inf 与 count 同一快照。指标响应是 text/plain 字符串，不改变业务 JSON/Proto 的 string 小数合同。

计数、bucket 与 decimal sum 在一个短临界区更新，编码前复制固定大小状态，编码不持锁。快照与输出空间有固定标签/桶边界；网络写入使用原 monitoring listener 的请求/读写期限。scrape 不查询依赖，实时就绪使用 `/readyz`；lifecycle_ready 的名称明确其生命周期范围。此组指标记录进入请求中间件/拦截器的结果，传输层和业务专用指标继续后续接入。

常用 PromQL：

```promql
sum(rate(deer_request_errors_total[5m])) by (transport)
/
sum(rate(deer_requests_total[5m])) by (transport)

histogram_quantile(0.99,
  sum(rate(deer_request_duration_seconds_bucket[5m])) by (le, transport))
```

本步验证格式、精确性、并发与实例隔离；trace/exemplar、Grafana/告警、生产远程抓取授权和容量实测尚未交付。协议依据 [Prometheus exposition format](https://prometheus.io/docs/instrumenting/exposition_formats/)，行为证据见 [P02.9.1](validation/P02-09-01.md)。
