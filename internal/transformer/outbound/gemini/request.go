package gemini

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/bestruirui/octopus/internal/transformer/model"
	"github.com/bestruirui/octopus/internal/utils/log"
)

func (o *MessagesOutbound) TransformRequest(ctx context.Context, request *model.InternalLLMRequest, baseUrl, key string) (*http.Request, error) {
	if request == nil {
		return nil, fmt.Errorf("request is nil")
	}
	request, _ = prepareGeminiRequest(request, request.Model)

	// Convert internal request to Gemini format
	geminiReq := convertLLMToGeminiRequest(request)

	body, err := json.Marshal(geminiReq)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal gemini request: %w", err)
	}

	// Build URL
	parsedUrl, err := url.Parse(strings.TrimSuffix(baseUrl, "/"))
	if err != nil {
		return nil, fmt.Errorf("failed to parse base url: %w", err)
	}

	// G-H5: When the channel BaseURL omits the API version segment
	// (`https://generativelanguage.googleapis.com`), the downstream request
	// would land on `/models/...` which 404s. Fall back to `/v1beta` when
	// no version prefix is configured; leave explicit `/v1` or `/v1beta`
	// paths alone.
	if !pathHasGeminiVersion(parsedUrl.Path) {
		parsedUrl.Path = strings.TrimRight(parsedUrl.Path, "/") + "/v1beta"
	}

	// Determine if streaming
	isStream := request.Stream != nil && *request.Stream
	method := "generateContent"
	if isStream {
		method = "streamGenerateContent"
	}

	// Build path: /models/{model}:{method}
	modelName := request.Model
	if !strings.Contains(modelName, "/") {
		modelName = "models/" + modelName
	}
	parsedUrl.Path = fmt.Sprintf("%s/%s:%s", parsedUrl.Path, modelName, method)

	// G-H6: Carry the API key in `x-goog-api-key` — the query-string form
	// still works but leaks the secret into proxy access logs and is
	// discouraged by Google's current docs.
	if isStream {
		q := parsedUrl.Query()
		q.Set("alt", "sse")
		parsedUrl.RawQuery = q.Encode()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, parsedUrl.String(), bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if key != "" {
		req.Header.Set("x-goog-api-key", key)
	}

	return req, nil
}

// Helper functions

// pathHasGeminiVersion reports whether the configured base-URL path already
// contains a Gemini API version segment (`/v1`, `/v1beta`, etc.). Used by
// G-H5 to decide whether to prepend `/v1beta` as a fallback when channels
// were provisioned with a bare hostname. Matching on a leading `/v` prefix
// covers the versions Google documents (v1, v1beta, v1beta2, v1alpha) and
// will survive future bumps without churn.
func pathHasGeminiVersion(p string) bool {
	segment := strings.Trim(p, "/")
	if segment == "" {
		return false
	}
	first := segment
	if idx := strings.Index(segment, "/"); idx >= 0 {
		first = segment[:idx]
	}
	if len(first) < 2 || first[0] != 'v' {
		return false
	}
	// Must be `v<digit>...`; `/viewer` etc. should not count.
	return first[1] >= '0' && first[1] <= '9'
}

// canonicalGeminiModality normalises a client-supplied modality keyword into
// the Gemini wire shape. Gemini accepts TEXT / IMAGE / AUDIO (upper case);
// unknown values return the empty string so the caller drops them rather
// than letting a 400 surface at request time.
func canonicalGeminiModality(m string) string {
	switch strings.ToLower(strings.TrimSpace(m)) {
	case "text":
		return "TEXT"
	case "image":
		return "IMAGE"
	case "audio":
		return "AUDIO"
	default:
		return ""
	}
}

// SupportsResponseModality reports whether a client output modality can be
// represented by Gemini's responseModalities field.
func SupportsResponseModality(m string) bool {
	return canonicalGeminiModality(m) != ""
}

