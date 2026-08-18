export type ModelIconKey =
    | 'OpenAI'
    | 'Claude'
    | 'Gemini'
    | 'Gemma'
    | 'Google'
    | 'DeepSeek'
    | 'Grok'
    | 'Qwen'
    | 'Zhipu'
    | 'Minimax'
    | 'Kimi'
    | 'Mistral'
    | 'Meta'
    | 'Doubao'
    | 'Yi'
    | 'Hunyuan'
    | 'Spark'
    | 'Wenxin'
    | 'InternLM'
    | 'Stepfun'
    | 'Nvidia'
    | 'Azure'
    | 'Aws'
    | 'Volcengine'
    | 'SiliconCloud'
    | 'Groq'
    | 'Together'
    | 'Fireworks'
    | 'Replicate'
    | 'Ollama'
    | 'OpenRouter'
    | 'Cloudflare'
    | 'Cerebras'
    | 'SambaNova'
    | 'Novita'
    | 'HuggingFace'
    | 'Cohere'
    | 'Perplexity'
    | 'Microsoft'
    | 'KwaiKAT'
    | 'Jina'
    | 'Ai360'
    | 'Kling'
    | 'Jimeng'
    | 'Vidu'
    | 'Midjourney'
    | 'Suno'
    | 'V0';
export type ModelIconMatchSource = 'model' | 'namespace' | 'fallback';

export type ModelIconMatch =
    | { key: ModelIconKey; source: Exclude<ModelIconMatchSource, 'fallback'> }
    | { source: 'fallback'; fallbackText: string };

type MatchRule = {
    key: ModelIconKey;
    aliases: readonly string[];
};

type CompiledMatchRule = {
    key: ModelIconKey;
    aliases: readonly RegExp[];
};

// Model-family rules are ordered only as a tie-breaker. The earliest family
// marker in the model ID wins, so `deepseek-r1-distill-qwen` remains DeepSeek
// while `qwen2.5-deepseek-distill` remains Qwen.
const MODEL_FAMILY_RULES: readonly MatchRule[] = [
    { key: 'OpenAI', aliases: ['gpt', 'o1', 'o3', 'o4', 'chatgpt', 'codex', 'text-embedding', 'dall-e', 'whisper', 'tts', 'openai'] },
    { key: 'Claude', aliases: ['claude', 'anthropic'] },
    { key: 'Gemini', aliases: ['gemini', 'imagen', 'veo', 'learnlm'] },
    { key: 'Google', aliases: ['palm', 'google'] },
    { key: 'Gemma', aliases: ['gemma'] },
    { key: 'DeepSeek', aliases: ['deepseek'] },
    { key: 'Grok', aliases: ['grok'] },
    { key: 'Qwen', aliases: ['qwen', 'qwq', 'alibaba'] },
    { key: 'Zhipu', aliases: ['glm', 'chatglm', 'cogview', 'cogvideo', 'zhipu', 'z-ai', 'zai'] },
    { key: 'Minimax', aliases: ['minimax', 'abab'] },
    { key: 'Kimi', aliases: ['kimi', 'moonshot'] },
    { key: 'Mistral', aliases: ['mistral', 'mixtral', 'codestral', 'pixtral', 'voxtral', 'magistral'] },
    { key: 'Meta', aliases: ['llama', 'codellama', 'meta'] },
    { key: 'Doubao', aliases: ['doubao', 'skylark', 'bytedance'] },
    { key: 'Yi', aliases: ['yi', '01-ai'] },
    { key: 'Hunyuan', aliases: ['hunyuan'] },
    { key: 'Spark', aliases: ['spark'] },
    { key: 'Wenxin', aliases: ['ernie', 'wenxin', 'baidu'] },
    { key: 'InternLM', aliases: ['internlm'] },
    { key: 'Stepfun', aliases: ['stepfun', 'step'] },
    { key: 'Nvidia', aliases: ['nemotron'] },
    { key: 'Cohere', aliases: ['command', 'c4ai'] },
    { key: 'Perplexity', aliases: ['perplexity', 'pplx', 'sonar'] },
    { key: 'Microsoft', aliases: ['phi'] },
    { key: 'KwaiKAT', aliases: ['kwaikat', 'kat'] },
    { key: 'Jina', aliases: ['jina'] },
    { key: 'Ai360', aliases: ['360'] },
    { key: 'Kling', aliases: ['kling'] },
    { key: 'Jimeng', aliases: ['jimeng'] },
    { key: 'Vidu', aliases: ['vidu'] },
    { key: 'Midjourney', aliases: ['midjourney', 'mj'] },
    { key: 'Suno', aliases: ['suno'] },
    { key: 'V0', aliases: ['v0'] },
];

