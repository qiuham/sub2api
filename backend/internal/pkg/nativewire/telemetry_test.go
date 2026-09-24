package nativewire

import (
	"encoding/json"
	"os"
	"testing"
)

func TestVerifiedClaudeTelemetryBaselinesMatchCapturedTools(t *testing.T) {
	fixture, err := os.ReadFile("testdata/claude_telemetry_tools.json")
	if err != nil {
		t.Fatal(err)
	}
	var captures map[string]struct {
		On  []string `json:"on"`
		Off []string `json:"off"`
	}
	if err := json.Unmarshal(fixture, &captures); err != nil {
		t.Fatal(err)
	}
	if len(captures) != 3 {
		t.Fatalf("captured versions=%d, want 3", len(captures))
	}
	for version, capture := range captures {
		t.Run(version, func(t *testing.T) {
			baseline, ok := VerifiedClaudeTelemetryBaseline(version)
			if !ok {
				t.Fatal("captured version lacks a baseline")
			}
			for _, tc := range []struct {
				name  string
				names []string
				want  string
			}{
				{"off", capture.Off, baseline.Off}, {"on", capture.On, baseline.On},
			} {
				tools := make([]map[string]string, 0, len(tc.names))
				for _, name := range tc.names {
					tools = append(tools, map[string]string{"name": name})
				}
				body, _ := json.Marshal(map[string]any{"tools": tools})
				got, err := ClaudeTelemetrySignature(body)
				if err != nil || got != tc.want {
					t.Fatalf("%s signature=%s want=%s err=%v", tc.name, got, tc.want, err)
				}
			}
		})
	}
	if _, ok := VerifiedClaudeTelemetryBaseline("9.9.9"); ok {
		t.Fatal("unknown version accepted")
	}
}

func TestClaudeTelemetrySignatureIgnoresDynamicValues(t *testing.T) {
	a := []byte(`{"model":"claude-sonnet","metadata":{"user_id":"a"},"tools":[{"name":"Monitor","input_schema":{"type":"object"}}]}`)
	b := []byte(`{"model":"claude-opus","metadata":{"user_id":"b"},"tools":[{"name":"Monitor","input_schema":{"type":"object"}}]}`)
	sa, err := ClaudeTelemetrySignature(a)
	if err != nil {
		t.Fatal(err)
	}
	sb, err := ClaudeTelemetrySignature(b)
	if err != nil {
		t.Fatal(err)
	}
	if sa != sb {
		t.Fatalf("dynamic values changed structural signature: %s != %s", sa, sb)
	}
}

func TestClaudeTelemetrySignatureIgnoresUnrelatedMessageShapeAndToolOrder(t *testing.T) {
	a := []byte(`{"model":"claude-sonnet","messages":[{"role":"user","content":"hello"}],"tools":[{"name":"Read"},{"name":"Write"}]}`)
	b := []byte(`{"model":"claude-opus","messages":[{"role":"user","content":[{"type":"text","text":"hello"}]},{"role":"assistant","content":"done"}],"tools":[{"name":"Write"},{"name":"Read"}],"metadata":{"user_id":"different"}}`)
	sa, err := ClaudeTelemetrySignature(a)
	if err != nil {
		t.Fatal(err)
	}
	sb, err := ClaudeTelemetrySignature(b)
	if err != nil {
		t.Fatal(err)
	}
	if sa != sb {
		t.Fatalf("unrelated request shape or tool order changed telemetry signature: %s != %s", sa, sb)
	}
}

func TestClassifyClaudeTelemetryUsesVersionBaselineShape(t *testing.T) {
	on := []byte(`{"tools":[{"name":"Monitor"},{"name":"PushNotification"}],"analytics":{"enabled":true}}`)
	off := []byte(`{"tools":[{"name":"Monitor"}]}`)
	onSig, err := ClaudeTelemetrySignature(on)
	if err != nil {
		t.Fatal(err)
	}
	offSig, err := ClaudeTelemetrySignature(off)
	if err != nil {
		t.Fatal(err)
	}
	state, got, err := ClassifyClaudeTelemetry(on, TelemetryBaseline{On: onSig, Off: offSig})
	if err != nil || state != TelemetryOn || got != onSig {
		t.Fatalf("on classification = %q %q %v", state, got, err)
	}
	state, got, err = ClassifyClaudeTelemetry(off, TelemetryBaseline{On: onSig, Off: offSig})
	if err != nil || state != TelemetryOff || got != offSig {
		t.Fatalf("off classification = %q %q %v", state, got, err)
	}
	state, _, err = ClassifyClaudeTelemetry([]byte(`{"tools":[]}`), TelemetryBaseline{On: onSig, Off: offSig})
	if err == nil || state != TelemetryUnknown {
		t.Fatalf("unknown classification = %q %v", state, err)
	}
}
