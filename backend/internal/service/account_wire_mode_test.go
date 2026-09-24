package service

import "testing"

func TestAccountWireModeDefaultsToSub2API(t *testing.T) {
	a := &Account{}
	if got := a.GetWireMode(); got != "sub2api" || a.IsNativeWireEnabled() {
		t.Fatalf("default wire mode = %q native=%v", got, a.IsNativeWireEnabled())
	}
}

func TestAccountWireModeAcceptsOnlyNative(t *testing.T) {
	for _, tc := range []struct {
		value  any
		want   string
		native bool
	}{
		{value: " native ", want: "native", native: true},
		{value: "sub2api", want: "sub2api"},
		{value: "future-mode", want: "sub2api"},
		{value: true, want: "sub2api"},
	} {
		a := &Account{Extra: map[string]any{"native_wire_mode": tc.value}}
		if got := a.GetWireMode(); got != tc.want || a.IsNativeWireEnabled() != tc.native {
			t.Fatalf("value %#v -> mode=%q native=%v", tc.value, got, a.IsNativeWireEnabled())
		}
	}
}
