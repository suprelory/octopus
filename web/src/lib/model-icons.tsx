import { memo, type ComponentType, type CSSProperties } from 'react';
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
import JinaIcon from '@lobehub/icons/es/Jina/components/Mono';
import Ai360Icon from '@lobehub/icons/es/Ai360/components/Mono';
import KlingIcon from '@lobehub/icons/es/Kling/components/Mono';
import JimengIcon from '@lobehub/icons/es/Jimeng/components/Mono';
import ViduIcon from '@lobehub/icons/es/Vidu/components/Mono';
import MidjourneyIcon from '@lobehub/icons/es/Midjourney/components/Mono';
import SunoIcon from '@lobehub/icons/es/Suno/components/Mono';
import V0Icon from '@lobehub/icons/es/V0/components/Mono';
import {
    resolveGroupIconKey,
    resolveModelIcon,
    type ModelIconKey,
    type ModelIconMatchOptions,
} from './model-icon-matcher';

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

function modelIcon(Icon: BrandIcon, color: string): ModelIcon {
    return { Avatar: createAvatar(Icon, color), color };
}

const MODEL_ICONS: Record<ModelIconKey, ModelIcon> = {
    OpenAI: modelIcon(OpenAIIcon, '#10A37F'),
    Claude: modelIcon(ClaudeIcon, '#D7765A'),
    Gemini: modelIcon(GeminiIcon, '#4285F4'),
    Gemma: modelIcon(GemmaIcon, '#4285F4'),
    Google: modelIcon(GoogleIcon, '#4285F4'),
    DeepSeek: modelIcon(DeepSeekIcon, '#4D6BFE'),
    Grok: modelIcon(GrokIcon, '#000000'),
    Qwen: modelIcon(QwenIcon, '#6B4EFF'),
    Zhipu: modelIcon(ZhipuIcon, '#3C5BFC'),
    Minimax: modelIcon(MinimaxIcon, '#1A1A2E'),
    Kimi: modelIcon(KimiIcon, '#000000'),
    Mistral: modelIcon(MistralIcon, '#F7D046'),
    Meta: modelIcon(MetaIcon, '#0668E1'),
    Doubao: modelIcon(DoubaoIcon, '#00D6C2'),
    Yi: modelIcon(YiIcon, '#1B1464'),
    Hunyuan: modelIcon(HunyuanIcon, '#0052D9'),
    Spark: modelIcon(SparkIcon, '#0078FF'),
    Wenxin: modelIcon(WenxinIcon, '#2932E1'),
    InternLM: modelIcon(InternLMIcon, '#2F54EB'),
    Stepfun: modelIcon(StepfunIcon, '#5B5CFF'),
    Nvidia: modelIcon(NvidiaIcon, '#76B900'),
    Azure: modelIcon(AzureIcon, '#0078D4'),
    Aws: modelIcon(AwsIcon, '#FF9900'),
    Volcengine: modelIcon(VolcengineIcon, '#3370FF'),
    SiliconCloud: modelIcon(SiliconCloudIcon, '#7C3AED'),
    Groq: modelIcon(GroqIcon, '#F55036'),
    Together: modelIcon(TogetherIcon, '#0F6FFF'),
    Fireworks: modelIcon(FireworksIcon, '#FF6B00'),
    Replicate: modelIcon(ReplicateIcon, '#000000'),
    Ollama: modelIcon(OllamaIcon, '#FFFFFF'),
    OpenRouter: modelIcon(OpenRouterIcon, '#6366F1'),
    Cloudflare: modelIcon(CloudflareIcon, '#F38020'),
    Cerebras: modelIcon(CerebrasIcon, '#FF5722'),
    SambaNova: modelIcon(SambaNovaIcon, '#FF6B00'),
    Novita: modelIcon(NovitaIcon, '#7C3AED'),
    HuggingFace: modelIcon(HuggingFaceIcon, '#FFD21E'),
    Cohere: modelIcon(CohereIcon, '#39594D'),
    Perplexity: modelIcon(PerplexityIcon, '#20B8CD'),
    Microsoft: modelIcon(MicrosoftIcon, '#00BCF2'),
    KwaiKAT: modelIcon(KwaiKATIcon, '#1969FC'),
    Jina: modelIcon(JinaIcon, '#000000'),
    Ai360: modelIcon(Ai360Icon, '#23B7E5'),
    Kling: modelIcon(KlingIcon, '#111827'),
    Jimeng: modelIcon(JimengIcon, '#1C64F2'),
    Vidu: modelIcon(ViduIcon, '#2563EB'),
    Midjourney: modelIcon(MidjourneyIcon, '#000000'),
    Suno: modelIcon(SunoIcon, '#000000'),
    V0: modelIcon(V0Icon, '#000000'),
};

const FALLBACK_COLOR = '#64748B';
const fallbackAvatars = new Map<string, AvatarComponent>();

function createFallbackAvatar(text: string): AvatarComponent {
    const cached = fallbackAvatars.get(text);
    if (cached) return cached;

    const FallbackAvatar = memo(function FallbackAvatar({ size = 24, shape = 'circle', className, style }: AvatarProps) {
        const dimension = typeof size === 'number' ? `${size}px` : size;
        const fontSize = typeof size === 'number' ? Math.max(11, Math.round(size * 0.42)) : 12;

        return (
            <span
                className={className}
                style={{
                    alignItems: 'center',
                    backgroundColor: FALLBACK_COLOR,
                    borderRadius: shape === 'square' ? '25%' : '9999px',
                    color: '#FFFFFF',
                    display: 'inline-flex',
                    flex: 'none',
                    fontSize,
                    fontWeight: 700,
                    height: dimension,
                    justifyContent: 'center',
                    width: dimension,
                    ...style,
                }}
            >
                {text}
            </span>
        );
    });

    FallbackAvatar.displayName = `ModelFallbackAvatar(${text})`;
    fallbackAvatars.set(text, FallbackAvatar);
    return FallbackAvatar;
}

export function getModelIcon(modelName: string, options: ModelIconMatchOptions = {}): ModelIcon {
    const match = resolveModelIcon(modelName, options);
    if (match.key) return MODEL_ICONS[match.key];

    return {
        Avatar: createFallbackAvatar(match.fallbackText),
        color: FALLBACK_COLOR,
    };
}

export function getGroupIcon(groupName: string, modelNames: string[] = []): ModelIcon | undefined {
    const key = resolveGroupIconKey(groupName, modelNames);
    return key ? MODEL_ICONS[key] : undefined;
}
