# go-pkg-llmusage 设计与实施计划

## 定位

`go-pkg-llmusage` 是一个只负责识别、提取和规范化 LLM API usage 的 Go 库，主包位于：

```text
github.com/lwmacct/260714-go-pkg-llmusage/pkg/llmusage
```

它面向透明代理、API gateway、录制回放和离线分析。调用方把实际收到的响应字节增量喂给 decoder，库返回零到多个 usage result；库不修改响应，不发网络请求，也不持有调用方的请求身份。

这个包负责：

- 识别 OpenAI、Anthropic、Gemini 等 wire protocol 的 usage 位置和完成条件。
- 同时支持普通 JSON 与 SSE 等流式响应。
- 合并一个响应中分散在多个事件里的 usage，例如 Anthropic `message_start` 与 `message_delta`。
- 返回一组保守的公共 token 指标，同时保留 provider-specific usage JSON。
- 对大响应做增量、选择性 JSON 提取，避免完整反序列化正文。

这个包不负责：

- HTTP transport/body wrapper、Content-Type 判断、重试或请求取消。
- `trace_id`、tenant、API key、用户身份等代理上下文。
- Fluent、Kafka、数据库、日志或 metrics 输出。
- 模型价格表、货币换算、折扣、配额扣减或账单判定。
- 根据 OpenAI-compatible wire format 猜测真实供应商。

价格计算必须留在独立组件中。相同模型名可能由不同渠道定价，价格也比 wire protocol 更频繁变化。

## 协议名称而非供应商名称

公共 API 使用 `Protocol` 描述响应的 wire contract：

- `openai.responses`
- `openai.chat-completions`
- `anthropic.messages`
- `google.generate-content`

OpenRouter、DeepSeek、Azure OpenAI 或内部兼容服务返回 OpenAI Chat 格式时，protocol 仍可为 `openai.chat-completions`，但这不表示供应商一定是 OpenAI。`Result` 不提供推测的 provider；调用方若知道渠道，应在自己的 envelope 中记录。

`ProtocolAuto` 只根据 payload signature 选择 decoder。生产代理已知道目标 API 时应显式配置 protocol，避免兼容格式的歧义。

## v0.1 公共模型

已实现的公共 API：

```go
package llmusage

import "encoding/json"

type Protocol string

const (
	ProtocolAuto                  Protocol = "auto"
	ProtocolOpenAIResponses       Protocol = "openai.responses"
	ProtocolOpenAIChatCompletions Protocol = "openai.chat-completions"
	ProtocolAnthropicMessages     Protocol = "anthropic.messages"
	ProtocolGoogleGenerateContent Protocol = "google.generate-content"
)

type Format string

const (
	FormatJSON Format = "json"
	FormatSSE  Format = "sse"
)

type Options struct {
	Protocol        Protocol
	Format          Format
	MaxFrameBytes   int
	MaxResultBytes  int
	MaxNestingDepth int
}

type Usage struct {
	InputTokens         int64 `json:"input_tokens"`
	OutputTokens        int64 `json:"output_tokens"`
	TotalTokens         int64 `json:"total_tokens"`
	CachedInputTokens   int64 `json:"cached_input_tokens,omitempty"`
	CacheWriteTokens    int64 `json:"cache_write_tokens,omitempty"`
	ReasoningTokens     int64 `json:"reasoning_tokens,omitempty"`
}

type TotalSource string

const (
	TotalReported TotalSource = "reported"
	TotalDerived  TotalSource = "derived"
	TotalUnknown  TotalSource = "unknown"
)

type Result struct {
	Protocol      Protocol        `json:"protocol"`
	ResponseID    string          `json:"response_id,omitempty"`
	Model         string          `json:"model,omitempty"`
	Usage         Usage           `json:"usage"`
	TotalSource   TotalSource     `json:"total_source"`
	RawUsage      json.RawMessage `json:"raw_usage"`
	Sequence      uint64          `json:"sequence"`
}

type Decoder struct { /* internal state */ }

func NewDecoder(options Options) (*Decoder, error)
func (d *Decoder) Feed(data []byte) ([]Result, error)
func (d *Decoder) Finish() ([]Result, error)
func Parse(data []byte, options Options) ([]Result, error)
```

约束：

- `Decoder` 不是并发安全的；一个 HTTP response 使用一个 decoder。
- `Feed` 不保留调用方 byte slice。一个调用可能返回多个 result。
- `Finish` 只可生效一次，用于 EOF 或 body close 时处理最后一个无空行 SSE event/完整 JSON。
- `Parse` 是非增量便利函数，内部仍使用 decoder，不另建一套解析逻辑。
- 默认 `Options{}` 因缺少 protocol/format 返回 `ErrInvalidOptions`；auto detection 通过显式的 `ProtocolAuto` 开启。
- `RawUsage` 是合并后的 provider-specific usage object，不保证与单个上游字节片段完全相同。未知字段保留，已知字段按协议状态机合并。

