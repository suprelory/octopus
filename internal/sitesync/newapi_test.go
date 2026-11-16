package sitesync

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	dbpkg "github.com/bestruirui/octopus/internal/db"
	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/op"
)

func TestNewAPITokenInventoryPagesAndReveals(t *testing.T) {
	pages, reveals, revealed := 0, 0, 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Header.Get("Authorization") != "Bearer management-token" {
			t.Error("missing management authentication")
		}
		switch r.URL.Path {
		case "/api/token/":
			pages++
			page, _ := strconv.Atoi(r.URL.Query().Get("p"))
			items := []any{}
			for id := (page-1)*100 + 1; id <= page*100 && id <= 101; id++ {
				items = append(items, map[string]any{"id": id, "name": fmt.Sprint(id), "key": fmt.Sprintf("%04d**********tail", id), "status": 1, "group": "default"})
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "data": map[string]any{"items": items, "total": 101, "page": page, "page_size": 100}})
		case "/api/token/batch/keys":
			if r.Method != http.MethodPost {
				t.Error("key reveal must use POST")
			}
			reveals++
			var body struct {
				IDs []int `json:"ids"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
				return
			}
			if len(body.IDs) > 100 {
				t.Error("reveal batch exceeded upstream limit")
			}
			keys := map[string]string{}
			for _, id := range body.IDs {
				keys[strconv.Itoa(id)] = fmt.Sprintf("%04d-full-key-tail", id)
				revealed++
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "data": map[string]any{"keys": keys}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	tokens, err := fetchManagementTokens(t.Context(), &model.Site{Platform: model.SitePlatformNewAPI, BaseURL: server.URL}, &model.SiteAccount{}, "management-token")
	if err != nil {
		t.Fatal(err)
	}
	if len(tokens) != 101 || pages != 2 || reveals != 2 || revealed != 101 {
		t.Fatalf("inventory=%d pages=%d reveals=%d revealed=%d", len(tokens), pages, reveals, revealed)
	}
	for _, token := range tokens {
		if !isUsableSiteToken(token) || token.UpstreamID <= 0 {
			t.Fatalf("token %q is not ready", token.Name)
		}
	}
}

func TestNewAPITokenInventoryRejectsIncompleteResponses(t *testing.T) {
	for _, mode := range []string{"second_page_fails", "repeated_page", "changed_total", "missing_items", "missing_revealed_key", "reveal_forbidden"} {
		t.Run(mode, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if r.URL.Path == "/api/token/batch/keys" {
					if mode == "reveal_forbidden" {
						w.WriteHeader(http.StatusForbidden)
					}
					_, _ = w.Write([]byte(`{"success":true,"data":{"keys":{}}}`))
					return
				}
				page, _ := strconv.Atoi(r.URL.Query().Get("p"))
				if mode == "missing_items" {
					_, _ = w.Write([]byte(`{"success":true,"data":{}}`))
					return
				}
				if page > 1 && mode == "second_page_fails" {
					w.WriteHeader(http.StatusServiceUnavailable)
					return
				}
				total := 2
				if mode == "missing_revealed_key" || mode == "reveal_forbidden" {
					total = 1
				}
				if page > 1 && mode == "changed_total" {
					total = 3
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "data": map[string]any{"total": total, "items": []any{map[string]any{"id": 7, "key": "abcd**********tail", "status": 1, "group": "default"}}}})
			}))
			defer server.Close()
			tokens, err := fetchManagementTokens(t.Context(), &model.Site{Platform: model.SitePlatformNewAPI, BaseURL: server.URL}, &model.SiteAccount{}, "management-token")
			if err == nil || tokens != nil {
				t.Fatalf("incomplete inventory must fail without returning a replacement snapshot: err=%v count=%d", err, len(tokens))
			}
		})
	}
}

func TestNewAPITokenRevealCompatibilityAndSavedKey(t *testing.T) {
	for _, saved := range []bool{false, true} {
		t.Run(fmt.Sprintf("saved=%v", saved), func(t *testing.T) {
			reveals := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch r.URL.Path {
				case "/api/token/":
					_, _ = w.Write([]byte(`{"data":{"items":[{"id":7,"name":"renamed","key":"abcd**********tail","group":"vip","status":1}]}}`))
				case "/api/token/batch/keys":
					http.NotFound(w, r)
				case "/api/token/7/key":
					reveals++
					_, _ = w.Write([]byte(`{"success":true,"data":{"key":"abcd-full-key-tail"}}`))
				default:
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			account := &model.SiteAccount{}
			if saved {
				account.Tokens = []model.SiteToken{{UpstreamID: 7, Token: "sk-abcd-full-key-tail", Name: "old-name", GroupKey: "default", Enabled: false}}
			}
			tokens, err := fetchManagementTokens(t.Context(), &model.Site{Platform: model.SitePlatformNewAPI, BaseURL: server.URL}, account, "management-token")
			if err != nil {
				t.Fatal(err)
			}
			if len(tokens) != 1 || !isUsableSiteToken(tokens[0]) {
				t.Fatal("full key was not recovered before model fetching")
			}
			if saved && (reveals != 0 || tokens[0].Enabled) {
				t.Fatal("saved key/local disabled state was not reused by upstream ID")
			}
			if !saved && reveals != 1 {
				t.Fatal("individual reveal compatibility failed")
			}
		})
	}
}

func TestNewAPITokenStatusDoesNotOverrideLocalSwitch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		items := []any{}
		for status := 1; status <= 4; status++ {
			items = append(items, map[string]any{"id": status, "name": fmt.Sprint(status), "key": fmt.Sprintf("key-%d", status), "group": "default", "status": status})
		}
		items = append(items, map[string]any{"id": 5, "key": "expired", "status": 1, "group": "default", "expired_time": time.Now().Add(-time.Hour).Unix()}, map[string]any{"id": 6, "key": "exhausted", "status": 1, "group": "default", "remain_quota": 0, "unlimited_quota": false})
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"items": items}})
	}))
	defer server.Close()
	tokens, err := fetchManagementTokens(t.Context(), &model.Site{Platform: model.SitePlatformNewAPI, BaseURL: server.URL}, &model.SiteAccount{}, "management-token")
	if err != nil {
		t.Fatal(err)
	}
	existing := []model.SiteToken{{ID: 1, UpstreamID: 2, Name: "2", Token: "key-2", GroupKey: "default", Enabled: true}}
	merged := mergePersistedSiteTokens(1, existing, tokens, time.Now())
	for _, token := range merged {
		if isUsableSiteToken(token) != (token.UpstreamID == 1) {
			t.Fatalf("incorrect availability for upstream token %d status %d", token.UpstreamID, token.UpstreamStatus)
		}
		if !token.Enabled {
			t.Fatalf("upstream status overwrote local switch for token %d", token.UpstreamID)
		}
	}
}

func TestNewAPISyncUsesTokenModelsAndEffectiveGroups(t *testing.T) {
	const fullKey = "abcd-full-key-tail"
	for _, tc := range []struct {
		name, group, owner              string
		direct, restricted, empty       bool
		manualRoute, pricingUnavailable bool
		modelStatus                     int
	}{
		{name: "fresh_masked_key", group: "default", owner: "default"},
		{name: "auto", group: "auto", owner: "default"},
		{name: "auto_manual_route", group: "auto", owner: "default", manualRoute: true},
		{name: "auto_cached_route", group: "auto", owner: "default", pricingUnavailable: true},
		{name: "inherited_vip", owner: "vip"},
		{name: "direct_vip", group: "vip", owner: "vip", direct: true},
		{name: "restricted_empty", group: "default", owner: "default", restricted: true, empty: true},
		{name: "authoritative_empty", group: "default", owner: "default", empty: true},
		{name: "unauthorized", group: "default", owner: "default", modelStatus: 401},
		{name: "forbidden", group: "default", owner: "default", modelStatus: 403},
		{name: "outage", group: "default", owner: "default", modelStatus: 503},
		{name: "restricted_legacy_endpoint", group: "default", owner: "default", restricted: true, modelStatus: 404},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := setupProjectTestDB(t)
			userModelCalls, revealCalls := 0, 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				var response any
				switch r.URL.Path {
				case "/api/token/":
					response = map[string]any{"success": true, "data": map[string]any{"total": 1, "items": []any{map[string]any{"id": 42, "name": "primary", "key": "abcd**********tail", "group": tc.group, "status": 1, "model_limits_enabled": tc.restricted, "model_limits": "gpt-unavailable"}}}}
				case "/api/token/batch/keys":
					revealCalls++
					response = map[string]any{"success": true, "data": map[string]any{"keys": map[string]string{"42": fullKey}}}
				case "/api/user/self/groups":
					groups := map[string]any{tc.owner: map[string]any{"ratio": 1, "desc": tc.owner}}
					if tc.group == "auto" {
						groups["auto"] = map[string]any{"ratio": "auto", "desc": "Auto"}
					}
					response = map[string]any{"success": true, "data": groups}
				case "/v1/models":
					if r.Header.Get("Authorization") != "Bearer sk-"+fullKey {
						t.Error("model request used a masked or unnormalized key")
					}
					if tc.modelStatus != 0 {
						w.WriteHeader(tc.modelStatus)
						response = map[string]any{"error": map[string]string{"message": "model query failed"}}
					} else {
						items := []any{}
						if !tc.empty {
							items = append(items, map[string]string{"id": "gpt-4o"})
						}
						response = map[string]any{"success": true, "object": "list", "data": items}
					}
				case "/api/user/models":
					userModelCalls++
					response = map[string]any{"success": true, "data": []string{"gpt-4o"}}
				case "/api/pricing":
					if tc.pricingUnavailable {
						w.WriteHeader(http.StatusServiceUnavailable)
						return
					}
					response = map[string]any{"success": true, "auto_groups": []string{tc.owner}, "data": []any{map[string]any{"model_name": "gpt-4o", "enable_groups": []string{tc.owner}, "supported_endpoint_types": []string{"openai"}}}}
				case "/api/user/self":
					response = map[string]any{"success": true, "data": map[string]any{"id": 7, "group": tc.owner, "quota": 5000000, "used_quota": 500000, "today_income": 0}}
				default:
					http.NotFound(w, r)
					return
				}
				_ = json.NewEncoder(w).Encode(response)
			}))
			defer server.Close()
			site := model.Site{Name: "new-api-contract", Kind: model.SiteKindRelay, Platform: model.SitePlatformNewAPI, BaseURL: server.URL, Enabled: true}
			if err := dbpkg.GetDB().Create(&site).Error; err != nil {
				t.Fatal(err)
			}
			userID := 7
			account := model.SiteAccount{SiteID: site.ID, Name: "account", CredentialType: model.SiteCredentialTypeAccessToken, AccessToken: "management-token", PlatformUserID: &userID, Enabled: true}
			if tc.direct {
				account.CredentialType = model.SiteCredentialTypeAPIKey
				account.APIKey = fullKey
			}
			if err := dbpkg.GetDB().Create(&account).Error; err != nil {
				t.Fatal(err)
			}
			if tc.manualRoute || tc.pricingUnavailable {
				metadata := model.SiteModelRouteMetadata{RouteSupported: true, RouteType: model.SiteModelRouteTypeAnthropic, EnableGroups: []string{"default"}, SupportedEndpointTypes: []string{"anthropic"}}
				existing := model.SiteModel{SiteAccountID: account.ID, GroupKey: tc.group, ModelName: "gpt-4o", Source: "sync", RouteType: model.SiteModelRouteTypeAnthropic, ManualOverride: tc.manualRoute, RouteRawPayload: metadata.Marshal()}
				if err := dbpkg.GetDB().Create(&existing).Error; err != nil {
					t.Fatal(err)
				}
			}
			result, err := runAccountSync(ctx, account.ID, nil)
			if tc.modelStatus != 0 {
				if err == nil {
					t.Fatalf("HTTP %d must not be hidden by the user catalog", tc.modelStatus)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			if userModelCalls != 0 {
				t.Fatal("key-specific models were replaced by the user catalog")
			}
			if !tc.direct && revealCalls != 1 {
				t.Fatalf("expected one key reveal, got %d", revealCalls)
			}
			wantChannels := 1
			if tc.empty || tc.modelStatus != 0 {
				wantChannels = 0
			}
			if result == nil || result.ChannelCount != wantChannels {
				t.Fatalf("expected %d channels, got %+v", wantChannels, result)
			}
			if wantChannels == 1 {
				channel, err := op.ChannelGet(result.ManagedChannels[0], ctx)
				if err != nil {
					t.Fatal(err)
				}
				if channel.Model != "gpt-4o" || len(channel.Keys) != 1 || channel.Keys[0].ChannelKey != "sk-"+fullKey {
					t.Fatal("projected channel does not match the token model response")
				}
				if (tc.manualRoute || tc.pricingUnavailable) && channel.Type != model.SiteModelRouteTypeAnthropic.ToOutboundType() {
					t.Fatal("existing route was overwritten while confirming token membership")
				}
			}
		})
	}
}

func TestNewAPISyncPreservesUnknownBalances(t *testing.T) {
	for _, tc := range []struct {
		name, profile                     string
		direct                            bool
		wantBalance, wantUsed, wantIncome float64
		wantStatus                        model.SiteExecutionStatus
	}{
		{name: "profile_unavailable", wantBalance: 17, wantUsed: 3, wantIncome: 2, wantStatus: model.SiteExecutionStatusPartial},
		{name: "legitimate_zero", profile: `{"id":7,"quota":0,"used_quota":0,"today_income":0}`, wantStatus: model.SiteExecutionStatusSuccess},
		{name: "optional_fields_unavailable", profile: `{"id":7,"quota":2500000}`, wantBalance: 5, wantUsed: 3, wantIncome: 2, wantStatus: model.SiteExecutionStatusSuccess},
		{name: "direct_key", direct: true, wantBalance: 17, wantUsed: 3, wantIncome: 2, wantStatus: model.SiteExecutionStatusSuccess},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := setupProjectTestDB(t)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch r.URL.Path {
				case "/api/token/":
					_, _ = w.Write([]byte(`{"data":{"items":[{"id":1,"name":"primary","key":"full-key","group":"default","status":1}]}}`))
				case "/api/user/self/groups":
					_, _ = w.Write([]byte(`{"data":{"default":{"desc":"Default","ratio":1}}}`))
				case "/v1/models":
					_, _ = w.Write([]byte(`{"data":[{"id":"gpt-4o"}]}`))
				case "/api/pricing":
					_, _ = w.Write([]byte(`{"data":[{"model_name":"gpt-4o","enable_groups":["default"],"supported_endpoint_types":["openai"]}]}`))
				case "/api/user/self":
					if tc.profile == "" {
						w.WriteHeader(http.StatusServiceUnavailable)
						return
					}
					_, _ = fmt.Fprintf(w, `{"success":true,"data":%s}`, tc.profile)
				case "/api/log/self":
					w.WriteHeader(http.StatusServiceUnavailable)
				default:
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			site := model.Site{Name: "balance-test", Platform: model.SitePlatformNewAPI, BaseURL: server.URL, Enabled: true}
			if err := dbpkg.GetDB().Create(&site).Error; err != nil {
				t.Fatal(err)
			}
			id := 7
			account := model.SiteAccount{SiteID: site.ID, Name: "balance-account", CredentialType: model.SiteCredentialTypeAccessToken, AccessToken: "management-token", PlatformUserID: &id, Enabled: true, Balance: 17, BalanceUsed: 3, TodayIncome: 2}
			if tc.direct {
				account.CredentialType = model.SiteCredentialTypeAPIKey
				account.APIKey = "full-key"
			}
			if err := dbpkg.GetDB().Create(&account).Error; err != nil {
				t.Fatal(err)
			}
			result, err := runAccountSync(ctx, account.ID, nil)
			if err != nil {
				t.Fatal(err)
			}
			stored, err := op.SiteAccountGet(account.ID, ctx)
			if err != nil {
				t.Fatal(err)
			}
			if result.Status != tc.wantStatus || stored.Balance != tc.wantBalance || stored.BalanceUsed != tc.wantUsed || stored.TodayIncome != tc.wantIncome {
				t.Fatalf("status=%s balance=%v used=%v income=%v", result.Status, stored.Balance, stored.BalanceUsed, stored.TodayIncome)
			}
		})
	}
}

func TestNewAPISyncRetainsInventoryOnFailureAndHonorsUpstreamState(t *testing.T) {
	ctx := setupProjectTestDB(t)
	mode := "normal"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/token/":
			if mode == "pagination_failure" && r.URL.Query().Get("p") == "2" {
				w.WriteHeader(http.StatusServiceUnavailable)
				return
			}
			total, status := 1, 1
			if mode == "pagination_failure" {
				total = 2
			}
			if mode == "disabled" {
				status = 2
			}
			key := "abcd**********tail"
			if mode == "reveal_failure" {
				key = "newk**********tail"
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"total": total, "items": []any{map[string]any{"id": 7, "name": "primary", "key": key, "group": "default", "status": status}}}})
		case "/api/token/batch/keys":
			if mode == "reveal_failure" {
				w.WriteHeader(http.StatusForbidden)
				return
			}
			_, _ = w.Write([]byte(`{"success":true,"data":{"keys":{"7":"abcd-full-key-tail"}}}`))
		case "/api/user/self/groups":
			_, _ = w.Write([]byte(`{"data":{"default":{"desc":"Default","ratio":1}}}`))
		case "/v1/models":
			_, _ = w.Write([]byte(`{"data":[{"id":"gpt-4o"}]}`))
		case "/api/pricing":
			_, _ = w.Write([]byte(`{"data":[{"model_name":"gpt-4o","enable_groups":["default"],"supported_endpoint_types":["openai"]}]}`))
		case "/api/user/self":
			_, _ = w.Write([]byte(`{"success":true,"data":{"id":7,"quota":0,"used_quota":0,"today_income":0}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	site := model.Site{Name: "inventory-test", Platform: model.SitePlatformNewAPI, BaseURL: server.URL, Enabled: true}
	if err := dbpkg.GetDB().Create(&site).Error; err != nil {
		t.Fatal(err)
	}
	id := 7
	account := model.SiteAccount{SiteID: site.ID, Name: "account", CredentialType: model.SiteCredentialTypeAccessToken, AccessToken: "management-token", PlatformUserID: &id, Enabled: true}
	if err := dbpkg.GetDB().Create(&account).Error; err != nil {
		t.Fatal(err)
	}
	initial, err := runAccountSync(ctx, account.ID, nil)
	if err != nil || initial.ChannelCount != 1 {
		t.Fatalf("initial sync failed: %+v %v", initial, err)
	}
	for _, failure := range []string{"pagination_failure", "reveal_failure"} {
		mode = failure
		if _, err := runAccountSync(ctx, account.ID, nil); err == nil {
			t.Fatalf("%s should fail", failure)
		}
		stored, err := op.SiteAccountGet(account.ID, ctx)
		if err != nil {
			t.Fatal(err)
		}
		if len(stored.Tokens) != 1 || stored.Tokens[0].Token != "abcd-full-key-tail" || len(stored.ChannelBindings) != 1 || stored.ChannelBindings[0].ChannelID != initial.ManagedChannels[0] {
			t.Fatalf("%s altered the saved inventory", failure)
		}
	}
	mode = "disabled"
	disabled, err := runAccountSync(ctx, account.ID, nil)
	if err == nil || disabled == nil || disabled.ChannelCount != 0 {
		t.Fatal("disabled upstream key must not remain in a channel")
	}
	stored, err := op.SiteAccountGet(account.ID, ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(stored.ChannelBindings) != 0 || !stored.Tokens[0].Enabled {
		t.Fatal("upstream disable must remove channels without changing the local switch")
	}
	mode = "normal"
	restored, err := runAccountSync(ctx, account.ID, nil)
	if err != nil || restored.ChannelCount != 1 {
		t.Fatal("re-enabled upstream token did not restore its channel")
	}
	if err := dbpkg.GetDB().Model(&model.SiteToken{}).Where("site_account_id = ?", account.ID).Update("enabled", false).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := runAccountSync(ctx, account.ID, nil); err != nil {
		t.Fatal(err)
	}
	stored, err = op.SiteAccountGet(account.ID, ctx)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Tokens[0].Enabled {
		t.Fatal("sync reset the local disabled switch")
	}
}

func TestNewAPITokenSchemaUpgradePreservesExistingKeys(t *testing.T) {
	setupProjectTestDB(t)
	database := dbpkg.GetDB()
	site := model.Site{Name: "upgrade-test", Platform: model.SitePlatformNewAPI, BaseURL: "https://example.com", Enabled: true}
	if err := database.Create(&site).Error; err != nil {
		t.Fatal(err)
	}
	account := model.SiteAccount{ID: 7, SiteID: site.ID, Name: "existing-account", CredentialType: model.SiteCredentialTypeAccessToken, Enabled: true}
	if err := database.Create(&account).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.Migrator().DropTable(&model.SiteToken{}); err != nil {
		t.Fatal(err)
	}
	// The previous schema had a true default and no upstream identity/status.
	if err := database.Exec(`CREATE TABLE site_tokens (
		id integer PRIMARY KEY AUTOINCREMENT, site_account_id integer NOT NULL,
		name text, token text NOT NULL, value_status varchar(32) NOT NULL DEFAULT 'ready',
		group_key varchar(128), group_name text, enabled numeric DEFAULT true,
		source text, is_default numeric DEFAULT false, last_sync_at datetime
	)`).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.Exec(`INSERT INTO site_tokens (id, site_account_id, name, token, group_key, enabled) VALUES (1, 7, 'existing', 'legacy-full-key', 'default', 0)`).Error; err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := database.AutoMigrate(&model.SiteToken{}); err != nil {
			t.Fatal(err)
		}
		var token model.SiteToken
		if err := database.First(&token, 1).Error; err != nil {
			t.Fatal(err)
		}
		if token.Token != "legacy-full-key" || token.Enabled || token.UpstreamID != 0 || !model.IsSiteTokenUpstreamAvailable(token) {
			t.Fatal("schema upgrade changed an existing credential or its local switch")
		}
	}
}
