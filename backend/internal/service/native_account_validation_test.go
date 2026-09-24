package service

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/model"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
)

func TestValidateNativeAccountProfileAtSave(t *testing.T) {
	claude := tlsfingerprint.VerifiedNativeProfile("claude", "2.1.280")
	codex := tlsfingerprint.VerifiedNativeProfile("codex", "0.155.1")
	profiles := &TLSFingerprintProfileService{localCache: map[int64]*model.TLSFingerprintProfile{
		1: {ID: 1, Name: claude.Name, CipherSuites: claude.CipherSuites, Curves: claude.Curves, PointFormats: claude.PointFormats, SignatureAlgorithms: claude.SignatureAlgorithms, SupportedVersions: claude.SupportedVersions, KeyShareGroups: claude.KeyShareGroups, PSKModes: claude.PSKModes, Extensions: claude.Extensions},
		2: {ID: 2, Name: codex.Name, CipherSuites: codex.CipherSuites, Curves: codex.Curves, PointFormats: codex.PointFormats, SignatureAlgorithms: codex.SignatureAlgorithms, SupportedVersions: codex.SupportedVersions, KeyShareGroups: codex.KeyShareGroups, PSKModes: codex.PSKModes, Extensions: codex.Extensions},
	}}
	account := &Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth, Extra: map[string]any{
		"native_wire_mode": "native", "enable_tls_fingerprint": true, "tls_fingerprint_profile_id": int64(2),
	}}
	if err := validateNativeAccountProfile(account, profiles); err != nil {
		t.Fatalf("valid Codex template rejected: %v", err)
	}
	account.Extra["tls_fingerprint_profile_id"] = int64(1)
	if err := validateNativeAccountProfile(account, profiles); err == nil {
		t.Fatal("Claude template accepted for Codex account")
	}
	delete(account.Extra, "tls_fingerprint_profile_id")
	if err := validateNativeAccountProfile(account, profiles); err == nil {
		t.Fatal("missing template accepted")
	}
	account.Extra["native_wire_mode"] = "sub2api"
	if err := validateNativeAccountProfile(account, nil); err != nil {
		t.Fatalf("stock account should remain unaffected: %v", err)
	}
}

func TestBulkNativeModeRequiresMatchingProfileBeforeWrite(t *testing.T) {
	profile := tlsfingerprint.VerifiedNativeProfile("codex", "0.156.1")
	profiles := &TLSFingerprintProfileService{localCache: map[int64]*model.TLSFingerprintProfile{
		7: {ID: 7, Name: profile.Name, CipherSuites: profile.CipherSuites, Curves: profile.Curves, PointFormats: profile.PointFormats, SignatureAlgorithms: profile.SignatureAlgorithms, SupportedVersions: profile.SupportedVersions, KeyShareGroups: profile.KeyShareGroups, PSKModes: profile.PSKModes, Extensions: profile.Extensions},
	}}
	repo := &upstreamBillingProbeAccountRepo{accounts: map[int64]*Account{
		1: {ID: 1, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Extra: map[string]any{}},
	}}
	svc := &adminServiceImpl{accountRepo: repo, tlsProfileService: profiles}
	_, err := svc.BulkUpdateAccounts(context.Background(), &BulkUpdateAccountsInput{
		AccountIDs: []int64{1}, Extra: map[string]any{"native_wire_mode": "native"},
	})
	if err == nil || len(repo.bulkUpdates) != 0 {
		t.Fatalf("incomplete Native update reached repository: err=%v writes=%d", err, len(repo.bulkUpdates))
	}
	_, err = svc.BulkUpdateAccounts(context.Background(), &BulkUpdateAccountsInput{
		AccountIDs: []int64{1}, Extra: map[string]any{
			"native_wire_mode": "native", "enable_tls_fingerprint": true, "tls_fingerprint_profile_id": int64(7),
		},
	})
	if err != nil || len(repo.bulkUpdates) != 1 {
		t.Fatalf("valid Native bulk update rejected: err=%v writes=%d", err, len(repo.bulkUpdates))
	}
}

