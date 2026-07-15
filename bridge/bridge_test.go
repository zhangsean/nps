package bridge

import (
	"net"
	"sync"
	"testing"
	"time"
)

func TestNewTunnelTargetConnectRetryConfig(t *testing.T) {
	tunnel := NewTunnel(0, "tcp", false, &sync.Map{}, 60, 3, 4, 0)
	if tunnel.clientConnectTimeout != 3*time.Second {
		t.Fatalf("unexpected client connect timeout %s", tunnel.clientConnectTimeout)
	}
	if tunnel.targetConnectTimeout != 4*time.Second {
		t.Fatalf("unexpected target connect timeout %s", tunnel.targetConnectTimeout)
	}
	if tunnel.targetConnectRetryCount != 0 {
		t.Fatalf("unexpected target connect retry count %d", tunnel.targetConnectRetryCount)
	}
}

func TestNewTunnelTargetConnectRetryDefault(t *testing.T) {
	tunnel := NewTunnel(0, "tcp", false, &sync.Map{}, 60, 0, 0, -1)
	if tunnel.clientConnectTimeout != defaultClientConnectTimeout {
		t.Fatalf("unexpected default client connect timeout %s", tunnel.clientConnectTimeout)
	}
	if tunnel.targetConnectTimeout != defaultTargetConnectTimeout {
		t.Fatalf("unexpected default target connect timeout %s", tunnel.targetConnectTimeout)
	}
	if tunnel.targetConnectRetryCount != defaultTargetConnectRetryCount {
		t.Fatalf("unexpected default target connect retry count %d", tunnel.targetConnectRetryCount)
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

	tunnel := NewTunnel(0, "tcp", false, &sync.Map{}, 60, 1, 1, 1)
	conn, err := tunnel.dialLocalProxyTargetWithRetry("tcp", listener.Addr().String())
	if err != nil {
		t.Fatalf("dialLocalProxyTargetWithRetry returned error: %v", err)
	}
	_ = conn.Close()
	<-done
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
