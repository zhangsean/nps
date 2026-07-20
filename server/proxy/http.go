package proxy

import (
	"bufio"
	"bytes"
	"crypto/tls"
	"io"
	"math/rand"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"ehang.io/nps/bridge"
	"ehang.io/nps/lib/cache"
	"ehang.io/nps/lib/common"
	"ehang.io/nps/lib/conn"
	"ehang.io/nps/lib/file"
	"ehang.io/nps/server/connection"
	"github.com/astaxie/beego/logs"
)

type httpServer struct {
	BaseServer
	httpPort                int
	httpsPort               int
	httpServer              *http.Server
	httpsServer             *http.Server
	httpsListener           net.Listener
	useCache                bool
	addOrigin               bool
	cache                   *cache.Cache
	cacheLen                int
	upstreamResponseTimeout time.Duration
}

func NewHttp(bridge *bridge.Bridge, c *file.Tunnel, httpPort, httpsPort int, useCache bool, cacheLen int, addOrigin bool, upstreamResponseTimeout time.Duration) *httpServer {
	httpServer := &httpServer{
		BaseServer: BaseServer{
			task:   c,
			bridge: bridge,
			Mutex:  sync.Mutex{},
		},
		httpPort:                httpPort,
		httpsPort:               httpsPort,
		useCache:                useCache,
		cacheLen:                cacheLen,
		addOrigin:               addOrigin,
		upstreamResponseTimeout: upstreamResponseTimeout,
	}
	if useCache {
		httpServer.cache = cache.New(cacheLen)
	}
	return httpServer
}

func (s *httpServer) Start() error {
	var err error
	if s.errorContent, err = common.ReadAllFromFile(filepath.Join(common.GetRunPath(), "web", "static", "page", "error.html")); err != nil {
		s.errorContent = []byte("nps 404")
	}
	if s.httpPort > 0 {
		s.httpServer = s.NewServer(s.httpPort, "http")
		go func() {
			l, err := connection.GetHttpListener()
			if err != nil {
				logs.Error(err)
				os.Exit(0)
			}
			err = s.httpServer.Serve(l)
			if err != nil {
				logs.Error(err)
				os.Exit(0)
			}
		}()
	}
	if s.httpsPort > 0 {
		s.httpsServer = s.NewServer(s.httpsPort, "https")
		go func() {
			s.httpsListener, err = connection.GetHttpsListener()
			if err != nil {
				logs.Error(err)
				os.Exit(0)
			}
			logs.Error(NewHttpsServer(s.httpsListener, s.bridge, s.useCache, s.cacheLen, s.upstreamResponseTimeout).Start())
		}()
	}
	return nil
}

func (s *httpServer) Close() error {
	if s.httpsListener != nil {
		s.httpsListener.Close()
	}
	if s.httpsServer != nil {
		s.httpsServer.Close()
	}
	if s.httpServer != nil {
		s.httpServer.Close()
	}
	return nil
}

func (s *httpServer) handleTunneling(w http.ResponseWriter, r *http.Request) {

	var host *file.Host
	var err error
	if isHealthCheckRequest(r) {
		writeHealthCheck(w, r)
		return
	}
	host, err = file.GetDb().GetInfoByHost(r.Host, r)
	if err != nil {
		s.finishHTTPNotFoundAccessLog(r, getRequestRemoteAddr(r, ""), err.Error())
		if isEmptyRequestHost(r) {
			s.writeHttpNotFound(w, r)
			return
		}
		logs.Debug("the url %s %s %s can't be parsed!", r.URL.Scheme, r.Host, r.RequestURI)
		s.writeHttpNotFound(w, r)
		return
	}

	// 自动 http 301 https
	if host.AutoHttps && r.TLS == nil {
		http.Redirect(w, r, "https://"+r.Host+r.RequestURI, http.StatusMovedPermanently)
		return
	}

	if r.Header.Get("Upgrade") != "" {
		rProxy := WebSocketHttpReverseProxy(s)
		rProxy.ServeHTTP(w, r)
	} else {
		hijacker, ok := w.(http.Hijacker)
		if !ok {
			http.Error(w, "Hijacking not supported", http.StatusInternalServerError)
			return
		}
		c, _, err := hijacker.Hijack()
		if err != nil {
			http.Error(w, err.Error(), http.StatusServiceUnavailable)
		}

		s.handleHttp(conn.NewConn(c), r)
	}

}

