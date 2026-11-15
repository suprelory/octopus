# 独立签到模块

本模块管理签到站点、账号、任务、记录和收益。签到数据使用独立的表、接口、凭据与执行队列；没有渠道、分组、订阅站或订阅账号引用，也不会生成渠道、同步模型或读取渠道 Key。创建空数据库后即可使用。

## 已实现的范围

- 独立站点与多账号管理，站点/账号启停、账号自动签到开关。
- 网站 Cookie、网站 Access Token（Bearer）和平台支持的用户名/密码凭据，可配置或发现 New-Api-User 用户 ID。
- 独立的平台适配层，统一描述凭据、执行方式、原生状态查询和可编辑能力。
- New API 原生签到与月度记录确认；One API、OneHub、DoneHub 和自定义 HTTP 的可编辑请求规则。
- Sub2API 类型、自定义签到接口和独立账号的 Access Token / Refresh Token 自动续期。
- AnyRouter 的认证、受控接口回退和会话复用；AgentRouter 的登录即签到与只读日志确认。
- 单账号、所选站点、全量手动签到，以及按站点时区和时间窗口自动签到。
- 可选的提交前状态检查、提交后确认、跨日结果查询。
- 持久化后台任务、取消等待项、断线进度恢复、SSE 更新和轮询兜底。
- 每日记录、请求阶段记录、按站点/账号筛选的收益统计。
- 加密凭据备份、旧版数据库备份兼容、恢复后的任务隔离。
- 共享代理池、站点与账号固定绑定、系统代理及账号继承展示。
- New API / Sub2API 余额查询、只读账号检测、最近检测结果与请求阶段。
- 自定义多平台通知、消息模板、事件/站点筛选、低余额阈值、冷却、测试发送与持久化投递记录。
- 简体中文、繁体中文、英文管理界面。

当前不包含余额差估算、JavaScript/WASM 提取器和验证码处理。Refresh Token 自动续期用于 Sub2API 的 Bearer 账号；账号密码登录用于 AnyRouter 和 AgentRouter。

## 使用方式

1. 在导航栏打开“签到”，新增站点，填写实际签到服务的地址、平台、时区和执行窗口。
2. 按平台校对请求和状态确认方式。New API 默认使用内置月度记录；AnyRouter 和 AgentRouter 的协议由适配器管理；其他平台的实际接口需按部署校对。
3. 新增账号，填写表单列出的凭据类型。网站 API Key 与网站登录令牌用途不同，请使用上游管理接口实际接受的凭据。AgentRouter 必须提供用户名或邮箱及密码。
4. 按需开启自动签到。默认仅启用站点和账号，自动签到默认关闭。
5. New API / Sub2API 账号可先使用“刷新余额”或“只读检测”，通过“最近结果”查看登录状态和请求阶段。
6. 使用“签到”“批量签到”或“全量签到”执行，打开任务查看进度，通过记录查看结果与请求阶段。
7. 打开签到工具栏的“通知”，添加通知目标、保存连接信息并测试发送，再按需开启自动通知。新目标默认关闭自动通知，显式测试发送不受此开关限制。

手动签到选择启用站点中的启用账号，不要求账号开启自动签到。自动提交要求账号开启自动签到，并处于站点的执行窗口内。停用站点或账号会停止后续网络操作；已经开始的请求按本轮超时收尾。

编辑站点或账号需要其当前 revision；替换凭据还需要 credential_version。正在执行中的账号及其站点不能修改或删除，冲突返回 HTTP 409。账号编辑时留空凭据表示保留原值；修改凭据类型或用户 ID 时需要重新填写凭据。用户名和密码必须一起替换，替换会清除缓存会话。平台切换会拒绝不兼容的已有账号凭据。

删除会清除账号凭据、关闭自动签到，并保留站点/账号的删除标记和历史快照。收益与历史记录仍可查询。

## 请求与判定规则

站点地址可以包含路径前缀，请求路径追加在该前缀后。请求支持 GET 或 POST；路径必须位于当前站点，拒绝跨站地址和相对路径跳转。不自动跟随重定向，也不因连接复用而重放签到请求。签到自定义代理地址不接受用户名、密码、查询参数或片段；带认证的代理可通过[代理池](PROXY.md)配置。自定义请求头拒绝 Cookie、Authorization、API Key 等身份字段；JSON 请求体拒绝常见凭据字段。只在账号凭据中配置网站 Cookie、Bearer Token 或登录密码，避免配置查询和备份携带明文秘密。

