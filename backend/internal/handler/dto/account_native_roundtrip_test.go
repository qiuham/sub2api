package dto

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

func TestNativeAccountEditDTOExposesModeAndBoundTLSProfile(t *testing.T) {
	for _, tc := range []struct {
		name, platform string
	}{
		{"Claude", service.PlatformAnthropic},
		{"Codex", service.PlatformOpenAI},
	} {
		t.Run(tc.name, func(t *testing.T) {
			account := &service.Account{ID: 7, Platform: tc.platform, Type: service.AccountTypeOAuth,
				Extra: map[string]any{"native_wire_mode": "native", "enable_tls_fingerprint": true, "tls_fingerprint_profile_id": int64(9)},
			}
			view := AccountFromServiceShallow(account)
			if view.Extra["native_wire_mode"] != "native" || view.EnableTLSFingerprint == nil || !*view.EnableTLSFingerprint ||
				view.TLSFingerprintProfileID == nil || *view.TLSFingerprintProfileID != 9 {
				t.Fatalf("edit DTO lost Native mode or TLS binding: %+v", view)
			}
		})
	}
}
