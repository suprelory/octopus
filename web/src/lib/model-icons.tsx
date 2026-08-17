import { memo, type ComponentType, type CSSProperties } from 'react';
import { Bot } from 'lucide-react';
import OpenAIIcon from '@lobehub/icons/es/OpenAI/components/Mono';
import ClaudeIcon from '@lobehub/icons/es/Claude/components/Mono';
import GeminiIcon from '@lobehub/icons/es/Gemini/components/Mono';
import DeepSeekIcon from '@lobehub/icons/es/DeepSeek/components/Mono';
import MistralIcon from '@lobehub/icons/es/Mistral/components/Mono';
import QwenIcon from '@lobehub/icons/es/Qwen/components/Mono';
import MetaIcon from '@lobehub/icons/es/Meta/components/Mono';
import OllamaIcon from '@lobehub/icons/es/Ollama/components/Mono';
import GroqIcon from '@lobehub/icons/es/Groq/components/Mono';
import CohereIcon from '@lobehub/icons/es/Cohere/components/Mono';
import PerplexityIcon from '@lobehub/icons/es/Perplexity/components/Mono';
import ZhipuIcon from '@lobehub/icons/es/Zhipu/components/Mono';
import YiIcon from '@lobehub/icons/es/Yi/components/Mono';
import KimiIcon from '@lobehub/icons/es/Kimi/components/Mono';
import MinimaxIcon from '@lobehub/icons/es/Minimax/components/Mono';
import DoubaoIcon from '@lobehub/icons/es/Doubao/components/Mono';
import HunyuanIcon from '@lobehub/icons/es/Hunyuan/components/Mono';
import SparkIcon from '@lobehub/icons/es/Spark/components/Mono';
import WenxinIcon from '@lobehub/icons/es/Wenxin/components/Mono';
import NvidiaIcon from '@lobehub/icons/es/Nvidia/components/Mono';
import AzureIcon from '@lobehub/icons/es/Azure/components/Mono';
import AwsIcon from '@lobehub/icons/es/Aws/components/Mono';
import TogetherIcon from '@lobehub/icons/es/Together/components/Mono';
import FireworksIcon from '@lobehub/icons/es/Fireworks/components/Mono';
import ReplicateIcon from '@lobehub/icons/es/Replicate/components/Mono';
import HuggingFaceIcon from '@lobehub/icons/es/HuggingFace/components/Mono';
import GrokIcon from '@lobehub/icons/es/Grok/components/Mono';
import GoogleIcon from '@lobehub/icons/es/Google/components/Mono';
import CerebrasIcon from '@lobehub/icons/es/Cerebras/components/Mono';
import SambaNovaIcon from '@lobehub/icons/es/SambaNova/components/Mono';
import CloudflareIcon from '@lobehub/icons/es/Cloudflare/components/Mono';
import OpenRouterIcon from '@lobehub/icons/es/OpenRouter/components/Mono';
import VolcengineIcon from '@lobehub/icons/es/Volcengine/components/Mono';
import SiliconCloudIcon from '@lobehub/icons/es/SiliconCloud/components/Mono';
import NovitaIcon from '@lobehub/icons/es/Novita/components/Mono';
import InternLMIcon from '@lobehub/icons/es/InternLM/components/Mono';
import StepfunIcon from '@lobehub/icons/es/Stepfun/components/Mono';
import GemmaIcon from '@lobehub/icons/es/Gemma/components/Mono';
import MicrosoftIcon from '@lobehub/icons/es/Microsoft/components/Mono';
import KwaiKATIcon from '@lobehub/icons/es/KwaiKAT/components/Mono';

type BrandIcon = ComponentType<{
    size?: number | string;
    className?: string;
    color?: string;
    style?: CSSProperties;
}>;

type AvatarProps = {
    size?: number | string;
    shape?: 'circle' | 'square';
    className?: string;
    style?: CSSProperties;
};

type AvatarComponent = ComponentType<AvatarProps>;

export type ModelIcon = {
    Avatar: AvatarComponent;
    color: string;
};

type ModelIconConfig = ModelIcon & {
    prefixes: string[];
};

function foregroundFor(background: string) {
    const hex = background.replace('#', '');
    if (!/^[0-9a-f]{6}$/i.test(hex)) return '#FFFFFF';

    const red = Number.parseInt(hex.slice(0, 2), 16);
    const green = Number.parseInt(hex.slice(2, 4), 16);
    const blue = Number.parseInt(hex.slice(4, 6), 16);
    const luminance = (red * 0.299 + green * 0.587 + blue * 0.114) / 255;
    return luminance > 0.62 ? '#111827' : '#FFFFFF';
}

function createAvatar(Icon: BrandIcon, background: string): AvatarComponent {
    const foreground = foregroundFor(background);

    const BrandAvatar = memo(function BrandAvatar({ size = 24, shape = 'circle', className, style }: AvatarProps) {
        const dimension = typeof size === 'number' ? `${size}px` : size;
        const iconSize = typeof size === 'number' ? Math.max(12, Math.round(size * 0.58)) : 14;

        return (
            <span
                className={className}
                style={{
                    alignItems: 'center',
                    backgroundColor: background,
                    borderRadius: shape === 'square' ? '25%' : '9999px',
                    color: foreground,
                    display: 'inline-flex',
                    flex: 'none',
                    height: dimension,
                    justifyContent: 'center',
                    width: dimension,
                    ...style,
                }}
            >
                <Icon color={foreground} size={iconSize} />
            </span>
        );
    });

    BrandAvatar.displayName = 'ModelBrandAvatar';
    return BrandAvatar;
}

