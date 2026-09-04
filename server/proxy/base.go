package proxy

import (
	"errors"
	"fmt"
	"html"
	"net"
	"net/http"
	"sort"
	"strings"
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

var globalBlackIpState = struct {
	sync.RWMutex
	values map[string]struct{}
}{values: make(map[string]struct{})}

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
	s.writeHTTPErrorBody(c, statusCode, s.httpErrorBody(statusCode))
}

func (s *BaseServer) writeHTTPUpstreamError(c net.Conn, statusCode int, targetAddr string, detailLines ...string) {
	s.writeHTTPErrorBody(c, statusCode, s.httpUpstreamErrorBody(statusCode, targetAddr, detailLines...))
}

func (s *BaseServer) writeHTTPErrorBody(c net.Conn, statusCode int, body []byte) {
	if c == nil {
		return
	}
	statusCode, statusText := normalizeHTTPErrorStatus(statusCode)
	_, _ = c.Write([]byte(fmt.Sprintf("HTTP/1.1 %d %s\r\n\r\n", statusCode, statusText)))
	if len(body) > 0 {
		_, _ = c.Write(body)
	}
}

func (s *BaseServer) httpErrorResponseBytes(statusCode int) int64 {
	return httpErrorResponseBytesWithBody(statusCode, s.httpErrorBody(statusCode))
}

func (s *BaseServer) httpUpstreamErrorResponseBytes(statusCode int, targetAddr string, detailLines ...string) int64 {
	return httpErrorResponseBytesWithBody(statusCode, s.httpUpstreamErrorBody(statusCode, targetAddr, detailLines...))
}

func httpErrorResponseBytesWithBody(statusCode int, body []byte) int64 {
	statusCode, statusText := normalizeHTTPErrorStatus(statusCode)
	return int64(len(fmt.Sprintf("HTTP/1.1 %d %s\r\n\r\n", statusCode, statusText)) + len(body))
}

func (s *BaseServer) httpErrorBody(statusCode int) []byte {
	statusCode, statusText := normalizeHTTPErrorStatus(statusCode)
	if statusCode == http.StatusNotFound && len(s.errorContent) > 0 {
		return s.errorContent
	}
	return buildHTTPErrorBody(statusCode, httpErrorDisplayText(statusCode, statusText), "")
}

func (s *BaseServer) httpUpstreamErrorBody(statusCode int, targetAddr string, detailLines ...string) []byte {
	statusCode, statusText := normalizeHTTPErrorStatus(statusCode)
	return buildHTTPErrorBody(statusCode, httpErrorDisplayText(statusCode, statusText), targetAddr, detailLines...)
}

func buildHTTPErrorBody(statusCode int, displayText string, targetAddr string, detailLines ...string) []byte {
	detailHTML := ""
	if targetAddr = strings.TrimSpace(targetAddr); targetAddr != "" {
		detailHTML += fmt.Sprintf("\n<p>Target: %s</p>", html.EscapeString(targetAddr))
	}
	for _, detail := range detailLines {
		if detail = strings.TrimSpace(detail); detail != "" {
			detailHTML += fmt.Sprintf("\n<p>%s</p>", html.EscapeString(detail))
		}
	}
	return []byte(fmt.Sprintf(`<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <title>%d %s</title>
</head>
<body>
<h3>[%d] %s</h3>%s
</body>
</html>`, statusCode, displayText, statusCode, displayText, detailHTML))
}

func httpErrorDisplayText(statusCode int, statusText string) string {
	if statusCode == http.StatusBadGateway {
		return "Bad Upstream"
	}
	return statusText
}

func normalizeHTTPErrorStatus(statusCode int) (int, string) {
	statusText := http.StatusText(statusCode)
	if statusText == "" {
		statusCode = http.StatusInternalServerError
		statusText = http.StatusText(statusCode)
	}
	return statusCode, statusText
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

func ParseGlobalBlackIpList(value string) ([]string, error) {
	items := strings.FieldsFunc(value, func(r rune) bool {
		return r == ',' || r == ';' || r == '\n' || r == '\r' || r == '\t' || r == ' '
	})
	unique := make(map[string]struct{}, len(items))
	for _, item := range items {
		ip := net.ParseIP(strings.TrimSpace(strings.Trim(item, "[]")))
		if ip == nil {
			return nil, fmt.Errorf("global_black_ip_list contains invalid IP address %q", item)
		}
		unique[ip.String()] = struct{}{}
	}
	result := make([]string, 0, len(unique))
	for ip := range unique {
		result = append(result, ip)
	}
	sort.Strings(result)
	return result, nil
}

func SetGlobalBlackIpList(values []string) {
	next := make(map[string]struct{}, len(values))
	for _, value := range values {
		if ip := net.ParseIP(strings.TrimSpace(strings.Trim(value, "[]"))); ip != nil {
			next[ip.String()] = struct{}{}
		}
	}
	globalBlackIpState.Lock()
	globalBlackIpState.values = next
	globalBlackIpState.Unlock()
}

func GlobalBlackIpList() []string {
	globalBlackIpState.RLock()
	result := make([]string, 0, len(globalBlackIpState.values))
	for ip := range globalBlackIpState.values {
		result = append(result, ip)
	}
	globalBlackIpState.RUnlock()
	sort.Strings(result)
	return result
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
		if _, ok := bridge.LocalProxyTargetFastFailRetryAfter(err); ok {
			logs.Trace("get connection from client id %d fast-failed while target is temporarily isolated", client.Id)
		} else {
			logs.Warn("get connection from client id %d  error %s", client.Id, err.Error())
		}
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
	ip := strings.TrimSpace(strings.Trim(ipPort, "[]"))
	if host, _, err := net.SplitHostPort(ipPort); err == nil {
		ip = strings.Trim(host, "[]")
	}
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return false
	}
	globalBlackIpState.RLock()
	_, blocked := globalBlackIpState.values[parsed.String()]
	globalBlackIpState.RUnlock()
	if blocked {
		logs.Error("IP地址[" + parsed.String() + "]在全局黑名单列表内")
	}
	return blocked
}
