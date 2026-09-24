package service

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func nativeClaudeTelemetryTestBody(on bool) []byte {
	names := strings.Fields("Agent Bash CronCreate CronDelete CronList Edit EnterWorktree ExitWorktree ListAgents NotebookEdit Read ReportFindings ScheduleWakeup SendMessage Skill TaskCreate TaskGet TaskList TaskStop TaskUpdate WebFetch WebSearch Workflow Write")
	if on {
		names = append(names, "DesignSync", "Monitor", "PushNotification")
	}
	tools := make([]map[string]string, 0, len(names))
	for _, name := range names {
		tools = append(tools, map[string]string{"name": name})
	}
	body, _ := json.Marshal(map[string]any{
		"model": "claude-sonnet-4-5", "max_tokens": 16, "stream": true,
		"tools": tools, "messages": []any{map[string]any{"role": "user", "content": "fixture"}},
	})
	return body
}

func nativeTelemetryTestContext(version string) (*gin.Context, *httptest.ResponseRecorder) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	if version != "" {
		c.Request.Header.Set("User-Agent", "claude-cli/"+version+" (external, cli)")
	}
	return c, recorder
}

func TestRejectNativeClaudeTelemetryUsesVerifiedVersionBaselines(t *testing.T) {
	gin.SetMode(gin.TestMode)
	account := &Account{Platform: PlatformAnthropic, Extra: map[string]any{"native_wire_mode": "native"}}
	for _, version := range []string{"2.1.280", "2.1.281"} {
		c, _ := nativeTelemetryTestContext(version)
		if err := rejectNativeClaudeTelemetry(t.Context(), c, account, nativeClaudeTelemetryTestBody(false)); err != nil {
			t.Fatalf("version %s telemetry-off rejected: %v", version, err)
		}
		c, recorder := nativeTelemetryTestContext(version)
		if err := rejectNativeClaudeTelemetry(t.Context(), c, account, nativeClaudeTelemetryTestBody(true)); err == nil || recorder.Code != http.StatusForbidden {
			t.Fatalf("version %s telemetry-on forwarded: err=%v status=%d", version, err, recorder.Code)
		}
	}
}

func TestRejectNativeClaudeTelemetryUnknownShapeAndVersion(t *testing.T) {
	gin.SetMode(gin.TestMode)
	account := &Account{Platform: PlatformAnthropic, Extra: map[string]any{"native_wire_mode": "native"}}
	for _, tc := range []struct {
		version string
		body    []byte
	}{
		{"2.1.281", []byte(`{"tools":[{"name":"UnknownTool"}]}`)},
		{"2.1.281", []byte(`{"tools":[]}`)},
		{"9.9.9", nativeClaudeTelemetryTestBody(false)},
		{"", nativeClaudeTelemetryTestBody(false)},
	} {
		c, recorder := nativeTelemetryTestContext(tc.version)
		if err := rejectNativeClaudeTelemetry(t.Context(), c, account, tc.body); err == nil || recorder.Code != http.StatusForbidden {
			t.Fatalf("unknown version=%q body=%s forwarded: err=%v status=%d", tc.version, tc.body, err, recorder.Code)
		}
	}
}
