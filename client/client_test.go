package client

import (
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	npsconn "ehang.io/nps/lib/conn"
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

func TestDialTargetWithRetrySuccess(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen error: %v", err)
	}
	defer listener.Close()
	done := make(chan struct{})
	go func() {
		defer close(done)
		conn, err := listener.Accept()
		if err == nil {
			_ = conn.Close()
		}
	}()

	conn, err := dialTargetWithRetry("tcp", listener.Addr().String(), time.Second, 1, 0)
	if err != nil {
		t.Fatalf("dialTargetWithRetry returned error: %v", err)
	}
	_ = conn.Close()
	<-done
}

func TestDialTargetWithRetrySleepsBeforeRetry(t *testing.T) {
	oldSleep := targetConnectRetrySleep
	var delays []time.Duration
	targetConnectRetrySleep = func(delay time.Duration) {
		delays = append(delays, delay)
	}
	t.Cleanup(func() {
		targetConnectRetrySleep = oldSleep
	})

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen error: %v", err)
	}
	target := listener.Addr().String()
	_ = listener.Close()

	if conn, err := dialTargetWithRetry("tcp", target, 10*time.Millisecond, 1, 500*time.Millisecond); err == nil {
		_ = conn.Close()
		t.Fatalf("expected dial error")
	}
	if len(delays) != 1 {
		t.Fatalf("expected one retry sleep, got %d", len(delays))
	}
	if delays[0] < time.Millisecond || delays[0] > 500*time.Millisecond {
		t.Fatalf("retry delay %s out of range", delays[0])
	}
}

func TestDialTargetsWithRetryPollsTargets(t *testing.T) {
	badListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen bad target error: %v", err)
	}
	badTarget := badListener.Addr().String()
	_ = badListener.Close()

	goodListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen good target error: %v", err)
	}
	defer goodListener.Close()
	done := make(chan struct{})
	go func() {
		defer close(done)
		conn, err := goodListener.Accept()
		if err == nil {
			_ = conn.Close()
		}
	}()

	conn, err := dialTargetsWithRetry("tcp", []string{badTarget, goodListener.Addr().String()}, time.Second, 1, 0)
	if err != nil {
		t.Fatalf("dialTargetsWithRetry returned error: %v", err)
	}
	_ = conn.Close()
	<-done
}

func TestDialTargetCircuitIsolatesFailingTarget(t *testing.T) {
	oldCircuit := targetConnectCircuit
	targetConnectCircuit = npsconn.NewTargetCircuitBreaker(2, time.Minute)
	t.Cleanup(func() {
		targetConnectCircuit = oldCircuit
	})

	badListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen bad target error: %v", err)
	}
	badTarget := badListener.Addr().String()
	_ = badListener.Close()

	for i := 0; i < 2; i++ {
		dialedConn, err := dialTargetWithRetry("tcp", badTarget, 10*time.Millisecond, 0, 0)
		if err == nil {
			_ = dialedConn.Close()
			t.Fatalf("expected bad target dial %d to fail", i+1)
		}
	}
	if dialedConn, err := dialTargetWithRetry("tcp", badTarget, 10*time.Millisecond, 0, 0); err == nil {
		_ = dialedConn.Close()
		t.Fatal("expected isolated bad target to fail")
	} else if _, ok := err.(*npsconn.TargetCircuitOpenError); !ok {
		t.Fatalf("expected circuit open error, got %T: %v", err, err)
	}

	goodListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen good target error: %v", err)
	}
	defer goodListener.Close()
	done := make(chan struct{})
	go func() {
		defer close(done)
		conn, err := goodListener.Accept()
		if err == nil {
			_ = conn.Close()
		}
	}()

	dialedConn, err := dialTargetWithRetry("tcp", goodListener.Addr().String(), time.Second, 0, 0)
	if err != nil {
		t.Fatalf("healthy target should not be affected by isolated target: %v", err)
	}
	_ = dialedConn.Close()
	<-done
}

func TestRandomTargetConnectRetryDelayRange(t *testing.T) {
	for i := 0; i < 100; i++ {
		delay := randomTargetConnectRetryDelay(500 * time.Millisecond)
		if delay < time.Millisecond || delay > 500*time.Millisecond {
			t.Fatalf("retry delay %s out of range", delay)
		}
	}
	if delay := randomTargetConnectRetryDelay(0); delay != 0 {
		t.Fatalf("expected zero retry delay when interval disabled, got %s", delay)
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
