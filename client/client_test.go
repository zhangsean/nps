package client

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestFetchPublicCip(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("Current IP Address: 47.96.89.55\n"))
	}))
	defer server.Close()

	got, err := fetchPublicCip(server.URL)
	if err != nil {
		t.Fatalf("fetchPublicCip returned error: %v", err)
	}
	if got != "47.96.89.55" {
		t.Fatalf("expected normalized ip 47.96.89.55, got %q", got)
	}
}

func TestFetchPublicCipInvalidResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("not an ip 999.999.999.999"))
	}))
	defer server.Close()

	if got, err := fetchPublicCip(server.URL); err == nil {
		t.Fatalf("expected invalid response error, got ip %q", got)
	}
}

func TestNewRPClientDefaultCipQuery(t *testing.T) {
	client := NewRPClient("127.0.0.1:8024", "vkey", "tcp", "", nil, 60, "", "", 0)
	if client.cipUrl != DefaultCipURL {
		t.Fatalf("expected default cip url %q, got %q", DefaultCipURL, client.cipUrl)
	}
	if client.cipInterval != time.Duration(DefaultCipInterval)*time.Second {
		t.Fatalf("expected default cip interval %d seconds, got %s", DefaultCipInterval, client.cipInterval)
	}
}

func TestExtractPublicCipResponseSkipsInvalidCandidates(t *testing.T) {
	got, err := extractPublicCipResponse("bad 999.999.999.999 ok 36.22.237.47")
	if err != nil {
		t.Fatalf("extractPublicCipResponse returned error: %v", err)
	}
	if got != "36.22.237.47" {
		t.Fatalf("expected valid ip 36.22.237.47, got %q", got)
	}
}
