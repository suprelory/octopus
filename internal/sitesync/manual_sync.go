package sitesync

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/bestruirui/octopus/internal/model"
)

const (
	ManualSyncModeMerge   = "merge"
	ManualSyncModeReplace = "replace"

	ManualSyncFormatResponses = "responses"
	ManualSyncFormatSnapshot  = "snapshot"

	manualSyncSource = "manual_import"
)

var manualSyncFingerprintKey = func() []byte {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err == nil {
		return key
	}
	fallback := sha256.Sum256([]byte("octopus-manual-sync-preview"))
	return fallback[:]
}()

type ManualSyncRequest struct {
	Mode               string                    `json:"mode"`
	Format             string                    `json:"format"`
	TokenResponse      json.RawMessage           `json:"token_response,omitempty"`
	GroupResponses     []json.RawMessage         `json:"group_responses,omitempty"`
	ModelResponses     []ManualSyncModelResponse `json:"model_responses,omitempty"`
	AccountResponse    json.RawMessage           `json:"account_response,omitempty"`
	Snapshot           *ManualSyncSnapshotInput  `json:"snapshot,omitempty"`
	PreviewFingerprint string                    `json:"preview_fingerprint,omitempty"`
}

type ManualSyncModelResponse struct {
	GroupKey string          `json:"group_key"`
	Response json.RawMessage `json:"response"`
}

type ManualSyncSnapshotInput struct {
	AccessToken *string                            `json:"access_token,omitempty"`
	Tokens      *[]ManualSyncTokenInput            `json:"tokens,omitempty"`
	Groups      *[]ManualSyncGroupInput            `json:"groups,omitempty"`
	Models      *map[string][]ManualSyncModelInput `json:"models,omitempty"`
	Balance     *float64                           `json:"balance,omitempty"`
	BalanceUsed *float64                           `json:"balance_used,omitempty"`
	TodayIncome *float64                           `json:"today_income,omitempty"`
}

type ManualSyncTokenInput struct {
	Name      string `json:"name"`
	Token     string `json:"token"`
	GroupKey  string `json:"group_key"`
	GroupName string `json:"group_name"`
	Enabled   *bool  `json:"enabled,omitempty"`
	IsDefault *bool  `json:"is_default,omitempty"`
}

type ManualSyncGroupInput struct {
	GroupKey string `json:"group_key"`
	Name     string `json:"name"`
}

type ManualSyncModelInput struct {
	ModelName string                   `json:"model_name"`
	RouteType model.SiteModelRouteType `json:"route_type,omitempty"`
}

func (m *ManualSyncModelInput) UnmarshalJSON(data []byte) error {
	var name string
	if err := json.Unmarshal(data, &name); err == nil {
		m.ModelName = strings.TrimSpace(name)
		m.RouteType = ""
		return nil
	}

	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	m.ModelName = firstNonEmptyString(
		jsonString(raw["model_name"]),
		jsonString(raw["modelName"]),
		jsonString(raw["model"]),
		jsonString(raw["id"]),
		jsonString(raw["name"]),
	)
	m.RouteType = model.SiteModelRouteType(firstNonEmptyString(
		jsonString(raw["route_type"]),
		jsonString(raw["routeType"]),
	))
	return nil
}

type ManualSyncPreview struct {
	AccountID            int                      `json:"account_id"`
	SiteID               int                      `json:"site_id"`
	Mode                 string                   `json:"mode"`
	Format               string                   `json:"format"`
	ImportedTokenCount   int                      `json:"imported_token_count"`
	ImportedGroupCount   int                      `json:"imported_group_count"`
	ImportedModelCount   int                      `json:"imported_model_count"`
	TokenCount           int                      `json:"token_count"`
	UsableTokenCount     int                      `json:"usable_token_count"`
	MaskedTokenCount     int                      `json:"masked_token_count"`
	GroupCount           int                      `json:"group_count"`
	ModelCount           int                      `json:"model_count"`
	ChannelCountEstimate int                      `json:"channel_count_estimate"`
	BalanceProvided      bool                     `json:"balance_provided"`
	Balance              float64                  `json:"balance"`
	BalanceUsedProvided  bool                     `json:"balance_used_provided"`
	BalanceUsed          float64                  `json:"balance_used"`
	TodayIncomeProvided  bool                     `json:"today_income_provided"`
	TodayIncome          float64                  `json:"today_income"`
	Groups               []ManualSyncPreviewGroup `json:"groups"`
	Warnings             []string                 `json:"warnings"`
	CanApply             bool                     `json:"can_apply"`
	PreviewFingerprint   string                   `json:"preview_fingerprint"`
}

type ManualSyncPreviewGroup struct {
	GroupKey         string   `json:"group_key"`
	GroupName        string   `json:"group_name"`
	TokenCount       int      `json:"token_count"`
	UsableTokenCount int      `json:"usable_token_count"`
	MaskedTokenCount int      `json:"masked_token_count"`
	ModelCount       int      `json:"model_count"`
	ModelAction      string   `json:"model_action"`
	RouteTypes       []string `json:"route_types"`
	WillProject      bool     `json:"will_project"`
}

type ManualSyncApplyResult struct {
	Preview    ManualSyncPreview    `json:"preview"`
	SyncResult model.SiteSyncResult `json:"sync_result"`
}

type manualSyncValidationError struct {
	message string
}

func (e *manualSyncValidationError) Error() string {
	return e.message
}

func manualSyncInvalid(format string, args ...any) error {
	return &manualSyncValidationError{message: fmt.Sprintf(format, args...)}
}

func IsManualSyncValidationError(err error) bool {
	var target *manualSyncValidationError
	return errors.As(err, &target)
}

type manualSyncSections struct {
	tokensProvided  bool
	groupsProvided  bool
	accountProvided bool
	tokens          []model.SiteToken
	groups          []model.SiteUserGroup
	models          map[string][]model.SiteModel
	balance         *float64
	balanceUsed     *float64
	todayIncome     *float64
	accessToken     *string
	warnings        []string
}

type manualSyncPlan struct {
	snapshot    *syncSnapshot
	preview     ManualSyncPreview
	finalTokens []model.SiteToken
	finalGroups []model.SiteUserGroup
	finalModels []model.SiteModel
}

func PreviewManualSync(ctx context.Context, accountID int, req ManualSyncRequest) (*ManualSyncPreview, error) {
	siteRecord, account, err := loadSiteAccount(ctx, accountID)
	if err != nil {
		return nil, sanitizeSiteError(err)
	}
	plan, err := buildManualSyncPlan(siteRecord, account, req)
	if err != nil {
		return nil, err
	}
	return &plan.preview, nil
}

