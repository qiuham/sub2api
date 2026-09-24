package service

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/model"
	"github.com/Wei-Shaw/sub2api/internal/pkg/nativewire"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	coderws "github.com/coder/websocket"
	"github.com/gin-gonic/gin"
)

type cliMockUpstream struct {
	mu            sync.Mutex
	requests      int
	body          []byte
	authorization string
	wireHeaders   string
}

type cliCaptureListener struct {
	net.Listener
	mu           sync.Mutex
	firstHeaders string
}

type cliCaptureConn struct {
	net.Conn
	listener *cliCaptureListener
	buf      []byte
	done     bool
}

func (l *cliCaptureListener) Accept() (net.Conn, error) {
	conn, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	return &cliCaptureConn{Conn: conn, listener: l}, nil
}

func (c *cliCaptureConn) Read(p []byte) (int, error) {
	n, err := c.Conn.Read(p)
	if !c.done && n > 0 {
		c.buf = append(c.buf, p[:n]...)
		if end := bytes.Index(c.buf, []byte("\r\n\r\n")); end >= 0 {
			c.listener.mu.Lock()
			if c.listener.firstHeaders == "" {
				c.listener.firstHeaders = string(c.buf[:end])
			}
			c.listener.mu.Unlock()
			c.done = true
			c.buf = nil
		}
	}
	return n, err
}

func cliHeaderNames(raw string) []string {
	lines := strings.Split(raw, "\r\n")
	names := make([]string, 0, len(lines))
	for _, line := range lines[1:] {
		if name, _, ok := strings.Cut(line, ":"); ok {
			names = append(names, name)
		}
	}
	return names
}

func cliHeaderWireDifference(ingress, outbound string) (shared, sameCase, ordered int) {
	a, b := cliHeaderNames(ingress), cliHeaderNames(outbound)
	outboundByName := make(map[string]string, len(b))
	for _, name := range b {
		outboundByName[strings.ToLower(name)] = name
	}
	commonA := make([]string, 0, len(a))
	for _, name := range a {
		if other, ok := outboundByName[strings.ToLower(name)]; ok {
			shared++
			if name == other {
				sameCase++
			}
			commonA = append(commonA, strings.ToLower(name))
		}
	}
	commonB := make([]string, 0, len(b))
	for _, name := range b {
		for _, original := range commonA {
			if strings.EqualFold(name, original) {
				commonB = append(commonB, strings.ToLower(name))
				break
			}
		}
	}
	dp := make([][]int, len(commonA)+1)
	for i := range dp {
		dp[i] = make([]int, len(commonB)+1)
	}
	for i := 1; i <= len(commonA); i++ {
		for j := 1; j <= len(commonB); j++ {
			if commonA[i-1] == commonB[j-1] {
				dp[i][j] = dp[i-1][j-1] + 1
			} else if dp[i-1][j] > dp[i][j-1] {
				dp[i][j] = dp[i-1][j]
			} else {
				dp[i][j] = dp[i][j-1]
			}
		}
	}
	return shared, sameCase, dp[len(commonA)][len(commonB)]
}

func cliHeaderStartMethod(raw string) string {
	line, _, _ := strings.Cut(raw, "\r\n")
	method, _, _ := strings.Cut(line, " ")
	return method
}

func cliToolNames(body []byte) []string {
	var request struct {
		Tools []struct {
			Name string `json:"name"`
		} `json:"tools"`
	}
	if json.Unmarshal(body, &request) != nil {
		return nil
	}
	names := make([]string, 0, len(request.Tools))
	for _, tool := range request.Tools {
		names = append(names, tool.Name)
	}
	sort.Strings(names)
	return names
}