function modelIcon(prefixes: string[], Icon: BrandIcon, color: string): ModelIconConfig {
    return { prefixes, Avatar: createAvatar(Icon, color), color };
}

/** Provider/model prefix mappings, aligned with internal/price/price.go. */
const MODEL_ICON_PATTERNS: ModelIconConfig[] = [
    modelIcon(['gpt-', 'o1', 'o3', 'o4', 'chatgpt', 'text-embedding', 'dall-e', 'openai'], OpenAIIcon, '#10A37F'),
    modelIcon(['claude', 'anthropic'], ClaudeIcon, '#D7765A'),
    modelIcon(['gemini'], GeminiIcon, '#4285F4'),
    modelIcon(['gemma'], GemmaIcon, '#4285F4'),
    modelIcon(['palm', 'google'], GoogleIcon, '#4285F4'),
    modelIcon(['deepseek'], DeepSeekIcon, '#4D6BFE'),
    modelIcon(['grok', 'xai'], GrokIcon, '#000000'),
    modelIcon(['qwen', 'qwq', 'alibaba'], QwenIcon, '#6B4EFF'),
    modelIcon(['glm', 'chatglm', 'zhipu', 'z-ai', 'zai-'], ZhipuIcon, '#3C5BFC'),
    modelIcon(['minimax', 'abab'], MinimaxIcon, '#1A1A2E'),
    modelIcon(['moonshot', 'kimi'], KimiIcon, '#000000'),
    modelIcon(['mistral', 'mixtral', 'codestral', 'pixtral'], MistralIcon, '#F7D046'),
    modelIcon(['llama', 'meta-llama', 'meta'], MetaIcon, '#0668E1'),
    modelIcon(['doubao', 'skylark', 'bytedance'], DoubaoIcon, '#00D6C2'),
    modelIcon(['yi-', '01-ai'], YiIcon, '#1B1464'),
    modelIcon(['hunyuan'], HunyuanIcon, '#0052D9'),
    modelIcon(['spark'], SparkIcon, '#0078FF'),
    modelIcon(['ernie', 'wenxin', 'baidu'], WenxinIcon, '#2932E1'),
    modelIcon(['internlm'], InternLMIcon, '#2F54EB'),
    modelIcon(['stepfun', 'step-'], StepfunIcon, '#5B5CFF'),
    modelIcon(['nvidia', 'nemotron'], NvidiaIcon, '#76B900'),
    modelIcon(['azure'], AzureIcon, '#0078D4'),
    modelIcon(['aws', 'amazon', 'bedrock'], AwsIcon, '#FF9900'),
    modelIcon(['volcengine'], VolcengineIcon, '#3370FF'),
    modelIcon(['siliconflow'], SiliconCloudIcon, '#7C3AED'),
    modelIcon(['groq'], GroqIcon, '#F55036'),
    modelIcon(['together'], TogetherIcon, '#0F6FFF'),
    modelIcon(['fireworks'], FireworksIcon, '#FF6B00'),
    modelIcon(['replicate'], ReplicateIcon, '#000000'),
    modelIcon(['ollama'], OllamaIcon, '#FFFFFF'),
    modelIcon(['openrouter'], OpenRouterIcon, '#6366F1'),
    modelIcon(['cloudflare'], CloudflareIcon, '#F38020'),
    modelIcon(['cerebras'], CerebrasIcon, '#FF5722'),
    modelIcon(['sambanova'], SambaNovaIcon, '#FF6B00'),
    modelIcon(['novita'], NovitaIcon, '#7C3AED'),
    modelIcon(['huggingface', 'hf'], HuggingFaceIcon, '#FFD21E'),
    modelIcon(['cohere', 'command'], CohereIcon, '#39594D'),
    modelIcon(['perplexity'], PerplexityIcon, '#20B8CD'),
    modelIcon(['phi-'], MicrosoftIcon, '#00BCF2'),
    modelIcon(['kat'], KwaiKATIcon, '#1969FC'),
];

const DEFAULT_CONFIG: ModelIcon = {
    Avatar: createAvatar(Bot, '#64748B'),
    color: '#64748B',
};

function findModelIcon(modelName: string): ModelIcon | undefined {
    const nameToMatch = modelName.includes('/') ? modelName.split('/')[1] : modelName;
    const lowerName = nameToMatch.toLowerCase();
    return MODEL_ICON_PATTERNS.find(({ prefixes }) =>
        prefixes.some((prefix) => lowerName.startsWith(prefix))
    );
}

function findGroupNameIcon(groupName: string): ModelIcon | undefined {
    const lowerName = groupName.trim().toLowerCase();
    if (!lowerName) return undefined;

    return MODEL_ICON_PATTERNS.find(({ prefixes }) => prefixes.some((prefix) => {
        const keyword = prefix.replace(/[-_]$/, '');
        if (!keyword) return false;

        if (keyword.length >= 5 && lowerName.includes(keyword)) return true;

        const escaped = keyword.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
        return new RegExp(`(^|[^a-z0-9])${escaped}([^a-z0-9]|$)`, 'i').test(lowerName);
    }));
}

export function getModelIcon(modelName: string): ModelIcon {
    return findModelIcon(modelName) ?? DEFAULT_CONFIG;
}

export function getGroupIcon(groupName: string, modelNames: string[] = []): ModelIcon | undefined {
    const groupNameIcon = findGroupNameIcon(groupName);
    if (groupNameIcon) return groupNameIcon;

    for (const modelName of modelNames) {
        const modelIconConfig = findModelIcon(modelName);
        if (modelIconConfig) return modelIconConfig;
    }

    return undefined;
}