func ApplyManualSync(ctx context.Context, accountID int, req ManualSyncRequest) (*ManualSyncApplyResult, error) {
	siteRecord, account, err := loadSiteAccount(ctx, accountID)
	if err != nil {
		return nil, sanitizeSiteError(err)
	}
	plan, err := buildManualSyncPlan(siteRecord, account, req)
	if err != nil {
		return nil, err
	}
	if !plan.preview.CanApply {
		return nil, manualSyncInvalid("预览中没有可应用的数据，请补充响应内容后重试")
	}
	providedFingerprint := strings.TrimSpace(req.PreviewFingerprint)
	if providedFingerprint == "" || !hmac.Equal([]byte(providedFingerprint), []byte(plan.preview.PreviewFingerprint)) {
		return nil, manualSyncInvalid("预览已失效，请重新预览后再应用")
	}

	if err := persistSyncSnapshot(ctx, account.ID, plan.snapshot); err != nil {
		return nil, sanitizeSiteError(err)
	}
	channelIDs, err := ProjectAccount(ctx, account.ID)
	if err != nil {
		return nil, sanitizeSiteError(err)
	}

	modelNames := make([]string, 0, len(plan.finalModels))
	for _, item := range plan.finalModels {
		modelNames = append(modelNames, item.ModelName)
	}
	sort.Strings(modelNames)
	result := model.SiteSyncResult{
		AccountID:       account.ID,
		SiteID:          siteRecord.ID,
		Status:          plan.snapshot.status,
		ChannelCount:    len(channelIDs),
		GroupCount:      len(plan.finalGroups),
		TokenCount:      len(plan.finalTokens),
		ModelCount:      len(plan.finalModels),
		ManagedChannels: channelIDs,
		Models:          modelNames,
		GroupResults:    exportSiteSyncGroupResults(plan.snapshot.groupResults),
		Message:         plan.snapshot.message,
	}
	plan.preview.ChannelCountEstimate = len(channelIDs)
	return &ManualSyncApplyResult{Preview: plan.preview, SyncResult: result}, nil
}

func buildManualSyncPlan(siteRecord *model.Site, account *model.SiteAccount, req ManualSyncRequest) (*manualSyncPlan, error) {
	if siteRecord == nil || account == nil {
		return nil, manualSyncInvalid("站点或账号不存在")
	}
	mode, format, err := normalizeManualSyncRequest(&req)
	if err != nil {
		return nil, err
	}
	sections, err := parseManualSyncSections(siteRecord, req)
	if err != nil {
		return nil, err
	}

	finalTokens := buildManualSyncTokens(account, sections, mode)
	affectedModels, groupResults, explicitModelGroups := buildManualSyncModels(account, sections, mode)
	groupResults, affectedModels = addManualTokenRecoveryGroups(account, finalTokens, sections, groupResults, affectedModels, explicitModelGroups, mode)
	groupResults = addManualMissingKeyGroups(account, finalTokens, sections, groupResults, explicitModelGroups, mode)

	existingModelMap := make(map[string]model.SiteModel, len(account.Models))
	for _, item := range account.Models {
		key := model.NormalizeSiteGroupKey(item.GroupKey) + "\x00" + strings.TrimSpace(item.ModelName)
		existingModelMap[key] = item
	}
	preparedModels := preparePersistedSyncModels(account.ID, affectedModels, existingModelMap, time.Now())
	finalModels := mergePersistedSiteModelsByGroup(account.Models, preparedModels, groupResults)
	sortSiteModels(finalModels)

	finalGroups := buildManualSyncGroups(account, sections, mode, finalTokens, finalModels, explicitModelGroups)
	groupNames := make(map[string]string, len(finalGroups))
	for _, group := range finalGroups {
		groupNames[model.NormalizeSiteGroupKey(group.GroupKey)] = model.NormalizeSiteGroupName(group.GroupKey, group.Name)
	}
	for index := range groupResults {
		groupKey := model.NormalizeSiteGroupKey(groupResults[index].GroupKey)
		groupResults[index].GroupKey = groupKey
		groupResults[index].GroupName = model.NormalizeSiteGroupName(groupKey, groupNames[groupKey])
		groupResults[index].HasKey = hasUsableToken(tokensForGroup(finalTokens, groupKey))
	}
	sort.Slice(groupResults, func(i, j int) bool { return groupResults[i].GroupKey < groupResults[j].GroupKey })

	balance := account.Balance
	if sections.balance != nil {
		balance = *sections.balance
	}
	balanceUsed := account.BalanceUsed
	if sections.balanceUsed != nil {
		balanceUsed = *sections.balanceUsed
	}
	todayIncome := account.TodayIncome
	if sections.todayIncome != nil {
		todayIncome = *sections.todayIncome
	}
	accessToken := ""
	if sections.accessToken != nil {
		accessToken = strings.TrimSpace(*sections.accessToken)
	}

	snapshot := &syncSnapshot{
		accessToken:  accessToken,
		groups:       cloneSiteGroups(finalGroups),
		tokens:       cloneSiteTokens(finalTokens),
		models:       cloneSiteModels(affectedModels),
		groupResults: append([]siteGroupSyncResult(nil), groupResults...),
		status:       model.SiteExecutionStatusSuccess,
		balance:      balance,
		balanceUsed:  balanceUsed,
		todayIncome:  todayIncome,
		message:      buildManualSyncMessage(sections, groupResults),
	}

	previewGroups, channelCount := buildManualSyncPreviewGroups(siteRecord, account, finalGroups, finalTokens, finalModels, explicitModelGroups, mode, groupResults)
	warnings := append([]string(nil), sections.warnings...)
	warnings = append(warnings, buildManualSyncWarnings(account, sections, finalTokens, finalModels, explicitModelGroups, mode)...)
	warnings = normalizeManualWarnings(warnings)

	usableTokenCount, maskedTokenCount := countManualTokens(finalTokens)
	preview := ManualSyncPreview{
		AccountID:            account.ID,
		SiteID:               siteRecord.ID,
		Mode:                 mode,
		Format:               format,
		ImportedTokenCount:   len(sections.tokens),
		ImportedGroupCount:   len(sections.groups),
		ImportedModelCount:   countManualImportedModels(sections.models),
		TokenCount:           len(finalTokens),
		UsableTokenCount:     usableTokenCount,
		MaskedTokenCount:     maskedTokenCount,
		GroupCount:           len(finalGroups),
		ModelCount:           len(finalModels),
		ChannelCountEstimate: channelCount,
		BalanceProvided:      sections.balance != nil,
		Balance:              balance,
		BalanceUsedProvided:  sections.balanceUsed != nil,
		BalanceUsed:          balanceUsed,
		TodayIncomeProvided:  sections.todayIncome != nil,
		TodayIncome:          todayIncome,
		Groups:               previewGroups,
		Warnings:             warnings,
		CanApply:             manualSyncHasActionableSection(sections),
	}
	preview.PreviewFingerprint = buildManualSyncFingerprint(account.ID, mode, format, sections)

	return &manualSyncPlan{
		snapshot:    snapshot,
		preview:     preview,
		finalTokens: finalTokens,
		finalGroups: finalGroups,
		finalModels: finalModels,
	}, nil
}

