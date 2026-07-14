package bridge

import (
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
