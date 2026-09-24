package service

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestOpenAINativeWireBuildPreservesOfficialBody(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := []byte(`{"model":"gpt-6-sol","input":[{"role":"user","content":"OK"}],"stream":true,"store":false,"client_metadata":{"session_id":"s1","thread_id":"t1"}}`)
	account := &Account{ID: 88, Platform: PlatformOpenAI, Type: AccountTypeOAuth,
		Credentials: map[string]any{"chatgpt_account_id": "acct-native"},
		Extra:       map[string]any{"native_wire_mode": "native"}}
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	c.Request.Header.Set("content-type", "application/json")
	c.Request.Header.Set("session-id", "s1")
	c.Request.Header.Set("thread-id", "t1")
	svc := &OpenAIGatewayService{}
	req, err := svc.buildUpstreamRequestOpenAIPassthrough(context.Background(), c, account, body, "oauth-token")
	require.NoError(t, err)
	got, err := io.ReadAll(req.Body)
	require.NoError(t, err)
	require.Equal(t, body, got)
	require.Equal(t, "Bearer oauth-token", req.Header.Get("Authorization"))
}