const VENDOR_RULES: readonly MatchRule[] = [
    { key: 'OpenAI', aliases: ['openai'] },
    { key: 'Claude', aliases: ['anthropic'] },
    { key: 'Google', aliases: ['google', 'google-ai'] },
    { key: 'DeepSeek', aliases: ['deepseek'] },
    { key: 'Grok', aliases: ['xai', 'x-ai'] },
    { key: 'Qwen', aliases: ['alibaba', 'aliyun', 'dashscope'] },
    { key: 'Zhipu', aliases: ['zhipu', 'zhipuai', 'z-ai', 'zai'] },
    { key: 'Minimax', aliases: ['minimax'] },
    { key: 'Kimi', aliases: ['moonshot', 'moonshotai'] },
    { key: 'Mistral', aliases: ['mistral', 'mistralai'] },
    { key: 'Meta', aliases: ['meta', 'meta-llama'] },
    { key: 'Doubao', aliases: ['bytedance', 'volcengine-doubao'] },
    { key: 'Yi', aliases: ['01-ai', '01ai'] },
    { key: 'Hunyuan', aliases: ['tencent'] },
    { key: 'Spark', aliases: ['iflytek'] },
    { key: 'Wenxin', aliases: ['baidu'] },
    { key: 'Nvidia', aliases: ['nvidia'] },
    { key: 'Azure', aliases: ['azure', 'azure-ai', 'azureai'] },
    { key: 'Aws', aliases: ['aws', 'amazon', 'bedrock'] },
    { key: 'Volcengine', aliases: ['volcengine'] },
    { key: 'SiliconCloud', aliases: ['siliconflow', 'siliconcloud'] },
    { key: 'Groq', aliases: ['groq'] },
    { key: 'Together', aliases: ['together', 'togetherai'] },
    { key: 'Fireworks', aliases: ['fireworks', 'fireworksai'] },
    { key: 'Replicate', aliases: ['replicate'] },
    { key: 'Ollama', aliases: ['ollama'] },
    { key: 'OpenRouter', aliases: ['openrouter'] },
    { key: 'Cloudflare', aliases: ['cloudflare'] },
    { key: 'Cerebras', aliases: ['cerebras'] },
    { key: 'SambaNova', aliases: ['sambanova'] },
    { key: 'Novita', aliases: ['novita', 'novitaai'] },
    { key: 'HuggingFace', aliases: ['huggingface', 'hugging-face', 'hf'] },
    { key: 'Cohere', aliases: ['cohere'] },
    { key: 'Perplexity', aliases: ['perplexity'] },
    { key: 'KwaiKAT', aliases: ['kuaishou', 'kwaipilot'] },
    { key: 'Jina', aliases: ['jina'] },
    { key: 'Ai360', aliases: ['360', 'ai360'] },
    { key: 'Kling', aliases: ['kling', 'kuaishou-kling'] },
    { key: 'Jimeng', aliases: ['jimeng'] },
    { key: 'Vidu', aliases: ['vidu'] },
    { key: 'V0', aliases: ['v0', 'vercel-v0'] },
];

function normalizeForMatch(value: string): string {
    return value
        .normalize('NFKC')
        .trim()
        .toLowerCase()
        .replace(/[^\p{L}\p{N}]+/gu, '-')
        .replace(/^-+|-+$/g, '');
}

function normalizeIdentity(value: string): string {
    return normalizeForMatch(value).replace(/-/g, '');
}