func normalizeManualSyncRequest(req *ManualSyncRequest) (string, string, error) {
	mode := strings.ToLower(strings.TrimSpace(req.Mode))
	if mode == "" {
		mode = ManualSyncModeMerge
	}
	if mode != ManualSyncModeMerge && mode != ManualSyncModeReplace {
		return "", "", manualSyncInvalid("不支持的导入模式：%s", req.Mode)
	}
	format := strings.ToLower(strings.TrimSpace(req.Format))
	if format == "" {
		format = ManualSyncFormatResponses
	}
	if format != ManualSyncFormatResponses && format != ManualSyncFormatSnapshot {
		return "", "", manualSyncInvalid("不支持的输入格式：%s", req.Format)
	}
	req.Mode = mode
	req.Format = format
	return mode, format, nil
}

func parseManualSyncSections(siteRecord *model.Site, req ManualSyncRequest) (manualSyncSections, error) {
	sections := manualSyncSections{models: make(map[string][]model.SiteModel)}
	var err error
	switch req.Format {
	case ManualSyncFormatResponses:
		sections, err = parseManualResponseSections(siteRecord, req)
	case ManualSyncFormatSnapshot:
		sections, err = parseManualSnapshotSections(req.Snapshot)
	}
	if err != nil {
		return manualSyncSections{}, err
	}
	if !manualSyncHasProvidedSection(sections) {
		return manualSyncSections{}, manualSyncInvalid("未找到可导入的数据区段")
	}
	return sections, nil
}

func manualSyncHasProvidedSection(sections manualSyncSections) bool {
	return sections.tokensProvided ||
		sections.groupsProvided ||
		sections.accountProvided ||
		len(sections.models) > 0 ||
		sections.balance != nil ||
		sections.balanceUsed != nil ||
		sections.todayIncome != nil ||
		sections.accessToken != nil
}

func manualSyncHasActionableSection(sections manualSyncSections) bool {
	return sections.tokensProvided ||
		sections.groupsProvided ||
		len(sections.models) > 0 ||
		sections.balance != nil ||
		sections.balanceUsed != nil ||
		sections.todayIncome != nil ||
		sections.accessToken != nil
}

func parseManualResponseSections(siteRecord *model.Site, req ManualSyncRequest) (manualSyncSections, error) {
	sections := manualSyncSections{models: make(map[string][]model.SiteModel)}
	if manualRawProvided(req.TokenResponse) {
		value, err := decodeManualResponse(req.TokenResponse, "Token 响应")
		if err != nil {
			return sections, err
		}
		sections.tokensProvided = true
		sections.tokens = parseManualTokens(value)
		if len(sections.tokens) == 0 {
			sections.warnings = append(sections.warnings, "Token 响应中未解析到 Key")
		}
	}

	for index, raw := range req.GroupResponses {
		if !manualRawProvided(raw) {
			continue
		}
		value, err := decodeManualResponse(raw, fmt.Sprintf("分组响应 %d", index+1))
		if err != nil {
			return sections, err
		}
		sections.groupsProvided = true
		for _, group := range parseGroupItemsFromAny(value) {
			group.GroupKey = model.NormalizeSiteGroupKey(group.GroupKey)
			group.Name = model.NormalizeSiteGroupName(group.GroupKey, group.Name)
			group.RawPayload = ""
			sections.groups = upsertManualGroup(sections.groups, group)
		}
	}
	if sections.groupsProvided && len(sections.groups) == 0 {
		sections.warnings = append(sections.warnings, "分组响应中未解析到分组")
	}

	for index, item := range req.ModelResponses {
		groupKey := model.NormalizeSiteGroupKey(item.GroupKey)
		if strings.TrimSpace(item.GroupKey) == "" {
			return sections, manualSyncInvalid("模型响应 %d 缺少分组标识", index+1)
		}
		if !manualRawProvided(item.Response) {
			return sections, manualSyncInvalid("分组 %q 的模型响应为空", groupKey)
		}
		value, err := decodeManualResponse(item.Response, fmt.Sprintf("分组 %q 的模型响应", groupKey))
		if err != nil {
			return sections, err
		}
		names := collectManualModelNames(value)
		for _, name := range names {
			sections.models[groupKey] = upsertManualModel(sections.models[groupKey], model.SiteModel{
				GroupKey:  groupKey,
				ModelName: name,
				Source:    manualSyncSource,
			})
		}
		if _, ok := sections.models[groupKey]; !ok {
			sections.models[groupKey] = nil
		}
		if len(names) == 0 {
			sections.warnings = append(sections.warnings, fmt.Sprintf("分组 %q 的响应中未解析到模型", groupKey))
		}
	}

	if manualRawProvided(req.AccountResponse) {
		value, err := decodeManualResponse(req.AccountResponse, "账户响应")
		if err != nil {
			return sections, err
		}
		sections.accountProvided = true
		sections.balance, sections.balanceUsed, sections.todayIncome = parseManualAccountBalance(siteRecord.Platform, value)
		if sections.balance == nil && sections.balanceUsed == nil && sections.todayIncome == nil {
			sections.warnings = append(sections.warnings, "账户响应中未解析到余额、已用额度或今日收入")
		}
	}
	return sections, nil
}

