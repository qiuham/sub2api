package service

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestPassthroughAccountIdentityUsesSelectedCredentials(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, mode := range []string{"sub2api", "native"} {
		for _, tc := range []struct {
			name, selectedID      string
			shadow, missingParent bool
		}{
			{"selected", "selected-account", false, false},
			{"missing_id", "", false, false},
			{"shadow", "parent-account", true, false},
			{"missing_parent", "", true, true},
		} {
			t.Run(mode+"/"+tc.name, func(t *testing.T) {
				account := &Account{ID: 7, Platform: PlatformOpenAI, Type: AccountTypeOAuth,
					Credentials: map[string]any{"chatgpt_account_id": tc.selectedID},
					Extra:       map[string]any{"native_wire_mode": mode}}
				repo := &stubChatGPTHeadersRepo{byID: map[int64]*Account{}}
				if tc.shadow {
					parentID := int64(9)
					account.ParentAccountID = &parentID
					account.Credentials = map[string]any{"chatgpt_account_id": "wrong-shadow-id"}
					if !tc.missingParent {
						repo.byID[9] = &Account{ID: 9, Platform: PlatformOpenAI, Type: AccountTypeOAuth,
							Credentials: map[string]any{"chatgpt_account_id": tc.selectedID}}
					}
				}
				c, _ := gin.CreateTestContext(httptest.NewRecorder())
				c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
				c.Request.Header.Set("Authorization", "Bearer untrusted-client-token")
				c.Request.Header.Set("Chatgpt-Account-Id", "untrusted-client-account")
				c.Request.Header.Set("X-Openai-Fedramp", "true")
				svc := &OpenAIGatewayService{cfg: &config.Config{}, accountRepo: repo}
				body := []byte("{\"model\":\"fixture-model\",\"input\":[]}")
				req, err := svc.buildUpstreamRequestOpenAIPassthrough(context.Background(), c, account, body, "selected-token")
				if tc.missingParent {
					require.ErrorContains(t, err, "spark shadow parent 9 not found")
					require.Nil(t, req)
					return
				}
				require.NoError(t, err)
				defer func() { _ = req.Body.Close() }()
				require.Equal(t, "Bearer selected-token", req.Header.Get("Authorization"))
				require.Equal(t, tc.selectedID, req.Header.Get("Chatgpt-Account-Id"))
				require.Empty(t, req.Header.Get("X-Openai-Fedramp"))
				payload, err := io.ReadAll(req.Body)
				require.NoError(t, err)
				require.Equal(t, body, payload)
				require.Equal(t, "untrusted-client-account", c.Request.Header.Get("Chatgpt-Account-Id"))
			})
		}
	}
}
