package goroutine

import (
	"ehang.io/nps/lib/common"
	"ehang.io/nps/lib/file"
	"io"
	"net"
	"sync"
)

// CopyConns copies a full-duplex stream until either direction ends. Closing
// both ends at that point is important: it wakes the other copy operation and
// guarantees that no worker is retained by a half-dead mux connection.
func CopyConns(conn1 io.ReadWriteCloser, conn2 net.Conn, flow *file.Flow) {
	var wg sync.WaitGroup
	var closeOnce sync.Once
	var in, out int64

	closeBoth := func() {
		_ = conn1.Close()
		_ = conn2.Close()
	}
	copyOne := func(dst io.Writer, src io.Reader, n *int64) {
		defer wg.Done()
		*n, _ = common.CopyBuffer(dst, src)
		closeOnce.Do(closeBoth)
	}

	wg.Add(2)
	go copyOne(conn1, conn2, &in) // outside to mux: incoming
	copyOne(conn2, conn1, &out)   // mux to outside: outgoing
	wg.Wait()
	if flow != nil {
		flow.Add(in, out)
	}
}