请求体为 JSON，最大 64 KiB。请求头最多 32 个，Cookie、Authorization、New-Api-User 等身份头通过账号注入。请求配置支持：

| 位置 | 占位符 |
| --- | --- |
| 请求路径 | {{checkin_date}} |
| JSON 字符串值、业务请求头 | {{checkin_date}}、{{user_id}} |

checkin_date 是站点时区下本条记录固定的 YYYY-MM-DD 日期。user_id 在 JSON 字符串中进行转义，不向模板暴露 Cookie、Access Token 或其他账号凭据。状态查询必须配置为只读接口。

未配置状态查询时，默认要求 2xx JSON 响应中的 success 为布尔值 true；Sub2API 默认使用数字 code = 0。可以通过字段路径与 JSON 标量配置成功/重复签到规则，需根据实际签到接口校对。普通 HTML 页面、登录页、验证页和不匹配的响应不会被当作成功。明确的“今日已签到”响应记录为已签到，不增加收益。

字段路径支持点分对象和数组索引，例如 data.records.0.checked_in。值同时匹配类型和值，true 与字符串 "true" 不等价。状态查询还要求：

- HTTP 2xx，合法 JSON；success 存在时必须是布尔值 true。
- “已签到”与“未签到”条件恰好匹配一个。
- 配置了日期字段时，返回值必须等于本条记录的目标签到日期。

默认状态配置只是编辑起点，需根据站点真实响应校对：

~~~json
{
  "request": {
    "method": "GET",
    "path": "/api/checkin/status?date={{checkin_date}}",
    "body": "",
    "headers": []
  },
  "checked": { "path": "data.checked_in", "value": true },
  "unchecked": { "path": "data.checked_in", "value": false },
  "date_path": "data.date"
}
~~~

“测试响应样例”只解析提供的 JSON，不请求上游、不提交签到、不保存站点配置。

## 避免重复提交与恢复

每个账号、每个站点业务日期只存在一条 CheckinRun，数据库使用唯一索引约束。多个手动入口或自动调度共用该记录；已经成功的记录不再提交，也不会重复累计奖励。

配置状态接口时，执行流程如下：

1. 前置查询明确显示未签到才继续；已签到直接结束，无法确认则保留待确认记录。
2. 在数据库事务中保存提交意图、凭据/配置指纹和确认期限。
3. 只有提交意图保存成功才发送签到请求。
4. 按状态接口确认最终结果，更新同一条每日记录。
5. 提交后请求超时、断连或进程退出，恢复时只查询状态，不盲目重发。

SQLite 的提交意图事务临时使用 FULL 同步并占用独立连接，提交完成后恢复连接原同步设置。其他数据库使用其事务持久性配置。

没有状态接口且提交结果不明时，记录为“需人工处理”。明确返回 404/405，或结构明确的凭据拒绝响应，才可能允许再次提交；其他不明确的失败保持提交标记。已验证当前配置的接口不可用时，自动调度暂停尝试；修改站点配置会清除能力结论，手动执行也可以重新验证。

后台扫描每分钟运行：

- 已提交的待确认记录继续查询，直至确认、停用或期限到达。关闭“自动签到”只停止新的自动提交；若要停止后续确认，请停用账号。
- 尚未提交的待确认记录，只会在账号开启自动签到且位于执行窗口内时继续执行。
- 超过确认期限后转为“需人工处理”，仍可手动查询结果。
- 配置或凭据在提交后变更，自动确认停止；用户核对后可手动继续确认，此时不认领新的签到奖励。
- 跨日确认需要日期字段。只有“今日状态”的接口不能用于确认前一天的记录。

时间窗口必须位于同一天，可使用 24:00 作为结束时间。随机延迟不会越过窗口；计算后的下次时间写入数据库，重启不会重新抽取。自然日和夏令时按站点 IANA 时区计算。

任务取消只取消等待项，已执行中的请求完成本轮收尾。任务计数表示该任务当轮的执行结果；后续确认以最新签到记录为准。

## 收益

通用规则默认提取 data.reward，默认单位 points、倍率 1。New API 新配置默认提取 data.quota_awarded、单位 quota，原生状态查询会选取目标日期的记录。AgentRouter 不从余额或登录响应推断收益，成功时奖励仍可为未知。零奖励有效；缺失和非法奖励分别统计，不转成零。金额使用整数微单位存储，最多 6 位小数，按十进制四舍五入，超出 int64 范围时标记无效。API 中微单位使用字符串，避免浏览器浮点数损失精度。