func (s *httpServer) handleHttp(c *conn.Conn, r *http.Request) {
	var (
		host         *file.Host
		err          error
		scheme       = r.URL.Scheme
		isReset      bool
		remoteAddr   string
		accessLog    *httpAccessLogRecord
		hostTmp      *file.Host
		hostErr      error
		requestBytes []byte
	)
	defer func() {
		c.Close()
	}()
reset:
	if isReset {
		host.Client.AddConn()
	}

	remoteAddr = strings.TrimSpace(r.Header.Get("X-Forwarded-For"))
	if len(remoteAddr) == 0 {
		remoteAddr = c.RemoteAddr().String()
	}

	// 判断访问地址是否在全局黑名单内
	if IsGlobalBlackIp(c.RemoteAddr().String()) {
		accessLog = newHTTPAccessLogRecord(r, remoteAddr, nil, "", false)
		accessLog.SetStatusCode(http.StatusNotFound)
		accessLog.SetRequestBytes(estimateHTTPAccessLogRequestBytes(r))
		accessLog.SetResponseBytes(s.connectionFailResponseBytes())
		accessLog.FinishWithPhase(httpAccessLogPhaseAccessControl, "global black ip")
		c.Close()
		return
	}

	if host, err = file.GetDb().GetInfoByHost(r.Host, r); err != nil {
		accessLog = newHTTPAccessLogRecord(r, remoteAddr, nil, "", false)
		accessLog.SetStatusCode(http.StatusNotFound)
		accessLog.SetRequestBytes(estimateHTTPAccessLogRequestBytes(r))
		accessLog.SetResponseBytes(s.connectionFailResponseBytes())
		accessLog.FinishWithPhase(httpAccessLogPhaseHostMatch, err.Error())
		if isEmptyRequestHost(r) {
			c.Close()
			return
		}
		logs.Notice("the url %s %s %s can't be parsed!, host %s, url %s, remote address %s", r.URL.Scheme, r.Host, r.RequestURI, r.Host, r.URL.Path, remoteAddr)
		s.writeConnFail(c.Conn)
		c.Close()
		return
	}
	accessLog = newHTTPAccessLogRecord(r, remoteAddr, host, "", false)
	accessLog.SetRequestBytes(estimateHTTPAccessLogRequestBytes(r))

	if err := s.CheckFlowAndConnNum(host.Client); err != nil {
		accessLog.SetStatusCode(http.StatusNotFound)
		accessLog.SetResponseBytes(s.connectionFailResponseBytes())
		accessLog.FinishWithPhase(httpAccessLogPhaseAccessControl, err.Error())
		logs.Warn("client id %d, host id %d, error %s, when https connection", host.Client.Id, host.Id, err.Error())
		s.writeConnFail(c.Conn)
		c.Close()
		return
	}
	if !isReset {
		defer host.Client.AddConn()
	}
	if err = s.auth(r, c, host.Client.Cnf.U, host.Client.Cnf.P); err != nil {
		accessLog.SetStatusCode(http.StatusUnauthorized)
		accessLog.SetResponseBytes(int64(len(common.UnauthorizedBytes)))
		accessLog.FinishWithPhase(httpAccessLogPhaseAuth, err.Error())
		logs.Warn("auth error", err, r.RemoteAddr)
		return
	}

	// 判断访问地址是否在黑名单内
	if common.IsBlackIp(c.RemoteAddr().String(), host.Client.VerifyKey, host.Client.BlackIpList) {
		accessLog.SetStatusCode(http.StatusNotFound)
		accessLog.SetResponseBytes(s.connectionFailResponseBytes())
		accessLog.FinishWithPhase(httpAccessLogPhaseAccessControl, "black ip")
		c.Close()
		return
	}

	for {
		currentAccessLog := accessLog
		if currentAccessLog == nil {
			currentAccessLog = newHTTPAccessLogRecord(r, remoteAddr, host, "", false)
			currentAccessLog.SetRequestBytes(estimateHTTPAccessLogRequestBytes(r))
		}
		accessLog = nil
		//if the cache start and the request is in the cache list, return the cache
		if s.useCache {
			if v, ok := s.cache.Get(filepath.Join(host.Host, r.URL.Path)); ok {
				n, err := c.Write(v.([]byte))
				if err != nil {
					currentAccessLog.SetResponseBytes(int64(n))
					currentAccessLog.FinishWithPhase(httpAccessLogPhaseResponseWrite, "downstream response write failed while returning cache: "+err.Error())
					break
				}
				currentAccessLog.SetStatusCode(http.StatusOK)
				currentAccessLog.SetResponseBytes(int64(n))
				currentAccessLog.Finish("")
				logs.Trace("%s request, method %s, host %s, url %s, remote address %s, return cache", r.URL.Scheme, r.Method, r.Host, r.URL.Path, c.RemoteAddr().String())
				host.Client.Flow.Add(int64(n), int64(n))
				//if return cache and does not create a new conn with client and Connection is not set or close, close the connection.
				if strings.ToLower(r.Header.Get("Connection")) == "close" || strings.ToLower(r.Header.Get("Connection")) == "" {
					break
				}
				goto readReq
			}
		}

		//change the host and header and set proxy setting
		common.ChangeHostAndHeader(r, host.HostChange, host.HeaderChange, c.Conn.RemoteAddr().String())
		requestBytes, err = buildProxyHTTPRequestBytes(r)
		if err != nil {
			statusCode := http.StatusBadGateway
			currentAccessLog.SetPhase(httpAccessLogPhaseRequestBuild)
			currentAccessLog.SetStatusCode(statusCode)
			currentAccessLog.SetResponseBytes(s.httpErrorResponseBytes(statusCode))
			currentAccessLog.Finish("build upstream request failed: " + err.Error())
			s.writeHTTPError(c.Conn, statusCode)
			logs.Error("Build proxy request error:", err)
			break
		}
		currentAccessLog.SetRequestBytes(int64(len(requestBytes)))
		if !s.proxyHTTPRequestWithRetry(c, host, r, requestBytes, currentAccessLog) {
			break
		}
		host.Client.Flow.Add(int64(len(requestBytes)), int64(len(requestBytes)))

	readReq:
		//read req from connection
		r, err = http.ReadRequest(bufio.NewReader(c))
		if err != nil {
			break
		}
		r.URL.Scheme = scheme
		//What happened ，Why one character less???
		r.Method = resetReqMethod(r.Method)
		hostTmp, hostErr = file.GetDb().GetInfoByHost(r.Host, r)
		if hostErr != nil {
			s.finishHTTPNotFoundAccessLogWithBytes(r, remoteAddr, estimateHTTPAccessLogRequestBytes(r), 0, hostErr.Error())
			if isEmptyRequestHost(r) {
				break
			}
			logs.Notice("The url %s://%s%s can't be parsed!", r.URL.Scheme, r.Host, r.RequestURI)
			break
		}
		host = hostTmp
		isReset = true
		goto reset
	}
}

