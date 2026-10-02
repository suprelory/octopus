# 站点签到能力、收益与多渠道通知

## 独立签到页面

导航栏的“签到”页面集中管理签到站点、账号、全量与批量签到、记录和收益。“站点”页面管理订阅站的账号同步与托管渠道。

1. 在“签到”页面新增签到站点，填写实际签到服务的地址和平台；地址可以与订阅站不同。
2. 使用平台内置签到时，关联订阅站，再在签到账号中选择该站的订阅账号。地址为空或未修改时，选择关联站点会填入其平台和地址；执行前会校验一致性，并实时读取订阅账号的 Access Token、用户 ID 或登录凭据。刷新得到的 token 写回订阅账号，签到记录和时间窗口仍属于签到账号。
3. 使用外部签到站时，启用自定义 HTTP 签到，填写实际签到地址、方法和站内路径，并在签到账号中填写该签到站的完整 Cookie。需要用余额差计算奖励时，先关联订阅站，再在签到账号的“余额账号（可选）”中选择实际接收奖励的订阅账号，沿用其余额查询能力。按需开启自动签到和随机延迟。
4. 保存后展开账号，点击“签到并验证”或“立即签到”；也可以通过“手动签到 URL”打开浏览器中的签到页面。

签到账号不执行分组、Key 和模型同步，也不生成托管渠道。签到请求的代理和时间窗口沿用签到站及签到账号的设置；自定义 HTTP 签到的关联余额查询使用订阅站及订阅账号自己的地址、平台、代理和凭据。删除关联订阅站或账号后，平台签到会提示重新选择账号，不再使用旧凭据；外部 Cookie 签到可继续独立执行。需要暂停签到时，在签到页面停用对应站点或账号。

升级时，已有签到配置会迁入独立签到站，保留账号凭据、开关、时区、执行时间和历史记录，同时关闭原订阅账号的签到，避免重复执行。迁移保持原自动请求地址，并保留原手动签到链接；需要改为外站签到时，在新签到站中修改地址及账号凭据。恢复旧备份和导入 ALL-API-Hub / metapi 的签到配置也会进行分离，重复导入不会覆盖独立账号已修改的设置。

已有平台签到账号在保留迁移来源且地址、平台一致时，会自动绑定原订阅账号。原本独立且未关联订阅站的平台签到账号仍兼容原凭据。自定义 HTTP 签到会迁移旧 Cookie 请求头或 Cookie 格式的 access token；只有 Bearer token 的旧配置需要补填签到站 Cookie。显式修改过的 Cookie 或账号绑定不会被重复迁移覆盖，备份恢复会重映射账号绑定。

账号接口支持 `credential_type: "linked_account"` 和 `linked_account_id`，或 `credential_type: "cookie"` 和 `cookie`。Cookie 账号也可保存可选的 `linked_account_id`，该账号必须属于签到站关联的订阅站，传 `null` 可取消余额账号关联。Cookie 必须为不超过 16 KiB 的单行值，自动放入 `Cookie` 请求头并覆盖旧的共享 Cookie Header。自定义 HTTP 的请求体及专属 Header 支持 `{{cookie}}`、`{{username}}`；旧平台凭据占位符不再注入 token、密码、API Key 或用户 ID。Cookie 和关联账号凭据均从签到日志及错误详情中脱敏。

自动生成的签到站沿用订阅站名称，不添加“· 签到”。两类站点可同名，同类站点仍要求名称唯一；签到站内遇到重名时追加数字序号。升级会修正仍使用旧自动名称的关联签到站，保留自定义名称。

接口沿用 `/api/v1/site`：创建签到站时指定 `kind: "checkin"`，可选的 `linked_site_id` 指向订阅站，传 `null` 可取消关联；省略 `kind` 时创建订阅站。站点类型创建后保持不变。列表通过 `kind` 区分两类记录，单账号、批量及定时签到仅执行签到站账号。

