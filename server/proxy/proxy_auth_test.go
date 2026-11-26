package proxy

import (
	"encoding/base64"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"ehang.io/nps/lib/conn"
	"ehang.io/nps/lib/file"
)

func testProxyAuthTunnel(mode, authUser, authPass, clientUser, clientPass string, accounts map[string]string) *file.Tunnel {
	tunnel := &file.Tunnel{
		Mode:     mode,
		AuthUser: authUser,
		AuthPass: authPass,
		Client: &file.Client{
			Cnf: &file.Config{U: clientUser, P: clientPass},
		},
	}
	if accounts != nil {
		tunnel.MultiAccount = &file.MultiAccount{AccountMap: accounts}
	}
	return tunnel
}

func proxyAuthorization(user, pass string) string {
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(user+":"+pass))
}

func TestProxyAuthRejectsWrongCredentialsWith407(t *testing.T) {
	server := &TunnelModeServer{BaseServer: BaseServer{task: testProxyAuthTunnel("httpProxy", "proxy", "secret", "", "", nil)}}
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	c := conn.NewConn(serverConn)
	request := httptest.NewRequest(http.MethodConnect, "http://example.com", nil)
	request.Header.Set("Proxy-Authorization", proxyAuthorization("proxy", "wrong"))

	errCh := make(chan error, 1)
	go func() {
		errCh <- server.proxyAuth(request, c)
	}()
	if err := clientConn.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	response, _ := io.ReadAll(clientConn)
	if err := <-errCh; err == nil {
		t.Fatal("wrong proxy credentials should be rejected")
	}
	text := string(response)
	if !strings.HasPrefix(text, "HTTP/1.1 407 Proxy Authentication Required\r\n") {
		t.Fatalf("unexpected proxy auth response: %q", text)
	}
	if !strings.Contains(text, "Proxy-Authenticate: Basic") {
		t.Fatalf("proxy auth response should ask for Basic credentials: %q", text)
	}
}

func TestProxyAuthAcceptsCorrectCredentials(t *testing.T) {
	server := &TunnelModeServer{BaseServer: BaseServer{task: testProxyAuthTunnel("httpProxy", "proxy", "secret", "", "", nil)}}
	request := httptest.NewRequest(http.MethodGet, "http://example.com", nil)
	request.Header.Set("Proxy-Authorization", proxyAuthorization("proxy", "secret"))

	if err := server.proxyAuth(request, conn.NewConn(nil)); err != nil {
		t.Fatalf("correct proxy credentials rejected: %s", err)
	}
}

func TestProxyAuthFallsBackToClientCredentials(t *testing.T) {
	server := &TunnelModeServer{BaseServer: BaseServer{task: testProxyAuthTunnel("httpProxy", "", "", "client", "client-pass", nil)}}
	request := httptest.NewRequest(http.MethodGet, "http://example.com", nil)
	request.Header.Set("Proxy-Authorization", proxyAuthorization("client", "client-pass"))

	if err := server.proxyAuth(request, conn.NewConn(nil)); err != nil {
		t.Fatalf("client-level proxy credentials rejected: %s", err)
	}
}

func TestProxyAuthOpenWhenNothingConfigured(t *testing.T) {
	server := &TunnelModeServer{BaseServer: BaseServer{task: testProxyAuthTunnel("httpProxy", "", "", "", "", nil)}}
	request := httptest.NewRequest(http.MethodGet, "http://example.com", nil)

	if err := server.proxyAuth(request, conn.NewConn(nil)); err != nil {
		t.Fatalf("proxy without credentials should stay open: %s", err)
	}
}

func TestSocks5AuthUsesTunnelCredentialsFirst(t *testing.T) {
	server := &Sock5ModeServer{BaseServer: BaseServer{task: testProxyAuthTunnel("socks5", "proxy", "secret", "client", "client-pass", map[string]string{"multi": "multi-pass"})}}

	if !server.authRequired() {
		t.Fatal("socks5 auth should be required")
	}
	if !server.checkAuth("proxy", "secret") {
		t.Fatal("tunnel credentials should authenticate")
	}
	if server.checkAuth("client", "client-pass") {
		t.Fatal("client credentials should not authenticate when tunnel credentials are set")
	}
	if server.checkAuth("multi", "multi-pass") {
		t.Fatal("multi-account credentials should not authenticate when tunnel credentials are set")
	}
}

func TestSocks5AuthFallsBackToMultiAccount(t *testing.T) {
	server := &Sock5ModeServer{BaseServer: BaseServer{task: testProxyAuthTunnel("socks5", "", "", "client", "client-pass", map[string]string{"multi": "multi-pass"})}}

	if !server.checkAuth("multi", "multi-pass") {
		t.Fatal("multi-account credentials should authenticate")
	}
	if server.checkAuth("client", "client-pass") {
		t.Fatal("client credentials should be ignored when multi-account is configured")
	}
}

func TestSocks5AuthFallsBackToClientCredentials(t *testing.T) {
	server := &Sock5ModeServer{BaseServer: BaseServer{task: testProxyAuthTunnel("socks5", "", "", "client", "client-pass", nil)}}

	if !server.authRequired() {
		t.Fatal("socks5 auth should be required")
	}
	if !server.checkAuth("client", "client-pass") {
		t.Fatal("client credentials should authenticate")
	}
	if server.checkAuth("client", "wrong") {
		t.Fatal("wrong client credentials should be rejected")
	}
}

func TestSocks5AuthOpenWhenNothingConfigured(t *testing.T) {
	server := &Sock5ModeServer{BaseServer: BaseServer{task: testProxyAuthTunnel("socks5", "", "", "", "", map[string]string{})}}

	if server.authRequired() {
		t.Fatal("socks5 without credentials should not require auth")
	}
}

func TestStripProxyAuthorizationHeader(t *testing.T) {
	raw := "GET http://example.com/ HTTP/1.1\r\nHost: example.com\r\nproxy-authorization: Basic eA==\r\nProxy-Connection: keep-alive\r\n\r\n"
	want := "GET http://example.com/ HTTP/1.1\r\nHost: example.com\r\nProxy-Connection: keep-alive\r\n\r\n"

	if got := string(stripProxyAuthorizationHeader([]byte(raw))); got != want {
		t.Fatalf("stripped request = %q, want %q", got, want)
	}
	if got := string(stripProxyAuthorizationHeader(nil)); got != "" {
		t.Fatalf("empty request = %q", got)
	}
}
