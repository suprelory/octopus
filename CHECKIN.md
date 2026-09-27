# 站点签到收益与异常通知

“站点概览 → 签到收益”展示今日、近 7 天、近 30 天和累计奖励，并支持按站点、账号筛选及站点明细。收益范围由该面板的筛选器决定，保留已删除站点和账号的历史日志。日期按浏览器时区计算，近 7 / 30 天包含今天。

只汇总成功签到日志中的有效、非负数字奖励；重复签到不会重复累计。缺失奖励和无效数字分别计数。奖励保留上游返回的单位，不进行汇率或额度换算，不同站点的总和不一定代表同一种货币。账户的 `today_income` 仍来自上游余额或收入记录，与签到奖励统计分别展示。

签到成功或上游返回“今日已签到”后，会尝试刷新账户余额、累计用量和今日收入。刷新复用现有的平台接口、凭据及代理设置，最多等待 10 秒。接口失败、缺失字段或无效数字会保留对应旧值；真实的零余额会正常保存。刷新发生在签到日志提交后，不会改写签到结果。未实现余额接口的平台会跳过刷新。

## 通知设置

在“设置 → 连接与任务 → 签到异常通知”填写 Webhook 地址并启用，设置自动保存。通知默认关闭。

| 设置 | 默认值 | 行为 |
| --- | --- | --- |
| `checkin_notify_enabled` | `false` | 开启定时签到异常通知 |
| `checkin_notify_webhook_url` | 空 | 接收 JSON POST 的 HTTP / HTTPS 地址 |
| `checkin_notify_cooldown_seconds` | `3600` | 同一账号、事件和异常原因的通知冷却时间，范围 0–604800 秒；0 不冷却 |
| `checkin_low_balance_threshold` | `0` | 签到成功后，刷新所得余额低于该 USD 阈值时提醒；0 关闭低余额提醒 |

仅定时签到触发通知。单账号手动签到和手动全量签到的结果仍通过页面与执行日志查看。失败通知在日志保存后发送；低余额通知只使用本次成功刷新并保存的余额，不使用过期余额。

接收端支持两个事件：`site_checkin_failed` 和 `site_checkin_low_balance`。例如：

```json
{
  "event": "site_checkin_failed",
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

低余额事件额外包含 `balance` 和 `threshold`，`reason` 为 `low_balance`。消息复用执行日志中的脱敏文本，不发送凭据、站点 URL、自定义请求头或上游原始响应。接收端应返回 2xx；发送端不跟随重定向。

通知异步发送，每次最多等待 10 秒，最多同时发送 4 条，总待发送数量上限为 128。队列已满时丢弃新通知并记录服务日志。发送失败不会改变签到结果，也不会占用冷却期；后续定时执行仍出现异常时可以再次发送。冷却状态保存在当前进程内，重启会清空，多个服务实例独立计时，最多保留 10000 个未过期的通知键。

## 统计接口

`GET /api/v1/site/checkin-stats` 需要管理员登录认证，支持 `site_id`、`account_id`、`timezone`（IANA 时区）、`from` 和 `until`（RFC3339 时间）。`from` 为包含边界，`until` 为排除边界；未指定时区时使用服务所在时区。

返回 `today_reward`、`recent_7_days_reward`、`recent_30_days_reward`、`total_reward`，执行状态计数、`invalid_reward_count`、`unknown_reward_count`、实际使用的 `timezone` 和 `by_site` 明细。统计按日志 ID 分页扫描，不会一次加载所有日志。站点收益面板每分钟更新，单次签到或批量签到完成后也会刷新。

统计使用现有签到日志，余额保存到现有账户记录，通知设置纳入现有设置备份；无需新增数据库表。
