package bridge

import (
	"net"
	"sync"
	"testing"
	"time"

	"ehang.io/nps/lib/conn"
)

func TestNewTunnelTargetConnectRetryConfig(t *testing.T) {
	tunnel := NewTunnel(0, "tcp", false, &sync.Map{}, 60, 3, 4, 0, 250)
	if tunnel.clientConnectTimeout != 3*time.Second {
		t.Fatalf("unexpected client connect timeout %s", tunnel.clientConnectTimeout)
	}
	if tunnel.targetConnectTimeout != 4*time.Second {
		t.Fatalf("unexpected target connect timeout %s", tunnel.targetConnectTimeout)
	}
	if tunnel.targetConnectRetryCount != 0 {
		t.Fatalf("unexpected target connect retry count %d", tunnel.targetConnectRetryCount)
	}
	if tunnel.targetConnectRetryInterval != 250*time.Millisecond {
		t.Fatalf("unexpected target connect retry interval %s", tunnel.targetConnectRetryInterval)
	}
}

func TestNewTunnelTargetConnectRetryDefault(t *testing.T) {
	tunnel := NewTunnel(0, "tcp", false, &sync.Map{}, 60, 0, 0, -1, -1)
	if tunnel.clientConnectTimeout != defaultClientConnectTimeout {
		t.Fatalf("unexpected default client connect timeout %s", tunnel.clientConnectTimeout)
	}
	if tunnel.targetConnectTimeout != defaultTargetConnectTimeout {
		t.Fatalf("unexpected default target connect timeout %s", tunnel.targetConnectTimeout)
	}
	if tunnel.targetConnectRetryCount != defaultTargetConnectRetryCount {
		t.Fatalf("unexpected default target connect retry count %d", tunnel.targetConnectRetryCount)
	}
	if tunnel.targetConnectRetryInterval != 0 {
		t.Fatalf("unexpected default target connect retry interval %s", tunnel.targetConnectRetryInterval)
	}
}

func TestDialLocalProxyTargetWithRetrySuccess(t *testing.T) {
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

	tunnel := NewTunnel(0, "tcp", false, &sync.Map{}, 60, 1, 1, 1, 0)
	conn, err := tunnel.dialLocalProxyTargetWithRetry("tcp", listener.Addr().String(), nil)
	if err != nil {
		t.Fatalf("dialLocalProxyTargetWithRetry returned error: %v", err)
	}
	_ = conn.Close()
	<-done
}

func TestDialLocalProxyTargetWithRetrySleepsBeforeRetry(t *testing.T) {
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

	tunnel := NewTunnel(0, "tcp", false, &sync.Map{}, 60, 1, 1, 1, 500)
	var retries []conn.RetryInfo
	if conn, err := tunnel.dialLocalProxyTargetWithRetry("tcp", target, func(info conn.RetryInfo) {
		retries = append(retries, info)
	}); err == nil {
		_ = conn.Close()
		t.Fatalf("expected dial error")
	}
	if len(delays) != 1 {
		t.Fatalf("expected one retry sleep, got %d", len(delays))
	}
	if delays[0] < time.Millisecond || delays[0] > 500*time.Millisecond {
		t.Fatalf("retry delay %s out of range", delays[0])
	}
	if len(retries) != 1 {
		t.Fatalf("expected one retry hook, got %d", len(retries))
	}
	if retries[0].Source != "local_proxy" || retries[0].ConnType != "tcp" || retries[0].Target != target || retries[0].Attempt != 1 || retries[0].Attempts != 2 {
		t.Fatalf("unexpected retry info: %+v", retries[0])
	}
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

func TestLocalProxyTargetConnType(t *testing.T) {
	tests := []struct {
		name     string
		connType string
		want     string
	}{
		{name: "http", connType: "http", want: "tcp"},
		{name: "tcp", connType: "tcp", want: "tcp"},
		{name: "udp", connType: "udp", want: "udp"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := localProxyTargetConnType(tt.connType); got != tt.want {
				t.Fatalf("unexpected conn type %q, want %q", got, tt.want)
			}
		})
	}
}
