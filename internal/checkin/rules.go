package checkin

import (
	"bytes"
	"encoding/json"
	"errors"
	"math/big"
	"regexp"
	"strconv"
	"strings"

	"github.com/bestruirui/octopus/internal/model"
)

var decimalPattern = regexp.MustCompile(`^[0-9]+(?:\.[0-9]+)?(?:[eE][+-]?[0-9]{1,3})?$`)
var scalarNumberPattern = regexp.MustCompile(`^-?[0-9]+(?:\.[0-9]+)?(?:[eE][+-]?[0-9]{1,3})?$`)

func decodeObject(body []byte) (any, error) {
	if len(body) > 1<<20 {
		return nil, errors.New("response exceeds 1 MiB")
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	var data any
	if err := decoder.Decode(&data); err != nil {
		return nil, err
	}
	if !json.Valid(body) {
		return nil, errors.New("invalid JSON response")
	}
	return data, nil
}

func field(data any, path string) (any, bool) {
	for _, part := range strings.Split(path, ".") {
		switch value := data.(type) {
		case map[string]any:
			var ok bool
			data, ok = value[part]
			if !ok {
				return nil, false
			}
		case []any:
			i, err := strconv.Atoi(part)
			if err != nil || i < 0 || i >= len(value) {
				return nil, false
			}
			data = value[i]
		default:
			return nil, false
		}
	}
	return data, true
}

func matches(data any, rule model.CheckinMatch) bool {
	actual, exists := field(data, rule.Path)
	if !exists {
		return false
	}
	expected, err := decodeObject(rule.Value)
	if err != nil {
		return false
	}
	switch a := actual.(type) {
	case bool:
		b, ok := expected.(bool)
		return ok && a == b
	case string:
		b, ok := expected.(string)
		return ok && a == b
	case json.Number:
		b, ok := expected.(json.Number)
		if !ok || len(a.String()) > 128 || len(b.String()) > 128 || !scalarNumberPattern.MatchString(a.String()) || !scalarNumberPattern.MatchString(b.String()) {
			return false
		}
		x, xok := new(big.Rat).SetString(a.String())
		y, yok := new(big.Rat).SetString(b.String())
		return xok && yok && x.Cmp(y) == 0
	}
	return false
}

func evaluateStatus(config model.CheckinStatusRule, httpStatus int, body []byte, date string) string {
	if httpStatus < 200 || httpStatus >= 300 {
		return "unknown"
	}
	data, err := decodeObject(body)
	if err != nil {
		return "unknown"
	}
	if v, ok := field(data, "success"); ok {
		if b, boolean := v.(bool); !boolean || !b {
			return "unknown"
		}
	}
	if config.DatePath != "" {
		v, ok := field(data, config.DatePath)
		if !ok || v != date {
			return "unknown"
		}
	}
	checked, unchecked := matches(data, config.Checked), matches(data, config.Unchecked)
	if checked == unchecked {
		return "unknown"
	}
	if checked {
		return "checked_in"
	}
	return "not_checked_in"
}

func evaluateSiteStatus(config model.CheckinSiteConfig, httpStatus int, body []byte, date string) string {
	if config.Status == nil {
		return "unknown"
	}
	if config.Adapter == "sub2api" {
		data, err := decodeObject(body)
		if err != nil {
			return "unknown"
		}
		if _, exists := field(data, "code"); exists && !matches(data, model.CheckinMatch{Path: "code", Value: json.RawMessage("0")}) {
			return "unknown"
		}
	}
	if config.Adapter == "anyrouter" {
		data, err := decodeObject(body)
		if err != nil || !routerSuccess(data) || routerChallenge(HTTPResult{Body: body}, data) {
			return "unknown"
		}
	}
	return evaluateStatus(*config.Status, httpStatus, body, date)
}

func decimalMicros(value, multiplier string) (*int64, error) {
	if len(value) > 128 || !decimalPattern.MatchString(value) || !decimalPattern.MatchString(multiplier) {
		return nil, errors.New("reward must be a finite nonnegative decimal")
	}
	a, ok := new(big.Rat).SetString(value)
	if !ok {
		return nil, errors.New("invalid reward")
	}
	b, ok := new(big.Rat).SetString(multiplier)
	if !ok || b.Sign() <= 0 {
		return nil, errors.New("invalid multiplier")
	}
	a.Mul(a, b).Mul(a, big.NewRat(1_000_000, 1))
	quotient, remainder := new(big.Int), new(big.Int)
	quotient.QuoRem(a.Num(), a.Denom(), remainder)
	if remainder.Mul(remainder, big.NewInt(2)).Cmp(a.Denom()) >= 0 {
		quotient.Add(quotient, big.NewInt(1))
	}
	if !quotient.IsInt64() {
		return nil, errors.New("reward exceeds supported range")
	}
	v := quotient.Int64()
	return &v, nil
}

func extractReward(rule model.CheckinRewardRule, body []byte) (*int64, string) {
	data, err := decodeObject(body)
	if err != nil {
		return nil, "unknown"
	}
	value, found := field(data, rule.Path)
	if !found || value == nil {
		return nil, "unknown"
	}
	var text string
	switch v := value.(type) {
	case string:
		text = strings.TrimSpace(v)
	case json.Number:
		text = v.String()
	default:
		return nil, "invalid"
	}
	amount, err := decimalMicros(text, rule.Multiplier)
	if err != nil {
		return nil, "invalid"
	}
	return amount, "known"
}

func formatMicros(value *big.Int) string {
	s := value.String()
	if len(s) <= 6 {
		s = strings.Repeat("0", 7-len(s)) + s
	}
	return strings.TrimRight(strings.TrimRight(s[:len(s)-6]+"."+s[len(s)-6:], "0"), ".")
}

type RulePreview struct {
	State       string  `json:"state"`
	Reward      *string `json:"reward"`
	RewardState string  `json:"reward_state"`
}

func TestRules(config model.CheckinSiteConfig, status int, body []byte, date string) (RulePreview, error) {
	if err := model.NormalizeCheckinSite(&config); err != nil {
		return RulePreview{}, err
	}
	if _, err := decodeObject(body); err != nil {
		return RulePreview{}, model.InvalidCheckin("provide a JSON response sample up to 1 MiB")
	}
	state := interpretSubmit(config, status, body).Status
	rewardBody := body
	response := HTTPResult{StatusCode: status, Body: body}
	if config.Adapter == "anyrouter" {
		state = routerSubmitResult(response).Status
	}
	if config.Status != nil {
		state = evaluateSiteStatus(config, status, body, date)
	} else if config.Adapter == "new-api" && model.CheckinNativeStatus(config) {
		data, _ := decodeObject(body)
		if _, exists := field(data, "data.stats"); exists {
			result := newAPIStatus(response, date, date)
			state, rewardBody = result.State, result.RewardBody
		}
	}
	if config.Adapter == "agentrouter" {
		data, _ := decodeObject(body)
		state = "unknown"
		day, hasDate := field(data, "data.checkin_date")
		if routerSuccessResponse(response) && routerResponseUserID(data) != "" && matches(data, model.CheckinMatch{Path: "success", Value: json.RawMessage("true")}) && matches(data, model.CheckinMatch{Path: "data.checked_in", Value: json.RawMessage("true")}) && (!hasDate || day == date) {
			state = "checked_in"
		}
		return RulePreview{State: state, RewardState: "unknown"}, nil
	}
	amount, rewardState := extractReward(config.Reward, rewardBody)
	result := RulePreview{State: state, RewardState: rewardState}
	if amount != nil {
		text := formatMicros(big.NewInt(*amount))
		result.Reward = &text
	}
	return result, nil
}