### 数值语义

公共字段只表达 token 数量，不表达价格：

- `InputTokens` 和 `OutputTokens` 使用协议的主计数字段。
- `CachedInputTokens` 表示 cache read/hit。
- `CacheWriteTokens` 表示 cache creation/write；没有该概念的协议保持 0。
- `ReasoningTokens` 是 output/thought token 的可识别子集，不额外加到 `OutputTokens`。
- provider 提供 total 时原样使用并标记 `reported`。
- provider 未提供 total 时，仅在协议规则明确时推导并标记 `derived`，否则为 `unknown`。

不要仅凭数值为 0 判断字段是否存在。各 protocol parser 内部使用 presence bit/pointer 维护字段存在性，公共 result 只在协议定义的完成点输出。

不同 provider 对 cache 是否已包含在 input/total 中并不完全一致。库不尝试重新计算“应付费总 token”；调用方需要精确计费时应同时使用 `Protocol`、规范字段和 `RawUsage`。

## Decoder 架构

```text
Feed(bytes)
  -> wire framer (JSON / SSE)
    -> protocol detector or selected protocol
      -> protocol state machine
        -> selective JSON capture
          -> normalized Result + merged RawUsage
```

当前内部结构及后续扩展位置：

```text
pkg/llmusage/
  doc.go
  config.go
  decoder.go
  errors.go
  result.go
  protocol.go
  example_test.go
  internal/framing/sse.go
  internal/jsoncapture/scanner.go
  internal/framing/sse_test.go
  internal/jsoncapture/scanner_test.go
  testdata/openai-responses/*.json
  testdata/openai-responses/*.sse
examples/basic/main.go
```

provider decoder 放在 `internal`，避免首版把不稳定的扩展接口暴露为长期兼容承诺。增加内置协议通过内部 registry 完成；等至少出现一个真实的外部自定义协议需求后，再设计公开 registration API。

### Wire framer

SSE framer 支持 BOM、LF、CRLF、CR、多行 `data`、field 无冒号、单个可选空格、comment、`id`、`retry` 和跨任意 chunk 边界。它输出内部 `Event`，不把完整流暴露到公共 API。

普通 JSON decoder 接受一个完整 JSON document。后续如需 NDJSON，新增 `FormatNDJSON`，不要把它伪装成 SSE。

`MaxFrameBytes` 限制需要保留的 SSE metadata 和事件识别字段。OpenAI Responses 的 `response.completed` 可能很大，选择性 scanner 只保留 event type、response id/model/usage 等目标值，跳过 output、instructions、tools 等大字段。`MaxResultBytes` 单独限制被保留的 raw usage/identity；`MaxNestingDepth` 限制被保留和被跳过 JSON 的嵌套深度。

### 协议状态机

OpenAI Responses：

- JSON：读取根 response 的 `id`、`model`、`usage`。
- SSE：只在 `response.completed` 输出；从 `data.response` 提取。
- 规范字段为 `input_tokens`、`output_tokens`、`total_tokens`、`input_tokens_details.cached_tokens`/`cache_write_tokens`、`output_tokens_details.reasoning_tokens`。

OpenAI Chat Completions：

- JSON：根 `usage`，response id/model 位于根。
- SSE：只处理带非 null `usage` 的 chunk；通常要求请求方设置 `stream_options.include_usage=true`。
- OpenAI-compatible 扩展字段保留在 `RawUsage`，不把 `cost` 当成本库的规范字段。

Anthropic Messages：

- JSON：根 `usage`，id/model 位于根。
- SSE：从 `message_start.message` 捕获 input/cache/model/id，再用 `message_delta.usage` 的累计 output 和可能出现的完整 usage 更新快照；在 `message_delta` 得到最终 usage，最迟于 `message_stop` 输出一次。
- 保留 `cache_creation_input_tokens`、`cache_read_input_tokens`、`cache_creation.ephemeral_5m_input_tokens`、`ephemeral_1h_input_tokens` 和 server tool usage。
- 不把多个累计 `message_delta` 相加，以最后的非缺失值为准。

Google GenerateContent：

- JSON 和 SSE chunk 都读取 `usageMetadata`。
- 映射 `promptTokenCount`、`candidatesTokenCount`、`totalTokenCount`、`cachedContentTokenCount`、`thoughtsTokenCount`。
- modality detail、tool-use prompt detail 保留在 `RawUsage`。
- 流中如果出现多个 cumulative snapshot，只在完成 chunk/EOF 输出最后快照，不累计求和。

## 错误与恢复

建议导出 sentinel errors，并用 `ParseError` 携带上下文：