// Capture actual transport output over TCP, rather than Request.Write (which
// bypasses the Native serializer). This is still a fixture upstream, not OAuth.
func serializeMockRequestHeaders(req *http.Request, body []byte) (string, error) {
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.WriteHeader(http.StatusNoContent)
	}))
	listener := &cliCaptureListener{Listener: server.Listener}
	server.Listener = listener
	server.Start()
	defer server.Close()
	clone := req.Clone(req.Context())
	target, err := url.Parse(server.URL)
	if err != nil {
		return "", err
	}
	clone.Host = req.URL.Host
	clone.URL.Scheme, clone.URL.Host = target.Scheme, target.Host
	clone.Body = io.NopCloser(bytes.NewReader(body))
	clone.ContentLength = int64(len(body))
	transport := nativewire.NewHTTP1Transport(&http.Transport{})
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 3 * time.Second}
	response, err := client.Do(clone)
	if err != nil {
		return "", err
	}
	_ = response.Body.Close()
	listener.mu.Lock()
	defer listener.mu.Unlock()
	return listener.firstHeaders, nil
}

func (u *cliMockUpstream) Do(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.requests++
	u.body, _ = io.ReadAll(req.Body)
	u.authorization = getHeaderRaw(req.Header, "authorization")
	var err error
	u.wireHeaders, err = serializeMockRequestHeaders(req, u.body)
	if err != nil {
		return nil, err
	}
	sse := "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_fixture\",\"type\":\"message\",\"role\":\"assistant\",\"content\":[],\"model\":\"claude-sonnet-4-5\",\"stop_reason\":null,\"stop_sequence\":null,\"usage\":{\"input_tokens\":1,\"output_tokens\":0}}}\n\n" +
		"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n" +
		"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"pong\"}}\n\n" +
		"event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n" +
		"event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\",\"stop_sequence\":null},\"usage\":{\"output_tokens\":1}}\n\n" +
		"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"
	return &http.Response{StatusCode: http.StatusOK, Header: http.Header{
		"Content-Type": {"text/event-stream"}, "Request-Id": {"mock-upstream-id"},
	}, Body: io.NopCloser(strings.NewReader(sse))}, nil
}

func (u *cliMockUpstream) DoWithTLS(req *http.Request, proxy string, id int64, concurrency int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return u.Do(req, proxy, id, concurrency)
}

