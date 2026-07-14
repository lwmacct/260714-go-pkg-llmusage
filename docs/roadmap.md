# 实施路线图

本文记录功能完成度和持续验收项，不绑定发布版本。模块版本由仓库 Git tag 决定；Directive Proxy 是否引入本包由消费方后续独立评估。

## 已完成

### 公共基础

- `NewDecoder`、`Feed`、`Finish` 与 `Parse` 公共 API。
- JSON/SSE framing、选择性 JSON scanner、资源上限与结构化错误。
- 规范化 usage、`TotalSource` 和完整/合并后的 `RawUsage`。
- decoder 生命周期、调用方 buffer 复用和非并发语义。

### OpenAI Responses

- JSON 根对象 usage 提取。
- SSE `response.completed` 完成点。
- input/output/total、cached input、cache write 和 reasoning 映射。
- 大型 output/instructions/tools 跳过，不完整物化 response。

### OpenAI Chat Completions

- JSON 与 SSE 最终 usage chunk。
- prompt/completion/total、cached prompt 和 reasoning 映射。
- `usage: null`、缺失 `include_usage`、流中断和 `[DONE]` 的零结果语义。
- OpenAI-compatible 扩展字段原样保留，不规范化 provider cost。

### Anthropic Messages

- JSON usage 提取。
- SSE `message_start`、累计 `message_delta` 与 `message_stop` 状态机。
- 按字段 presence 合并，累计 delta 不相加，缺少 stop 时在 EOF 输出。
- cache read/cache creation 规范化；cache 5m/1h 和 server tool usage 保留在 `RawUsage`。
- 未报告 total 时保守标记 `TotalUnknown`。

### Google GenerateContent

- JSON 与 SSE `usageMetadata` 提取。
- prompt/candidate/total、cached content 和 thoughts 映射。
- 流式累计 snapshot 只保留最后值，在 EOF 输出一次。
- modality 和 tool-use usage detail 保留在 `RawUsage`。

### 自动识别

- `ProtocolAuto` 覆盖所有内置 JSON/SSE protocol。
- 只识别 wire contract，不推断 provider。
- 未知或不能唯一识别的 payload 返回 `ErrUnsupported`。

## 持续加固

以下工作是协议维护的一部分，不设置一次性的完成终点：

- 随官方 schema 变化增加 fixture，补充脱敏后的真实兼容网关 fixture。
- 对新增 fixture 保持逐字节边界切分、未知字段、null/missing/zero 和 buffer 复用测试。
- 持续覆盖非法/负数/溢出计数、malformed target event、资源上限和最大嵌套。
- 对 SSE framing、选择性 scanner 和 protocol auto 保持 fuzz 测试。
- 用大型未选正文 benchmark 监控吞吐、allocations/op 和 retained bytes。

## 变更验收

每次增加协议或修正字段语义必须：

1. 增加或更新 fixture，并记录官方资料基线。
2. 更新 README 协议矩阵和协议特有语义。
3. 明确 `RawUsage` 合并方式与 total/cache/reasoning 的包含关系。
4. 通过 `go test ./...`、`go test -race ./...`、`go vet ./...`。
5. 不引入 HTTP、日志、Fluent、代理身份或定价依赖。
