package op

import (
	"encoding/json"
	"github.com/bestruirui/octopus/internal/db"
	"github.com/bestruirui/octopus/internal/model"
	"testing"
)

func TestCheckinBackupRemapsLinkedAccountAndPreservesCookie(t *testing.T) {
	ctx := setupBackupTestDB(t)
	occupied := &model.Site{Name: "Existing", Platform: model.SitePlatformAPI, BaseURL: "https://existing.example"}
	if err := SiteCreate(occupied, ctx); err != nil {
		t.Fatal(err)
	}
	if err := SiteAccountCreate(&model.SiteAccount{SiteID: occupied.ID, Name: "Existing", CredentialType: model.SiteCredentialTypeAPIKey, APIKey: "existing-key"}, ctx); err != nil {
		t.Fatal(err)
	}
	sourceSiteID, sourceID := 1, 1
	dump := &model.DBDump{Version: 1, Sites: []model.Site{
		{ID: 2, Name: "Rewards", Kind: model.SiteKindCheckin, LinkedSiteID: &sourceSiteID, Platform: model.SitePlatformOneAPI, BaseURL: "https://platform.example"},
		{ID: 1, Name: "Subscription", Kind: model.SiteKindRelay, Platform: model.SitePlatformOneAPI, BaseURL: "https://platform.example"},
		{ID: 3, Name: "External", Kind: model.SiteKindCheckin, Platform: model.SitePlatformAPI, BaseURL: "https://external.example", CheckinHTTPEnabled: true, CheckinHTTPPath: "/daily"},
	}, SiteAccounts: []model.SiteAccount{
		{ID: 2, SiteID: 2, Name: "Rewards", CredentialType: model.SiteCredentialTypeLinkedAccount, LinkedAccountID: &sourceID},
		{ID: 1, SiteID: 1, Name: "Source", CredentialType: model.SiteCredentialTypeAccessToken, AccessToken: "source-token"},
		{ID: 3, SiteID: 3, Name: "Cookie", CredentialType: model.SiteCredentialTypeCookie, Cookie: "session=backup-cookie"},
	}}
	if _, err := DBImportIncremental(ctx, dump); err != nil {
		t.Fatal(err)
	}
	var account, cookie model.SiteAccount
	if err := db.GetDB().Where("name = ?", "Rewards").First(&account).Error; err != nil {
		t.Fatal(err)
	}
	site, err := SiteGet(account.SiteID, ctx)
	if err != nil {
		t.Fatal(err)
	}
	source, err := SiteCheckinLinkedAccount(site, &account, ctx)
	if err != nil || source.ID == sourceID || source.AccessToken != "source-token" {
		t.Fatalf("binding not remapped: %+v, %v", source, err)
	}
	if err := db.GetDB().Where("name = ?", "Cookie").First(&cookie).Error; err != nil {
		t.Fatal(err)
	}
	if cookie.Cookie != "session=backup-cookie" {
		t.Fatal("cookie lost in backup")
	}
	if err := SiteAccountDel(source.ID, ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := DBImportIncremental(ctx, dump); err != nil {
		t.Fatal(err)
	}
	account = model.SiteAccount{}
	if err := db.GetDB().Where("name = ?", "Rewards").First(&account).Error; err != nil {
		t.Fatal(err)
	}
	if account.LinkedAccountID != nil {
		t.Fatal("reimport rebound a deleted source")
	}
}

func TestCheckinCredentialsRejectWrongSiteAndAcceptCookieUpdates(t *testing.T) {
	ctx := setupBackupTestDB(t)
	site := &model.Site{Name: "Subscription", Platform: model.SitePlatformOneAPI, BaseURL: "https://platform.example"}
	if err := SiteCreate(site, ctx); err != nil {
		t.Fatal(err)
	}
	if err := SiteAccountCreate(&model.SiteAccount{SiteID: site.ID, Name: "Invalid", CredentialType: model.SiteCredentialTypeCookie, Cookie: "session=secret"}, ctx); err == nil {
		t.Fatal("subscription accepted cookie credentials")
	}
	source := &model.SiteAccount{SiteID: site.ID, Name: "Source", CredentialType: model.SiteCredentialTypeAccessToken, AccessToken: "token"}
	if err := SiteAccountCreate(source, ctx); err != nil {
		t.Fatal(err)
	}
	checkin := &model.Site{Name: "External", Kind: model.SiteKindCheckin, Platform: site.Platform, BaseURL: "https://other.example", LinkedSiteID: &site.ID}
	if err := SiteCreate(checkin, ctx); err != nil {
		t.Fatal(err)
	}
	account := &model.SiteAccount{SiteID: checkin.ID, Name: "Linked", CredentialType: model.SiteCredentialTypeLinkedAccount, LinkedAccountID: &source.ID}
	if err := SiteAccountCreate(account, ctx); err == nil {
		t.Fatal("platform credentials could be sent to another URL")
	}
	custom, path := true, "/daily"
	if _, err := SiteUpdate(&model.SiteUpdateRequest{ID: checkin.ID, CheckinHTTPEnabled: &custom, CheckinHTTPPath: &path}, ctx); err != nil {
		t.Fatal(err)
	}
	account.CredentialType, account.LinkedAccountID, account.Cookie = model.SiteCredentialTypeCookie, nil, "session=old"
	if err := SiteAccountCreate(account, ctx); err != nil {
		t.Fatal(err)
	}
	var req model.SiteAccountUpdateRequest
	if err := json.Unmarshal([]byte(`{"cookie":"session=new","linked_account_id":null}`), &req); err != nil {
		t.Fatal(err)
	}
	req.ID = account.ID
	updated, err := SiteAccountUpdate(&req, ctx)
	if err != nil || updated.Cookie != "session=new" || !req.LinkedAccountIDSet || updated.LinkedAccountID != nil {
		t.Fatalf("cookie update: %+v, %v", updated, err)
	}
}
