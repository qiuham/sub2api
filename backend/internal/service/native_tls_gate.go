package service

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/gin-gonic/gin"
)

// nativeTLSProfile admits only captured client versions with the corresponding
// account-bound TLS template. It must run before OAuth token use or dialing.
func nativeTLSProfile(c *gin.Context, account *Account, profiles *TLSFingerprintProfileService) (*tlsfingerprint.Profile, error) {
	if account == nil || !account.IsNativeWireEnabled() {
		return nil, nil
	}
	if c == nil || c.Request == nil || profiles == nil {
		return nil, fmt.Errorf("native client or TLS profile service unavailable")
	}
	return nativeTLSProfileForHeaders(c.Request.Header, account, profiles)
}

func nativeTLSProfileForHeaders(headers http.Header, account *Account, profiles *TLSFingerprintProfileService) (*tlsfingerprint.Profile, error) {
	if account == nil || !account.IsNativeWireEnabled() {
		return nil, nil
	}
	if headers == nil || profiles == nil {
		return nil, fmt.Errorf("native headers or TLS profile service unavailable")
	}
	family, version := "", ""
	ua := headers.Get("User-Agent")
	switch {
	case account.IsAnthropicOAuthOrSetupToken():
		family = "claude"
		if claudeCliUserAgentRe.MatchString(ua) {
			version = ExtractCLIVersion(ua)
		}
	case account.IsOpenAIOAuthLike():
		family = "codex"
		if openai.IsCodexOfficialClientRequestStrict(ua) {
			version, _ = openai.ParseCodexEngineVersion(ua)
		}
		if declared := strings.TrimSpace(headers.Get("version")); declared != "" && declared != version {
			return nil, fmt.Errorf("native client version headers disagree")
		}
	default:
		return nil, fmt.Errorf("native mode requires a supported OAuth account")
	}
	verified := tlsfingerprint.VerifiedNativeProfile(family, version)
	if verified == nil {
		return nil, fmt.Errorf("native %s client version is not verified: %q", family, version)
	}
	if !account.IsTLSFingerprintEnabled() || account.GetTLSFingerprintProfileID() <= 0 {
		return nil, fmt.Errorf("native %s account requires one bound TLS profile", family)
	}
	bound := profiles.GetProfileByID(account.GetTLSFingerprintProfileID())
	if !tlsfingerprint.MatchesVerifiedNativeProfile(bound, verified) {
		return nil, fmt.Errorf("native %s account TLS profile does not match client version %s", family, version)
	}
	return bound, nil
}

func rejectNativeTLSProfile(c *gin.Context, err error) error {
	if err != nil && c != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": gin.H{"type": "native_tls_profile_rejected", "message": err.Error()}})
	}
	return err
}
