package nativewire

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// TelemetryState is deliberately tri-state: an unknown client shape must not
// be mistaken for telemetry-off in strict Native mode.
type TelemetryState string

const (
	TelemetryOn      TelemetryState = "on"
	TelemetryOff     TelemetryState = "off"
	TelemetryUnknown TelemetryState = "unknown"
)

type TelemetryBaseline struct {
	On  string
	Off string
}

// VerifiedClaudeTelemetryBaseline contains tool-set signatures captured from
// paired full CLI runs with nonessential traffic enabled and disabled. Unknown
// versions have no baseline and therefore cannot be classified as off.
func VerifiedClaudeTelemetryBaseline(version string) (TelemetryBaseline, bool) {
	switch version {
	case "2.1.258":
		return TelemetryBaseline{
			On:  "ed68ea530e59dc17c89968172f250db40349c7f40b8c3fe6f6c2e036d7328be1",
			Off: "a0c6e1679f2700bb352f8f67cb620cbbd3bd7e18c3b11d2268ac160dd91282b8",
		}, true
	case "2.1.280", "2.1.281":
		return TelemetryBaseline{
			On:  "10a77d16a591d201129b1aab3b59c8041e95b5fbca39af0d18ec3b9b992eb54c",
			Off: "bc30d90489c36c0194d8af136f7658d7eec4ed7c27ce86dea7e277e100553287",
		}, true
	default:
		return TelemetryBaseline{}, false
	}
}

// ClaudeTelemetrySignature fingerprints the advertised tool set, not the
// conversation. Captured telemetry-on/off requests differ in tool membership;
// prompts, message count, tool order, and metadata are unrelated to that signal.
func ClaudeTelemetrySignature(body []byte) (string, error) {
	var request struct {
		Tools []struct {
			Name string `json:"name"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(body, &request); err != nil {
		return "", err
	}
	if len(request.Tools) == 0 {
		return "", fmt.Errorf("claude telemetry tools are missing")
	}
	names := make([]string, 0, len(request.Tools))
	for _, tool := range request.Tools {
		name := strings.TrimSpace(tool.Name)
		if name == "" {
			return "", fmt.Errorf("claude telemetry tool name is empty")
		}
		names = append(names, name)
	}
	sort.Strings(names)
	b, err := json.Marshal(names)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}

func ClassifyClaudeTelemetry(body []byte, baseline TelemetryBaseline) (TelemetryState, string, error) {
	sig, err := ClaudeTelemetrySignature(body)
	if err != nil {
		return TelemetryUnknown, "", err
	}
	switch sig {
	case baseline.On:
		if sig != "" {
			return TelemetryOn, sig, nil
		}
	case baseline.Off:
		if sig != "" {
			return TelemetryOff, sig, nil
		}
	}
	return TelemetryUnknown, sig, nil
}