“全量签到”通过 `POST /api/v1/site/checkin-all` 创建后台任务；勾选站点后的“批量签到”通过 `POST /api/v1/site/batch` 提交 `{"action":"checkin","ids":[1,2]}`，返回同样的任务对象，并在 `site_ids` 中保留所选范围。两者均只执行启用站点中已启用且开启自动签到、站点签到能力可用的账号，手动触发时不等待账号的下次执行时间。任务进度和日志沿用签到批次查询接口。同一范围的重复提交复用正在执行的任务；存在其他范围的活动任务时返回 409，需等待其完成。无效、已归档或订阅站的 ID 会被拒绝，不会扩大为全量任务。

所有执行入口都会在读取最新账号状态后检查签到站点及账号是否停用、站点是否归档；不满足条件时记录跳过，不请求上游，也不改写上次实际执行结果或增加失败次数。

## 收益与余额

“签到 → 签到收益”展示今日、近 7 天、近 30 天和累计奖励，并支持按站点、账号筛选及站点明细。收益范围由该面板的筛选器决定，保留已删除站点和账号的历史日志。日期按浏览器时区计算，近 7 / 30 天包含今天。

只汇总成功签到日志中的有效、非负数字奖励；重复签到不会重复累计。缺失奖励和无效数字分别计数。默认提取的上游奖励保留原单位，不进行汇率或额度换算；自定义提取代码应返回 USD 金额，余额差补记也使用账户余额的 USD 单位，均保留最多 6 位小数。不同来源或站点的总和不一定代表同一种货币。账户的 `today_income` 仍来自上游余额或收入记录，与签到奖励统计分别展示。

平台签到在取得执行凭据后、发出签到请求前，会尝试读取一次最新余额（最多等待 10 秒，不读取收入日志、不覆盖缓存余额）。如果本次签到成功且未返回 `data.reward`，会在签到后余额刷新成功时，用“签到后余额 − 签到前余额”补记本次奖励，并用于签到记录、收益统计和通知。已有奖励（包括 0）优先保留；“今日已签到”、任一次余额读取失败或差额为负时不补记，差额为零时记录 0。余额差只反映两次查询之间的净变化，同期消费或充值可能影响金额。

平台签到成功或上游返回“今日已签到”后，会尝试刷新账户余额、累计用量和今日收入；绑定订阅账号时，前后余额均读取该订阅账号，更新订阅账号余额，并在签到账号展示该余额。刷新复用现有的平台接口、凭据及签到代理设置，最多等待 10 秒。接口失败、缺失字段或无效数字会保留对应旧值；真实的零余额会正常保存。刷新发生在签到日志提交后，仅可补齐缺失奖励，不改变签到状态；刷新或补记失败均不会使已成功的签到变为失败。

自定义 HTTP 签到选择“余额账号”后也遵循上述余额差规则，查询实际关联订阅站的接口。签到始终使用外站 Cookie，订阅站登录得到的新 token 只保存到订阅账号；余额账号失效、登录失败或余额查询失败均不影响外站签到。未选择余额账号时跳过前后余额查询，仍可直接提取响应奖励。余额查询复用 NewAPI、AnyRouter、OneAPI、OneHub、DoneHub 和 Sub2API 的现有实现，无需配置新的余额请求。

## 自定义 HTTP 奖励提取代码

在自定义 HTTP 签到配置中，可选填“奖励提取代码（JavaScript）”，保存为站点的 `checkin_reward_extractor`。填写函数体，通过 `response` 读取签到返回的 JSON，返回非负、有限的数字或数字字符串；返回 `null` / `undefined` 表示未提取到奖励。

优先级为：**自定义代码结果 → 默认 `data.reward` → 关联订阅账号签到前后的余额差**。有效的 0 也会优先保留。代码报错、超时或返回无效值时继续尝试后续规则，签到结果保持原有判定。失败或重复签到不执行提取代码、不记录奖励；非 JSON 响应跳过代码提取。

通用模板：

```javascript
return response.data?.reward ?? response.reward ?? null;
```

New API 模板（`quota_awarded` 按 500000 quota / USD 换算）：

```javascript
const quota = response.data?.quota_awarded;
return quota == null ? null : Number(quota) / 500000;
```

其他格式可自行修改字段路径，例如 `return response.data?.amount ?? null;`。模板的换算比例需与实际站点单位一致。

