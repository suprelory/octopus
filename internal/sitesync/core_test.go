package sitesync

import (
	"errors"
	"strings"
	"testing"

	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/op"
)

func TestMarkAccountSyncFailurePersistsFailedStatus(t *testing.T) {
	ctx := setupProjectTestDB(t)
	_, account := createProjectionFixture(t, ctx)

	markAccountSyncFailure(ctx, account.ID, errors.New("site channel projection failed"), "")

	reloaded, err := op.SiteAccountGet(account.ID, ctx)
	if err != nil {
		t.Fatalf("SiteAccountGet failed: %v", err)
	}
	if reloaded.LastSyncStatus != model.SiteExecutionStatusFailed {
		t.Fatalf("expected failed last_sync_status, got %q", reloaded.LastSyncStatus)
	}
	if !strings.Contains(reloaded.LastSyncMessage, "projection failed") {
		t.Fatalf("expected projection failure message, got %q", reloaded.LastSyncMessage)
	}
}
