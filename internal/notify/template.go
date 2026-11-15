package notify

import (
	"encoding/json"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/bestruirui/octopus/internal/model"
)

var Variables = []string{"event", "status", "site", "account", "site_id", "account_id", "date", "timezone", "source", "reward", "reward_unit", "balance", "balance_unit", "balance_time", "time"}
var placeholder = regexp.MustCompile(`\{\{\s*([a-z_]+)\s*\}\}`)

func validateTemplate(source string, limit int, payload bool) error {
	if len(source) > limit || strings.ContainsRune(source, 0) {
		return invalid("notification template exceeds limits or contains NUL")
	}
	for _, match := range placeholder.FindAllStringSubmatch(source, -1) {
		if !slices.Contains(Variables, match[1]) && !(payload && (match[1] == "title" || match[1] == "message")) {
			return invalid("unknown notification template variable")
		}
	}
	rest := placeholder.ReplaceAllString(source, "")
	if strings.Contains(rest, "{{") || strings.Contains(rest, "}}") {
		return invalid("invalid notification template placeholder")
	}
	return nil
}

// JSON interpolation only replaces string values. The result is marshaled
// again, so quotes/newlines supplied by names cannot change the JSON structure.
func validateJSONTemplate(v any, depth int) error {
	if depth > 16 {
		return invalid("Webhook payload is nested too deeply")
	}
	switch v := v.(type) {
	case string:
		return validateTemplate(v, 16384, true)
	case map[string]any:
		for k, value := range v {
			if strings.Contains(k, "{{") || strings.Contains(k, "}}") {
				return invalid("Webhook placeholders must appear in JSON string values")
			}
			if err := validateJSONTemplate(value, depth+1); err != nil {
				return err
			}
		}
	case []any:
		for _, value := range v {
			if err := validateJSONTemplate(value, depth+1); err != nil {
				return err
			}
		}
	}
	return nil
}

func replace(source string, values map[string]string) string {
	return placeholder.ReplaceAllStringFunc(source, func(token string) string { return values[placeholder.FindStringSubmatch(token)[1]] })
}

func clean(value string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) && r != '\n' && r != '\t' {
			return -1
		}
		return r
	}, value)
}

func micros(value *int64) string {
	if value == nil {
		return "?"
	}
	// String arithmetic also handles MinInt64, without float rounding or overflow.
	raw := strconv.FormatInt(*value, 10)
	sign := ""
	if strings.HasPrefix(raw, "-") {
		sign, raw = "-", raw[1:]
	}
	for len(raw) <= 6 {
		raw = "0" + raw
	}
	whole, fraction := raw[:len(raw)-6], strings.TrimRight(raw[len(raw)-6:], "0")
	if fraction != "" {
		whole += "." + fraction
	}
	return sign + whole
}

func eventLabel(kind, lang string) string {
	labels := map[string][3]string{
		"success":            {"Check-in succeeded", "签到成功", "簽到成功"},
		"already_checked_in": {"Already checked in", "今日已签到", "今日已簽到"},
		"failed":             {"Check-in failed", "签到失败", "簽到失敗"},
		"attention":          {"Check-in needs attention", "签到需要关注", "簽到需要關注"},
		"low_balance":        {"Low balance", "余额不足", "餘額不足"},
		"test":               {"Notification test", "通知测试", "通知測試"},
	}
	i := 0
	if lang == "zh_hans" {
		i = 1
	}
	if lang == "zh_hant" {
		i = 2
	}
	if label, ok := labels[kind]; ok {
		return label[i]
	}
	return kind
}

func Values(e model.CheckinNotificationEvent, lang string) map[string]string {
	balanceTime := "?"
	if e.BalanceAt != nil {
		balanceTime = e.BalanceAt.Format(time.RFC3339)
	}
	values := map[string]string{"event": eventLabel(e.Kind, lang), "status": e.Status, "site": e.SiteName, "account": e.AccountName, "site_id": e.SiteID, "account_id": e.AccountID, "date": e.CheckinDate, "timezone": e.Timezone, "source": e.Source, "reward": micros(e.RewardMicros), "reward_unit": e.RewardUnit, "balance": micros(e.BalanceMicros), "balance_unit": e.BalanceUnit, "balance_time": balanceTime, "time": e.OccurredAt.Format(time.RFC3339)}
	for k, v := range values {
		values[k] = clean(v)
	}
	return values
}

func Render(c model.CheckinNotificationConfig, e model.CheckinNotificationEvent) (string, string, error) {
	title, body := c.TitleTemplate, c.BodyTemplate
	if title == "" {
		title = "Octopus · {{event}}"
	}
	if body == "" {
		body = "{{site}} / {{account}}\n{{date}} ({{timezone}})\nStatus: {{status}}\nReward: {{reward}} {{reward_unit}}\nBalance: {{balance}} {{balance_unit}}\nSource: {{source}}"
		if c.Language == "zh_hans" {
			body = "{{site}} / {{account}}\n{{date}}（{{timezone}}）\n状态：{{status}}\n签到收益：{{reward}} {{reward_unit}}\n余额：{{balance}} {{balance_unit}}\n来源：{{source}}"
		}
		if c.Language == "zh_hant" {
			body = "{{site}} / {{account}}\n{{date}}（{{timezone}}）\n狀態：{{status}}\n簽到收益：{{reward}} {{reward_unit}}\n餘額：{{balance}} {{balance_unit}}\n來源：{{source}}"
		}
	}
	if err := validateTemplate(title, 512, false); err != nil {
		return "", "", err
	}
	if err := validateTemplate(body, 8192, false); err != nil {
		return "", "", err
	}
	values := Values(e, c.Language)
	title = strings.TrimSpace(strings.Join(strings.Fields(clean(replace(title, values))), " "))
	body = strings.TrimSpace(clean(replace(body, values)))
	if title == "" || body == "" || len(title) > 2048 || len(body) > 32768 {
		return "", "", invalid("rendered notification is empty or exceeds size limits")
	}
	return title, body, nil
}

func webhookPayload(source string, values map[string]string) ([]byte, error) {
	if source == "" {
		return json.Marshal(map[string]string{"title": values["title"], "message": values["message"], "event": values["event"], "site": values["site"], "account": values["account"], "time": values["time"]})
	}
	var object any
	decoder := json.NewDecoder(strings.NewReader(source))
	decoder.UseNumber()
	if err := decoder.Decode(&object); err != nil {
		return nil, invalid("invalid Webhook JSON payload")
	}
	var walk func(any) any
	walk = func(v any) any {
		switch v := v.(type) {
		case string:
			return replace(v, values)
		case map[string]any:
			for k, entry := range v {
				v[k] = walk(entry)
			}
			return v
		case []any:
			for i, entry := range v {
				v[i] = walk(entry)
			}
			return v
		default:
			return v
		}
	}
	body, err := json.Marshal(walk(object))
	if len(body) > 65536 {
		return nil, invalid("rendered Webhook payload exceeds size limits")
	}
	return body, err
}