仅“本次签到成功”的有效奖励进入统计。“已经签到”、未确认或失败记录不累计收益。使用状态确认时，必须有本轮前置未签到证据、提交标记和后置已签到证据，才计为新增签到。

USD、CNY、EUR、GBP、JPY、HKD 分别按同币种跨站汇总；其他单位按站点隔离。今日、近 7 天、近 30 天以每条记录的站点业务日期计算，窗口包含今天。修改站点当前时区不会改写历史记录的时区。

New API 可自行修改奖励路径及单位；例如确认站点采用 500000 quota/USD 时，设置单位 USD、倍率 0.000002。系统不会自行推断额度与货币之间的换算。已有明确配置的奖励路径和单位保留原值。

采用提交响应作为奖励来源时，跨轮恢复无法重新读取原响应，可能出现“签到成功、奖励未知”。需要恢复后提取奖励的站点，优先配置状态响应为奖励来源。原始响应正文不保存在数据库中。

## 余额查询与只读账号检测

账号列表中的“刷新余额”读取登录状态及余额；“只读检测”还会在当前配置有内置只读协议时检查签到功能和业务日状态。两个操作都要求站点和账号启用，使用账号的加密凭据、有效代理快照和站点 User-Agent，遵守现有并发上限与同站间隔。

| 平台 | 账号只读接口 | 余额字段与单位 | 累计消耗 | 签到检测 |
| --- | --- | --- | --- | --- |
| New API | GET /api/user/self | data.quota，quota | data.used_quota，同为 quota | 使用默认原生状态配置时，GET /api/user/checkin?month=YYYY-MM |
| Sub2API | GET /api/v1/auth/me | data.balance，USD | 该接口不提供，显示未知 | 没有统一内置只读签到协议，显示未检测 |

协议依据为提供的 New API `controller/user.go`、签到路由与月度记录，以及 Sub2API `GetCurrentUser`、用户 DTO 和 API 合约测试。余额接口固定由适配实现决定，不接受用户指定路径或规则。自定义签到/状态请求不会在检测中执行；AnyRouter、AgentRouter 及其他尚未接入的平台通过能力字段明确标记为不可用，不尝试登录或猜测接口。

读取账号必须同时满足 HTTP 成功、平台成功标记及可验证的正整数账号 ID。多个成功标记互相矛盾、HTML/验证页、账号停用、身份 ID 不一致等结果不会被当成有效账号。已填写用户 ID 时必须与响应一致；Sub2API Token 带有 user_id 时也核对其一致性。已验证的 New API ID 可用于本轮状态查询，不覆盖手工配置的凭据。

余额与消耗均使用整数微单位，API 返回字符串以保留浏览器精度，最多 6 位小数；按十进制绝对值四舍五入后恢复符号，超出 int64 范围标记无效。余额允许负数，累计消耗不能为负。零、缺失和非法数值分别记录为 known、unknown、invalid。New API 保留 quota，不套用奖励配置的单位/倍率；Sub2API 不从充值总额或余额差推算累计消费。

账号的 balance 保存最近一次通过身份验证的余额响应及原时间；查询发生网络、鉴权或协议错误时保留上次快照，界面标注“上次已知余额”。有效账号响应缺少余额时，新快照显示未知，不继续展示旧值为当前余额。inspection 保存最近一轮的类型、登录状态、固定原因、业务日期、结果及最多 8 条脱敏请求阶段。替换账号凭据或更换站点地址/平台会清除旧余额；修改站点配置或账号代理绑定会清除最近检测结果。仅保留最近一轮检测阶段，不增加每日签到记录或历史任务。

只读操作与签到、令牌续期共用账号租约，运行期间不能编辑同账号或其站点。Sub2API 按现有规则提前续期，或在明确可恢复的 401 后刷新并重查一次；单轮最多一次成功轮换。网络或保存失败仍保留刷新意图和加密令牌重试结果。AgentRouter 登录不会被调用，检测不会更新每日签到状态、收益、提交标记、调度或站点自动签到支持结论。进程中断后的检测在租约到期时标记为已中断，不自动重放请求。

## 自定义多平台通知

协议与交互参考 `metapi-go/service/notify`、`octopus-plus/internal/notify` 和其签到通知实现，通知目标独立于渠道及订阅业务。最多保存 20 个未删除目标，同一平台可以配置多个目标。