“测试提取”仅运行填写的代码和 JSON 响应样例，不发送签到或余额请求，也不保存配置。对应接口为管理员认证的 `POST /api/v1/site/checkin-reward/test`：请求 `{"code":"return response.data.reward;","response":{"data":{"reward":0.1}}}`，返回 `{"reward":"0.1","found":true}`；无结果时 `found` 为 `false`。

提取器只支持同步 JavaScript，不提供网络、文件、账号凭据或模块导入能力。每次使用独立的 WASM 运行环境，限制 16 KiB 代码、1 MiB JSON 输入、16 MiB JavaScript 内存和 250 毫秒执行时间；排队与首次初始化另有 5 秒上限。配置随站点备份保存，恢复时也会重映射余额账号关联。

## 通知设置

在“设置 → 连接与任务 → 统一通知渠道”选择并配置通知渠道，点击“保存通知渠道”。多个渠道可以同时接收同一条结果；切换渠道不会丢失当前填写的内容。每个渠道的“测试此渠道”使用当前填写的配置，只发送一条测试通知，不保存配置，也不占用签到通知的冷却时间。清空渠道后保存即可停用。

支持与 meta-gateway 相同的五种渠道：

| 渠道 | 配置 | 发送方式 |
| --- | --- | --- |
| Webhook | HTTP / HTTPS 地址 | POST JSON，保留原有签到事件字段；Markdown 正文原样传递，并附 `format: "markdown"` |
| Bark | 含设备密钥的完整推送 URL，支持自建服务 | POST 标题、正文及 Octopus 分组；Markdown 转为纯文本 |
| Server酱 | SendKey | Server酱 Turbo 接口，直接传递 Markdown 正文 |
| Telegram | Bot Token、Chat ID | `sendMessage`，支持纯文本及由 Markdown 转换的 HTML，支持用户、群组和频道 |
| SMTP 邮件 | 服务器、端口、发件人、收件人，可选用户名和密码 | 支持 STARTTLS、隐式 TLS；Markdown 生成 HTML 和纯文本双版本；无需认证的邮件中继可显式选择不加密 |

SMTP 未填写端口时默认使用 587，显式选择 TLS 时默认使用 465；自动安全模式在 465 端口使用 TLS，其他端口要求 STARTTLS。多个收件人使用逗号分隔，最多 20 个。所有网络请求使用服务端网络连接。

每个渠道可以单独设置标题、正文模板及“纯文本 / Markdown”正文格式；旧配置未指定格式时沿用纯文本发送逻辑。标题始终为纯文本，模板留空或替换后全空时沿用事件原内容。支持 `{{site}}`、`{{account}}`、`{{message}}`、`{{detail}}`、`{{reward}}`、`{{balance}}` 等编辑器列出的变量，变量只替换一次，Markdown 模式会转义变量中的格式字符。编辑器使用示例数据预览，恢复默认会清除该渠道的模板和格式。

Markdown 支持标题、强调、链接、代码、列表、表格等常见语法，邮件与 Telegram 转换时禁用原始 HTML。Telegram 仅输出其支持的 HTML 标签，列表、表格和图片替代文字转为文字，并按显示文字的 UTF-16 长度截断长消息，保留完整标签。Bark 转为纯文本并保留链接地址；Webhook 的 Markdown 渲染由接收端决定。修改模板和格式不会重置通知冷却。

然后在“签到结果通知”开启通知，并选择需要的结果。此处开关、冷却时间和阈值自动保存，默认仍只通知定时签到异常。

| 设置 | 默认值 | 行为 |
| --- | --- | --- |
| `notification_channels` | 空 | 统一渠道 JSON 配置；未配置时沿用旧签到 Webhook，显式保存 `{}` 表示全部停用 |
| `checkin_notify_enabled` | `false` | 开启签到结果通知 |
| `checkin_notify_success_enabled` | `false` | 包含成功及“今日已签到”的结果 |
| `checkin_notify_manual_enabled` | `false` | 包含单账号、批量和全量手动签到 |
| `checkin_notify_webhook_url` | 空 | 兼容旧版本的 Webhook；保存统一渠道配置后由新配置决定发送目标 |
| `checkin_notify_cooldown_seconds` | `3600` | 每个渠道分别按账号、事件和原因冷却，范围 0–604800 秒；0 不冷却 |
| `checkin_low_balance_threshold` | `0` | 签到成功后，刷新所得余额低于该 USD 阈值时提醒；0 关闭低余额提醒 |

