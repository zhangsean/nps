package proxy

import (
	"bytes"
	"crypto/tls"
	"io"
	"net"
	"testing"
	"time"
)

type chunkedConn struct {
	chunks [][]byte
	index  int
	offset int
}

func (c *chunkedConn) Read(p []byte) (int, error) {
	if c.index >= len(c.chunks) {
		return 0, io.EOF
	}
	n := copy(p, c.chunks[c.index][c.offset:])
	c.offset += n
	if c.offset >= len(c.chunks[c.index]) {
		c.index++
		c.offset = 0
	}
	return n, nil
}

func (c *chunkedConn) Write(p []byte) (int, error)        { return len(p), nil }
func (c *chunkedConn) Close() error                       { return nil }
func (c *chunkedConn) LocalAddr() net.Addr                { return &net.TCPAddr{} }
func (c *chunkedConn) RemoteAddr() net.Addr               { return &net.TCPAddr{} }
func (c *chunkedConn) SetDeadline(t time.Time) error      { return nil }
func (c *chunkedConn) SetReadDeadline(t time.Time) error  { return nil }
func (c *chunkedConn) SetWriteDeadline(t time.Time) error { return nil }

// captureClientHello records the ClientHello produced by crypto/tls.
func captureClientHello(t *testing.T, serverName string) []byte {
	t.Helper()
	clientSide, serverSide := net.Pipe()
	done := make(chan struct{})
	go func() {
		defer close(done)
		client := tls.Client(clientSide, &tls.Config{ServerName: serverName, InsecureSkipVerify: true})
		_ = client.Handshake()
	}()

	_ = serverSide.SetReadDeadline(time.Now().Add(5 * time.Second))
	var hello []byte
	buf := make([]byte, 4096)
	for {
		n, err := serverSide.Read(buf)
		if n > 0 {
			hello = append(hello, buf[:n]...)
		}
		if err != nil {
			break
		}
		if len(hello) >= 5 {
			recordLen := int(hello[3])<<8 | int(hello[4])
			if len(hello) >= 5+recordLen {
				break
			}
		}
	}
	_ = serverSide.Close()
	<-done
	if len(hello) == 0 {
		t.Fatal("failed to capture client hello")
	}
	return hello
}

func TestGetServerNameFromClientHelloSplitAcrossReads(t *testing.T) {
	hello := captureClientHello(t, "waf.zjwm.cc")

	const chunkSize = 7
	var chunks [][]byte
	for offset := 0; offset < len(hello); offset += chunkSize {
		end := offset + chunkSize
		if end > len(hello) {
			end = len(hello)
		}
		chunks = append(chunks, append([]byte(nil), hello[offset:end]...))
	}

	sni, raw := GetServerNameFromClientHello(&chunkedConn{chunks: chunks})
	if sni != "waf.zjwm.cc" {
		t.Fatalf("got SNI %q, want %q", sni, "waf.zjwm.cc")
	}
	if !bytes.Equal(raw, hello) {
		t.Fatalf("raw bytes mismatch: got %d bytes, want %d bytes", len(raw), len(hello))
	}
}

func TestGetServerNameFromClientHelloSingleRead(t *testing.T) {
	hello := captureClientHello(t, "example.com")
	sni, raw := GetServerNameFromClientHello(&chunkedConn{chunks: [][]byte{hello}})
	if sni != "example.com" {
		t.Fatalf("got SNI %q, want %q", sni, "example.com")
	}
	if !bytes.Equal(raw, hello) {
		t.Fatalf("raw bytes mismatch: got %d bytes, want %d bytes", len(raw), len(hello))
	}
}

func TestGetServerNameFromClientHelloNonHandshakeRecord(t *testing.T) {
	sni, raw := GetServerNameFromClientHello(&chunkedConn{chunks: [][]byte{{0x15, 0x03, 0x03, 0x00, 0x02, 0x02, 0x28}}})
	if sni != "" {
		t.Fatalf("got SNI %q, want empty", sni)
	}
	if len(raw) != 5 {
		t.Fatalf("got %d raw bytes, want 5", len(raw))
	}
}