| 平台 | 连接配置 | 成功判定 |
| --- | --- | --- |
| Webhook | 完整 HTTP(S) 地址，可选自定义请求头和 JSON 消息体 | HTTP 2xx，支持 204 |
| Telegram | Bot Token、Chat ID，可选自托管 API 基础地址 | JSON `ok: true` |
| Bark | 含设备 Key 的完整推送地址 | JSON 数字 `code: 200` |
| Server酱 Turbo | SendKey，使用 `sctapi.ftqq.com` | JSON 数字 `code: 0` |
| 邮件 SMTP | 主机、端口、TLS 模式、发件人、收件人及可选认证 | SMTP DATA 被接受 |
| 飞书 | 完整机器人 Webhook，可选签名密钥 | JSON 数字 `code: 0` |
| 钉钉 | 完整机器人 Webhook，可选签名密钥 | JSON 数字 `errcode: 0` |
| 企业微信 | 完整机器人 Webhook | JSON 数字 `errcode: 0` |
| ntfy | 主题、基础地址、优先级及可选访问令牌 | JSON 消息 ID、主题及 message 事件 |

飞书使用秒级时间戳签名，钉钉使用毫秒级时间戳签名。所有 HTTP 发送禁用重定向并限制超时和响应大小；平台响应缺少成功标记、类型不符或返回 HTML 时，不标记成功。SMTP 支持 STARTTLS、隐式 TLS，以及不带认证的明文本地中继；提供账号密码时必须使用 TLS，验证服务端证书，最多 20 个收件人。DATA 已被接受后，QUIT 失败不会触发重发。

每个目标分别选择以下事件：

- 签到成功（success）、今日已签到（already_checked_in）、签到失败（failed）。
- 待确认或需要关注（attention）：pending / manual_required，包括自动确认超时。重复查询相同记录、相同状态不会重复通知。
- 低余额（low_balance）：仅在本次余额查询或只读检测保存了新的有效余额后比较。余额必须严格小于阈值，并且单位同为 USD 或 quota；不转换单位，不使用历史缓存，不将未知或无效余额当作零。零和负余额可正常参与比较，金额及阈值均使用整数微单位。

“包含手动签到、确认和余额查询”按本轮任务来源判断。当前余额查询只由手动操作发起，需要勾选此项才能收到低余额通知；通知本身不会额外查询余额或登录账号。站点范围须显式选择全部或指定站点，指定范围不能为空。

冷却按目标连接版本、账号、事件分别计算，取最近一次排队或投递状态更新时间；范围为 0–604800 秒，0 表示关闭。测试发送不消耗业务冷却。更换连接或代理绑定后使用新的冷却范围；更换消息模板不会清除冷却。

标题和正文使用纯文本模板，支持简体中文、繁体中文、英文默认模板。留空使用默认模板，可先用示例数据预览而不向通知平台发送请求。支持以下单次替换变量，不执行表达式，也不提供账号或通知连接凭据：

~~~text
{{event}} {{status}} {{site}} {{account}} {{site_id}} {{account_id}}
{{date}} {{timezone}} {{source}} {{reward}} {{reward_unit}}
{{balance}} {{balance_unit}} {{balance_time}} {{time}}
~~~

未知金额显示为 `?`。标题模板最多 512 字节，正文模板最多 8192 字节；展开后标题最多 2048 字节、正文最多 32768 字节，超限会在投递记录中显示模板错误。Telegram 等有更短限制的平台按 Unicode 边界截断消息并加省略号。

Webhook 默认发送 title、message、event、site、account、time 字段。自定义消息体须为 JSON 对象，可在字符串值中使用上述变量及 `{{title}}`、`{{message}}`；先替换字符串值再编码为 JSON，账号名称中的引号或换行不会改变消息结构。Webhook 的 event 变量使用稳定事件代码。自定义请求头允许 Authorization 等通知端身份头，禁止 Host、Content-Length、Content-Type 及连接控制头；这与签到站点的请求头配置相互独立。

连接地址、Token、签名密钥、自定义头部、Webhook 消息体及 SMTP 配置整体使用现有签到密钥加密，附加认证数据为 `notification:目标 UUID`。正常接口只返回“连接已保存”，不回传连接明文或密文。编辑时省略 connection 表示保留；更换平台须提供完整的新连接配置。模板和用于展示的事件快照单独存储，不写入原始上游响应或原始网络错误。

签到/检测结果先保存，再尽力写入通知队列；通知入队或发送失败不会回滚签到结果、触发签到重试或占有账号租约。结果提交与通知入队之间不是同一个事务，进程在此间中断时可能漏通知。成功入队的消息会持久化，投递状态为 queued / sending / sent / failed / uncertain / skipped，sent 表示通知服务已接受，不代表收件人已阅读。