func parseManualSnapshotSections(snapshot *ManualSyncSnapshotInput) (manualSyncSections, error) {
	sections := manualSyncSections{models: make(map[string][]model.SiteModel)}
	if snapshot == nil {
		return sections, manualSyncInvalid("统一快照不能为空")
	}
	if snapshot.Tokens != nil {
		sections.tokensProvided = true
		for index, item := range *snapshot.Tokens {
			tokenValue := strings.TrimSpace(item.Token)
			if tokenValue == "" {
				return sections, manualSyncInvalid("快照中的第 %d 个 Token 为空", index+1)
			}
			groupKey := model.NormalizeSiteGroupKey(item.GroupKey)
			enabled := true
			if item.Enabled != nil {
				enabled = *item.Enabled
			}
			isDefault := index == 0
			if item.IsDefault != nil {
				isDefault = *item.IsDefault
			}
			sections.tokens = append(sections.tokens, model.SiteToken{
				Name:        firstNonEmptyString(item.Name, fmt.Sprintf("token-%d", index+1)),
				Token:       tokenValue,
				ValueStatus: model.NormalizeSiteTokenValueStatus("", tokenValue),
				GroupKey:    groupKey,
				GroupName:   model.NormalizeSiteGroupName(groupKey, item.GroupName),
				Enabled:     enabled,
				Source:      manualSyncSource,
				IsDefault:   isDefault,
			})
		}
	}
	if snapshot.Groups != nil {
		sections.groupsProvided = true
		for _, item := range *snapshot.Groups {
			groupKey := model.NormalizeSiteGroupKey(item.GroupKey)
			sections.groups = upsertManualGroup(sections.groups, model.SiteUserGroup{
				GroupKey: groupKey,
				Name:     model.NormalizeSiteGroupName(groupKey, item.Name),
			})
		}
	}
	if snapshot.Models != nil {
		for rawGroupKey, items := range *snapshot.Models {
			groupKey := model.NormalizeSiteGroupKey(rawGroupKey)
			if strings.TrimSpace(rawGroupKey) == "" {
				return sections, manualSyncInvalid("快照模型映射包含空分组标识")
			}
			for _, item := range items {
				modelName := strings.TrimSpace(item.ModelName)
				if modelName == "" {
					return sections, manualSyncInvalid("分组 %q 中存在空模型名称", groupKey)
				}
				routeType := item.RouteType
				if strings.TrimSpace(string(routeType)) != "" && !isKnownManualRouteType(routeType) {
					return sections, manualSyncInvalid("模型 %q 的端点类型 %q 不受支持", modelName, routeType)
				}
				sections.models[groupKey] = upsertManualModel(sections.models[groupKey], model.SiteModel{
					GroupKey:  groupKey,
					ModelName: modelName,
					Source:    manualSyncSource,
					RouteType: routeType,
				})
			}
			if _, ok := sections.models[groupKey]; !ok {
				sections.models[groupKey] = nil
			}
		}
	}
	if snapshot.AccessToken != nil {
		value := strings.TrimSpace(*snapshot.AccessToken)
		if value != "" {
			sections.accessToken = &value
		}
	}
	sections.balance = snapshot.Balance
	sections.balanceUsed = snapshot.BalanceUsed
	sections.todayIncome = snapshot.TodayIncome
	if err := validateManualBalanceValues(sections.balance, sections.balanceUsed, sections.todayIncome); err != nil {
		return sections, err
	}
	return sections, nil
}

func manualRawProvided(raw json.RawMessage) bool {
	trimmed := strings.TrimSpace(string(raw))
	return trimmed != "" && trimmed != "null"
}

func decodeManualResponse(raw json.RawMessage, label string) (any, error) {
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, manualSyncInvalid("%s不是有效 JSON：%v", label, err)
	}
	if value == nil {
		return nil, manualSyncInvalid("%s不能为空", label)
	}
	if payload, ok := value.(map[string]any); ok {
		if rawSuccess, exists := payload["success"]; exists {
			if success, ok := rawSuccess.(bool); ok && !success {
				return nil, manualSyncInvalid("%s返回失败：%s", label, firstNonEmptyString(extractSiteResponseMessage(payload), "上游响应 success=false"))
			}
		}
		if rawCode, exists := payload["code"]; exists {
			code := anyToInt64(rawCode)
			if code != 0 && code != 1 && (code < 200 || code >= 300) {
				return nil, manualSyncInvalid("%s返回失败：%s", label, firstNonEmptyString(extractSiteResponseMessage(payload), fmt.Sprintf("code=%d", code)))
			}
		}
	}
	return value, nil
}

func parseManualTokens(value any) []model.SiteToken {
	items := parseTokenItemsFromAny(value)
	if len(items) == 0 {
		if item, ok := value.(map[string]any); ok && manualTokenValue(item) != "" {
			items = []map[string]any{item}
		}
	}
	tokens := make([]model.SiteToken, 0, len(items))
	for index, item := range items {
		tokenValue := manualTokenValue(item)
		if tokenValue == "" {
			continue
		}
		groupKey := model.NormalizeSiteGroupKey(firstNonEmptyString(
			jsonString(item["group_key"]),
			jsonString(item["groupKey"]),
			jsonString(item["group_id"]),
			jsonString(item["groupId"]),
			jsonString(nestedValue(item, "group", "id")),
			jsonString(item["token_group"]),
			jsonString(item["tokenGroup"]),
			jsonString(item["group_name"]),
			jsonString(item["groupName"]),
			jsonString(nestedValue(item, "group", "name")),
			jsonString(item["group"]),
		))
		groupName := model.NormalizeSiteGroupName(groupKey, firstNonEmptyString(
			jsonString(item["group_name"]),
			jsonString(item["groupName"]),
			jsonString(nestedValue(item, "group", "name")),
			jsonString(item["group"]),
			jsonString(item["token_group"]),
			jsonString(item["tokenGroup"]),
		))
		tokens = append(tokens, model.SiteToken{
			Name:        firstNonEmptyString(jsonString(item["name"]), jsonString(item["remark"]), fmt.Sprintf("token-%d", index+1)),
			Token:       tokenValue,
			ValueStatus: model.NormalizeSiteTokenValueStatus("", tokenValue),
			GroupKey:    groupKey,
			GroupName:   groupName,
			Enabled:     parseSub2APITokenEnabled(item),
			Source:      manualSyncSource,
			IsDefault:   jsonBool(item["is_default"]) || jsonBool(item["isDefault"]) || index == 0,
		})
	}
	return tokens
}

func manualTokenValue(item map[string]any) string {
	return firstNonEmptyString(
		jsonString(item["key"]),
		jsonString(item["token"]),
		jsonString(item["api_key"]),
		jsonString(item["apiKey"]),
		jsonString(item["token_value"]),
		jsonString(item["tokenValue"]),
		jsonString(item["access_token"]),
		jsonString(item["accessToken"]),
	)
}

func collectManualModelNames(value any) []string {
	return normalizeModelNames(collectManualModelNamesRecursive(value))
}

func collectManualModelNamesRecursive(value any) []string {
	switch typed := value.(type) {
	case string:
		if name := strings.TrimSpace(strings.TrimPrefix(typed, "models/")); name != "" {
			return []string{name}
		}
	case []any:
		result := make([]string, 0, len(typed))
		for _, item := range typed {
			result = append(result, collectManualModelNamesRecursive(item)...)
		}
		return result
	case []string:
		return typed
	case map[string]any:
		if name := firstNonEmptyString(
			jsonString(typed["id"]),
			jsonString(typed["model"]),
			jsonString(typed["model_name"]),
			jsonString(typed["modelName"]),
			jsonString(typed["name"]),
		); name != "" {
			return []string{strings.TrimPrefix(name, "models/")}
		}
		for _, key := range []string{"data", "items", "models", "list", "records", "rows", "available_models", "availableModels"} {
			if child, ok := typed[key]; ok {
				if names := collectManualModelNamesRecursive(child); len(names) > 0 {
					return names
				}
				if isManualEmptyCollection(child) {
					return nil
				}
			}
		}
		keys := make([]string, 0, len(typed))
		for key := range typed {
			if isManualModelEnvelopeKey(key) {
				continue
			}
			if trimmed := strings.TrimSpace(key); trimmed != "" {
				keys = append(keys, strings.TrimPrefix(trimmed, "models/"))
			}
		}
		return keys
	}
	return nil
}