```go
var (
	ErrInvalidOptions  = errors.New("llmusage: invalid options")
	ErrUnsupported     = errors.New("llmusage: unsupported protocol or format")
	ErrMalformedStream = errors.New("llmusage: malformed stream")
	ErrLimitExceeded   = errors.New("llmusage: limit exceeded")
	ErrFinished        = errors.New("llmusage: decoder finished")
)

type ParseError struct {
	Protocol Protocol
	Format   Format
	Stage    string
	Offset   int64
	Err      error
}
```

`errors.Is/As` 必须可用。错误策略：

- 明确选择的协议遇到目标 usage event 的非法 JSON时返回错误。
- 无关 event 的未知字段或未知 event type 忽略。
- `ProtocolAuto` 在检测窗口结束后仍无法识别时返回 `ErrUnsupported`，不猜测最接近的格式。
- limit error 后 decoder 进入 terminal state，避免调用方误以为后续 result 完整。
- `Finish` 后再次 `Feed` 返回 `ErrFinished`；重复 `Finish` 返回空结果和 nil，方便 defer 清理。

库只报告错误，不记录日志。透明代理可以自行选择 fail-open，离线计费任务可以选择 fail-closed。

## 首发范围与版本计划

### v0.1：已实现的最小闭环

- `ProtocolOpenAIResponses` + `FormatSSE`。
- OpenAI Responses 普通 JSON一并支持，因为复用相同 selective scanner 成本很低。
- 增量 SSE framer、选择性 JSON scanner、limits、公共 result/error API。
- 任意 chunk 切分、超大未选字段和 fuzz test。

v0.1 将先在本仓库持续打磨、积累 fixture 并稳定公开 API。Directive Proxy 的引入时间由消费方后续独立决定，不是本阶段验收条件。

### v0.2：主流 OpenAI-compatible

- OpenAI Chat Completions JSON/SSE。
- `ProtocolAuto` 的 OpenAI Responses/Chat 检测。
- include_usage 缺失时返回零 result，而不是伪造 usage。

### v0.3：多事件聚合

- Anthropic Messages JSON/SSE。
- 明确 presence merge、cumulative snapshot 和 cache creation/read 语义。

### v0.4：Gemini

- Google GenerateContent JSON/SSE。
- modality/tool/thought usage fixture。

## 测试策略

所有协议使用公开文档 fixture 加脱敏后的真实 fixture。fixture 要注明来源日期和是否经过兼容网关转换。

通用测试：

- 对同一 fixture 在每一个 byte boundary 切分，结果必须完全一致。
- 每次 `Feed` 后立即复用/覆写输入 buffer，验证 decoder 不借用调用方内存。
- SSE 的 BOM、LF/CRLF/CR、多行 data、comment、field 顺序、EOF 无空行。
- 空 usage、usage 为 null、字段为 0、缺失字段、未知 detail、非法数字、负数、溢出。
- 超大 output/instructions/tools 被跳过，内存随 retained fields 而不是 response size 增长。
- malformed target event、limit exceeded、Finish/Feed 生命周期。
- fuzz SSE framing、selective scanner 和所有 protocol detector；目标是不 panic、不越界、不无界增长。
- benchmark 大型 OpenAI completed event，记录 allocations/op 和 retained bytes。

协议测试：

- OpenAI Responses completed 与非 completed event、reasoning/cache write/read details。
- OpenAI Chat 最终 usage chunk、`[DONE]`、兼容扩展字段。
- Anthropic start/delta/stop 合并、多个累计 delta、cache 5m/1h。
- Gemini cumulative usage、thought tokens、cached content、modality detail。

CI 最低执行：

```shell
go test ./...
go test -race ./...
go vet ./...
```

## 文档与发布

仓库布局参考 `go-pkg-fluent`：根 README 提供定位、安装、快速开始、协议矩阵、内存/并发语义和错误处理；`examples/basic` 给出增量读取示例；公共 symbol 提供完整 godoc。

每次增加协议或修正字段语义都需要：

1. 增加/更新 fixture 和文档来源日期。
2. 更新协议支持矩阵。
3. 说明是向后兼容的新字段、解析修复还是语义变更。
4. 语义变更不能静默发布；必要时升级 major version。

## 官方资料基线

设计于 2026-07-14 核对以下资料：

- OpenAI Responses API reference：<https://developers.openai.com/api/reference/resources/responses/>
- Anthropic streaming Messages：<https://docs.anthropic.com/en/api/messages-streaming>
- Gemini GenerateContent：<https://ai.google.dev/api/generate-content>

实现时把对应响应片段保存为 testdata，避免测试依赖网络。官方字段更新应通过新 fixture 驱动，而不是仅凭模型名称写特殊分支。
