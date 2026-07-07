package common

import "testing"

func TestNormalizeClientDisplayAddr(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
		ok   bool
	}{
		{name: "ipv4", in: "36.22.237.47", want: "36.22.237.47", ok: true},
		{name: "ipv4 port", in: "36.22.237.47:8024", want: "36.22.237.47", ok: true},
		{name: "ipv6", in: "2001:db8::1", want: "2001:db8::1", ok: true},
		{name: "bracketed ipv6 port", in: "[2001:db8::1]:8024", want: "2001:db8::1", ok: true},
		{name: "invalid host", in: "example.com", ok: false},
		{name: "invalid empty", in: " ", ok: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := NormalizeClientDisplayAddr(tt.in)
			if ok != tt.ok {
				t.Fatalf("ok = %v, want %v", ok, tt.ok)
			}
			if got != tt.want {
				t.Fatalf("addr = %q, want %q", got, tt.want)
			}
		})
	}
}