func (s *httpServer) proxyHTTPRequestWithRetry(c *conn.Conn, host *file.Host, r *http.Request, requestBytes []byte, accessLog *httpAccessLogRecord) bool {
	attempts := s.upstreamDisconnectRetryAttempts()
	if attempts < 1 {
		attempts = 1
	}
	retryInterval := s.upstreamDisconnectRetryInterval()
	for attempt := 1; attempt <= attempts; attempt++ {
		ok, retryable, phase, targetAddr, err := s.proxyHTTPRequestOnce(c, host, r, requestBytes, accessLog)
		if ok {
			return true
		}
		if !retryable || attempt == attempts {
			finishedAttempts := attempt
			if phase == httpAccessLogPhaseTargetConnect {
				finishedAttempts = attempts
			}
			s.finishHTTPUpstreamError(c, accessLog, phase, err, finishedAttempts)
			return false
		}
		delay := randomHTTPUpstreamRetryDelay(retryInterval)
		accessLog.WriteUpstreamRetry(conn.RetryInfo{
			Source:   "local_proxy",
			ConnType: common.CONN_TCP,
			Target:   targetAddr,
			Attempt:  attempt,
			Attempts: attempts,
			Delay:    delay,
			Error:    upstreamDisconnectedErrorText(phase, err),
		}, phase)
		if delay > 0 {
			time.Sleep(delay)
		}
	}
	return false
}