发送队列最多 1000 条活动消息，独立并发为 2，同一目标串行；每次发送最多 20 秒，目标发送租约为 45 秒。HTTP 429 遵循 Retry-After（限制在 1 秒至 1 小时），每条消息的自动投递总次数最多 3 次。网络超时、5xx、不明确响应或 sending 状态进程中断均记为 uncertain，不自动重发。投递记录提供显式重试，使用当前目标配置；结果不明时应先核对接收端，重试可能重复送达。手动重试不重置已消耗的自动重试额度。

修改、停用或删除目标会取消该目标尚未发送的旧队列消息；发送中的目标不能修改或删除，返回 409。删除会清除连接密文，但保留投递历史。HTTP 通知可选择直连、应用系统代理或共享代理池节点；SMTP 仅直连，代理不可用时不会自动退回直连。

## 凭据、代理与备份

Cookie、Access Token、Refresh Token、用户名、密码和派生登录会话使用 AES-256-GCM 加密；账号 ID 作为附加认证数据，密文不能挪到其他账号使用。正常账号接口不返回明文或密文，只返回用户 ID、是否缓存会话、保存时间以及令牌续期元数据。请求阶段记录和 SSE 只保存固定原因及进度，不记录响应正文、请求头或凭据。

主密钥按以下优先级读取：

1. OCTOPUS_CHECKIN_MASTER_KEY：Base64 编码的 32 字节密钥。
2. OCTOPUS_CHECKIN_KEY_FILE：保存原始 32 字节密钥的文件。
3. 默认 data/checkin.key；首次使用自动生成。

路径相对于进程工作目录，使用自定义数据库目录时可以另外指定密钥路径。Docker 部署应持久化 data 目录。数据库 JSON 备份不包含主密钥，请单独备份；恢复到其他实例时先配置匹配的主密钥。密钥不匹配时拒绝导入有效账号的加密凭据。

站点支持直连、系统代理、自定义 HTTP(S)/SOCKS5/SOCKS5H 地址及代理池节点；账号还可继承站点。系统代理仅在明确选择时读取应用设置中的 `proxy_url`，签到不自动继承渠道或环境代理。每轮认证、刷新、提交和确认固定使用同一代理快照；节点停用或解析失败时不会改为直连。地址或认证变化会改变已提交记录的有效代理指纹，进入原有人工确认流程，保留提交意图与令牌轮换状态。代理池认证使用独立的 `data/proxy.key`，完整配置和备份方式见 [PROXY.md](PROXY.md)。

全库备份版本为 7，仍接受版本 6、5 和历史未标记版本。签到数据位于 checkin 字段，当前子版本为 6，仍接受子版本 1–5；包含全部八张表及删除标记。子版本 2 增加令牌续期元数据和稳定的凭据身份版本；子版本 3 增加平台能力、加密登录会话和 authenticate 请求阶段；子版本 4 增加代理池引用与运行时代理指纹；子版本 5 增加账号余额快照和最近只读检测；子版本 6 增加加密通知目标与投递记录。启动时自动添加新表及可空观测字段，旧账号保持未查询状态。恢复时：

- 校验密钥、字段、重复 ID、每日唯一性和完整引用关系。
- 先导入公共代理节点，再重映射站点、账号和通知目标绑定；节点数字 ID 冲突不会改变有效代理身份。
- 保留现有配置与凭据，不用旧备份覆盖本地修改。
- 新导入账号关闭自动签到，清除租约和调度时间。
- 未完成记录转为需人工处理，保留提交标记。
- 未完成任务转为中止，等待项不重新入队。
- 保留未完成的令牌轮换标记，避免恢复后重复使用可能已被消费的 Refresh Token。
- 保留余额原值、未知状态及获取时间；运行中的检测转为已中断，不重放查询或续期。
- 新账号导入已有站点时，若该站点的现有地址或平台与备份不同，清除来自旧站点的观测快照。
- 新通知目标强制关闭自动通知，保留已加密连接；queued / sending 消息转为 skipped，不重放投递。已有本地通知配置与密文保持原值。
- 每日记录冲突时复用现有记录并重映射历史引用，重复导入不重复累计收益。

导出/导入期间暂停签到模块的写操作并等待活动签到、检测和通知发送收尾；导入后在恢复调度前刷新设置缓存。当前支持单个应用实例运行签到调度；不提供多个实例共享数据库的完整高可用协调。

## Sub2API 自动续期

