package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	coderws "github.com/coder/websocket"
	"github.com/stretchr/testify/require"
)

func TestNativeWSOrderedHTTP1TransportCarriesMessages(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := coderws.Accept(w, r, &coderws.AcceptOptions{CompressionMode: coderws.CompressionContextTakeover})
		if err != nil {
			return
		}
		defer func() { _ = conn.CloseNow() }()
		conn.SetReadLimit(1 << 20)
		for {
			kind, body, err := conn.Read(r.Context())
			if err != nil {
				return
			}
			if conn.Write(r.Context(), kind, body) != nil {
				return
			}
		}
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ctx = withNativeWSProfile(ctx, tlsfingerprint.VerifiedNativeProfile("codex", "0.156.1"))
	headers := http.Header{"User-Agent": {"codex_cli_rs/0.156.1 (Linux; x86_64)"}, "Authorization": {"Bearer fixture"}}
	conn, _, _, err := newDefaultOpenAIWSClientDialer().Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http"), headers, "")
	require.NoError(t, err)
	defer func() { _ = conn.Close() }()
	for _, text := range []string{"ping", strings.Repeat("响应内容", 4096)} {
		payload := map[string]string{"type": "fixture", "text": text}
		want, err := json.Marshal(payload)
		require.NoError(t, err)
		require.NoError(t, conn.WriteJSON(ctx, payload))
		got, err := conn.ReadMessage(ctx)
		require.NoError(t, err)
		require.JSONEq(t, string(want), string(got))
	}
	t.Log("WS_UPGRADE=true MESSAGE_ROUNDTRIPS=2 LARGE_MESSAGE=true")
}
