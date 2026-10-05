# API Key 费用与请求限额

`MaxRPM` 限制每分钟请求数，`MaxCost` 限制按 Octopus 价格和用量统计计算的累计费用。`0` 表示未设置对应限额，两者独立。

设置 `MaxCost > 0` 的密钥采用保守准入：一个密钥同一时间只允许一个计费请求，并为它预留当前剩余额度。其他并发请求返回 HTTP 429 和 `Retry-After: 1`；请求完成结算后释放预留。此限制同时覆盖 HTTP POST 与每轮 WebSocket `response.create`。WebSocket 升级、模型列表和统计查询不占用费用预留。未设置费用限额的密钥保留并发能力。

发送前按最终上游请求检查费用，因此渠道参数覆盖不能移除或放大输出上限来绕过检查。价格使用客户端请求的模型名，与现有计费口径一致；一次请求的价格快照用于准入和结算。必须存在有限、非负且不全为零的价格。

| 请求协议 | 必需的输出上限 |
| --- | --- |
| OpenAI Chat Completions | 正整数 `max_completion_tokens` 或 `max_tokens`；同时提供时按较大值估算 |
| OpenAI Responses / WebSocket | 正整数 `max_output_tokens` |
| Anthropic Messages | 正整数 `max_tokens` |
| Gemini 上游 | 正整数 `generationConfig.maxOutputTokens` |
| Embeddings | 无输出 token 费用，检查输入预算 |

输入估算使用最终请求的 UTF-8 字节数加 1024 token 的协议开销余量，并采用输入、缓存读取、缓存写入单价中的最大值；输出按 token 上限和候选数量计算。一次请求内的重试与传输恢复共用预算，每次实际发送都消耗估算额度；发送失败也不退还本次请求内的尝试额度。

有限额的密钥支持可估算的文本输入及普通函数工具定义。图片接口、Responses Compact、远程媒体、托管工具，以及 `previous_response_id`、远程 conversation、缓存引用或加密内容等不透明输入，目前无法给出可靠的单次费用边界，因此返回 HTTP 400。缺少输出上限、价格不可用或估算超过余额时也返回 400，错误码为 `auth.api_key_cost_exceeded`，具体原因在响应消息中。累计已结算费用达到限额时，沿用 HTTP 401 的拒绝行为。

这套机制防止多个请求同时使用同一笔本地余额，并约束单次请求及重试的估算费用。最终费用仍来自供应商 usage 和既有的输入 token 估算；供应商不返回 usage、忽略输出上限、采用不同价格或在取消后继续计费时，Octopus 无法保证供应商账单不超额。费用统计沿用缓存和周期落库，预留也仅在单个 Octopus 进程内有效；它不提供跨副本或进程崩溃后的严格财务限额。