通知只在签到结果写入日志后发送，跳过的任务不通知。成功结果可包含奖励和本次刷新所得的余额。低余额提醒只使用本次成功刷新并保存的余额，不使用过期余额；触发低余额提醒时只发送这一条提醒，不再重复发送成功通知。自定义 HTTP 签到选择余额账号且本次刷新成功时，同样可包含余额并触发低余额提醒。

Webhook 接收端支持 `site_checkin_failed`、`site_checkin_low_balance` 和 `site_checkin_success` 三个事件。原有字段保持兼容，并新增标题、等级、触发来源和可选奖励。例如：

```json
{
  "event": "site_checkin_failed",
  "level": "error",
  "title": "签到失败",
  "source": "scheduled",
  "log_id": "1790507233018",
  "site_id": 1,
  "account_id": 11,
  "site_name": "Example site",
  "account_name": "Primary account",
  "status": "failed",
  "reason": "upstream_http_error",
  "message": "HTTP 401 Unauthorized",
  "failure_count": 3,
  "occurred_at": "2026-09-27T11:00:00Z"
}
```

低余额事件额外包含 `balance` 和 `threshold`，`reason` 为 `low_balance`、`level` 为 `warning`。成功事件的 `level` 为 `info`，`reason` 为 `checked_in` 或 `already_checked_in`，有奖励时包含 `reward`。所有渠道复用执行日志中的脱敏文本，不发送签到凭据、站点 URL、自定义请求头或上游原始响应。

Webhook 接收端应返回 2xx；Bark、Server酱和 Telegram 还会检查业务响应是否成功。所有 HTTP 渠道均不跟随重定向，发送失败日志和测试接口不会返回通知渠道的凭据或原始响应。

统一渠道配置示例（按需保留字段，空字段不启用对应渠道）：

```json
{
  "webhook_url": "https://example.com/notify",
  "bark_url": "https://api.day.app/device-key",
  "serverchan_key": "SCT...",
  "telegram_bot_token": "123456:bot-token",
  "telegram_chat_id": "-1001234567890",
  "smtp_host": "smtp.example.com",
  "smtp_port": 587,
  "smtp_tls": "starttls",
  "smtp_user": "sender@example.com",
  "smtp_password": "app-password",
  "smtp_from": "Octopus <sender@example.com>",
  "smtp_to": "admin@example.com",
  "templates": {
    "telegram": { "format": "markdown", "body": "**{{site}}**\n账号：{{account}}\n{{message}}" },
    "smtp": { "format": "markdown", "title": "{{emoji}} {{title}}", "body": "## {{event}}\n\n{{message}}" }
  }
}
```

通知按渠道异步发送，每次最多等待 10 秒（包括 SMTP 会话），最多同时发送 4 条，总待发送数量上限为 128。队列已满时丢弃新通知并记录服务日志。发送失败不会改变签到结果，也不会占用该渠道的冷却期；后续符合通知条件的签到可以再次发送，已成功的其他渠道仍保持冷却。更换渠道地址或凭据只重置该渠道的冷却。冷却状态保存在当前进程内，重启会清空，多个服务实例独立计时，最多保留 10000 个未过期的通知键。

渠道配置和通知开关沿用现有设置接口与备份，无需新增数据库表。`POST /api/v1/setting/notification/test` 需要管理员认证，请求为 `{"channel":"webhook","config":{"webhook_url":"https://example.com/notify"}}`；每次只测试指定渠道。

## 统计接口

`GET /api/v1/site/checkin-stats` 需要管理员登录认证，支持 `site_id`、`account_id`、`timezone`（IANA 时区）、`from` 和 `until`（RFC3339 时间）。`from` 为包含边界，`until` 为排除边界；未指定时区时使用服务所在时区。