func isManualEmptyCollection(value any) bool {
	switch typed := value.(type) {
	case []any:
		return len(typed) == 0
	case []string:
		return len(typed) == 0
	case map[string]any:
		return len(typed) == 0
	default:
		return false
	}
}

func isManualModelEnvelopeKey(key string) bool {
	switch strings.ToLower(strings.TrimSpace(key)) {
	case "", "success", "message", "msg", "code", "error", "errors", "object", "total", "page", "page_size", "pagesize", "has_more", "hasmore":
		return true
	default:
		return false
	}
}

func parseManualAccountBalance(platform model.SitePlatform, value any) (*float64, *float64, *float64) {
	for _, payload := range manualAccountPayloadCandidates(value) {
		balance, balanceUsed, todayIncome := parseManualAccountBalanceMap(platform, payload)
		if balance != nil || balanceUsed != nil || todayIncome != nil {
			return balance, balanceUsed, todayIncome
		}
	}
	return nil, nil, nil
}

func manualAccountPayloadCandidates(value any) []map[string]any {
	payload, ok := value.(map[string]any)
	if !ok {
		return nil
	}
	result := make([]map[string]any, 0, 5)
	for _, candidate := range []any{
		nestedValue(payload, "data", "user"),
		nestedValue(payload, "data", "account"),
		payload["data"],
		payload["user"],
		payload["account"],
		payload,
	} {
		if item, ok := candidate.(map[string]any); ok {
			result = append(result, item)
		}
	}
	return result
}

func parseManualAccountBalanceMap(platform model.SitePlatform, payload map[string]any) (*float64, *float64, *float64) {
	var balance, balanceUsed, todayIncome *float64
	if value, ok := manualFloatFromMap(payload, "balance", "remaining_balance", "remainingBalance"); ok {
		balance = floatPointer(value)
	}
	if value, ok := manualFloatFromMap(payload, "balance_used", "balanceUsed", "used_balance", "usedBalance", "total_spent", "totalSpent"); ok {
		balanceUsed = floatPointer(value)
	}
	if value, ok := manualFloatFromMap(payload, "today_income", "todayIncome"); ok {
		todayIncome = floatPointer(value)
	}

	quota, hasQuota := manualFloatFromMap(payload, "quota")
	usedQuota, hasUsedQuota := manualFloatFromMap(payload, "used_quota", "usedQuota")
	if hasQuota {
		quotaIsRemaining := platform == model.SitePlatformNewAPI || platform == model.SitePlatformAnyRouter || platform == model.SitePlatformDoneHub
		value := quota
		if !quotaIsRemaining && hasUsedQuota {
			value = math.Max(quota-usedQuota, 0)
		}
		balance = floatPointer(value / siteBalanceQuotaPerUSD)
	}
	if hasUsedQuota {
		balanceUsed = floatPointer(usedQuota / siteBalanceQuotaPerUSD)
	}
	if raw, ok := payload["today_income"]; ok && hasQuota {
		if value, valid := manualFloat(raw); valid {
			todayIncome = floatPointer(value / siteBalanceQuotaPerUSD)
		}
	}
	return balance, balanceUsed, todayIncome
}

func manualFloatFromMap(payload map[string]any, keys ...string) (float64, bool) {
	for _, key := range keys {
		if raw, ok := payload[key]; ok {
			if value, valid := manualFloat(raw); valid {
				return value, true
			}
		}
	}
	return 0, false
}

func manualFloat(value any) (float64, bool) {
	switch typed := value.(type) {
	case float64:
		return typed, !math.IsNaN(typed) && !math.IsInf(typed, 0)
	case float32:
		result := float64(typed)
		return result, !math.IsNaN(result) && !math.IsInf(result, 0)
	case int:
		return float64(typed), true
	case int64:
		return float64(typed), true
	case json.Number:
		result, err := typed.Float64()
		return result, err == nil && !math.IsNaN(result) && !math.IsInf(result, 0)
	case string:
		result, err := strconv.ParseFloat(strings.TrimSpace(typed), 64)
		return result, err == nil && !math.IsNaN(result) && !math.IsInf(result, 0)
	default:
		return 0, false
	}
}

func validateManualBalanceValues(values ...*float64) error {
	for _, value := range values {
		if value != nil && (math.IsNaN(*value) || math.IsInf(*value, 0)) {
			return manualSyncInvalid("余额字段必须是有限数字")
		}
	}
	return nil
}

func floatPointer(value float64) *float64 {
	copy := value
	return &copy
}

func isKnownManualRouteType(routeType model.SiteModelRouteType) bool {
	switch routeType {
	case model.SiteModelRouteTypeOpenAIChat,
		model.SiteModelRouteTypeOpenAIResponse,
		model.SiteModelRouteTypeAnthropic,
		model.SiteModelRouteTypeGemini,
		model.SiteModelRouteTypeVolcengine,
		model.SiteModelRouteTypeOpenAIEmbedding,
		model.SiteModelRouteTypeUnknown:
		return true
	default:
		return false
	}
}

func buildManualSyncTokens(account *model.SiteAccount, sections manualSyncSections, mode string) []model.SiteToken {
	base := make([]model.SiteToken, 0, len(account.Tokens)+len(sections.tokens))
	if !sections.tokensProvided || mode == ManualSyncModeMerge {
		base = append(base, cloneSiteTokens(account.Tokens)...)
	} else {
		for _, token := range account.Tokens {
			if strings.TrimSpace(token.Source) == "manual" {
				base = append(base, token)
			}
		}
	}
	for _, incoming := range sections.tokens {
		matched := -1
		for index, existing := range base {
			if sameManualTokenIdentity(existing, incoming) {
				matched = index
				break
			}
		}
		if matched >= 0 {
			base[matched] = incoming
		} else {
			base = append(base, incoming)
		}
	}
	return mergePersistedSiteTokens(account.ID, account.Tokens, base, time.Now())
}

func sameManualTokenIdentity(left model.SiteToken, right model.SiteToken) bool {
	if model.NormalizeSiteGroupKey(left.GroupKey) != model.NormalizeSiteGroupKey(right.GroupKey) {
		return false
	}
	if sameComparableSiteTokenValue(left.Token, right.Token) {
		return true
	}
	leftName := normalizeSiteTokenName(left.Name)
	rightName := normalizeSiteTokenName(right.Name)
	return leftName != "" && rightName != "" && leftName == rightName
}