func TestAdminCreateAndEditRejectIncompleteNativeBeforeWrite(t *testing.T) {
	repo := &upstreamBillingProbeAccountRepo{accounts: map[int64]*Account{
		1: {ID: 1, Name: "existing", Platform: PlatformOpenAI, Type: AccountTypeSetupToken, Extra: map[string]any{}},
	}}
	svc := &adminServiceImpl{accountRepo: repo}
	_, err := svc.CreateAccount(context.Background(), &CreateAccountInput{
		Name: "new", Platform: PlatformOpenAI, Type: AccountTypeSetupToken,
		SkipDefaultGroupBind: true, Extra: map[string]any{"native_wire_mode": "native"},
	})
	if err == nil || len(repo.accounts) != 1 {
		t.Fatalf("incomplete Native create reached repository: err=%v accounts=%d", err, len(repo.accounts))
	}
	_, err = svc.UpdateAccount(context.Background(), 1, &UpdateAccountInput{
		Extra: map[string]any{"native_wire_mode": "native"},
	})
	if err == nil || repo.accounts[1].IsNativeWireEnabled() {
		t.Fatalf("incomplete Native edit reached repository: err=%v account=%+v", err, repo.accounts[1])
	}
}

func TestAdminNativeEditSaveReloadAndDisable(t *testing.T) {
	profile := tlsfingerprint.VerifiedNativeProfile("codex", "0.156.1")
	profiles := &TLSFingerprintProfileService{localCache: map[int64]*model.TLSFingerprintProfile{
		7: {ID: 7, Name: profile.Name, CipherSuites: profile.CipherSuites, Curves: profile.Curves, PointFormats: profile.PointFormats, SignatureAlgorithms: profile.SignatureAlgorithms, SupportedVersions: profile.SupportedVersions, KeyShareGroups: profile.KeyShareGroups, PSKModes: profile.PSKModes, Extensions: profile.Extensions},
	}}
	repo := &upstreamBillingProbeAccountRepo{accounts: map[int64]*Account{
		1: {ID: 1, Name: "Codex", Platform: PlatformOpenAI, Type: AccountTypeOAuth, Extra: map[string]any{"existing_setting": "keep-me"}},
	}}
	svc := &adminServiceImpl{accountRepo: repo, tlsProfileService: profiles}
	_, err := svc.UpdateAccount(context.Background(), 1, &UpdateAccountInput{Extra: map[string]any{
		"existing_setting": "keep-me", "native_wire_mode": "native", "enable_tls_fingerprint": true, "tls_fingerprint_profile_id": int64(7),
	}})
	if err != nil {
		t.Fatalf("enable Native: %v", err)
	}
	reloaded, err := repo.GetByID(context.Background(), 1)
	if err != nil || !reloaded.IsNativeWireEnabled() || !reloaded.IsTLSFingerprintEnabled() || reloaded.GetTLSFingerprintProfileID() != 7 || reloaded.Extra["existing_setting"] != "keep-me" {
		t.Fatalf("Native did not survive save/reload: account=%+v err=%v", reloaded, err)
	}
	_, err = svc.UpdateAccount(context.Background(), 1, &UpdateAccountInput{Extra: map[string]any{
		"existing_setting": "keep-me", "enable_tls_fingerprint": true, "tls_fingerprint_profile_id": int64(7),
	}})
	if err != nil {
		t.Fatalf("disable Native: %v", err)
	}
	reloaded, err = repo.GetByID(context.Background(), 1)
	if err != nil || reloaded.IsNativeWireEnabled() || reloaded.Extra["existing_setting"] != "keep-me" {
		t.Fatalf("Sub2API did not survive save/reload: account=%+v err=%v", reloaded, err)
	}
}

func TestNativeInvalidProfileIsExcludedFromScheduling(t *testing.T) {
	service := &GatewayService{tlsFPProfileService: &TLSFingerprintProfileService{localCache: map[int64]*model.TLSFingerprintProfile{}}}
	invalid := &Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: "active", Extra: map[string]any{"native_wire_mode": "native"}}
	if service.isAccountSchedulableForSelection(invalid) {
		t.Fatal("invalid Native account entered scheduling candidates")
	}
}
