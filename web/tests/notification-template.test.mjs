import assert from 'node:assert/strict';
import test from 'node:test';
import { renderToStaticMarkup } from 'react-dom/server';
import { createElement } from 'react';
import Markdown from 'react-markdown';
import { escapeNotificationMarkdown, renderNotificationTemplate, validNotificationTemplate } from '../src/components/modules/setting/notification-template.ts';

test('notification preview substitutes known values once and keeps missing values empty', () => {
    assert.equal(renderNotificationTemplate('{{ site }}\n{{message}} / {{balance}}', { site: 'A "site"', message: '{{title}}' }), 'A "site"\n{{title}} / ');
    assert.equal(renderNotificationTemplate('{{unknown}}', {}), null);
    assert.equal(renderNotificationTemplate('{{message', {}), null);
});

test('code preview keeps variable punctuation literal and prevents delimiter injection', () => {
    for (const source of ['`{{site}}`', '```text\n{{site}}\n```', '~~~text\n{{site}}\n~~~', '```text\n{{site}}']) {
        const site = 'account_*`x`<tag>&' + (source.includes('\n') ? '\n```\n~~~\n**injected**' : '');
        const html = renderToStaticMarkup(createElement(Markdown, null, renderNotificationTemplate(source, { site }, true)));
        assert.match(html, /account_\*`x`&lt;tag&gt;&amp;/);
        assert.doesNotMatch(html, /<strong>|<tag>|account\\_/);
        assert.match(html, /<code/);
    }
});

test('notification template validation enforces UTF-8 limits and matching variables', () => {
    assert.equal(validNotificationTemplate({}), true);
    assert.equal(validNotificationTemplate({ title: '{{emoji}} {{title}}', body: '{{message}}\n{{time}}' }), true);
    assert.equal(validNotificationTemplate({ format: 'text' }), true);
    assert.equal(validNotificationTemplate({ format: 'markdown', body: '**{{site}}**' }), true);
    assert.equal(validNotificationTemplate({ format: 'html' }), false);
    for (const template of [{ title: 'a\nb' }, { body: '{{password}}' }, { body: '{{site' }, { body: '中'.repeat(3000) }, { title: '中'.repeat(200) }, { body: '\0' }]) {
        assert.equal(validNotificationTemplate(template), false);
    }
});

test('Markdown preview escapes variable syntax once while preserving template formatting', () => {
    const site = 'A_* <b>unsafe</b> & [link](https://evil.example) {{title}}';
    assert.equal(renderNotificationTemplate('**{{site}}**', { site }, true), `**${escapeNotificationMarkdown(site)}**`);
    assert.equal(renderNotificationTemplate('{{site}}', { site }), site);
    assert.equal(escapeNotificationMarkdown('emoji 😀\r\n- text\0'), 'emoji 😀\n\\- text');
});
