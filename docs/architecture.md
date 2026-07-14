# 内部架构

`llmusage` 的公共 API 与协议实现分离，依赖保持单向：

```text
pkg/llmusage public facade
  -> internal/engine
       -> internal/protocol
       -> internal/sse
  internal/protocol
       -> internal/jsonscan
```

## 各层职责

- `pkg/llmusage`：公共类型、options 校验、错误包装，以及内部 result 到公共 result 的转换。
- `internal/engine`：一个 response 的生命周期、JSON/SSE 调度、pending result 和 byte offset。
- `internal/protocol`：协议 factory、自动识别、完成条件、累计状态合并和 usage 规范化。
- `internal/sse`：只实现 SSE framing，不理解任何 LLM event 或 `[DONE]`。
- `internal/jsonscan`：增量验证 JSON，并只保留调用方选择的字段。

同一协议 decoder 创建的多个 `jsonscan.Scanner` 共享 retained-byte budget，避免 auto detector 或并行路径各自获得一份完整上限。scanner 完成并复制出 result/state 后释放预算；未选择的正文只验证和跳过，不消耗 retained budget。SSE metadata 使用独立的逐 event 累计预算，`data:` 字段始终流式传递。

内部协议 decoder 按 framing 分为 `JSONDecoder` 与 `SSEDecoder`。显式协议 decoder 不承担识别职责；只有 auto detector 判断 wire contract，因此协议解析错误不会被误当成识别信号。

## 自动识别

JSON auto detector 使用一个 union scanner 捕获各协议的稳定签名，完整文档只扫描一次。签名必须唯一，否则返回 `ErrUnsupported`。

SSE auto detector 在选中协议前只运行有限的选择性 scanner：

- `response.*` 识别 OpenAI Responses；
- `object = chat.completion.chunk` 识别 OpenAI Chat Completions；
- `message_start`、`message_delta`、`message_stop` 等强事件识别 Anthropic Messages；
- `usageMetadata` 识别 Google GenerateContent。

`ping`、`error` 等弱事件不会锁定协议。选中后只保留一个协议状态机。`[DONE]` 属于 OpenAI Chat decoder，不是 SSE framing 的通用规则。

## 扩展内置协议

新增协议时：

1. 在 `internal/protocol/types.go` 增加内部 kind。
2. 实现相应 JSON/SSE decoder，并在 `factory.go` 注册。
3. 为 auto detector 增加唯一、稳定且有官方资料依据的 wire signature。
4. 增加 fixture、逐字节边界测试、非法计数与资源限制测试。
5. 更新 README 支持矩阵和协议字段语义。

当前不提供公开协议注册 API。只有出现真实的外部协议实现需求后，才应设计稳定的扩展契约。