1. 新增站点时选择 Sub2API，填写该部署实际提供的签到路径；Sub2API 没有统一内置的签到路径，不能仅凭平台类型确定接口。默认成功规则使用 Sub2API 的数字 code = 0，可按实际响应修改并配置状态查询。
2. 新增账号时使用 Bearer，填写网站登录 Access Token；选填同一登录会话的 Refresh Token 以启用自动续期。此功能不依赖订阅站或渠道账号，也不会自动登录。
3. 可选填到期时间，界面按本地时间输入，API 使用 credentials.expires_at（RFC3339）。JWT 的 exp 只辅助安排刷新，不作为本地认证依据。显式到期时间与 JWT 同时存在时取较早值；不填写且无法解析时，在遇到明确可恢复的 401 后尝试刷新。
4. 站点的 user_agent 统一用于刷新、签到和状态查询；Sub2API 请求专属 Header 不能另外配置 User-Agent。三个阶段使用同一个账号代理。开启会话绑定的上游需要与取得登录令牌时的网络和 User-Agent 一致。

执行签到或确认前，已知令牌即将到期时会提前续期。提前量最多 5 分钟；已知签发时间的短期令牌使用有效期最后 20%。续期调用当前站点地址（含路径前缀）下的 POST /api/v1/auth/refresh，JSON 请求只携带 refresh_token，不携带过期的 Authorization。成功响应必须包含 code = 0，以及 data.access_token、data.refresh_token 和正数 data.expires_in。

前置/后置状态查询遇到 TOKEN_EXPIRED、ACCESS_TOKEN_EXPIRED、INVALID_TOKEN 或 UNAUTHORIZED 的 401，可刷新后重查一次；单轮最多进行一次成功的令牌轮换。签到提交后的 401 只能恢复会话并查询结果，保持提交标记，不重发 GET 或 POST 签到。没有状态接口时转为需人工处理。403、会话撤销、账号停用和 IP/User-Agent 绑定错误不会触发循环刷新。

同账号的所有任务共用现有账号租约。刷新前持久化轮换意图，返回的新令牌对和有效期一起加密保存；自动续期增加 credential_version，但保持凭据身份版本，因此不会中断同一账号已提交记录的后续确认。原 JWT 带有 user_id 或 sid 时，新令牌必须保持一致，否则停止使用并要求核对令牌对。该比较仅用于拒绝身份变化，不替代上游认证。手工替换凭据仍改变身份版本，并维持原有人工核对策略。

Refresh Token 通常只能使用一次。429 会保留凭据并退避一分钟；网络错误、5xx 或不完整的成功响应可能发生在旧令牌已经被消费之后，因此保留轮换标记并停止再次刷新。若旧 Access Token 的有效期仍明确足够，可继续使用它；失效后需要更新登录凭据。已收到新令牌但数据库暂时写入失败时，当前进程保留加密结果，后续操作只重试保存。若进程在保存前退出，需要重新获取凭据。请使用独立登录会话，避免浏览器与应用争用同一个 Refresh Token。

编辑账号时，凭据字段全部留空表示保留；填写 Access Token 会替换整组凭据，此时 Refresh Token 留空表示关闭续期，新 Access Token 不沿用旧的到期时间。更新 Refresh Token 或显式到期时间也需要同时提供 Access Token。旧表单不能覆盖已经轮换的凭据，版本冲突返回 409。账号列表展示续期配置、到期时间、最近续期时间和固定的失败原因；记录中增加 refresh 请求阶段。

## 平台协议与会话

| 平台 | 凭据 | 提交方式 | 状态确认 |
| --- | --- | --- | --- |
| New API | Cookie / Bearer | 默认 POST /api/user/checkin，可编辑 | 原生月度记录，或自定义规则 |
| One API / OneHub / DoneHub | Cookie / Bearer | 可编辑请求预设 | 自定义规则 |
| Sub2API | Bearer / Cookie | 必须配置实际签到接口 | 自定义规则；Bearer 可续期 |
| AnyRouter | Cookie / Bearer / 用户名密码 | 适配器管理 sign_in / checkin | 可选自定义规则 |
| AgentRouter | 用户名密码 | POST /api/user/login | checked_in 或目标签到日志 |
| Custom HTTP | Cookie / Bearer | 自定义请求 | 可选自定义规则 |