func buildManualSyncModels(account *model.SiteAccount, sections manualSyncSections, mode string) ([]model.SiteModel, []siteGroupSyncResult, map[string]struct{}) {
	existingByGroup := make(map[string][]model.SiteModel)
	for _, item := range account.Models {
		groupKey := model.NormalizeSiteGroupKey(item.GroupKey)
		item.GroupKey = groupKey
		existingByGroup[groupKey] = append(existingByGroup[groupKey], item)
	}
	groupKeys := make([]string, 0, len(sections.models))
	for groupKey := range sections.models {
		groupKeys = append(groupKeys, model.NormalizeSiteGroupKey(groupKey))
	}
	sort.Strings(groupKeys)

	affected := make([]model.SiteModel, 0)
	results := make([]siteGroupSyncResult, 0, len(groupKeys))
	explicit := make(map[string]struct{}, len(groupKeys))
	for _, groupKey := range groupKeys {
		explicit[groupKey] = struct{}{}
		incoming := sections.models[groupKey]
		desired := make([]model.SiteModel, 0, len(existingByGroup[groupKey])+len(incoming))
		if mode == ManualSyncModeMerge {
			desired = append(desired, cloneSiteModels(existingByGroup[groupKey])...)
		}
		for _, item := range incoming {
			desired = upsertManualModel(desired, item)
		}
		sortSiteModels(desired)
		if mode == ManualSyncModeMerge && len(incoming) == 0 {
			continue
		}
		affected = append(affected, desired...)
		result := siteGroupSyncResult{
			GroupKey:      groupKey,
			Status:        siteGroupSyncStatusSynced,
			Authoritative: true,
			ModelCount:    len(desired),
			Message:       fmt.Sprintf("手动导入后确认 %d 个模型", len(desired)),
		}
		if len(desired) == 0 {
			result.Status = siteGroupSyncStatusEmpty
			result.Message = "手动导入确认该分组当前没有模型"
		}
		results = append(results, result)
	}
	return affected, results, explicit
}

func addManualTokenRecoveryGroups(
	account *model.SiteAccount,
	finalTokens []model.SiteToken,
	sections manualSyncSections,
	results []siteGroupSyncResult,
	affected []model.SiteModel,
	explicit map[string]struct{},
	mode string,
) ([]siteGroupSyncResult, []model.SiteModel) {
	if !sections.tokensProvided {
		return results, affected
	}
	touched := collectManualSyncTouchedGroups(account, sections, explicit, mode)
	for groupKey := range touched {
		if _, ok := explicit[groupKey]; ok || !hasUsableToken(tokensForGroup(finalTokens, groupKey)) || !manualGroupNeedsTokenRecovery(account.UserGroups, groupKey) {
			continue
		}
		groupModels := modelsForGroup(account.Models, groupKey)
		if len(groupModels) == 0 {
			continue
		}
		affected = append(affected, cloneSiteModels(groupModels)...)
		results = append(results, siteGroupSyncResult{
			GroupKey:      groupKey,
			Status:        siteGroupSyncStatusSynced,
			Authoritative: true,
			ModelCount:    len(groupModels),
			Message:       fmt.Sprintf("手动导入可用 Key 后沿用 %d 个历史模型", len(groupModels)),
		})
	}
	return results, affected
}

func addManualMissingKeyGroups(
	account *model.SiteAccount,
	finalTokens []model.SiteToken,
	sections manualSyncSections,
	results []siteGroupSyncResult,
	explicit map[string]struct{},
	mode string,
) []siteGroupSyncResult {
	touched := collectManualSyncTouchedGroups(account, sections, explicit, mode)
	if len(touched) == 0 {
		return results
	}

	resultIndexes := make(map[string]int, len(results))
	for index := range results {
		resultIndexes[model.NormalizeSiteGroupKey(results[index].GroupKey)] = index
	}
	for groupKey := range touched {
		groupKey = model.NormalizeSiteGroupKey(groupKey)
		if hasUsableToken(tokensForGroup(finalTokens, groupKey)) {
			continue
		}
		missingKeyResult := siteGroupSyncResult{
			GroupKey: groupKey,
			Status:   siteGroupSyncStatusMissingKey,
			Message:  "手动导入后该分组没有可用 Key，无法投影，已清理历史投影",
		}
		if index, ok := resultIndexes[groupKey]; ok {
			results[index] = missingKeyResult
			continue
		}
		resultIndexes[groupKey] = len(results)
		results = append(results, missingKeyResult)
	}
	return results
}

func collectManualSyncTouchedGroups(account *model.SiteAccount, sections manualSyncSections, explicit map[string]struct{}, mode string) map[string]struct{} {
	touched := make(map[string]struct{})
	for groupKey := range explicit {
		touched[model.NormalizeSiteGroupKey(groupKey)] = struct{}{}
	}
	if !sections.tokensProvided {
		return touched
	}
	if mode == ManualSyncModeReplace && account != nil {
		for _, group := range account.UserGroups {
			touched[model.NormalizeSiteGroupKey(group.GroupKey)] = struct{}{}
		}
		for _, token := range account.Tokens {
			touched[model.NormalizeSiteGroupKey(token.GroupKey)] = struct{}{}
		}
		for _, item := range account.Models {
			touched[model.NormalizeSiteGroupKey(item.GroupKey)] = struct{}{}
		}
	}
	for _, token := range sections.tokens {
		touched[model.NormalizeSiteGroupKey(token.GroupKey)] = struct{}{}
	}
	return touched
}

func manualGroupNeedsTokenRecovery(groups []model.SiteUserGroup, groupKey string) bool {
	groupKey = model.NormalizeSiteGroupKey(groupKey)
	for _, group := range groups {
		if model.NormalizeSiteGroupKey(group.GroupKey) != groupKey {
			continue
		}
		return group.ProjectionSuspended || group.ModelSyncStatus == model.SiteGroupModelSyncStatusMissingKey
	}
	return false
}

func buildManualSyncGroups(account *model.SiteAccount, sections manualSyncSections, mode string, tokens []model.SiteToken, models []model.SiteModel, explicitModelGroups map[string]struct{}) []model.SiteUserGroup {
	groupMap := make(map[string]model.SiteUserGroup)
	if !sections.groupsProvided || mode == ManualSyncModeMerge {
		for _, group := range account.UserGroups {
			groupKey := model.NormalizeSiteGroupKey(group.GroupKey)
			group.GroupKey = groupKey
			group.Name = model.NormalizeSiteGroupName(groupKey, group.Name)
			groupMap[groupKey] = group
		}
	}
	for _, incoming := range sections.groups {
		groupKey := model.NormalizeSiteGroupKey(incoming.GroupKey)
		incoming.GroupKey = groupKey
		incoming.Name = model.NormalizeSiteGroupName(groupKey, incoming.Name)
		if existing, ok := findSiteGroup(account.UserGroups, groupKey); ok {
			incoming.ID = existing.ID
			incoming.SiteAccountID = existing.SiteAccountID
		}
		groupMap[groupKey] = incoming
	}
	for _, token := range tokens {
		groupKey := model.NormalizeSiteGroupKey(token.GroupKey)
		if _, ok := groupMap[groupKey]; !ok {
			groupMap[groupKey] = model.SiteUserGroup{GroupKey: groupKey, Name: model.NormalizeSiteGroupName(groupKey, token.GroupName)}
		}
	}
	for _, item := range models {
		groupKey := model.NormalizeSiteGroupKey(item.GroupKey)
		if _, ok := groupMap[groupKey]; !ok {
			groupMap[groupKey] = model.SiteUserGroup{GroupKey: groupKey, Name: model.NormalizeSiteGroupName(groupKey, "")}
		}
	}
	for groupKey := range explicitModelGroups {
		if _, ok := groupMap[groupKey]; !ok {
			groupMap[groupKey] = model.SiteUserGroup{GroupKey: groupKey, Name: model.NormalizeSiteGroupName(groupKey, "")}
		}
	}
	if len(groupMap) == 0 {
		groupMap[model.SiteDefaultGroupKey] = model.SiteUserGroup{GroupKey: model.SiteDefaultGroupKey, Name: model.SiteDefaultGroupName}
	}
	groups := make([]model.SiteUserGroup, 0, len(groupMap))
	for _, group := range groupMap {
		groups = append(groups, group)
	}
	sort.Slice(groups, func(i, j int) bool { return groups[i].GroupKey < groups[j].GroupKey })
	return groups
}

