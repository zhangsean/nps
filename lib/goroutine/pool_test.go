package goroutine

import (
	"io"
	"net"
	"testing"
	"time"
)

func TestCopyConnsClosesBothSidesWhenOneDirectionEnds(t *testing.T) {
	copySide1, peer1 := net.Pipe()
	copySide2, peer2 := net.Pipe()
	done := make(chan struct{})
	go func() {
		CopyConns(copySide1, copySide2, nil)
		close(done)
	}()

	payload := []byte("through the tunnel")
	writeDone := make(chan error, 1)
	go func() {
		_, err := peer1.Write(payload)
		writeDone <- err
	}()
	got := make([]byte, len(payload))
	if _, err := io.ReadFull(peer2, got); err != nil {
		t.Fatalf("read copied payload: %v", err)
	}
	if string(got) != string(payload) {
		t.Fatalf("got %q, want %q", got, payload)
	}
	if err := <-writeDone; err != nil {
		t.Fatalf("write payload: %v", err)
	}

	_ = peer1.Close()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("CopyConns did not return after one side closed")
	}

	_ = peer2.SetReadDeadline(time.Now().Add(time.Second))
	if _, err := peer2.Read(make([]byte, 1)); err == nil {
		t.Fatal("other side remained open")
	}
}