New API 协议参考提供的 `D:\Projects\new-api` 中 `controller/checkin.go`、`model/checkin.go` 和鉴权路由。标准提交路径且未配置自定义状态规则时，读取 GET /api/user/checkin?month=YYYY-MM，要求 success=true、data.enabled=true 和有效的 data.stats.records。按 checkin_date 匹配目标记录，不能把 checked_in_today 用于前一天；当前日无目标记录且 checked_in_today=false 才允许提交。跨月确认查询原记录月份。可用 status_mode="disabled" 关闭原生查询，或配置自定义 status；自定义提交路径不自动套用原生查询。当前参考源码支持管理 Access Token，不同旧分支的 Cookie 和 New-Api-User 需求应按实际部署配置。

AnyRouter 协议参考 `octopus-plus/internal/sitesync/anyrouter_*.go`。Cookie 优先 POST /api/user/sign_in，Bearer 优先 POST /api/user/checkin。只有明确的路由不存在或认证拒绝才切换接口/认证；超时、断连、429、5xx、验证码页、未知响应和相互矛盾的状态都停止回退。对 Cookie 的认证切换先用只读 /api/user/self 验证，候选仅来自本账号自己的令牌。成功规则接受 success=true、ret=1、code=0；多个字段存在时必须全部一致且类型正确。明确的今日已签到不增加收益。

AnyRouter 登录或 /api/user/self 返回的数字用户 ID 会保存并用于 New-Api-User。输入的 ID 与上游返回 ID 不一致时停止，不枚举其他用户 ID。如果站点在查询自身前就要求该请求头，请手动填写本人 ID。账号密码方式优先复用会话，明确失效后可在提交前登录一次；确认阶段只使用已有会话。Cookie 按 Domain、Path、Secure 和过期时间管理，响应删除的 Cookie 不会由旧凭据重新填入，派生会话仅复用于原站点地址。

AgentRouter 协议参考 `octopus-plus/internal/sitesync/agentrouter.go`。登录就是签到，因此执行器先持久化 submit_intent，再发送一次登录请求，不调用 sign_in/checkin。登录成功或余额增加均不足以确认签到：必须有同业务日的布尔 checked_in=true，或 GET /api/log/self/ 返回的 type=4、内容为签到成功、时间位于目标业务日且不晚于当前时间的日志。日志查询使用原记录的时区和日期；已有的更早日志记为已签到，空日志不证明未签到。有效 checked_in 证据不要求上游一定返回会话，但没有可用会话时后续无法查询日志，需在网站核对结果。

登录后保存的 Cookie/Token 和用户 ID 与密码一起加密。自动保存增加 credential_version，保持凭据身份版本，且受账号租约和版本检查保护。保存失败时当前进程仅缓存加密结果，之后先重试数据库保存。AgentRouter 的提交标记覆盖登录；无论超时、写库失败或重启，恢复和“查询结果”都不会再次登录。如果响应丢失且无历史可用会话，记录转为需人工核查。

## 实现结构

| 路径 | 职责 |
| --- | --- |
| internal/model/checkin*.go | 独立实体、请求结构、校验、设置 |
| internal/op/checkin*.go | 数据访问、队列、租约、每日记录、备份、事务 |
| internal/checkin/adapter.go | 平台注册、认证/提交/查询/收益接口、请求审计 |
| internal/checkin/newapi.go、anyrouter.go、agentrouter.go、sub2api.go | 平台协议与认证处理 |
| internal/checkin/router_session.go | 独立账号会话、Cookie 作用域、加密保存与重试 |
| internal/checkin/inspection.go、internal/op/checkin_inspection.go | 余额与只读账号检测、共享租约、独立快照与恢复 |
| internal/checkin/notification.go、internal/op/checkin_notification*.go | 通知事件、加密目标、持久化投递队列、租约与备份 |
| internal/notify/ | 通知配置校验、模板、9 种平台协议与脱敏结果 |
| internal/checkin/executor.go | 统一的持久化提交边界与恢复状态机 |
| internal/checkin/ 其他文件 | HTTP、安全规则、调度、收益、事件 |
| internal/utils/checkinsecret/ | 主密钥和凭据加解密 |
| internal/server/handlers/checkin*.go | 管理员接口与 SSE |
| web/src/api/checkin.ts | 查询、变更、实时更新 |
| web/src/components/modules/checkin/ | 站点、账号、任务、记录、收益界面 |
| web/src/locales/checkin/ | 三种语言文案 |

八张表为 checkin_sites、checkin_accounts、checkin_runs、checkin_attempts、checkin_jobs、checkin_job_items、checkin_notification_targets、checkin_notification_deliveries。账号属于签到站点；任务项关联账号和每日记录；投递记录属于通知目标。不存在渠道或订阅业务外键。