func (s *httpServer) proxyHTTPRequestOnce(c *conn.Conn, host *file.Host, r *http.Request, requestBytes []byte, accessLog *httpAccessLogRecord) (ok bool, retryable bool, phase string, targetAddr string, err error) {
	targetHosts, err := host.Target.GetRoundRobinTargets()
	if err != nil {
		return false, false, httpAccessLogPhaseTargetConnect, "", err
	}
	targetAddr = targetHosts[0]
	accessLog.SetTarget(targetAddr)
	logs.Trace("Request: %s %s://%s%s, remote addr %s, remote target %s", r.Method, r.URL.Scheme, r.Host, r.URL.Path, c.RemoteAddr().String(), targetAddr)

	lk := conn.NewLink("http", targetAddr, host.Client.Cnf.Crypt, host.Client.Cnf.Compress, r.RemoteAddr, host.Target.LocalProxy)
	lk.SetTargetHosts(targetHosts)
	lk.SetTargetConnectRetryHook(accessLog.TargetConnectRetryHook("local_proxy"))
	targetConnectStart := time.Now()
	target, err := s.bridge.SendLinkInfo(host.Client.Id, lk, nil)
	accessLog.AddPhaseDuration(httpAccessLogPhaseTargetConnect, time.Since(targetConnectStart))
	if err != nil {
		logs.Notice("connect to target %s error %s", lk.Host, err)
		return false, false, httpAccessLogPhaseTargetConnect, targetAddr, err
	}
	connClient := conn.GetConn(target, lk.Crypt, lk.Compress, host.Client.Rate, true)
	defer connClient.Close()

	requestWriteStart := time.Now()
	lenConn := conn.NewLenConn(connClient)
	if _, err = lenConn.Write(requestBytes); err != nil {
		accessLog.SetPhase(httpAccessLogPhaseRequestWrite)
		accessLog.AddPhaseDuration(httpAccessLogPhaseRequestWrite, time.Since(requestWriteStart))
		logs.Error("Request write error:", err)
		return false, isRetryableUpstreamDisconnect(err), httpAccessLogPhaseRequestWrite, targetAddr, err
	}
	accessLog.AddPhaseDuration(httpAccessLogPhaseRequestWrite, time.Since(requestWriteStart))

	responseHeaderStart := time.Now()
	if s.upstreamResponseTimeout > 0 {
		_ = target.SetReadDeadline(time.Now().Add(s.upstreamResponseTimeout))
	}
	resp, err := http.ReadResponse(bufio.NewReader(connClient), r)
	if s.upstreamResponseTimeout > 0 {
		_ = target.SetReadDeadline(time.Time{})
	}
	accessLog.AddPhaseDuration(httpAccessLogPhaseResponseHeader, time.Since(responseHeaderStart))
	if err != nil || resp == nil {
		if err == nil {
			err = io.ErrUnexpectedEOF
		}
		accessLog.SetPhase(httpAccessLogPhaseResponseHeader)
		return false, isRetryableUpstreamDisconnect(err) && isResponseHeaderDisconnectRetryAllowed(host, r.Method), httpAccessLogPhaseResponseHeader, targetAddr, err
	}
	defer func() {
		if resp.Body != nil {
			_ = resp.Body.Close()
		}
	}()

	accessLog.SetStatusCode(resp.StatusCode)
	responseWriteStart := time.Now()
	responseConn := conn.NewLenConn(c)
	if err = resp.Write(responseConn); err != nil {
		accessLog.SetPhase(httpAccessLogPhaseResponseWrite)
		accessLog.AddPhaseDuration(httpAccessLogPhaseResponseWrite, time.Since(responseWriteStart))
		accessLog.SetResponseBytes(int64(responseConn.Len))
		accessLog.Finish("downstream response write failed after upstream status " + strconv.Itoa(resp.StatusCode) + ": " + err.Error())
		logs.Error(err)
		return false, false, httpAccessLogPhaseResponseWrite, targetAddr, err
	}
	accessLog.SetPhase(httpAccessLogPhaseComplete)
	accessLog.AddPhaseDuration(httpAccessLogPhaseResponseWrite, time.Since(responseWriteStart))
	accessLog.SetResponseBytes(int64(responseConn.Len))
	accessLog.Finish("")
	return true, false, httpAccessLogPhaseComplete, targetAddr, nil
}

