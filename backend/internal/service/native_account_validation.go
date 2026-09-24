package service

import (
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
)

// validateNativeAccountProfile rejects incomplete or cross-family templates at
// save time; the request gate still checks the actual client version.
func validateNativeAccountProfile(account *Account, profiles *TLSFingerprintProfileService) error {
	if account == nil || !account.IsNativeWireEnabled() {
		return nil
	}
	family := ""
	switch {
	case account.IsAnthropicOAuthOrSetupToken():
		family = "claude"
	case account.IsOpenAIOAuthLike():
		family = "codex"
	default:
		return infraerrors.BadRequest("NATIVE_ACCOUNT_UNSUPPORTED", "Native mode requires a Claude or Codex OAuth account")
	}
	if !account.IsTLSFingerprintEnabled() || account.GetTLSFingerprintProfileID() <= 0 {
		return infraerrors.BadRequest("NATIVE_TLS_PROFILE_REQUIRED", "Native mode requires one bound TLS profile")
	}
	if profiles == nil {
		return infraerrors.BadRequest("NATIVE_TLS_PROFILE_UNAVAILABLE", "Native TLS profile service is unavailable")
	}
	bound := profiles.GetProfileByID(account.GetTLSFingerprintProfileID())
	verified := tlsfingerprint.VerifiedNativeProfile(family, tlsfingerprint.LatestVerifiedNativeVersion(family))
	if !tlsfingerprint.MatchesVerifiedNativeProfile(bound, verified) {
		return infraerrors.BadRequest("NATIVE_TLS_PROFILE_MISMATCH", "Native TLS profile does not match the account client family")
	}
	return nil
}