// Run explicitly with RUN_NATIVE_CLAUDE_CLI_E2E=1; the CLI uses an isolated HOME
// and a fixture key, while the gateway's upstream is an in-process mock.
func TestNativeClaudeCLIIngressToMockUpstream(t *testing.T) {
	if os.Getenv("RUN_NATIVE_CLAUDE_CLI_E2E") != "1" {
		t.Skip("opt-in CLI acceptance")
	}
	cliBinary := os.Getenv("NATIVE_CLAUDE_TEST_BIN")
	if cliBinary == "" {
		cliBinary = "claude"
	}
	if _, err := exec.LookPath(cliBinary); err != nil {
		t.Skip("claude CLI absent")
	}
	gin.SetMode(gin.TestMode)
	profile := tlsfingerprint.VerifiedNativeProfile("claude", "2.1.281")
	profiles := &TLSFingerprintProfileService{localCache: map[int64]*model.TLSFingerprintProfile{
		7: {ID: 7, Name: profile.Name, CipherSuites: profile.CipherSuites, Curves: profile.Curves,
			PointFormats: profile.PointFormats, SignatureAlgorithms: profile.SignatureAlgorithms,
			SupportedVersions: profile.SupportedVersions, KeyShareGroups: profile.KeyShareGroups,
			PSKModes: profile.PSKModes, Extensions: profile.Extensions},
	}}
	upstream := &cliMockUpstream{}
	svc := &GatewayService{cfg: &config.Config{}, httpUpstream: upstream, tlsFPProfileService: profiles}
	router := gin.New()
	var ingressBody []byte
	router.POST("/v1/messages", func(c *gin.Context) {
		body, err := io.ReadAll(c.Request.Body)
		if err != nil {
			c.Status(http.StatusBadRequest)
			return
		}
		ingressBody = append([]byte(nil), body...)
		account := &Account{ID: 7, Name: "fixture", Platform: PlatformAnthropic, Type: AccountTypeOAuth,
			Concurrency: 1, Status: StatusActive, Schedulable: true,
			Credentials: map[string]any{"access_token": "fixture-upstream-token"},
			Extra: map[string]any{"native_wire_mode": "native", "enable_tls_fingerprint": true,
				"tls_fingerprint_profile_id": 7},
		}
		parsed, err := ParseGatewayRequest(NewRequestBodyRef(body), PlatformAnthropic)
		if err != nil {
			c.Status(http.StatusBadRequest)
			return
		}
		_, _ = svc.Forward(c.Request.Context(), c, account, parsed)
	})
	server := httptest.NewUnstartedServer(router)
	listener := &cliCaptureListener{Listener: server.Listener}
	server.Listener = listener
	server.Start()
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	//nolint:gosec // G702: opt-in local test executable supplied by the test operator, not HTTP input; no shell.
	cmd := exec.CommandContext(ctx, cliBinary, "-p", "ping", "--model", "claude-sonnet-4-5", "--max-turns", "1", "--output-format", "json")
	cmd.Env = append(os.Environ(), "HOME="+t.TempDir(), "ANTHROPIC_BASE_URL="+server.URL,
		"ANTHROPIC_API_KEY=fixture-ingress-key", "DISABLE_AUTOUPDATER=1")
	if os.Getenv("RUN_NATIVE_CLAUDE_TELEMETRY_PROBE") != "1" {
		cmd.Env = append(cmd.Env, "CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC=1")
	}
	output, commandErr := cmd.CombinedOutput()
	if os.Getenv("RUN_NATIVE_CLAUDE_TELEMETRY_PROBE") == "1" {
		upstream.mu.Lock()
		requests := upstream.requests
		upstream.mu.Unlock()
		if requests != 0 || len(ingressBody) == 0 || bytes.Contains(output, []byte(`"result":"pong"`)) {
			t.Fatalf("telemetry-on request reached upstream or CLI succeeded: requests=%d body=%d err=%v output=%s", requests, len(ingressBody), commandErr, bytes.TrimSpace(output))
		}
		sig, sigErr := nativewire.ClaudeTelemetrySignature(ingressBody)
		if sigErr != nil {
			t.Fatal(sigErr)
		}
		t.Logf("TELEMETRY_ON_BLOCKED=true UPSTREAM_REQUESTS=0 BODY_BYTES=%d TOOL_SIGNATURE=%s TOOL_NAMES=%v", len(ingressBody), sig, cliToolNames(ingressBody))
		return
	}
	if commandErr != nil || !bytes.Contains(output, []byte(`"result":"pong"`)) {
		t.Fatalf("CLI did not finish mock stream: err=%v output=%s", commandErr, bytes.TrimSpace(output))
	}
	upstream.mu.Lock()
	defer upstream.mu.Unlock()
	if upstream.requests == 0 {
		t.Fatalf("no upstream request; CLI output=%s", bytes.TrimSpace(output))
	}
	if len(upstream.body) == 0 || upstream.authorization != "Bearer fixture-upstream-token" {
		t.Fatalf("upstream body=%d auth=%q CLI output=%s", len(upstream.body), upstream.authorization, bytes.TrimSpace(output))
	}
	if !bytes.Equal(ingressBody, upstream.body) {
		t.Fatal("Claude CLI request body changed in Native forwarding")
	}
	t.Logf("CLI_REQUESTS=%d BODY_BYTES=%d BODY_BYTE_EQUAL=true UPSTREAM_AUTH_REPLACED=true CLI_RESULT=pong CLI_EXIT=0", upstream.requests, len(upstream.body))
	sig, sigErr := nativewire.ClaudeTelemetrySignature(upstream.body)
	if sigErr != nil {
		t.Fatal(sigErr)
	}
	t.Logf("CLAUDE_TOOL_SIGNATURE=%s", sig)
	t.Logf("CLAUDE_TOOL_NAMES=%v", cliToolNames(upstream.body))
	listener.mu.Lock()
	t.Logf("CLI_INGRESS_HEADERS=%v GATEWAY_OUTBOUND_HEADERS=%v", cliHeaderNames(listener.firstHeaders), cliHeaderNames(upstream.wireHeaders))
	if cliHeaderStartMethod(listener.firstHeaders) == cliHeaderStartMethod(upstream.wireHeaders) {
		shared, sameCase, ordered := cliHeaderWireDifference(listener.firstHeaders, upstream.wireHeaders)
		t.Logf("HEADER_WIRE_SHARED=%d SAME_CASE=%d ORDER_LCS=%d", shared, sameCase, ordered)
		if shared == 0 || ordered != shared || sameCase != shared {
			t.Errorf("Claude shared HTTP headers differ in order or case")
		}
	} else {
		t.Logf("HEADER_COMPARABLE=false INGRESS_METHOD=%s OUTBOUND_METHOD=%s", cliHeaderStartMethod(listener.firstHeaders), cliHeaderStartMethod(upstream.wireHeaders))
	}
	listener.mu.Unlock()
}