## 接口

所有接口位于 /api/v1/checkin，要求管理员登录 Cookie，渠道 API Key 不能访问。JSON 写入拒绝未知字段。

| 方法 | 路径 | 用途 |
| --- | --- | --- |
| GET / POST | /sites | 列表、创建 |
| GET / PATCH / DELETE | /sites/:id | 查询、编辑、删除 |
| GET / POST | /sites/:id/accounts | 账号列表、创建 |
| PATCH / DELETE | /accounts/:id | 编辑、删除 |
| PUT | /accounts/:id/credentials | 带版本替换凭据 |
| POST | /accounts/:id/checkin | 单账号任务 |
| POST | /accounts/:id/balance | 同步刷新账号余额，返回账号及最近查询结果 |
| POST | /accounts/:id/detect | 同步只读检测账号，返回账号及最近检测结果 |
| GET / POST | /jobs | 最近 50 个任务、创建任务 |
| GET | /jobs/:id | 任务与账号进度 |
| POST | /jobs/:id/cancel | 取消等待项 |
| GET | /runs | 游标分页与站点/账号/日期/状态筛选 |
| GET | /runs/:id | 当前每日记录 |
| GET | /runs/:id/attempts | 最近 100 条请求阶段记录 |
| POST | /runs/:id/confirm | 仅查询结果 |
| GET | /stats | 记录计数与收益 |
| GET | /capabilities | 平台凭据、执行方式、状态查询、只读检测/余额查询及可编辑能力 |
| GET / PATCH | /settings | 执行参数 |
| POST | /rules/test | 离线样例测试 |
| GET / POST | /notifications | 通知目标列表、创建 |
| PATCH / DELETE | /notifications/:id | 带 revision 编辑、删除目标 |
| POST | /notifications/:id/test | 带 revision 排队发送示例消息，返回 202 |
| POST | /notification-preview | 使用 language、title_template、body_template 离线预览 |
| GET | /notification-deliveries | 以 target_id、before、limit 筛选及分页，默认 50 条 |
| POST | /notification-deliveries/:id/retry | 带 attempts、target_revision 手动重新排队，返回 202 |
| GET | /events | SSE 进度失效通知 |

创建任务返回 202，不等待签到完成。请求支持 Idempotency-Key；同一键不同范围返回 409，同一手动范围的活动任务会复用。范围示例：

~~~json
{ "kind": "sites", "ids": ["签到站点 UUID"], "only_auto": false }
~~~

kind 支持 all、sites、accounts。all 不允许携带 IDs；无效 ID 或空的所选范围不会被扩大成全量任务。

余额/检测接口接受空请求体或 {}，拒绝额外字段。请求完成返回 200；上游失败记录在 inspection.status/reason，不泄露原始错误或响应。运行冲突返回 409，并发或站点间隔限制返回 503。查询账号列表只读取缓存快照，不主动请求上游。平台能力增加 account_detection、balance_query 两个布尔字段。

默认并发 4（上限 8），同站账号串行，相邻账号任务间隔 2 秒；队列最多 2000 项，单任务最多 1000 个账号。默认 HTTP 超时 30 秒，单轮最多 90 秒，确认查询最多占用 30 秒；每轮默认确认 3 次、间隔 2 秒、总确认期限 60 分钟。账户租约 3 分钟，长于单轮执行上限。

## 验证

使用临时 SQLite 数据库和 httptest 本地服务覆盖独立运行、加密、并发入口、提交持久性、恢复、状态规则、精度、时区、取消、编辑冲突、备份和管理员权限。不会在测试中调用真实签到站点。

通知协议使用模拟 HTTP transport、本地 HTTP / SMTP 服务验证签名、JSON 转义、Unicode 限制、明确成功标记、SMTP TLS 要求与 DATA 接受边界。集成测试覆盖事件去重、冷却、失败隔离、低余额新鲜度/单位、并发重试、租约恢复、代理引用和备份不重放。无需配置真实推送凭据。

~~~text
go test ./...
go test -race ./...
go vet ./...
cd web
pnpm build
pnpm exec eslint src/api/checkin.ts src/components/modules/checkin src/provider/locale.tsx src/components/app.tsx src/components/app-shell.tsx src/lib/page-preload.ts src/stores/app.ts
~~~

数据库实体和事务采用 GORM；当前自动化数据库测试使用 SQLite。MySQL/PostgreSQL 的真实服务集成验证需要对应测试环境。
