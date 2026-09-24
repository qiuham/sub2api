package tlsfingerprint

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

type clientHelloSummary struct {
	Length              int
	JA3                 string
	CipherSuites        []uint16
	Extensions          []uint16
	Curves              []uint16
	PointFormats        []uint8
	SignatureAlgorithms []uint16
	KeyShareGroups      []uint16
}

func TestNativeProfileEmptyJSONArraysMatchOmittedArrays(t *testing.T) {
	verified := VerifiedNativeProfile("codex", "0.155.1")
	bound := *verified
	bound.Name = "saved in database"
	bound.ALPNProtocols = []string{}
	if !MatchesVerifiedNativeProfile(&bound, verified) {
		t.Fatal("empty decoded ALPN array must match omitted ALPN")
	}
}

func TestNativeProfilesMatchOfficialClientHelloBytes(t *testing.T) {
	tests := []struct {
		name       string
		serverName string
		fixture    string
		profile    *Profile
	}{
		{
			name:       "claude-2.1.280-proxy",
			serverName: "api.anthropic.com",
			fixture:    "claude-2.1.280-proxy.hex",
			profile:    VerifiedNativeProfile("claude", "2.1.280"),
		},
		{
			name:       "claude-2.1.281-proxy",
			serverName: "api.anthropic.com",
			fixture:    "claude-2.1.281-proxy.hex",
			profile:    VerifiedNativeProfile("claude", "2.1.281"),
		},
		{
			name:       "codex-0.155.1-proxy",
			serverName: "chatgpt.com",
			fixture:    "codex-0.155.1-proxy.hex",
			profile:    VerifiedNativeProfile("codex", "0.155.1"),
		},
		{
			name:       "codex-0.156.1-proxy",
			serverName: "chatgpt.com",
			fixture:    "codex-0.156.1-proxy.hex",
			profile:    VerifiedNativeProfile("codex", "0.156.1"),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			official := readClientHelloFixture(t, test.fixture)
			candidate := captureProfileClientHello(t, test.serverName, test.profile)
			officialMasked, officialSummary := maskClientHello(t, official)
			candidateMasked, candidateSummary := maskClientHello(t, candidate)
			if !bytes.Equal(candidateMasked, officialMasked) {
				t.Fatalf("masked ClientHello mismatch\nofficial:  %+v\ncandidate: %+v\nfirst_diff: %s", officialSummary, candidateSummary, firstByteDiff(officialMasked, candidateMasked))
			}
			t.Logf("MASKED_BYTE_EQUAL=true JA3=%s LENGTH=%d", officialSummary.JA3, officialSummary.Length)
		})
	}
}

func TestOfficialClientHelloStableAcrossMeasuredVersions(t *testing.T) {
	tests := []struct {
		name     string
		fixtures []string
	}{
		{name: "claude-proxy-2.1.258-to-2.1.281", fixtures: []string{"claude-2.1.258-proxy.hex", "claude-2.1.280-proxy.hex", "claude-2.1.281-proxy.hex"}},
		{name: "codex-proxy-0.154.0-to-0.156.1", fixtures: []string{"codex-0.154.0-proxy.hex", "codex-0.155.1-proxy.hex", "codex-0.156.1-proxy.hex"}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var baseline []byte
			var summary clientHelloSummary
			for index, fixture := range test.fixtures {
				masked, currentSummary := maskClientHello(t, readClientHelloFixture(t, fixture))
				if index == 0 {
					baseline = masked
					summary = currentSummary
					continue
				}
				if !bytes.Equal(masked, baseline) {
					t.Fatalf("%s differs from first measured version: %s", fixture, firstByteDiff(baseline, masked))
				}
			}
			t.Logf("CROSS_VERSION_MASKED_EQUAL=true JA3=%s LENGTH=%d VERSIONS=%d", summary.JA3, summary.Length, len(test.fixtures))
		})
	}
}

func readClientHelloFixture(t *testing.T, name string) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "native_fidelity", name))
	if err != nil {
		t.Fatal(err)
	}
	record, err := hex.DecodeString(strings.TrimSpace(string(raw)))
	if err != nil {
		t.Fatal(err)
	}
	return record
}

