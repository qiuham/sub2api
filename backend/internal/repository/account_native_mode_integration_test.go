//go:build integration

package repository

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestNativeModeAccountDatabaseSaveReload(t *testing.T) {
	ctx := context.Background()
	repo := newAccountRepositoryWithSQL(testEntClient(t), integrationDB, nil)
	account := &service.Account{
		Name:        fmt.Sprintf("native-save-%d", time.Now().UnixNano()),
		Platform:    service.PlatformOpenAI,
		Type:        service.AccountTypeOAuth,
		Status:      service.StatusActive,
		Concurrency: 1,
		Priority:    1,
		Credentials: map[string]any{"access_token": "fixture-token"},
		Extra:       map[string]any{"existing_setting": "preserved"},
	}
	require.NoError(t, repo.Create(ctx, account))
	t.Cleanup(func() {
		_, _ = integrationDB.ExecContext(context.Background(), "DELETE FROM scheduler_outbox WHERE account_id = $1", account.ID)
		_, _ = integrationDB.ExecContext(context.Background(), "DELETE FROM accounts WHERE id = $1", account.ID)
	})

	saved, err := repo.GetByID(ctx, account.ID)
	require.NoError(t, err)
	saved.Extra["native_wire_mode"] = "native"
	saved.Extra["enable_tls_fingerprint"] = true
	saved.Extra["tls_fingerprint_profile_id"] = int64(7)
	require.NoError(t, repo.Update(ctx, saved))
	reloaded, err := repo.GetByID(ctx, account.ID)
	require.NoError(t, err)
	require.True(t, reloaded.IsNativeWireEnabled())
	require.True(t, reloaded.IsTLSFingerprintEnabled())
	require.Equal(t, int64(7), reloaded.GetTLSFingerprintProfileID())
	require.Equal(t, "preserved", reloaded.Extra["existing_setting"])

	delete(reloaded.Extra, "native_wire_mode")
	require.NoError(t, repo.Update(ctx, reloaded))
	stock, err := repo.GetByID(ctx, account.ID)
	require.NoError(t, err)
	require.False(t, stock.IsNativeWireEnabled())
	require.Equal(t, "preserved", stock.Extra["existing_setting"])
}