type codexCLIMockUpstream struct {
	mu            sync.Mutex
	requests      int
	body          []byte
	authorization string
	wireHeaders   string
}

func (u *codexCLIMockUpstream) Do(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.requests++
	u.body, _ = io.ReadAll(req.Body)
	u.authorization = getHeaderRaw(req.Header, "authorization")
	var err error
	u.wireHeaders, err = serializeMockRequestHeaders(req, u.body)
	if err != nil {
		return nil, err
	}
	sse := "event: response.created\ndata: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_fixture\",\"object\":\"response\",\"status\":\"in_progress\",\"output\":[]}}\n\n" +
		"event: response.output_item.added\ndata: {\"type\":\"response.output_item.added\",\"output_index\":0,\"item\":{\"id\":\"msg_fixture\",\"type\":\"message\",\"role\":\"assistant\",\"content\":[],\"status\":\"in_progress\"}}\n\n" +
		"event: response.content_part.added\ndata: {\"type\":\"response.content_part.added\",\"item_id\":\"msg_fixture\",\"output_index\":0,\"content_index\":0,\"part\":{\"type\":\"output_text\",\"text\":\"\",\"annotations\":[]}}\n\n" +
		"event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"item_id\":\"msg_fixture\",\"output_index\":0,\"content_index\":0,\"delta\":\"pong\"}\n\n" +
		"event: response.output_text.done\ndata: {\"type\":\"response.output_text.done\",\"item_id\":\"msg_fixture\",\"output_index\":0,\"content_index\":0,\"text\":\"pong\"}\n\n" +
		"event: response.content_part.done\ndata: {\"type\":\"response.content_part.done\",\"item_id\":\"msg_fixture\",\"output_index\":0,\"content_index\":0,\"part\":{\"type\":\"output_text\",\"text\":\"pong\",\"annotations\":[]}}\n\n" +
		"event: response.output_item.done\ndata: {\"type\":\"response.output_item.done\",\"output_index\":0,\"item\":{\"id\":\"msg_fixture\",\"type\":\"message\",\"role\":\"assistant\",\"content\":[{\"type\":\"output_text\",\"text\":\"pong\",\"annotations\":[]}],\"status\":\"completed\"}}\n\n" +
		"event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_fixture\",\"object\":\"response\",\"status\":\"completed\",\"output\":[{\"type\":\"message\",\"id\":\"msg_fixture\",\"role\":\"assistant\",\"content\":[{\"type\":\"output_text\",\"text\":\"pong\",\"annotations\":[]}]}],\"usage\":{\"input_tokens\":1,\"output_tokens\":1,\"total_tokens\":2}}}\n\n"
	return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/event-stream"}, "Request-Id": {"mock-upstream-id"}}, Body: io.NopCloser(strings.NewReader(sse))}, nil
}