func buildManualSyncPreviewGroups(
	siteRecord *model.Site,
	account *model.SiteAccount,
	groups []model.SiteUserGroup,
	tokens []model.SiteToken,
	models []model.SiteModel,
	explicitModelGroups map[string]struct{},
	mode string,
	results []siteGroupSyncResult,
) ([]ManualSyncPreviewGroup, int) {
	preparedGroups := cloneSiteGroups(groups)
	existingMap := make(map[string]model.SiteUserGroup, len(account.UserGroups))
	for _, group := range account.UserGroups {
		existingMap[model.NormalizeSiteGroupKey(group.GroupKey)] = group
	}
	resultMap := make(map[string]siteGroupSyncResult, len(results))
	for _, result := range results {
		resultMap[model.NormalizeSiteGroupKey(result.GroupKey)] = result
	}
	now := time.Now()
	for index := range preparedGroups {
		groupKey := model.NormalizeSiteGroupKey(preparedGroups[index].GroupKey)
		preparedGroups[index].GroupKey = groupKey
		var existing *model.SiteUserGroup
		if item, ok := existingMap[groupKey]; ok {
			copy := item
			existing = &copy
			preparedGroups[index].ProjectionDisabled = item.ProjectionDisabled
		}
		if result, ok := resultMap[groupKey]; ok {
			applyPersistedGroupSyncState(&preparedGroups[index], existing, result, now)
		} else if existing != nil {
			copyPersistedGroupSyncState(&preparedGroups[index], *existing)
		}
	}

	accountCopy := *account
	accountCopy.Tokens = tokens
	accountCopy.UserGroups = preparedGroups
	accountCopy.Models = models
	split := shouldSplitForAccount(&accountCopy, siteRecord)
	preview := make([]ManualSyncPreviewGroup, 0, len(preparedGroups))
	channelCount := 0
	for _, group := range preparedGroups {
		groupKey := model.NormalizeSiteGroupKey(group.GroupKey)
		groupTokens := tokensForGroup(tokens, groupKey)
		groupModels := modelsForGroup(models, groupKey)
		enabledModels := make([]model.SiteModel, 0, len(groupModels))
		routeSet := make(map[string]struct{})
		for _, item := range groupModels {
			routeType := item.RouteType
			if strings.TrimSpace(string(routeType)) == "" {
				routeType = model.InferSiteModelRouteType(item.ModelName)
			} else {
				routeType = model.NormalizeSiteModelRouteType(routeType)
			}
			item.RouteType = routeType
			routeSet[string(routeType)] = struct{}{}
			if !item.Disabled {
				enabledModels = append(enabledModels, item)
			}
		}
		routeTypes := make([]string, 0, len(routeSet))
		for routeType := range routeSet {
			routeTypes = append(routeTypes, routeType)
		}
		sort.Strings(routeTypes)
		willProject := isSiteGroupProjectionActive(siteRecord, &accountCopy, group, groupTokens) && len(enabledModels) > 0
		if willProject {
			channelCount += len(partitionSiteModelsByRouteType(enabledModels, split, siteRecord))
		}
		usable, masked := countManualTokens(groupTokens)
		action := "preserve"
		if _, ok := explicitModelGroups[groupKey]; ok {
			action = mode
		}
		preview = append(preview, ManualSyncPreviewGroup{
			GroupKey:         groupKey,
			GroupName:        model.NormalizeSiteGroupName(groupKey, group.Name),
			TokenCount:       len(groupTokens),
			UsableTokenCount: usable,
			MaskedTokenCount: masked,
			ModelCount:       len(groupModels),
			ModelAction:      action,
			RouteTypes:       routeTypes,
			WillProject:      willProject,
		})
	}
	return preview, channelCount
}

func buildManualSyncWarnings(account *model.SiteAccount, sections manualSyncSections, finalTokens []model.SiteToken, finalModels []model.SiteModel, explicitModelGroups map[string]struct{}, mode string) []string {
	warnings := make([]string, 0)
	for _, incoming := range sections.tokens {
		if !model.IsMaskedSiteTokenValue(incoming.Token) {
			continue
		}
		resolved := false
		for _, finalToken := range finalTokens {
			if sameManualTokenIdentity(incoming, finalToken) && model.IsReadySiteToken(finalToken) && !model.IsMaskedSiteTokenValue(finalToken.Token) {
				resolved = true
				break
			}
		}
		if !resolved {
			warnings = append(warnings, fmt.Sprintf("分组 %q 的 Key %q 仅包含脱敏值，导入后不会启用对应渠道", model.NormalizeSiteGroupKey(incoming.GroupKey), incoming.Name))
		}
	}
	for groupKey := range explicitModelGroups {
		if len(modelsForGroup(finalModels, groupKey)) == 0 {
			continue
		}
		if !hasUsableToken(tokensForGroup(finalTokens, groupKey)) {
			warnings = append(warnings, fmt.Sprintf("分组 %q 有模型但没有可用完整 Key，不会投影，并会清理历史投影", groupKey))
		}
	}
	if mode == ManualSyncModeReplace && sections.tokensProvided {
		manualCount := 0
		for _, token := range account.Tokens {
			if strings.TrimSpace(token.Source) == "manual" {
				manualCount++
			}
		}
		if manualCount > 0 {
			warnings = append(warnings, fmt.Sprintf("已保留 %d 个手工维护的 Key；替换模式只清理非手工 Key", manualCount))
		}
	}
	return warnings
}