func (s *httpServer) finishHTTPUpstreamError(c *conn.Conn, accessLog *httpAccessLogRecord, phase string, err error, attempts int) {
	statusCode := upstreamHTTPErrorStatusCode(err)
	if phase == httpAccessLogPhaseTargetConnect {
		statusCode = http.StatusBadGateway
	}
	accessLog.SetPhase(phase)
	accessLog.SetStatusCode(statusCode)
	accessLog.SetResponseBytes(s.httpErrorResponseBytes(statusCode))
	if phase == httpAccessLogPhaseTargetConnect {
		accessLog.Finish(upstreamUnavailableErrorText(err, attempts))
	} else if isRetryableUpstreamDisconnect(err) {
		accessLog.Finish(upstreamDisconnectedFinalErrorText(phase, err, attempts))
	} else if err != nil {
		accessLog.Finish(err.Error())
	} else {
		accessLog.Finish(upstreamUnavailableErrorText(err, attempts))
	}
	s.writeHTTPError(c.Conn, statusCode)
}

type upstreamRetryConfigProvider interface {
	TargetConnectRetryCount() int
	TargetConnectRetryInterval() time.Duration
}

func (s *httpServer) upstreamDisconnectRetryAttempts() int {
	provider, ok := s.bridge.(upstreamRetryConfigProvider)
	if !ok {
		return 1
	}
	retryCount := provider.TargetConnectRetryCount()
	if retryCount < 0 {
		return 1
	}
	return retryCount + 1
}

func (s *httpServer) upstreamDisconnectRetryInterval() time.Duration {
	provider, ok := s.bridge.(upstreamRetryConfigProvider)
	if !ok {
		return 0
	}
	interval := provider.TargetConnectRetryInterval()
	if interval < 0 {
		return 0
	}
	return interval
}

func buildProxyHTTPRequestBytes(r *http.Request) ([]byte, error) {
	var buf bytes.Buffer
	if err := r.Write(&buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func isRetryableUpstreamDisconnect(err error) bool {
	if err == nil {
		return false
	}
	lower := strings.ToLower(err.Error())
	return lower == "eof" ||
		strings.Contains(lower, "unexpected eof") ||
		strings.Contains(lower, "broken pipe") ||
		strings.Contains(lower, "connection reset") ||
		strings.Contains(lower, "forcibly closed") ||
		strings.Contains(lower, "use of closed network connection")
}

func isHTTPMethodRetryableAfterResponseHeaderDisconnect(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return true
	default:
		return false
	}
}

func isResponseHeaderDisconnectRetryAllowed(host *file.Host, method string) bool {
	if isHTTPMethodRetryableAfterResponseHeaderDisconnect(method) {
		return true
	}
	return host != nil && host.ResponseHeaderRetryNonIdempotent
}

func randomHTTPUpstreamRetryDelay(maxInterval time.Duration) time.Duration {
	maxMs := maxInterval.Milliseconds()
	if maxMs <= 0 {
		return 0
	}
	return time.Duration(rand.Int63n(maxMs)+1) * time.Millisecond
}

func upstreamDisconnectedErrorText(phase string, err error) string {
	if err == nil {
		return "upstream disconnected during " + phase
	}
	return "upstream disconnected during " + phase + ": " + err.Error()
}

func upstreamDisconnectedAfterRetriesErrorText(phase string, err error, attempts int) string {
	return upstreamDisconnectedErrorText(phase, err) + " after " + strconv.Itoa(attempts) + " attempts"
}

func upstreamDisconnectedFinalErrorText(phase string, err error, attempts int) string {
	if attempts <= 1 {
		return upstreamDisconnectedErrorText(phase, err)
	}
	return upstreamDisconnectedAfterRetriesErrorText(phase, err, attempts)
}

func upstreamUnavailableErrorText(err error, attempts int) string {
	if err == nil {
		return "upstream unavailable after " + strconv.Itoa(attempts) + " attempts"
	}
	return "upstream unavailable after " + strconv.Itoa(attempts) + " attempts: " + err.Error()
}

func resetReqMethod(method string) string {
	if method == "ET" {
		return "GET"
	}
	if method == "OST" {
		return "POST"
	}
	return method
}

func isEmptyRequestHost(r *http.Request) bool {
	return r == nil || isEmptyHostName(r.Host)
}

func isEmptyHostName(host string) bool {
	return strings.TrimSpace(host) == ""
}

func getRequestRemoteAddr(r *http.Request, fallback string) string {
	if r != nil {
		if remoteAddr := strings.TrimSpace(r.Header.Get("X-Forwarded-For")); remoteAddr != "" {
			return remoteAddr
		}
		if remoteAddr := strings.TrimSpace(r.RemoteAddr); remoteAddr != "" {
			return remoteAddr
		}
	}
	return fallback
}

func isHealthCheckRequest(r *http.Request) bool {
	return r != nil && r.URL != nil && r.URL.Path == "/health"
}

func writeHealthCheck(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		_, _ = w.Write([]byte("ok"))
	}
}

