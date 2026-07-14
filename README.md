# LLM Usage Go 解析库

`pkg/llmusage` 从 LLM API 响应中增量提取 token usage，并返回规范化计数与 provider-specific 原始 usage。它适用于透明代理、API gateway、录制回放和离线分析。

当前 v0.1 支持：

| Protocol | JSON | SSE |
| --- | --- | --- |
| OpenAI Responses (`openai.responses`) | 支持 | 支持 `response.completed` |
| OpenAI Chat Completions | 计划 v0.2 | 计划 v0.2 |
| Anthropic Messages | 计划 v0.3 | 计划 v0.3 |
| Google GenerateContent | 计划 v0.4 | 计划 v0.4 |

Protocol 表示 wire contract，不表示实际计费供应商。OpenAI-compatible 服务可以使用 OpenAI protocol，但库不会据此猜测供应商或价格。

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

## Result

`Result.Usage` 提供稳定的公共计数：

- `InputTokens`、`OutputTokens`、`TotalTokens`；
- `CachedInputTokens`、`CacheWriteTokens`；
- `ReasoningTokens`。

cached、cache write 和 reasoning 是 input/output 的明细子集，不应再次加到 total。`TotalSource` 表示 total 是 provider `reported`、由明确协议规则 `derived`，还是 `unknown`。

`RawUsage` 保留完整 usage object，包括库尚未认识的新字段。不同 provider 对 cache 是否包含在 input/total 中可能不同；精确计费应结合 protocol、公共计数和 `RawUsage`，不能只计算五个整数。这个包不维护价格表。

## 增量和内存语义

- `Decoder` 对应一个 response，且不是并发安全的。
- `Feed` 不保留调用方 slice；返回后可立即复用 buffer。
- `Finish` 处理 EOF 处没有空行的最后一个 SSE event；重复调用返回空结果和 nil。
- `Finish` 后调用 `Feed` 返回 `ErrFinished`。
- OpenAI `response.completed` 中的大型 `output`、`instructions` 和 `tools` 不会完整物化；解析器只保留 id、model 和 usage。
- SSE 支持 BOM、LF、CRLF、CR、多行 data、comment、id、retry 和任意 chunk 边界。

默认资源上限：

| Option | 默认值 | 含义 |
| --- | ---: | --- |
| `MaxFrameBytes` | 1 MiB | SSE metadata 与事件识别字段上限 |
| `MaxResultBytes` | 64 KiB | 单个 result 保留的 id/model/raw usage 总量上限 |
| `MaxNestingDepth` | 128 | JSON 最大嵌套深度，包含被跳过字段 |

设为 0 使用默认值；负值无效。超过限制返回 `ErrLimitExceeded`，decoder 随后保持 terminal error。

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

测试包含每一个 byte boundary 的增量切分、8 MiB 跳过字段、资源限制、fuzz target 和 4 MiB SSE benchmark。

## 官方资料

v0.1 fixture 于 2026-07-14 根据 OpenAI Responses 官方 API reference 固定：

- <https://developers.openai.com/api/reference/resources/responses/>

测试不访问网络；协议更新通过新的脱敏 fixture 驱动。