func (u *codexCLIMockUpstream) DoWithTLS(req *http.Request, proxy string, id int64, concurrency int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return u.Do(req, proxy, id, concurrency)
}

func TestNativeCodexCLIIngressToMockUpstream(t *testing.T) {
	if os.Getenv("RUN_NATIVE_CODEX_CLI_E2E") != "1" {
		t.Skip("opt-in CLI acceptance")
	}
	cliBinary := os.Getenv("NATIVE_CODEX_TEST_BIN")
	if cliBinary == "" {
		cliBinary = "codex"
	}
	if _, err := exec.LookPath(cliBinary); err != nil {
		t.Skip("codex CLI absent")
	}
	gin.SetMode(gin.TestMode)
	profile := tlsfingerprint.VerifiedNativeProfile("codex", "0.156.1")
	profiles := &TLSFingerprintProfileService{localCache: map[int64]*model.TLSFingerprintProfile{
		7: {ID: 7, Name: profile.Name, CipherSuites: profile.CipherSuites, Curves: profile.Curves,
			PointFormats: profile.PointFormats, SignatureAlgorithms: profile.SignatureAlgorithms,
			SupportedVersions: profile.SupportedVersions, KeyShareGroups: profile.KeyShareGroups,
			PSKModes: profile.PSKModes, Extensions: profile.Extensions},
	}}
	upstream := &codexCLIMockUpstream{}
	svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream, tlsFPProfileService: profiles}
	router := gin.New()
	var ingressBody []byte
	var wsIngressHeaders http.Header
	router.GET("/v1/responses", func(c *gin.Context) {
		wsIngressHeaders = c.Request.Header.Clone()
		c.Status(http.StatusNotFound) // observe the CLI's HTTP fallback after the WS attempt
	})
	router.POST("/v1/responses", func(c *gin.Context) {
		body, err := io.ReadAll(c.Request.Body)
		if err != nil {
			c.Status(http.StatusBadRequest)
			return
		}
		ingressBody = append([]byte(nil), body...)
		account := &Account{ID: 7, Name: "fixture", Platform: PlatformOpenAI, Type: AccountTypeOAuth,
			Concurrency: 1, Status: StatusActive, Schedulable: true,
			Credentials: map[string]any{"access_token": "fixture-upstream-token", "chatgpt_account_id": "fixture-account"},
			Extra:       map[string]any{"native_wire_mode": "native", "enable_tls_fingerprint": true, "tls_fingerprint_profile_id": 7},
		}
		_, _ = svc.Forward(c.Request.Context(), c, account, body)
	})
	server := httptest.NewUnstartedServer(router)
	listener := &cliCaptureListener{Listener: server.Listener}
	server.Listener = listener
	server.Start()
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	supportsWebSockets := "false"
	if os.Getenv("RUN_NATIVE_CODEX_WS_PROBE") == "1" {
		supportsWebSockets = "true"
	}
	config := "{name=\"Fixture\",base_url=\"" + server.URL + "/v1\",env_key=\"OPENAI_API_KEY\",wire_api=\"responses\",supports_websockets=" + supportsWebSockets + "}"
	//nolint:gosec // G702: opt-in local test executable supplied by the test operator, not HTTP input; no shell.
	cmd := exec.CommandContext(ctx, cliBinary, "exec", "--ephemeral", "--ignore-user-config", "--skip-git-repo-check",
		"-c", "model=\"gpt-6-sol\"", "-c", "model_provider=\"fixture\"", "-c", "model_providers.fixture="+config, "ping")
	cmd.Env = append(os.Environ(), "CODEX_HOME="+t.TempDir(), "OPENAI_API_KEY=fixture-ingress-key")
	output, commandErr := cmd.CombinedOutput()
	if commandErr != nil || !bytes.Contains(output, []byte("pong")) {
		t.Fatalf("Codex CLI did not finish mock stream: err=%v output=%s", commandErr, bytes.TrimSpace(output))
	}
	upstream.mu.Lock()
	defer upstream.mu.Unlock()
	if upstream.requests == 0 {
		t.Fatalf("no upstream request: err=%v output=%s", commandErr, bytes.TrimSpace(output))
	}
	if len(upstream.body) == 0 || upstream.authorization != "Bearer fixture-upstream-token" {
		t.Fatalf("upstream body=%d auth=%q CLI output=%s", len(upstream.body), upstream.authorization, bytes.TrimSpace(output))
	}
	if !bytes.Equal(ingressBody, upstream.body) {
		t.Fatal("Codex CLI request body changed in Native forwarding")
	}
	t.Logf("CLI_REQUESTS=%d BODY_BYTES=%d BODY_BYTE_EQUAL=true UPSTREAM_AUTH_REPLACED=true CLI_RESULT=pong CLI_EXIT=0", upstream.requests, len(upstream.body))
	listener.mu.Lock()
	t.Logf("CLI_INGRESS_HEADERS=%v GATEWAY_OUTBOUND_HEADERS=%v", cliHeaderNames(listener.firstHeaders), cliHeaderNames(upstream.wireHeaders))
	if cliHeaderStartMethod(listener.firstHeaders) == cliHeaderStartMethod(upstream.wireHeaders) {
		shared, sameCase, ordered := cliHeaderWireDifference(listener.firstHeaders, upstream.wireHeaders)
		t.Logf("HEADER_WIRE_SHARED=%d SAME_CASE=%d ORDER_LCS=%d", shared, sameCase, ordered)
		if shared == 0 || ordered != shared || sameCase != shared {
			t.Errorf("Codex shared HTTP headers differ in order or case")
		}
	} else {
		t.Logf("HEADER_COMPARABLE=false INGRESS_METHOD=%s OUTBOUND_METHOD=%s", cliHeaderStartMethod(listener.firstHeaders), cliHeaderStartMethod(upstream.wireHeaders))
	}
	listener.mu.Unlock()
	if os.Getenv("RUN_NATIVE_CODEX_WS_PROBE") == "1" {
		if cliHeaderStartMethod(listener.firstHeaders) != http.MethodGet || wsIngressHeaders == nil {
			t.Fatal("Codex did not attempt a WebSocket upgrade")
		}
		wsServer := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			conn, err := coderws.Accept(w, r, nil)
			if err == nil {
				_ = conn.CloseNow()
			}
		}))
		wsListener := &cliCaptureListener{Listener: wsServer.Listener}
		wsServer.Listener = wsListener
		wsServer.Start()
		defer wsServer.Close()
		headers := make(http.Header)
		nativewire.CopyRequestHeaders(headers, wsIngressHeaders)
		headers.Set("Authorization", "Bearer fixture-upstream-token")
		dialer := newDefaultOpenAIWSClientDialer()
		wsCtx, wsCancel := context.WithTimeout(context.Background(), 3*time.Second)
		conn, _, _, err := dialer.Dial(withNativeWSProfile(wsCtx, profile), "ws"+strings.TrimPrefix(wsServer.URL, "http"), headers, "")
		wsCancel()
		if err != nil {
			t.Fatal(err)
		}
		_ = conn.Close()
		wsListener.mu.Lock()
		shared, sameCase, ordered := cliHeaderWireDifference(listener.firstHeaders, wsListener.firstHeaders)
		t.Logf("NATIVE_WS_OUTBOUND_HEADERS=%v WS_HEADER_SHARED=%d SAME_CASE=%d ORDER_LCS=%d", cliHeaderNames(wsListener.firstHeaders), shared, sameCase, ordered)
		if shared == 0 || ordered != shared || sameCase != shared {
			t.Errorf("Codex WS shared headers differ in order or case")
		}
		wsListener.mu.Unlock()
	}
}
