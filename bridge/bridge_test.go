package bridge

import (
	"sync"
	"testing"
	"time"
)

func TestNewTunnelProxyConnectRetryConfig(t *testing.T) {
	tunnel := NewTunnel(0, "tcp", false, &sync.Map{}, 60, 3, 0)
	if tunnel.proxyConnectTimeout != 3*time.Second {
		t.Fatalf("unexpected proxy connect timeout %s", tunnel.proxyConnectTimeout)
	}
	if tunnel.proxyConnectRetryCount != 0 {
		t.Fatalf("unexpected proxy connect retry count %d", tunnel.proxyConnectRetryCount)
	}
}

func TestNewTunnelProxyConnectRetryDefault(t *testing.T) {
	tunnel := NewTunnel(0, "tcp", false, &sync.Map{}, 60, 0, -1)
	if tunnel.proxyConnectTimeout != defaultProxyConnectTimeout {
		t.Fatalf("unexpected default proxy connect timeout %s", tunnel.proxyConnectTimeout)
	}
	if tunnel.proxyConnectRetryCount != defaultProxyConnectRetryCount {
		t.Fatalf("unexpected default proxy connect retry count %d", tunnel.proxyConnectRetryCount)
	}
}