func convertLLMToGeminiRequest(request *model.InternalLLMRequest) *model.GeminiGenerateContentRequest {
	geminiReq := &model.GeminiGenerateContentRequest{
		Contents: []*model.GeminiContent{},
	}

	// Convert messages
	var systemInstruction *model.GeminiContent
	degradedToolCalls := map[string]string{}
	// toolCallNamesByID captures the Function.Name of every assistant tool
	// call seen so far, keyed by the call's ID. Gemini requires
	// `functionResponse.name` to match the originating `functionCall.name`
	// byte-for-byte, so we use this map to look the name up when a
	// subsequent tool-result message only carries an ID. Preserved across
	// assistant turns so multi-round conversations still resolve correctly.
	toolCallNamesByID := map[string]string{}

	for _, msg := range request.Messages {
		role := strings.ToLower(strings.TrimSpace(msg.Role))
		if role == "" {
			role = "user"
		} else if role == "model" {
			role = "assistant"
		}
		switch role {
		case "system", "developer":
			// Collect system messages into system instruction
			if systemInstruction == nil {
				systemInstruction = &model.GeminiContent{
					Parts: []*model.GeminiPart{},
				}
			}
			if msg.Content.Content != nil {
				systemInstruction.Parts = append(systemInstruction.Parts, &model.GeminiPart{
					Text: *msg.Content.Content,
				})
			}

		case "user":
			content := &model.GeminiContent{
				Role:  "user",
				Parts: []*model.GeminiPart{},
			}
			if msg.Content.Content != nil {
				content.Parts = append(content.Parts, &model.GeminiPart{
					Text: *msg.Content.Content,
				})
			}

			if msg.Content.MultipleContent != nil {
				for _, part := range msg.Content.MultipleContent {
					switch strings.ToLower(strings.TrimSpace(part.Type)) {
					case "text":
						if part.Text != nil {
							content.Parts = append(content.Parts, &model.GeminiPart{
								Text: *part.Text,
							})
						}
					case "image_url":
						partOut, _, warning := planGeminiImageURLConversion(part.ImageURL, "")
						if warning != "" {
							log.Warnf("gemini: %s", warning)
						}
						if partOut != nil {
							content.Parts = append(content.Parts, partOut)
						}
					case "input_audio":
						if part.Audio != nil {
							content.Parts = append(content.Parts, &model.GeminiPart{
								InlineData: &model.GeminiBlob{
									MimeType: audioTypeToMimeType(part.Audio.Format),
									Data:     part.Audio.Data,
								},
							})
						} else {
							log.Warnf("gemini: drops an input_audio content part with no audio source")
						}
					case "file":
						partOut, _, warning := planGeminiFileConversion(part.File, "")
						if warning != "" {
							log.Warnf("gemini: %s", warning)
						}
						if partOut != nil {
							content.Parts = append(content.Parts, partOut)
						}
					case "document":
						if p := convertDocumentToGeminiPart(part.Document, request); p != nil {
							content.Parts = append(content.Parts, p)
						}
					case "server_tool_use", "server_tool_result":
						// Gemini has no native server-tool equivalent. Drop
						// with a warning so the request still dispatches;
						// the relay layer may surface an X-Octopus-Warning
						// header.
						log.Warnf("gemini: dropping unsupported %q block", part.Type)
					}
				}
			}

			geminiReq.Contents = append(geminiReq.Contents, content)

		case "assistant":
			content := &model.GeminiContent{
				Role:  "model",
				Parts: []*model.GeminiPart{},
			}
			// Gemini 3 requires Part-level thoughtSignature verbatim on multi-turn function
			// calling. Replay Gemini-authored thinking parts as thoughts, and keep standalone
			// signature blocks for matching functionCall parts only.
			geminiBlocks := msg.ReasoningBlocksByProvider("gemini")
			content.Parts = append(content.Parts, buildGeminiThoughtParts(geminiBlocks)...)
			geminiSigByToolCallID := collectGeminiSignaturesByToolCallID(geminiBlocks)
			geminiSigs := collectGeminiLooseSignatures(geminiBlocks)
			geminiSigByName := collectGeminiSignaturesByName(geminiBlocks)
			sigIdx := 0
			// Handle text content
			if msg.Content.Content != nil && *msg.Content.Content != "" {
				content.Parts = append(content.Parts, &model.GeminiPart{Text: *msg.Content.Content})
			}
			// Handle tool calls
			if len(msg.ToolCalls) > 0 {
				for _, toolCall := range msg.ToolCalls {
					if toolCall.ID != "" && toolCall.Function.Name != "" {
						toolCallNamesByID[toolCall.ID] = toolCall.Function.Name
					}
					var args map[string]interface{}
					if err := json.Unmarshal([]byte(toolCall.Function.Arguments), &args); err != nil {
						log.Warnf("gemini: failed to unmarshal tool call arguments for %s: %v", toolCall.Function.Name, err)
					}
					part := &model.GeminiPart{
						FunctionCall: &model.GeminiFunctionCall{
							ID:   toolCall.ID,
							Name: toolCall.Function.Name,
							Args: args,
						},
					}

					ext := toolCall.GetGeminiExtensions()
					sig := ext.ThoughtSignature
					if strings.TrimSpace(sig) == "" {
						sig = ""
					}
					if sig == "" {
						// Prefer the strongest anchor first: explicit tool-call ID,
						// then function name, then legacy ordinal fallback.
						if byID, ok := geminiSigByToolCallID[toolCall.ID]; ok && byID != "" {
							sig = byID
							delete(geminiSigByToolCallID, toolCall.ID)
						} else if named, ok := geminiSigByName[toolCall.Function.Name]; ok && named != "" {
							sig = named
							delete(geminiSigByName, toolCall.Function.Name)
						} else if fallbackSig, ok := nextGeminiSignature(geminiSigs, &sigIdx); ok {
							sig = fallbackSig
						}
					}

					// ThoughtSignature is optional - attach if available for multi-turn reasoning
					if sig != "" {
						part.ThoughtSignature = sig
					}
					// Always send functionCall part, even without signature (cross-provider compatibility)
					content.Parts = append(content.Parts, part)
				}
			}
			geminiReq.Contents = append(geminiReq.Contents, content)

			if len(geminiBlocks) > 0 || sigIdx > 0 {
				log.Debugw("transformer.reasoning.signature.passthrough",
					"provider", "gemini",
					"direction", "inject",
					"signature_count", sigIdx,
					"available_signatures", len(geminiSigs),
				)
			}

		case "tool":
			// Tool result. If the corresponding assistant functionCall had to be
			// downgraded to plain text because no Gemini thoughtSignature was
			// available, degrade the tool result too so the request stays valid.
			functionName := resolveGeminiToolResponseName(&msg, toolCallNamesByID)
			content := convertLLMToolResultToGeminiContent(&msg, functionName)
			if msg.ToolCallID != nil {
				if toolName, ok := degradedToolCalls[*msg.ToolCallID]; ok {
					content = convertLLMToolResultToGeminiTextContent(&msg, toolName)
				}
			}
			geminiReq.Contents = append(geminiReq.Contents, content)
		}
	}

	geminiReq.SystemInstruction = systemInstruction

	// Convert generation config
	config := &model.GeminiGenerationConfig{}
	hasConfig := false

	if request.MaxTokens != nil {
		config.MaxOutputTokens = int(*request.MaxTokens)
		hasConfig = true
	} else if request.MaxCompletionTokens != nil {
		config.MaxOutputTokens = int(*request.MaxCompletionTokens)
		hasConfig = true
	}
	if request.Temperature != nil {
		config.Temperature = request.Temperature
		hasConfig = true
	}
	if request.TopP != nil {
		config.TopP = request.TopP
		hasConfig = true
	}
	if request.TopK != nil {
		topK := int(*request.TopK)
		config.TopK = &topK
		hasConfig = true
	}
	if request.PresencePenalty != nil {
		config.PresencePenalty = request.PresencePenalty
		hasConfig = true
	}
	if request.FrequencyPenalty != nil {
		config.FrequencyPenalty = request.FrequencyPenalty
		hasConfig = true
	}
	if request.Seed != nil {
		config.Seed = request.Seed
		hasConfig = true
	}
	if request.Logprobs != nil {
		enabled := *request.Logprobs
		config.ResponseLogprobs = &enabled
		hasConfig = true
	}
	if request.TopLogprobs != nil {
		// Gemini caps logprobs at 5; anything higher would 400 upstream.
		n := int(*request.TopLogprobs)
		if n > 5 {
			n = 5
		}
		if n < 0 {
			n = 0
		}
		config.Logprobs = &n
		hasConfig = true
	}
	if mediaResolution := request.TransformerMetadataValue(model.TransformerMetadataGeminiMediaResolution); mediaResolution != "" {
		config.MediaResolution = mediaResolution
		hasConfig = true
	}

	// SpeechConfig (G-H11): prefer the explicit raw passthrough, otherwise
	// synthesise a minimal speechConfig from request.Audio.Voice so the
	// generic {format, voice} pair still reaches Gemini audio-output
	// models without the caller having to build the full schema.
	geminiExt := request.GetGeminiExtensions()
	if len(geminiExt.SpeechConfig) > 0 {
		config.SpeechConfig = geminiExt.SpeechConfig
		hasConfig = true
	} else if request.Audio != nil && strings.TrimSpace(request.Audio.Voice) != "" {
		voice := strings.TrimSpace(request.Audio.Voice)
		if synth, err := json.Marshal(map[string]any{
			"voiceConfig": map[string]any{
				"prebuiltVoiceConfig": map[string]any{
					"voiceName": voice,
				},
			},
		}); err == nil {
			config.SpeechConfig = synth
			hasConfig = true
		}
	}
	if request.Stop != nil && request.Stop.MultipleStop != nil {
		config.StopSequences = request.Stop.MultipleStop
		hasConfig = true
	} else if request.Stop != nil && request.Stop.Stop != nil {
		config.StopSequences = []string{*request.Stop.Stop}
		hasConfig = true
	}

	// CandidateCount (G-M8): Gemini supports multi-candidate sampling but
	// the cross-provider InternalLLMRequest does not expose `n` as a
	// first-class field (it's commented out to enforce "n=1" elsewhere).
	// Use a TransformerMetadata escape hatch so Gemini-aware callers can
	// opt in without breaking the invariant. Ignore non-positive or
	// unparseable values — they either match the default or would 400
	// upstream.
	if raw := request.TransformerMetadataValue(model.TransformerMetadataGeminiCandidateCount); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 1 {
			config.CandidateCount = n
			hasConfig = true
		}
	}

	if request.ReasoningEffort != "" || request.ReasoningBudget != nil || request.AdaptiveThinking {
		decision := resolveThinkingConfig(request.Model, request.ReasoningBudget, request.ReasoningEffort, request.AdaptiveThinking)
		if decision.Supported {
			thinkingConfig := &model.GeminiThinkingConfig{
				IncludeThoughts: decision.IncludeThoughts,
			}
			if decision.UseLevel {
				// Empty Level signals "server-side dynamic default" for
				// Gemini 3.x — we deliberately avoid emitting an unsupported
				// "dynamic" / "none" string by leaving the field unset.
				if decision.Level != "" {
					thinkingConfig.ThinkingLevel = decision.Level
				}
			} else {
				b := decision.Budget
				thinkingConfig.ThinkingBudget = &b
			}
			config.ThinkingConfig = thinkingConfig
			hasConfig = true
		}
	}

	// Convert ResponseFormat to ResponseMimeType and ResponseSchema
	if request.ResponseFormat != nil {
		switch request.ResponseFormat.Type {
		case "json_object":
			config.ResponseMimeType = "application/json"
			hasConfig = true
		case "json_schema":
			config.ResponseMimeType = "application/json"
			if request.ResponseFormat.Schema != nil {
				geminiSchema, err := request.ResponseFormat.Schema.ToGemini()
				if err != nil {
					// Lossy: Gemini cannot express every Draft-07 keyword.
					// Log-and-continue rather than fail the whole request —
					// the schema was advisory anyway and JSON mode still
					// constrains output shape.
					log.Warnf("gemini: response schema lossy conversion: %v", err)
				}
				if geminiSchema != nil {
					config.ResponseSchema = geminiSchema
				}
			} else if len(request.ResponseFormat.RawSchema) > 0 {
				// Passthrough path: decode the raw bytes into GeminiSchema
				// shape best-effort. If decoding fails we still set the
				// MIME type so the model returns JSON.
				var fallback model.GeminiSchema
				if err := json.Unmarshal(request.ResponseFormat.RawSchema, &fallback); err == nil {
					config.ResponseSchema = &fallback
				} else {
					log.Warnf("gemini: response raw schema passthrough failed: %v", err)
				}
			}
			hasConfig = true
		case "text":
			config.ResponseMimeType = "text/plain"
			hasConfig = true
		}
	}

	// Convert Modalities to ResponseModalities.
	// Gemini requires upper-case modality tokens (TEXT / IMAGE / AUDIO).
	// The previous `strings.ToUpper(m[:1]) + strings.ToLower(m[1:])` produced
	// "Text"/"Image" which Gemini 2.5+ rejects with a 400.
	if len(request.Modalities) > 0 {
		convertedModalities := make([]string, 0, len(request.Modalities))
		for _, m := range request.Modalities {
			if wire := canonicalGeminiModality(m); wire != "" {
				convertedModalities = append(convertedModalities, wire)
			}
		}
		if len(convertedModalities) > 0 {
			config.ResponseModalities = convertedModalities
			hasConfig = true
		}
	}

	if hasConfig {
		geminiReq.GenerationConfig = config
	}

	// Convert SafetySettings from metadata if present
	if safetyJSON := request.TransformerMetadataValue(model.TransformerMetadataGeminiSafetySettings); safetyJSON != "" {
		var safetySettings []*model.GeminiSafetySetting
		if err := json.Unmarshal([]byte(safetyJSON), &safetySettings); err == nil {
			geminiReq.SafetySettings = safetySettings
		}
	}

	// Convert tools. Gemini's API treats GeminiTool entries as a
	// discriminated union — functionDeclarations + googleSearch cannot
	// co-exist per the current API — so we emit server tools as separate
	// GeminiTool entries and log a warning if the client mixes both.
	if len(request.Tools) > 0 {
		functionDeclarations := make([]*model.GeminiFunctionDeclaration, 0, len(request.Tools))
		serverTools := make([]*model.GeminiTool, 0, len(request.Tools))

		for _, tool := range request.Tools {
			switch tool.Type {
			case "function", "":
				var params map[string]any
				if len(tool.Function.Parameters) > 0 {
					// Best-effort: if schema can't be parsed, we still send the declaration without parameters.
					if err := json.Unmarshal(tool.Function.Parameters, &params); err != nil {
						log.Warnf("gemini: failed to unmarshal tool parameters for %s: %v", tool.Function.Name, err)
					}
				}
				cleanGeminiSchema(params)

				functionDeclarations = append(functionDeclarations, &model.GeminiFunctionDeclaration{
					Name:        tool.Function.Name,
					Description: tool.Function.Description,
					Parameters:  params,
				})
			case "server_search":
				serverTools = append(serverTools, &model.GeminiTool{GoogleSearch: &model.GeminiGoogleSearch{}})
			case "code_execution":
				serverTools = append(serverTools, &model.GeminiTool{CodeExecution: &model.GeminiCodeExecution{}})
			case "url_context":
				serverTools = append(serverTools, &model.GeminiTool{UrlContext: &model.GeminiUrlContext{}})
			default:
				log.Warnf("gemini: dropping unsupported tool type %q", tool.Type)
			}
		}

		tools := make([]*model.GeminiTool, 0, len(serverTools)+1)
		if len(functionDeclarations) > 0 {
			tools = append(tools, &model.GeminiTool{FunctionDeclarations: functionDeclarations})
		}
		tools = append(tools, serverTools...)

		if len(functionDeclarations) > 0 && len(serverTools) > 0 {
			log.Warnf("gemini: server tools and functionDeclarations declared together; provider may reject the request")
		}

		if len(tools) > 0 {
			geminiReq.Tools = tools
		}
	}

	// Convert tool choice to Gemini toolConfig.functionCallingConfig.
	// Gemini only exposes mode = AUTO/ANY/NONE + allowedFunctionNames, so the
	// rich OpenAI / Anthropic variants collapse into one of three modes.
	// Anthropic's disable_parallel_tool_use has no Gemini equivalent and is
	// dropped (Gemini always emits at most one functionCall per Part anyway).
	if request.ToolChoice != nil {
		mode := "AUTO"
		var allowed []string

		if request.ToolChoice.ToolChoice != nil {
			switch strings.ToLower(*request.ToolChoice.ToolChoice) {
			case "auto":
				mode = "AUTO"
			case "required", "any":
				mode = "ANY"
			case "none":
				mode = "NONE"
			}
		} else if named := request.ToolChoice.NamedToolChoice; named != nil {
			switch strings.ToLower(named.Type) {
			case "auto":
				mode = "AUTO"
			case "any", "required":
				mode = "ANY"
			case "none":
				mode = "NONE"
			case "function", "tool":
				mode = "ANY"
				if name := named.ResolvedFunctionName(); name != "" {
					allowed = []string{name}
				}
			}
		}

		geminiReq.ToolConfig = &model.GeminiToolConfig{
			FunctionCallingConfig: &model.GeminiFunctionCallingConfig{
				Mode:                 mode,
				AllowedFunctionNames: allowed,
			},
		}
	}

	// cachedContent reference (G-H8): forward so the upstream reuses the
	// managed cached prefix instead of re-reading the bytes.
	if geminiExt.CachedContentRef != nil {
		if ref := strings.TrimSpace(*geminiExt.CachedContentRef); ref != "" {
			geminiReq.CachedContent = ref
		}
	}

	// Labels (G-H8): Gemini accepts arbitrary string→string tags for
	// billing / analytics attribution. We reuse the OpenAI-style Metadata
	// channel since both APIs model the same concept (k/v tags). Callers
	// targeting Gemini specifically can set request.Metadata and know it
	// will surface as `labels` on the wire.
	if len(request.Metadata) > 0 {
		labels := make(map[string]string, len(request.Metadata))
		for k, v := range request.Metadata {
			labels[k] = v
		}
		geminiReq.Labels = labels
	}

	return geminiReq

}
