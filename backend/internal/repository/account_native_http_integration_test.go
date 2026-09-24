//go:build integration

package repository

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	adminhandler "github.com/Wei-Shaw/sub2api/internal/handler/admin"
	"github.com/Wei-Shaw/sub2api/internal/model"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestNativeAccountHTTPServiceDatabaseRoundTrip(t *testing.T) {
	for _, tc := range []struct {
		family, platform, version string
	}{
		{"claude", service.PlatformAnthropic, "2.1.281"},
		{"codex", service.PlatformOpenAI, "0.156.1"},
	} {
		t.Run(tc.family, func(t *testing.T) {
			ctx := context.Background()
			client := testEntClient(t)
			accounts := newAccountRepositoryWithSQL(client, integrationDB, nil)
			profiles := NewTLSFingerprintProfileRepository(client)
			verified := tlsfingerprint.VerifiedNativeProfile(tc.family, tc.version)
			profile, err := profiles.Create(ctx, &model.TLSFingerprintProfile{
				Name: fmt.Sprintf("native-http-%d", time.Now().UnixNano()), EnableGREASE: verified.EnableGREASE,
				CipherSuites: verified.CipherSuites, Curves: verified.Curves, PointFormats: verified.PointFormats,
				SignatureAlgorithms: verified.SignatureAlgorithms, ALPNProtocols: verified.ALPNProtocols,
				SupportedVersions: verified.SupportedVersions, KeyShareGroups: verified.KeyShareGroups,
				PSKModes: verified.PSKModes, Extensions: verified.Extensions,
			})
			require.NoError(t, err)
			tlsProfiles := service.NewTLSFingerprintProfileService(profiles, nil)
			account := &service.Account{
				Name:     fmt.Sprintf("native-http-account-%d", time.Now().UnixNano()),
				Platform: tc.platform, Type: service.AccountTypeOAuth,
				Status: service.StatusActive, Concurrency: 1, Priority: 1,
				Credentials: map[string]any{"access_token": "fixture-token"},
				Extra:       map[string]any{"existing_setting": "preserved"},
			}
			require.NoError(t, accounts.Create(ctx, account))
			t.Cleanup(func() {
				_, _ = integrationDB.ExecContext(context.Background(), "DELETE FROM scheduler_outbox WHERE account_id = $1", account.ID)
				_, _ = integrationDB.ExecContext(context.Background(), "DELETE FROM accounts WHERE id = $1", account.ID)
				_ = profiles.Delete(context.Background(), profile.ID)
			})

			cfg := &config.Config{RunMode: config.RunModeSimple}
			adminService := service.NewAdminService(cfg,
				nil, nil, accounts, nil, nil, nil, nil, nil, nil, nil, nil, nil,
				client, nil, nil, nil, nil, nil, nil, nil, nil, nil, tlsProfiles)
			handler := adminhandler.NewAccountHandler(adminService, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
			gin.SetMode(gin.TestMode)
			router := gin.New()
			router.PUT("/accounts/:id", handler.Update)
			router.GET("/accounts/:id", handler.GetByID)

			request := func(method, body string) *httptest.ResponseRecorder {
				t.Helper()
				req := httptest.NewRequest(method, fmt.Sprintf("/accounts/%d", account.ID), bytes.NewBufferString(body))
				req.Header.Set("Content-Type", "application/json")
				out := httptest.NewRecorder()
				router.ServeHTTP(out, req)
				return out
			}
			read := func() struct {
				Extra                   map[string]any `json:"extra"`
				EnableTLSFingerprint    bool           `json:"enable_tls_fingerprint"`
				TLSFingerprintProfileID int64          `json:"tls_fingerprint_profile_id"`
			} {
				t.Helper()
				out := request(http.MethodGet, "")
				require.Equal(t, http.StatusOK, out.Code, out.Body.String())
				var result struct {
					Data struct {
						Extra                   map[string]any `json:"extra"`
						EnableTLSFingerprint    bool           `json:"enable_tls_fingerprint"`
						TLSFingerprintProfileID int64          `json:"tls_fingerprint_profile_id"`
					} `json:"data"`
				}
				require.NoError(t, json.Unmarshal(out.Body.Bytes(), &result))
				return result.Data
			}

			enable := fmt.Sprintf(`{"extra":{"existing_setting":"preserved","native_wire_mode":"native","enable_tls_fingerprint":true,"tls_fingerprint_profile_id":%d}}`, profile.ID)
			require.Equal(t, http.StatusOK, request(http.MethodPut, enable).Code)
			native := read()
			require.Equal(t, "native", native.Extra["native_wire_mode"])
			require.True(t, native.EnableTLSFingerprint)
			require.Equal(t, profile.ID, native.TLSFingerprintProfileID)
			require.Equal(t, "preserved", native.Extra["existing_setting"])

			bad := `{"extra":{"existing_setting":"preserved","native_wire_mode":"native","enable_tls_fingerprint":true,"tls_fingerprint_profile_id":999999}}`
			require.Equal(t, http.StatusBadRequest, request(http.MethodPut, bad).Code)
			require.Equal(t, profile.ID, read().TLSFingerprintProfileID)

			stock := fmt.Sprintf(`{"extra":{"existing_setting":"preserved","enable_tls_fingerprint":true,"tls_fingerprint_profile_id":%d}}`, profile.ID)
			require.Equal(t, http.StatusOK, request(http.MethodPut, stock).Code)
			require.NotEqual(t, "native", read().Extra["native_wire_mode"])
		})
	}
}