func (s *httpServer) writeHttpNotFound(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusNotFound)
	if r.Method != http.MethodHead {
		_, _ = w.Write(s.errorContent)
	}
}

func (s *httpServer) finishHTTPNotFoundAccessLog(r *http.Request, remoteAddr string, errText string) {
	responseBytes := int64(0)
	if r == nil || r.Method != http.MethodHead {
		responseBytes = int64(len(s.errorContent))
	}
	s.finishHTTPNotFoundAccessLogWithBytes(r, remoteAddr, estimateHTTPAccessLogRequestBytes(r), responseBytes, errText)
}

func (s *httpServer) finishHTTPNotFoundAccessLogWithBytes(r *http.Request, remoteAddr string, requestBytes int64, responseBytes int64, errText string) {
	if errText == "" {
		errText = "host not matched"
	}
	accessLog := newHTTPAccessLogRecord(r, remoteAddr, nil, "", false)
	accessLog.SetPhase(httpAccessLogPhaseHostMatch)
	accessLog.SetStatusCode(http.StatusNotFound)
	accessLog.SetRequestBytes(requestBytes)
	accessLog.SetResponseBytes(responseBytes)
	accessLog.Finish(errText)
}

func (s *httpServer) connectionFailResponseBytes() int64 {
	return s.httpErrorResponseBytes(http.StatusNotFound)
}

func upstreamHTTPErrorStatusCode(err error) int {
	if err == nil {
		return http.StatusBadGateway
	}
	if strings.Contains(strings.ToLower(err.Error()), "timeout") {
		return http.StatusGatewayTimeout
	}
	return http.StatusBadGateway
}

func (s *httpServer) NewServer(port int, scheme string) *http.Server {
	return &http.Server{
		Addr: ":" + strconv.Itoa(port),
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r.URL.Scheme = scheme
			s.handleTunneling(w, r)
		}),
		// Disable HTTP/2.
		TLSNextProto: make(map[string]func(*http.Server, *tls.Conn, http.Handler)),
	}
}

func (s *httpServer) NewServerWithTls(port int, scheme string, l net.Listener, certFile string, keyFile string) error {

	if certFile == "" || keyFile == "" {
		logs.Error("证书文件为空")
		return nil
	}
	var certFileByte = []byte(certFile)
	var keyFileByte = []byte(keyFile)

	config := &tls.Config{}
	config.Certificates = make([]tls.Certificate, 1)

	var err error
	config.Certificates[0], err = tls.X509KeyPair(certFileByte, keyFileByte)
	if err != nil {
		return err
	}

	s2 := &http.Server{
		Addr: ":" + strconv.Itoa(port),
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r.URL.Scheme = scheme
			s.handleTunneling(w, r)
		}),
		// Disable HTTP/2.
		TLSNextProto: make(map[string]func(*http.Server, *tls.Conn, http.Handler)),
		TLSConfig:    config,
	}

	return s2.ServeTLS(l, "", "")
}
