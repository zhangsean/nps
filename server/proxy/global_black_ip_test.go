package proxy

import (
	"reflect"
	"testing"
)

func TestParseGlobalBlackIpListNormalizesAndDeduplicates(t *testing.T) {
	values, err := ParseGlobalBlackIpList("192.0.2.10, 192.0.2.10;2001:db8::10\n198.51.100.8")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"192.0.2.10", "198.51.100.8", "2001:db8::10"}
	if !reflect.DeepEqual(values, want) {
		t.Fatalf("values = %#v, want %#v", values, want)
	}
}

func TestParseGlobalBlackIpListRejectsInvalidValue(t *testing.T) {
	if _, err := ParseGlobalBlackIpList("192.0.2.10,invalid"); err == nil {
		t.Fatal("invalid IP address should be rejected")
	}
}

func TestIsGlobalBlackIpUsesRuntimeSnapshot(t *testing.T) {
	previous := GlobalBlackIpList()
	t.Cleanup(func() { SetGlobalBlackIpList(previous) })
	SetGlobalBlackIpList([]string{"192.0.2.10", "2001:db8::10"})

	for _, address := range []string{"192.0.2.10:443", "[2001:db8::10]:443", "192.0.2.10"} {
		if !IsGlobalBlackIp(address) {
			t.Fatalf("expected %s to be blocked", address)
		}
	}
	if IsGlobalBlackIp("192.0.2.11:443") {
		t.Fatal("unexpected IP address was blocked")
	}
}
