# LLM Usage Go 解析库

`pkg/llmusage` 从 LLM API 响应中增量提取 token usage，返回规范化计数与协议原始 usage。它适用于透明代理、API gateway、录制回放和离线分析。

| Protocol | JSON | SSE |
| --- | --- | --- |
| OpenAI Responses (`openai.responses`) | 支持 | 支持 `response.completed` |
| OpenAI Chat Completions (`openai.chat-completions`) | 支持 | 支持最终 usage chunk |
| Anthropic Messages (`anthropic.messages`) | 支持 | 支持 start/delta/stop 合并 |
| Google GenerateContent (`google.generate-content`) | 支持 | 支持累计 usage snapshot |

Protocol 表示 wire contract，不表示实际计费供应商。OpenRouter、DeepSeek、Azure OpenAI 或内部兼容服务可以使用 OpenAI protocol，但库不会据此猜测供应商或价格。

## 安装

```shell
go get github.com/lwmacct/260714-go-pkg-llmusage@latest
```

```go
import "github.com/lwmacct/260714-go-pkg-llmusage/pkg/llmusage"
```

## 快速开始

普通 JSON：

```go
results, err := llmusage.Parse(body, llmusage.Options{
	Protocol: llmusage.ProtocolOpenAIResponses,
	Format:   llmusage.FormatJSON,
})
```

流式响应为每个 HTTP response 创建一个 decoder，将实际读到的 bytes 原样传入：

```go
decoder, err := llmusage.NewDecoder(llmusage.Options{
	Protocol: llmusage.ProtocolOpenAIResponses,
	Format:   llmusage.FormatSSE,
})
if err != nil {
	return err
}

for {
	n, readErr := responseBody.Read(buffer)
	if n > 0 {
		results, decodeErr := decoder.Feed(buffer[:n])
		if decodeErr != nil {
			return decodeErr
		}
		consume(results)
	}
	if readErr != nil {
		results, decodeErr := decoder.Finish()
		consume(results)
		if readErr == io.EOF {
			return decodeErr
		}
		return errors.Join(readErr, decodeErr)
	}
}
```

完整示例位于 [`examples/basic/main.go`](examples/basic/main.go)。

已知 API endpoint 时应显式选择 protocol。`ProtocolAuto` 只识别响应的 wire contract；无法唯一识别的 payload 返回 `ErrUnsupported`，不会猜测兼容服务的真实供应商。

## 职责边界

这个包负责：

- 识别内置 wire protocol 的 usage 位置和完成条件；
- 解析普通 JSON 与 SSE，并合并同一响应中分散的累计 usage；
- 返回保守、稳定的公共 token 指标，同时保留未知协议字段；
- 选择性扫描大型 JSON，不完整物化正文。

这个包不负责 HTTP transport/body wrapper、Content-Type 判断、请求身份、日志与消息投递，也不负责价格表、折扣、货币换算、配额或账单判定。调用方应把 protocol、渠道身份和 usage 放进自己的业务 envelope，再由独立组件完成计费或投递。

## Result 与数值语义

`Result.Usage` 提供稳定的公共计数：

- `InputTokens`、`OutputTokens`、`TotalTokens`；
- `CachedInputTokens`、`CacheWriteTokens`；
- `ReasoningTokens`。

cache read、cache write 和 reasoning 是 input/output 的明细或协议相关计数，不应再次加到 total。`TotalSource` 表示 total 是 provider `reported`、由明确协议规则 `derived`，还是 `unknown`。

`RawUsage` 保留完整或合并后的 usage object，包括库尚未认识的新字段。不同 protocol 对 cache、reasoning 和 total 的包含关系不同；精确计费必须同时结合 `Protocol`、公共计数、`RawUsage` 和调用方已知的服务渠道，不能只计算几个公共整数。这个包不维护价格表。

协议特有语义：

- OpenAI Chat Completions 流只有在请求启用 `stream_options.include_usage` 后才会出现最终 usage chunk；流中断或未启用时返回零 result，不伪造 usage；`[DONE]` 不产生 result。
- Anthropic Messages 的 `message_delta.usage` 是累计快照；字段按 presence 覆盖，多个 delta 不相加。协议未报告 total，因此 `TotalSource` 为 `unknown`。
- Google GenerateContent 流可能给出多个累计 `usageMetadata`；decoder 保留最后快照并在 EOF 输出一次，不累计求和。

## 增量和内存语义

- `Decoder` 对应一个 response，且不是并发安全的。
- `Feed` 不保留调用方 slice；返回后可立即复用 buffer。
- `Finish` 处理 EOF 处没有空行的最后一个 SSE event；重复调用返回空结果和 nil。
- `Finish` 后调用 `Feed` 返回 `ErrFinished`。
- 大型正文、tool 定义和输出不会完整物化；解析器只保留协议识别字段、id、model 和 usage。
- SSE 支持 BOM、LF、CRLF、CR、多行 data、comment、id、retry 和任意 chunk 边界。

默认资源上限：

| Option | 默认值 | 含义 |
| --- | ---: | --- |
| `MaxSSEMetadataBytes` | 64 KiB | 单个 SSE event 累计 metadata 上限，不限制 `data:` 正文 |
| `MaxResultBytes` | 64 KiB | decoder 保留的协议识别字段、id/model/raw usage 共享上限 |
| `MaxNestingDepth` | 128 | JSON 最大嵌套深度，包含被跳过字段 |

设为 0 使用默认值；负值无效。超过限制返回 `ErrLimitExceeded`，decoder 随后保持 terminal error。

这些限制与模型 context window 相互独立。百万 token context 只会增大 usage counter 的数值；响应正文仍被流式跳过，不计入 `MaxResultBytes`。`MaxResultBytes` 是同一 decoder 内协议 scanner 共享的当前保留预算，而不是累计处理字节数。

## 错误处理

可通过 `errors.Is` 判断：

- `ErrInvalidOptions`
- `ErrUnsupported`
- `ErrMalformedStream`
- `ErrLimitExceeded`
- `ErrFinished`

通过 `errors.As(err, *ParseError)` 可取得 protocol、format、stage 和 byte offset。库不记录日志：透明代理可以选择 fail-open，离线计费任务可以选择 fail-closed。

## 开发验证

```shell
go test ./...
go test -race ./...
go vet ./...
```

测试包含所有 protocol fixture 的逐字节增量切分、未知字段、累计快照、资源限制、buffer 复用、fuzz target 和大型 SSE benchmark。测试不访问网络；协议更新通过官方资料与新增脱敏 fixture 驱动。

内部依赖与协议扩展规则见 [`docs/architecture.md`](docs/architecture.md)，实施状态与持续加固项见 [`docs/roadmap.md`](docs/roadmap.md)。

## 官方资料基线

协议和 fixture 于 2026-07-14 根据以下官方资料核对：

- OpenAI Responses API reference：<https://developers.openai.com/api/reference/resources/responses/>
- OpenAI Chat Completions streaming events：<https://developers.openai.com/api/reference/resources/chat/subresources/completions/streaming-events>
- Anthropic streaming Messages：<https://docs.anthropic.com/en/api/messages-streaming>
- Google Gemini GenerateContent：<https://ai.google.dev/api/generate-content>

官方字段更新应通过新 fixture 驱动，不根据模型名称添加特殊分支。
