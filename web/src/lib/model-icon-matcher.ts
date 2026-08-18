export const MODEL_ICON_KEYS = [
    'OpenAI',
    'Claude',
    'Gemini',
    'Gemma',
    'Google',
    'DeepSeek',
    'Grok',
    'Qwen',
    'Zhipu',
    'Minimax',
    'Kimi',
    'Mistral',
    'Meta',
    'Doubao',
    'Yi',
    'Hunyuan',
    'Spark',
    'Wenxin',
    'InternLM',
    'Stepfun',
    'Nvidia',
    'Azure',
    'Aws',
    'Volcengine',
    'SiliconCloud',
    'Groq',
    'Together',
    'Fireworks',
    'Replicate',
    'Ollama',
    'OpenRouter',
    'Cloudflare',
    'Cerebras',
    'SambaNova',
    'Novita',
    'HuggingFace',
    'Cohere',
    'Perplexity',
    'Microsoft',
    'KwaiKAT',
    'Jina',
    'Ai360',
    'Kling',
    'Jimeng',
    'Vidu',
    'Midjourney',
    'Suno',
    'V0',
] as const;

export type ModelIconKey = (typeof MODEL_ICON_KEYS)[number];
export type ModelIconMatchSource = 'explicit' | 'model' | 'vendor' | 'namespace' | 'fallback';

export type ModelIconMatchOptions = {
    /** Lobe icon export name, for example `OpenAI`, `Claude.Color`, or `Qwen.Avatar`. */
    icon?: string | null;
    /** Persisted vendor icon key used when the model has no family match. */
    vendorIcon?: string | null;
    /** Provider/vendor name used only when the model family cannot be identified. */
    vendor?: string | null;
};

export type ModelIconMatch = {
    key?: ModelIconKey;
    source: ModelIconMatchSource;
    fallbackText: string;
};

type MatchRule = {
    key: ModelIconKey;
    aliases: readonly string[];
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

const ICON_KEY_BY_IDENTITY = new Map<string, ModelIconKey>();

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

for (const key of MODEL_ICON_KEYS) {
    ICON_KEY_BY_IDENTITY.set(normalizeIdentity(key), key);
}

const EXPLICIT_ICON_ALIASES: Readonly<Record<string, ModelIconKey>> = {
    anthropic: 'Claude',
    xai: 'Grok',
    alibaba: 'Qwen',
    qwenlm: 'Qwen',
    zai: 'Zhipu',
    zhipuai: 'Zhipu',
    moonshot: 'Kimi',
    moonshotai: 'Kimi',
    baidu: 'Wenxin',
    iflytek: 'Spark',
    tencent: 'Hunyuan',
    bytedance: 'Doubao',
    '01ai': 'Yi',
    azureai: 'Azure',
    siliconflow: 'SiliconCloud',
    kuaishou: 'Kling',
};

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

function aliasPosition(value: string, alias: string): number | undefined {
    const normalizedValue = normalizeForMatch(value);
    const normalizedAlias = normalizeForMatch(alias);
    if (!normalizedValue || !normalizedAlias) return undefined;

    const escapedAlias = normalizedAlias.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
    const match = new RegExp(`(?:^|-)${escapedAlias}(?=$|-|\\d)`, 'u').exec(normalizedValue);
    if (!match) return undefined;
    return match.index + (match[0].startsWith('-') ? 1 : 0);
}

function findRuleKey(value: string, rules: readonly MatchRule[]): ModelIconKey | undefined {
    let best: { key: ModelIconKey; position: number; ruleIndex: number } | undefined;

    rules.forEach((rule, ruleIndex) => {
        for (const alias of rule.aliases) {
            const position = aliasPosition(value, alias);
            if (position === undefined) continue;
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

function findModelFamilyKey(modelName: string): ModelIconKey | undefined {
    const segments = modelPathSegments(modelName);
    for (let index = segments.length - 1; index >= 0; index -= 1) {
        const key = findRuleKey(segments[index], MODEL_FAMILY_RULES);
        if (key) return key;
    }
    return undefined;
}

function findNamespaceVendorKey(modelName: string): ModelIconKey | undefined {
    const segments = modelPathSegments(modelName);
    for (let index = segments.length - 2; index >= 0; index -= 1) {
        const key = VENDOR_KEY_BY_IDENTITY.get(normalizeIdentity(segments[index]));
        if (key) return key;
    }
    return undefined;
}

export function resolveExplicitIconKey(icon: string | null | undefined): ModelIconKey | undefined {
    const baseName = icon?.trim().split('.')[0] ?? '';
    const identity = normalizeIdentity(baseName);
    if (!identity) return undefined;
    return ICON_KEY_BY_IDENTITY.get(identity) ?? EXPLICIT_ICON_ALIASES[identity];
}

export function resolveVendorIconKey(vendor: string | null | undefined): ModelIconKey | undefined {
    if (!vendor?.trim()) return undefined;
    return VENDOR_KEY_BY_IDENTITY.get(normalizeIdentity(vendor)) ?? findRuleKey(vendor, VENDOR_RULES);
}

export function getModelFallbackText(modelName: string): string {
    const segments = modelPathSegments(modelName);
    const leaf = segments.at(-1) ?? modelName;
    return leaf.match(/[\p{L}\p{N}]/u)?.[0]?.toUpperCase() ?? '?';
}

export function resolveModelIcon(modelName: string, options: ModelIconMatchOptions = {}): ModelIconMatch {
    const fallbackText = getModelFallbackText(modelName);
    const explicitKey = resolveExplicitIconKey(options.icon);
    if (explicitKey) return { key: explicitKey, source: 'explicit', fallbackText };

    const modelKey = findModelFamilyKey(modelName);
    if (modelKey) return { key: modelKey, source: 'model', fallbackText };

    const vendorIconKey = resolveExplicitIconKey(options.vendorIcon);
    if (vendorIconKey) return { key: vendorIconKey, source: 'vendor', fallbackText };

    const vendorKey = resolveVendorIconKey(options.vendor);
    if (vendorKey) return { key: vendorKey, source: 'vendor', fallbackText };

    const namespaceKey = findNamespaceVendorKey(modelName) ?? findRuleKey(modelName, VENDOR_RULES);
    if (namespaceKey) return { key: namespaceKey, source: 'namespace', fallbackText };

    return { source: 'fallback', fallbackText };
}

export function resolveGroupIconKey(groupName: string, modelNames: readonly string[] = []): ModelIconKey | undefined {
    const groupKey = findRuleKey(groupName, MODEL_FAMILY_RULES) ?? findRuleKey(groupName, VENDOR_RULES);
    if (groupKey) return groupKey;

    for (const modelName of modelNames) {
        const modelKey = findModelFamilyKey(modelName) ?? findNamespaceVendorKey(modelName);
        if (modelKey) return modelKey;
    }

    return undefined;
}
