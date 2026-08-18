import assert from 'node:assert/strict';
import test from 'node:test';

import {
    getModelFallbackText,
    resolveGroupIconKey,
    resolveModelIcon,
} from '../src/lib/model-icon-matcher.ts';

test('explicit icon metadata has highest priority', () => {
    assert.deepEqual(resolveModelIcon('gpt-5.4', { icon: 'DeepSeek.Color', vendor: 'OpenAI' }), {
        key: 'DeepSeek',
        source: 'explicit',
        fallbackText: 'G',
    });
});

test('invalid explicit metadata falls back to the model family', () => {
    assert.equal(resolveModelIcon('us.anthropic.claude-opus-4-6', { icon: 'MissingIcon' }).key, 'Claude');
    assert.equal(resolveModelIcon('us.anthropic.claude-opus-4-6').source, 'model');
});

test('model family wins over a hosting vendor', () => {
    assert.deepEqual(resolveModelIcon('claude-sonnet-4-6', { vendor: 'OpenRouter' }), {
        key: 'Claude',
        source: 'model',
        fallbackText: 'C',
    });
});

test('vendor metadata is used when the model family is unknown', () => {
    assert.equal(resolveModelIcon('custom-chat-model', { vendor: '阿里巴巴' }).key, 'Qwen');
    assert.equal(resolveModelIcon('custom-chat-model', { vendor: 'Alibaba Cloud' }).source, 'vendor');
    assert.equal(resolveModelIcon('custom-chat-model', { vendorIcon: 'Claude.Color', vendor: 'OpenAI' }).key, 'Claude');
});

test('multi-level namespaces provide a vendor fallback', () => {
    assert.deepEqual(resolveModelIcon('gateway/openrouter/models/acme-chat'), {
        key: 'OpenRouter',
        source: 'namespace',
        fallbackText: 'A',
    });
});

test('model family is resolved from the leaf of a multi-level namespace', () => {
    assert.equal(resolveModelIcon('gateway/openrouter/models/deepseek-r1').key, 'DeepSeek');
    assert.equal(resolveModelIcon('models/meta-llama/Llama-3.3-70B').key, 'Meta');
    assert.equal(resolveModelIcon('google/palm-2').key, 'Google');
    assert.equal(resolveModelIcon('anthropic-legacy-chat').key, 'Claude');
    assert.equal(resolveModelIcon('alibaba-qwen-max').key, 'Qwen');
});

test('the earliest family marker resolves distilled model names deterministically', () => {
    assert.equal(resolveModelIcon('deepseek-r1-distill-qwen-32b').key, 'DeepSeek');
    assert.equal(resolveModelIcon('qwen2.5-deepseek-r1-distill').key, 'Qwen');
});

test('unknown models fall back to the leaf initial', () => {
    assert.deepEqual(resolveModelIcon('vendor/custom-model'), {
        source: 'fallback',
        fallbackText: 'C',
    });
    assert.equal(getModelFallbackText('供应商/模型-a'), '模');
});

test('group matching prefers the group name and then its models', () => {
    assert.equal(resolveGroupIconKey('DeepSeek VIP', ['gpt-5.4']), 'DeepSeek');
    assert.equal(resolveGroupIconKey('Premium', ['openrouter/claude-sonnet-4-6']), 'Claude');
});