func buildManualSyncMessage(sections manualSyncSections, results []siteGroupSyncResult) string {
	parts := make([]string, 0, 4)
	if sections.tokensProvided {
		parts = append(parts, fmt.Sprintf("解析 %d 个 Key", len(sections.tokens)))
	}
	if sections.groupsProvided {
		parts = append(parts, fmt.Sprintf("解析 %d 个分组", len(sections.groups)))
	}
	if len(sections.models) > 0 {
		parts = append(parts, fmt.Sprintf("处理 %d 个模型分组", len(sections.models)))
	}
	if sections.balance != nil || sections.balanceUsed != nil || sections.todayIncome != nil {
		parts = append(parts, "更新账户额度")
	}
	missingKeyCount := 0
	recoveredCount := 0
	for _, result := range results {
		switch result.Status {
		case siteGroupSyncStatusMissingKey:
			missingKeyCount++
		case siteGroupSyncStatusSynced:
			recoveredCount++
		}
	}
	if recoveredCount > 0 && len(sections.models) == 0 {
		parts = append(parts, "恢复历史模型投影")
	}
	if missingKeyCount > 0 {
		parts = append(parts, fmt.Sprintf("清理 %d 个缺少可用 Key 的分组历史投影", missingKeyCount))
	}
	if len(parts) == 0 {
		return "手动导入完成"
	}
	return "手动导入完成：" + strings.Join(parts, "，")
}

func buildManualSyncFingerprint(accountID int, mode string, format string, sections manualSyncSections) string {
	tokens := cloneSiteTokens(sections.tokens)
	sort.Slice(tokens, func(i, j int) bool {
		if tokens[i].GroupKey != tokens[j].GroupKey {
			return tokens[i].GroupKey < tokens[j].GroupKey
		}
		if tokens[i].Name != tokens[j].Name {
			return tokens[i].Name < tokens[j].Name
		}
		return tokens[i].Token < tokens[j].Token
	})
	groups := cloneSiteGroups(sections.groups)
	sort.Slice(groups, func(i, j int) bool { return groups[i].GroupKey < groups[j].GroupKey })
	models := make([]model.SiteModel, 0)
	for _, items := range sections.models {
		models = append(models, cloneSiteModels(items)...)
	}
	sortSiteModels(models)
	payload := struct {
		AccountID      int                   `json:"account_id"`
		Mode           string                `json:"mode"`
		Format         string                `json:"format"`
		TokensProvided bool                  `json:"tokens_provided"`
		GroupsProvided bool                  `json:"groups_provided"`
		Tokens         []model.SiteToken     `json:"tokens"`
		Groups         []model.SiteUserGroup `json:"groups"`
		Models         []model.SiteModel     `json:"models"`
		Balance        *float64              `json:"balance"`
		BalanceUsed    *float64              `json:"balance_used"`
		TodayIncome    *float64              `json:"today_income"`
		AccessToken    *string               `json:"access_token"`
	}{accountID, mode, format, sections.tokensProvided, sections.groupsProvided, tokens, groups, models, sections.balance, sections.balanceUsed, sections.todayIncome, sections.accessToken}
	encoded, _ := json.Marshal(payload)
	mac := hmac.New(sha256.New, manualSyncFingerprintKey)
	_, _ = mac.Write(encoded)
	return hex.EncodeToString(mac.Sum(nil))
}

func countManualImportedModels(models map[string][]model.SiteModel) int {
	count := 0
	for _, items := range models {
		count += len(items)
	}
	return count
}

func countManualTokens(tokens []model.SiteToken) (int, int) {
	usable := 0
	masked := 0
	for _, token := range tokens {
		if model.IsMaskedSiteTokenValue(token.Token) || model.NormalizeSiteTokenValueStatus(token.ValueStatus, token.Token) == model.SiteTokenValueStatusMaskedPending {
			masked++
		}
		if isUsableSiteToken(token) {
			usable++
		}
	}
	return usable, masked
}

func tokensForGroup(tokens []model.SiteToken, groupKey string) []model.SiteToken {
	groupKey = model.NormalizeSiteGroupKey(groupKey)
	result := make([]model.SiteToken, 0)
	for _, token := range tokens {
		if model.NormalizeSiteGroupKey(token.GroupKey) == groupKey {
			result = append(result, token)
		}
	}
	return result
}

func modelsForGroup(models []model.SiteModel, groupKey string) []model.SiteModel {
	groupKey = model.NormalizeSiteGroupKey(groupKey)
	result := make([]model.SiteModel, 0)
	for _, item := range models {
		if model.NormalizeSiteGroupKey(item.GroupKey) == groupKey {
			result = append(result, item)
		}
	}
	return result
}

func findSiteGroup(groups []model.SiteUserGroup, groupKey string) (model.SiteUserGroup, bool) {
	groupKey = model.NormalizeSiteGroupKey(groupKey)
	for _, group := range groups {
		if model.NormalizeSiteGroupKey(group.GroupKey) == groupKey {
			return group, true
		}
	}
	return model.SiteUserGroup{}, false
}

func upsertManualGroup(groups []model.SiteUserGroup, incoming model.SiteUserGroup) []model.SiteUserGroup {
	groupKey := model.NormalizeSiteGroupKey(incoming.GroupKey)
	incoming.GroupKey = groupKey
	incoming.Name = model.NormalizeSiteGroupName(groupKey, incoming.Name)
	for index := range groups {
		if model.NormalizeSiteGroupKey(groups[index].GroupKey) == groupKey {
			groups[index] = incoming
			return groups
		}
	}
	return append(groups, incoming)
}

func upsertManualModel(models []model.SiteModel, incoming model.SiteModel) []model.SiteModel {
	incoming.GroupKey = model.NormalizeSiteGroupKey(incoming.GroupKey)
	incoming.ModelName = strings.TrimSpace(incoming.ModelName)
	for index := range models {
		if model.NormalizeSiteGroupKey(models[index].GroupKey) == incoming.GroupKey && strings.TrimSpace(models[index].ModelName) == incoming.ModelName {
			models[index] = incoming
			return models
		}
	}
	return append(models, incoming)
}

func sortSiteModels(models []model.SiteModel) {
	sort.Slice(models, func(i, j int) bool {
		leftGroup := model.NormalizeSiteGroupKey(models[i].GroupKey)
		rightGroup := model.NormalizeSiteGroupKey(models[j].GroupKey)
		if leftGroup != rightGroup {
			return leftGroup < rightGroup
		}
		return strings.TrimSpace(models[i].ModelName) < strings.TrimSpace(models[j].ModelName)
	})
}

func cloneSiteTokens(items []model.SiteToken) []model.SiteToken {
	return append([]model.SiteToken(nil), items...)
}

func cloneSiteGroups(items []model.SiteUserGroup) []model.SiteUserGroup {
	return append([]model.SiteUserGroup(nil), items...)
}

func cloneSiteModels(items []model.SiteModel) []model.SiteModel {
	return append([]model.SiteModel(nil), items...)
}

func normalizeManualWarnings(items []string) []string {
	seen := make(map[string]struct{}, len(items))
	result := make([]string, 0, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		result = append(result, item)
	}
	return result
}
