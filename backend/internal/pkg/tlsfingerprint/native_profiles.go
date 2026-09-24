package tlsfingerprint

import "reflect"

// VerifiedNativeVersions is the single allowlist for measured client versions.
func VerifiedNativeVersions(family string) []string {
	switch family {
	case "claude":
		return []string{"2.1.258", "2.1.280", "2.1.281"}
	case "codex":
		return []string{"0.154.0", "0.155.1", "0.156.1"}
	default:
		return nil
	}
}

func LatestVerifiedNativeVersion(family string) string {
	versions := VerifiedNativeVersions(family)
	if len(versions) == 0 {
		return ""
	}
	return versions[len(versions)-1]
}

// VerifiedNativeProfile contains only client versions backed by captured
// ClientHello fixtures. A new version must be measured before it is admitted.
func VerifiedNativeProfile(family, version string) *Profile {
	verifiedVersion := false
	for _, candidate := range VerifiedNativeVersions(family) {
		if candidate == version {
			verifiedVersion = true
			break
		}
	}
	if !verifiedVersion {
		return nil
	}
	switch family {
	case "claude":
		return &Profile{
			Name:                "claude-proxy-2.1.258-2.1.281",
			CipherSuites:        []uint16{4865, 4866, 4867, 49199, 49195, 49200, 49196, 49191, 52393, 52392, 49161, 49171, 49162, 49172, 156, 157, 47, 53},
			Curves:              []uint16{4588, 29, 23, 24},
			PointFormats:        []uint16{0},
			SignatureAlgorithms: []uint16{1027, 2052, 1025, 1283, 2053, 1281, 2054, 1537, 513},
			SupportedVersions:   []uint16{0x0304, 0x0303},
			KeyShareGroups:      []uint16{4588, 29},
			PSKModes:            []uint16{1},
			Extensions:          []uint16{0, 23, 65281, 10, 11, 35, 13, 51, 45, 43},
		}
	case "codex":
		return &Profile{
			Name:                "codex-proxy-0.154.0-0.156.1",
			CipherSuites:        []uint16{4866, 4867, 4865, 49196, 49200, 159, 52393, 52392, 52394, 49195, 49199, 158, 49188, 49192, 107, 49187, 49191, 103, 49162, 49172, 57, 49161, 49171, 51, 157, 156, 61, 60, 53, 47},
			Curves:              []uint16{4588, 29, 23, 30, 24, 25, 256, 257},
			PointFormats:        []uint16{0},
			SignatureAlgorithms: []uint16{2309, 2310, 2308, 1027, 1283, 1539, 2055, 2056, 2074, 2075, 2076, 2057, 2058, 2059, 2052, 2053, 2054, 1025, 1281, 1537, 771, 769, 770, 1026, 1282, 1538},
			SupportedVersions:   []uint16{0x0304, 0x0303},
			KeyShareGroups:      []uint16{4588, 29},
			PSKModes:            []uint16{1},
			Extensions:          []uint16{65281, 0, 11, 10, 35, 22, 23, 13, 43, 45, 51},
		}
	}
	return nil
}

// MatchesVerifiedNativeProfile prevents an account-bound but unrelated profile
// from silently being used for a different native client version.
func MatchesVerifiedNativeProfile(bound, verified *Profile) bool {
	if bound == nil || verified == nil {
		return false
	}
	a, b := *bound, *verified
	a.Name, b.Name = "", ""
	// JSON-backed templates decode empty arrays as non-nil slices. They have
	// the same dialer behavior as omitted arrays and must match built-ins.
	if len(a.CipherSuites) == 0 {
		a.CipherSuites = nil
	}
	if len(a.Curves) == 0 {
		a.Curves = nil
	}
	if len(a.PointFormats) == 0 {
		a.PointFormats = nil
	}
	if len(a.SignatureAlgorithms) == 0 {
		a.SignatureAlgorithms = nil
	}
	if len(a.ALPNProtocols) == 0 {
		a.ALPNProtocols = nil
	}
	if len(a.SupportedVersions) == 0 {
		a.SupportedVersions = nil
	}
	if len(a.KeyShareGroups) == 0 {
		a.KeyShareGroups = nil
	}
	if len(a.PSKModes) == 0 {
		a.PSKModes = nil
	}
	if len(a.Extensions) == 0 {
		a.Extensions = nil
	}
	return reflect.DeepEqual(a, b)
}