const VENDOR_KEY_BY_IDENTITY = new Map<string, ModelIconKey>();
for (const rule of VENDOR_RULES) {
    for (const alias of rule.aliases) {
        VENDOR_KEY_BY_IDENTITY.set(normalizeIdentity(alias), rule.key);
    }
}
for (const [alias, key] of Object.entries({
    '阿里巴巴': 'Qwen',
    '智谱': 'Zhipu',
    '百度': 'Wenxin',
    '讯飞': 'Spark',
    '腾讯': 'Hunyuan',
    '零一万物': 'Yi',
    '字节跳动': 'Doubao',
    '快手': 'Kling',
    '即梦': 'Jimeng',
} satisfies Record<string, ModelIconKey>)) {
    VENDOR_KEY_BY_IDENTITY.set(normalizeIdentity(alias), key);
}

function compileRules(rules: readonly MatchRule[]): readonly CompiledMatchRule[] {
    return rules.map((rule) => ({
        key: rule.key,
        aliases: rule.aliases.map((alias) => {
            const normalizedAlias = normalizeForMatch(alias);
            const escapedAlias = normalizedAlias.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
            return new RegExp(`(?:^|-)${escapedAlias}(?=$|-|\\d)`, 'u');
        }),
    }));
}

const COMPILED_MODEL_FAMILY_RULES = compileRules(MODEL_FAMILY_RULES);
const COMPILED_VENDOR_RULES = compileRules(VENDOR_RULES);

function findRuleKey(value: string, rules: readonly CompiledMatchRule[]): ModelIconKey | undefined {
    const normalizedValue = normalizeForMatch(value);
    if (!normalizedValue) return undefined;
    let best: { key: ModelIconKey; position: number; ruleIndex: number } | undefined;

    rules.forEach((rule, ruleIndex) => {
        for (const matcher of rule.aliases) {
            const match = matcher.exec(normalizedValue);
            if (!match) continue;
            const position = match.index + (match[0].startsWith('-') ? 1 : 0);
            if (!best || position < best.position || (position === best.position && ruleIndex < best.ruleIndex)) {
                best = { key: rule.key, position, ruleIndex };
            }
        }
    });

    return best?.key;
}

function modelPathSegments(modelName: string): string[] {
    return modelName
        .trim()
        .split(/[\\/]+/)
        .map((segment) => segment.trim())
        .filter(Boolean);
}

function findModelFamilyKey(segments: readonly string[]): ModelIconKey | undefined {
    for (let index = segments.length - 1; index >= 0; index -= 1) {
        const key = findRuleKey(segments[index], COMPILED_MODEL_FAMILY_RULES);
        if (key) return key;
    }
    return undefined;
}

function findNamespaceVendorKey(segments: readonly string[]): ModelIconKey | undefined {
    for (let index = segments.length - 2; index >= 0; index -= 1) {
        const key = VENDOR_KEY_BY_IDENTITY.get(normalizeIdentity(segments[index]));
        if (key) return key;
    }
    return undefined;
}

function getModelFallbackText(segments: readonly string[], originalName: string): string {
    const leaf = segments.at(-1) ?? originalName;
    return leaf.match(/[\p{L}\p{N}]/u)?.[0]?.toUpperCase() ?? '?';
}

export function resolveModelIcon(modelName: string): ModelIconMatch {
    const segments = modelPathSegments(modelName);
    const modelKey = findModelFamilyKey(segments);
    if (modelKey) return { key: modelKey, source: 'model' };

    const namespaceKey = findNamespaceVendorKey(segments) ?? findRuleKey(modelName, COMPILED_VENDOR_RULES);
    if (namespaceKey) return { key: namespaceKey, source: 'namespace' };

    return { source: 'fallback', fallbackText: getModelFallbackText(segments, modelName) };
}

export function resolveGroupIconKey(groupName: string, modelNames: readonly string[] = []): ModelIconKey | undefined {
    const groupKey = findRuleKey(groupName, COMPILED_MODEL_FAMILY_RULES) ?? findRuleKey(groupName, COMPILED_VENDOR_RULES);
    if (groupKey) return groupKey;

    for (const modelName of modelNames) {
        const segments = modelPathSegments(modelName);
        const modelKey = findModelFamilyKey(segments) ?? findNamespaceVendorKey(segments);
        if (modelKey) return modelKey;
    }

    return undefined;
}