返回 `today_reward`、`recent_7_days_reward`、`recent_30_days_reward`、`total_reward`，执行状态计数、`invalid_reward_count`、`unknown_reward_count`、实际使用的 `timezone` 和 `by_site` 明细。统计按日志 ID 分页扫描，不会一次加载所有日志。站点收益面板每分钟更新，单次签到或批量签到完成后也会刷新。

统计使用现有签到日志，余额保存到现有账户记录，通知设置纳入现有设置备份；无需新增数据库表。

## 平台默认值、站点配置与接口验证

“签到 → 编辑签到站点 → 站点签到能力”支持三个模式：

- **平台默认（auto）**：优先采用本站当前请求配置的接口验证结果；尚未验证时，自定义 HTTP 签到或平台默认值决定是否启用。
- **启用（enabled）**：显式启用本站的内置或自定义接口，不受平台默认值及历史验证结果影响。
- **禁用（disabled）**：停止本站的手动、批量和定时签到，也覆盖自定义 HTTP 配置。

| 平台 | 初始默认值 | 内置请求 |
| --- | --- | --- |
| NewAPI、OneAPI、OneHub | 启用 | POST /api/user/checkin |
| AnyRouter | 启用 | 内置 Bearer / Cookie 签到及 sign_in 回退 |
| DoneHub | 关闭 | 可手动验证或显式启用 POST /api/user/checkin |
| Sub2API、API | 关闭 | 需配置自定义 HTTP 接口 |

默认关闭不代表该平台的所有站点都不支持签到。前端通过 `GET /api/v1/site/checkin-capabilities` 读取默认值，通过站点响应中的 `checkin_capability` 读取本站实际策略，不再维护平台黑名单。ALL-API-Hub 和 metapi 导入保留显式的账号自动签到偏好；未提供该偏好时使用平台默认值。账号偏好和站点能力共同决定是否进入批量、定时任务。

保存配置后，展开账号并点击“签到并验证”。它调用现有的 `POST /api/v1/site/account/checkin/:id`，**会实际执行签到**，并沿用签到账号凭据、请求头和代理设置；即使账号关闭自动签到或平台默认关闭，也可手动验证。明确禁用本站时不可执行。已验证支持的账号显示“立即签到”，再次执行仍会更新验证结果。

成功签到或明确的“今日已签到”响应确认支持。明确的路由不存在或“签到功能未启用”等站点级响应确认当前接口不可用；AnyRouter 会考虑回退接口后再判断。401 / 403、429、服务错误、网络失败、账号不存在或账号被禁用均不代表站点不支持。自定义 HTTP 需要 JSON 中的明确 `success` 成功标记或可识别的“已签到”提示；普通 2xx、登录 HTML、空响应及无成功标记的 JSON 均记录为 `failed / unconfirmed_checkin`，提示检查签到接口和 Cookie，并进入正常失败退避。无法确认的响应不运行奖励提取、不补记奖励、不更新上次成功时间，也不覆盖已有的明确签到能力证据。

自定义 HTTP 签到的响应提示包含“今日已签到”时，优先记录为 `success / already_checked_in`，不受 HTTP 状态码或 `success: false` 影响，也不累计返回的奖励。支持 JSON 的 `message`、`msg`、`error.message` 提示及非 JSON 响应中的文本；非 JSON 响应仅记录“今日已签到”，不保存原始响应正文。

验证只影响当前站点。在默认模式下，确认支持会开启该站点签到，确认不可用会停止自动任务并清空待执行时间；仍可手动重试验证。无法判断的响应不会覆盖以前的明确证据。站点响应及单账号签到结果包含是否启用、是否可验证、策略来源、支持状态和最后验证时间。

证据绑定平台、站点 URL、自定义签到方法 / 路径 / 请求体 / 请求头、站点请求头和代理选择。修改这些配置后旧证据失效，修改前已发出的请求也不能为新配置写入验证结果。模式或时间窗口调整不改变证据。配置和证据随站点备份保存；新建和更新接口不接受客户端伪造的验证结果。
