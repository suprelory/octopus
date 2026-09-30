package model

type APIFormat string

const (
	APIFormatOpenAIChatCompletion  APIFormat = "openai/chat_completions"
	APIFormatOpenAIResponse        APIFormat = "openai/responses"
	APIFormatOpenAIImageGeneration APIFormat = "openai/image_generation"
	APIFormatOpenAIEmbedding       APIFormat = "openai/embeddings"
	APIFormatGeminiContents        APIFormat = "gemini/contents"
	APIFormatAnthropicMessage      APIFormat = "anthropic/messages"
	APIFormatAiSDKText             APIFormat = "aisdk/text"
	APIFormatAiSDKDataStream       APIFormat = "aisdk/datastream"
)

const (
	TransformerMetadataWSExecutionMode              = "octopus_ws_execution_mode"
	TransformerMetadataWSExecutionModeReplayExact   = "replay_exact"
	TransformerMetadataAnthropicUserID              = "anthropic_user_id"
	TransformerMetadataAnthropicSystemArrayFormat   = "anthropic_system_array_format"
	TransformerMetadataAnthropicContext1M           = "anthropic_context_1m"
	TransformerMetadataAnthropicMaxTokensRepairFrom = "anthropic_max_tokens_repair_from"
	TransformerMetadataOpenAIOrganization           = "openai_organization"
	TransformerMetadataOpenAIProject                = "openai_project"
	TransformerMetadataGeminiFilesAPIURI            = "gemini_files_api_uri"
	TransformerMetadataGeminiMediaResolution        = "gemini_media_resolution"
	TransformerMetadataGeminiCandidateCount         = "gemini_candidate_count"
	TransformerMetadataGeminiSafetySettings         = "gemini_safety_settings"
)