func captureProfileClientHello(t *testing.T, serverName string, profile *Profile) []byte {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = listener.Close() }()

	captured := make(chan []byte, 1)
	serverErr := make(chan error, 1)
	go func() {
		conn, acceptErr := listener.Accept()
		if acceptErr != nil {
			serverErr <- acceptErr
			return
		}
		defer func() { _ = conn.Close() }()
		record, readErr := readTLSRecord(conn)
		if readErr != nil {
			serverErr <- readErr
			return
		}
		captured <- record
	}()

	baseDialer := func(ctx context.Context, network, address string) (net.Conn, error) {
		var dialer net.Dialer
		return dialer.DialContext(ctx, network, listener.Addr().String())
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, _ = NewDialer(profile, baseDialer).DialTLSContext(ctx, "tcp", net.JoinHostPort(serverName, "443"))

	select {
	case record := <-captured:
		return record
	case err := <-serverErr:
		t.Fatal(err)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	return nil
}

func readTLSRecord(reader io.Reader) ([]byte, error) {
	header := make([]byte, 5)
	if _, err := io.ReadFull(reader, header); err != nil {
		return nil, err
	}
	length := int(header[3])<<8 | int(header[4])
	body := make([]byte, length)
	if _, err := io.ReadFull(reader, body); err != nil {
		return nil, err
	}
	return append(header, body...), nil
}

func maskClientHello(t *testing.T, record []byte) ([]byte, clientHelloSummary) {
	t.Helper()
	masked := append([]byte(nil), record...)
	if len(masked) < 44 || masked[0] != 22 || masked[5] != 1 {
		t.Fatalf("invalid ClientHello record: length=%d", len(masked))
	}
	position := 9
	legacyVersion := uint16(masked[position])<<8 | uint16(masked[position+1])
	position += 2
	clear(masked[position : position+32])
	position += 32
	sessionLength := int(masked[position])
	position++
	clear(masked[position : position+sessionLength])
	position += sessionLength
	cipherLength := int(masked[position])<<8 | int(masked[position+1])
	position += 2
	ciphers := readUint16s(masked[position : position+cipherLength])
	position += cipherLength
	compressionLength := int(masked[position])
	position += 1 + compressionLength
	extensionsLength := int(masked[position])<<8 | int(masked[position+1])
	position += 2
	extensionsEnd := position + extensionsLength
	if extensionsEnd > len(masked) {
		t.Fatalf("extensions overflow: end=%d length=%d", extensionsEnd, len(masked))
	}

	var extensions []uint16
	var curves []uint16
	var pointFormats []uint8
	var signatureAlgorithms []uint16
	var keyShareGroups []uint16
	for position < extensionsEnd {
		extensionType := uint16(masked[position])<<8 | uint16(masked[position+1])
		extensionLength := int(masked[position+2])<<8 | int(masked[position+3])
		dataStart := position + 4
		dataEnd := dataStart + extensionLength
		if dataEnd > extensionsEnd {
			t.Fatalf("extension %d overflow", extensionType)
		}
		extensions = append(extensions, extensionType)
		switch extensionType {
		case 10:
			curves = readUint16Vector(masked[dataStart:dataEnd])
		case 11:
			if extensionLength > 0 {
				count := int(masked[dataStart])
				pointFormats = append([]uint8(nil), masked[dataStart+1:dataStart+1+count]...)
			}
		case 13:
			signatureAlgorithms = readUint16Vector(masked[dataStart:dataEnd])
		case 51:
			keyPosition := dataStart + 2
			for keyPosition < dataEnd {
				group := uint16(masked[keyPosition])<<8 | uint16(masked[keyPosition+1])
				keyLength := int(masked[keyPosition+2])<<8 | int(masked[keyPosition+3])
				keyShareGroups = append(keyShareGroups, group)
				clear(masked[keyPosition+4 : keyPosition+4+keyLength])
				keyPosition += 4 + keyLength
			}
		case 21, 41, 65037:
			clear(masked[dataStart:dataEnd])
		}
		position = dataEnd
	}

	ja3Text := strings.Join([]string{
		strconv.Itoa(int(legacyVersion)),
		joinUint16sWithoutGREASE(ciphers),
		joinUint16sWithoutGREASE(extensions),
		joinUint16sWithoutGREASE(curves),
		joinUint8s(pointFormats),
	}, ",")
	ja3 := fmt.Sprintf("%x", md5.Sum([]byte(ja3Text)))
	return masked, clientHelloSummary{
		Length:              len(record),
		JA3:                 ja3,
		CipherSuites:        ciphers,
		Extensions:          extensions,
		Curves:              curves,
		PointFormats:        pointFormats,
		SignatureAlgorithms: signatureAlgorithms,
		KeyShareGroups:      keyShareGroups,
	}
}

func readUint16Vector(data []byte) []uint16 {
	if len(data) < 2 {
		return nil
	}
	length := int(data[0])<<8 | int(data[1])
	return readUint16s(data[2 : 2+length])
}

func readUint16s(data []byte) []uint16 {
	values := make([]uint16, 0, len(data)/2)
	for index := 0; index+1 < len(data); index += 2 {
		values = append(values, uint16(data[index])<<8|uint16(data[index+1]))
	}
	return values
}

func joinUint16sWithoutGREASE(values []uint16) string {
	parts := make([]string, 0, len(values))
	for _, value := range values {
		if isGREASEValue(value) {
			continue
		}
		parts = append(parts, strconv.Itoa(int(value)))
	}
	return strings.Join(parts, "-")
}

func joinUint8s(values []uint8) string {
	parts := make([]string, 0, len(values))
	for _, value := range values {
		parts = append(parts, strconv.Itoa(int(value)))
	}
	return strings.Join(parts, "-")
}

func firstByteDiff(left, right []byte) string {
	limit := len(left)
	if len(right) < limit {
		limit = len(right)
	}
	for index := 0; index < limit; index++ {
		if left[index] != right[index] {
			return fmt.Sprintf("offset=%d official=%02x candidate=%02x official_len=%d candidate_len=%d", index, left[index], right[index], len(left), len(right))
		}
	}
	return fmt.Sprintf("shared_prefix=%d official_len=%d candidate_len=%d", limit, len(left), len(right))
}
