package proxy

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"sort"
	"sync"

	"ehang.io/nps/bridge"
	"ehang.io/nps/lib/common"
	"ehang.io/nps/lib/conn"
	"ehang.io/nps/lib/file"
	"github.com/astaxie/beego/logs"
)

type Service interface {
	Start() error
	Close() error
}

type NetBridge interface {
	SendLinkInfo(clientId int, link *conn.Link, t *file.Tunnel) (target net.Conn, err error)
}

// BaseServer struct
type BaseServer struct {
	id           int
	bridge       NetBridge
	task         *file.Tunnel
	errorContent []byte
	sync.Mutex
}

func NewBaseServer(bridge *bridge.Bridge, task *file.Tunnel) *BaseServer {
	return &BaseServer{
		bridge:       bridge,
		task:         task,
		errorContent: nil,
		Mutex:        sync.Mutex{},
	}
}

// add the flow
func (s *BaseServer) FlowAdd(in, out int64) {
	s.Lock()
	defer s.Unlock()
	s.task.Flow.ExportFlow += out
	s.task.Flow.InletFlow += in
}

// change the flow
func (s *BaseServer) FlowAddHost(host *file.Host, in, out int64) {
	s.Lock()
	defer s.Unlock()
	host.Flow.ExportFlow += out
	host.Flow.InletFlow += in
}

// write fail bytes to the connection
func (s *BaseServer) writeConnFail(c net.Conn) {
	s.writeHTTPError(c, http.StatusNotFound)
}

func (s *BaseServer) writeHTTPError(c net.Conn, statusCode int) {
	if c == nil {
		return
	}
	statusText := http.StatusText(statusCode)
	if statusText == "" {
		statusText = http.StatusText(http.StatusInternalServerError)
		statusCode = http.StatusInternalServerError
	}
	_, _ = c.Write([]byte(fmt.Sprintf("HTTP/1.1 %d %s\r\n\r\n", statusCode, statusText)))
	if len(s.errorContent) > 0 {
		_, _ = c.Write(s.errorContent)
	}
}

func (s *BaseServer) httpErrorResponseBytes(statusCode int) int64 {
	statusText := http.StatusText(statusCode)
	if statusText == "" {
		statusText = http.StatusText(http.StatusInternalServerError)
		statusCode = http.StatusInternalServerError
	}
	return int64(len(fmt.Sprintf("HTTP/1.1 %d %s\r\n\r\n", statusCode, statusText)) + len(s.errorContent))
}

// auth check
func (s *BaseServer) auth(r *http.Request, c *conn.Conn, u, p string) error {
	if u != "" && p != "" && !common.CheckAuth(r, u, p) {
		c.Write([]byte(common.UnauthorizedBytes))
		c.Close()
		return errors.New("401 Unauthorized")
	}
	return nil
}

// check flow limit of the client ,and decrease the allow num of client
func (s *BaseServer) CheckFlowAndConnNum(client *file.Client) error {
	if client.Flow.FlowLimit > 0 && (client.Flow.FlowLimit<<20) < (client.Flow.ExportFlow+client.Flow.InletFlow) {
		return errors.New("Traffic exceeded")
	}
	if !client.GetConn() {
		return errors.New("Connections exceed the current client limit")
	}
	return nil
}

func in(target string, str_array []string) bool {
	sort.Strings(str_array)
	index := sort.SearchStrings(str_array, target)
	if index < len(str_array) && str_array[index] == target {
		return true
	}
	return false
}

// create a new connection and start bytes copying
func (s *BaseServer) DealClient(c *conn.Conn, client *file.Client, addr string,
	rb []byte, tp string, f func(), flow *file.Flow, localProxy bool, task *file.Tunnel, targetHosts []string, retryHooks ...conn.TargetConnectRetryHook) error {

	// 判断访问地址是否在全局黑名单内
	if IsGlobalBlackIp(c.RemoteAddr().String()) {
		c.Close()
		return nil
	}

	// 判断访问地址是否在黑名单内
	if common.IsBlackIp(c.RemoteAddr().String(), client.VerifyKey, client.BlackIpList) {
		c.Close()
		return nil
	}

	link := conn.NewLink(tp, addr, client.Cnf.Crypt, client.Cnf.Compress, c.Conn.RemoteAddr().String(), localProxy)
	link.SetTargetHosts(targetHosts)
	if len(retryHooks) > 0 {
		link.SetTargetConnectRetryHook(retryHooks[0])
	}
	if target, err := s.bridge.SendLinkInfo(client.Id, link, s.task); err != nil {
		logs.Warn("get connection from client id %d  error %s", client.Id, err.Error())
		c.Close()
		return err
	} else {
		if f != nil {
			f()
		}
		conn.CopyWaitGroup(target, c.Conn, link.Crypt, link.Compress, client.Rate, flow, true, rb, task)
	}
	return nil
}

// 判断访问地址是否在全局黑名单内
func IsGlobalBlackIp(ipPort string) bool {
	// 判断访问地址是否在全局黑名单内
	global := file.GetDb().GetGlobal()
	if global != nil {
		ip := common.GetIpByAddr(ipPort)
		if in(ip, global.BlackIpList) {
			logs.Error("IP地址[" + ip + "]在全局黑名单列表内")
			return true
		}
	}

	return false
}
