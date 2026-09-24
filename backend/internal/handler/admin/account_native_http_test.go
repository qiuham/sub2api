package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type nativeHTTPAccountService struct {
	*stubAdminService
	account service.Account
}

func (s *nativeHTTPAccountService) GetAccount(context.Context, int64) (*service.Account, error) {
	return &s.account, nil
}

func (s *nativeHTTPAccountService) UpdateAccount(_ context.Context, _ int64, input *service.UpdateAccountInput) (*service.Account, error) {
	s.account.Extra = input.Extra
	return &s.account, nil
}

func TestNativeAccountHTTPUpdateThenGet(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc := &nativeHTTPAccountService{stubAdminService: newStubAdminService(), account: service.Account{
		ID: 7, Name: "Codex", Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth,
		Status: service.StatusActive, Extra: map[string]any{"existing_setting": "preserved"},
	}}
	h := NewAccountHandler(svc, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	h.cfg = &config.Config{RunMode: config.RunModeSimple}
	router := gin.New()
	router.PUT("/accounts/:id", h.Update)
	router.GET("/accounts/:id", h.GetByID)

	for _, tc := range []struct {
		name, body string
		wantNative bool
	}{
		{"enable", `{"extra":{"existing_setting":"preserved","native_wire_mode":"native","enable_tls_fingerprint":true,"tls_fingerprint_profile_id":7}}`, true},
		{"disable", `{"extra":{"existing_setting":"preserved","enable_tls_fingerprint":true,"tls_fingerprint_profile_id":7}}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPut, "/accounts/7", bytes.NewBufferString(tc.body))
			request.Header.Set("Content-Type", "application/json")
			update := httptest.NewRecorder()
			router.ServeHTTP(update, request)
			require.Equal(t, http.StatusOK, update.Code, update.Body.String())

			get := httptest.NewRecorder()
			router.ServeHTTP(get, httptest.NewRequest(http.MethodGet, "/accounts/7", nil))
			require.Equal(t, http.StatusOK, get.Code, get.Body.String())
			var result struct {
				Data struct {
					Extra                   map[string]any `json:"extra"`
					EnableTLSFingerprint    bool           `json:"enable_tls_fingerprint"`
					TLSFingerprintProfileID int64          `json:"tls_fingerprint_profile_id"`
				} `json:"data"`
			}
			require.NoError(t, json.Unmarshal(get.Body.Bytes(), &result))
			require.Equal(t, tc.wantNative, result.Data.Extra["native_wire_mode"] == "native")
			require.Equal(t, "preserved", result.Data.Extra["existing_setting"])
			require.True(t, result.Data.EnableTLSFingerprint)
			require.Equal(t, int64(7), result.Data.TLSFingerprintProfileID)
		})
	}
}
