import assert from 'node:assert/strict';
import test from 'node:test';
import { renderNotificationTemplate, validNotificationTemplate } from '../src/components/modules/setting/notification-template.ts';

test('notification preview substitutes known values once and keeps missing values empty', () => {
    assert.equal(renderNotificationTemplate('{{ site }}\n{{message}} / {{balance}}', { site: 'A "site"', message: '{{title}}' }), 'A "site"\n{{title}} / ');
    assert.equal(renderNotificationTemplate('{{unknown}}', {}), null);
    assert.equal(renderNotificationTemplate('{{message', {}), null);
});

test('notification template validation enforces UTF-8 limits and matching variables', () => {
    assert.equal(validNotificationTemplate({}), true);
    assert.equal(validNotificationTemplate({ title: '{{emoji}} {{title}}', body: '{{message}}\n{{time}}' }), true);
    for (const template of [{ title: 'a\nb' }, { body: '{{password}}' }, { body: '{{site' }, { body: '中'.repeat(3000) }, { title: '中'.repeat(200) }, { body: '\0' }]) {
        assert.equal(validNotificationTemplate(template), false);
    }
});
