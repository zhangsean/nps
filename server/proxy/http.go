package proxy

import (
	"bufio"
	"crypto/tls"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

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
	httpPort      int
	httpsPort     int
	httpServer    *http.Server
	httpsServer   *http.Server
	httpsListener net.Listener
	useCache      bool
	addOrigin     bool
	cache         *cache.Cache
	cacheLen      int
}

type pendingHTTPResponseLog struct {
	request   *http.Request
	accessLog *httpAccessLogRecord
}

func NewHttp(bridge *bridge.Bridge, c *file.Tunnel, httpPort, httpsPort int, useCache bool, cacheLen int, addOrigin bool) *httpServer {
	httpServer := &httpServer{
		BaseServer: BaseServer{
			task:   c,
			bridge: bridge,
			Mutex:  sync.Mutex{},
		},
		httpPort:  httpPort,
		httpsPort: httpsPort,
		useCache:  useCache,
		cacheLen:  cacheLen,
		addOrigin: addOrigin,
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
			logs.Error(NewHttpsServer(s.httpsListener, s.bridge, s.useCache, s.cacheLen).Start())
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
		host       *file.Host
		target     net.Conn
		err        error
		connClient io.ReadWriteCloser
		scheme     = r.URL.Scheme
		lk         *conn.Link
		targetAddr string
		lenConn    *conn.LenConn
		isReset    bool
		wg         sync.WaitGroup
		remoteAddr string
		accessLogs []*httpAccessLogRecord
		accessLog  *httpAccessLogRecord
	)
	defer func() {
		if connClient != nil {
			connClient.Close()
		} else {
			s.writeConnFail(c.Conn)
		}
		c.Close()
	}()
	defer func() {
		for _, accessLog := range accessLogs {
			accessLog.Finish("")
		}
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
		c.Close()
		return
	}

	if host, err = file.GetDb().GetInfoByHost(r.Host, r); err != nil {
		accessLog = newHTTPAccessLogRecord(r, remoteAddr, nil, "", false)
		accessLog.SetStatusCode(http.StatusNotFound)
		accessLog.SetRequestBytes(estimateHTTPAccessLogRequestBytes(r))
		accessLog.SetResponseBytes(s.connectionFailResponseBytes())
		accessLog.Finish(err.Error())
		if isEmptyRequestHost(r) {
			c.Close()
			return
		}
		logs.Notice("the url %s %s %s can't be parsed!, host %s, url %s, remote address %s", r.URL.Scheme, r.Host, r.RequestURI, r.Host, r.URL.Path, remoteAddr)
		c.Close()
		return
	}
	accessLog = newHTTPAccessLogRecord(r, remoteAddr, host, "", false)
	accessLog.SetRequestBytes(estimateHTTPAccessLogRequestBytes(r))

	if err := s.CheckFlowAndConnNum(host.Client); err != nil {
		accessLog.SetStatusCode(http.StatusNotFound)
		accessLog.SetResponseBytes(s.connectionFailResponseBytes())
		accessLog.Finish(err.Error())
		logs.Warn("client id %d, host id %d, error %s, when https connection", host.Client.Id, host.Id, err.Error())
		c.Close()
		return
	}
	if !isReset {
		defer host.Client.AddConn()
	}
	if err = s.auth(r, c, host.Client.Cnf.U, host.Client.Cnf.P); err != nil {
		accessLog.SetStatusCode(http.StatusUnauthorized)
		accessLog.SetResponseBytes(int64(len(common.UnauthorizedBytes)))
		accessLog.Finish(err.Error())
		logs.Warn("auth error", err, r.RemoteAddr)
		return
	}
	if targetAddr, err = host.Target.GetRandomTarget(); err != nil {
		accessLog.SetStatusCode(http.StatusNotFound)
		accessLog.SetResponseBytes(s.connectionFailResponseBytes())
		accessLog.Finish(err.Error())
		logs.Warn(err.Error())
		return
	}
	accessLog.SetTarget(targetAddr)

	// 判断访问地址是否在黑名单内
	if common.IsBlackIp(c.RemoteAddr().String(), host.Client.VerifyKey, host.Client.BlackIpList) {
		accessLog.SetStatusCode(http.StatusNotFound)
		accessLog.SetResponseBytes(s.connectionFailResponseBytes())
		accessLog.Finish("black ip")
		c.Close()
		return
	}

	lk = conn.NewLink("http", targetAddr, host.Client.Cnf.Crypt, host.Client.Cnf.Compress, r.RemoteAddr, host.Target.LocalProxy)
	if target, err = s.bridge.SendLinkInfo(host.Client.Id, lk, nil); err != nil {
		accessLog.SetStatusCode(http.StatusNotFound)
		accessLog.SetResponseBytes(s.connectionFailResponseBytes())
		accessLog.Finish(err.Error())
		logs.Notice("connect to target %s error %s", lk.Host, err)
		return
	}
	connClient = conn.GetConn(target, lk.Crypt, lk.Compress, host.Client.Rate, true)
	pendingResponses := make(chan pendingHTTPResponseLog, 16)

	//read from inc-client
	wg.Add(1)
	go func() {
		isReset = false
		defer connClient.Close()
		defer func() {
			wg.Done()
			if !isReset {
				c.Close()
			}
		}()

		responseReader := bufio.NewReader(connClient)
		for pendingResponse := range pendingResponses {
			resp, err := http.ReadResponse(responseReader, pendingResponse.request)
			if err != nil || resp == nil {
				if pendingResponse.accessLog != nil && err != nil {
					pendingResponse.accessLog.Finish(err.Error())
				}
				return
			}
			pendingResponse.accessLog.SetStatusCode(resp.StatusCode)
			lenConn := conn.NewLenConn(c)
			if err := resp.Write(lenConn); err != nil {
				pendingResponse.accessLog.SetResponseBytes(int64(lenConn.Len))
				pendingResponse.accessLog.Finish(err.Error())
				logs.Error(err)
				return
			}
			if resp.Body != nil {
				_ = resp.Body.Close()
			}
			pendingResponse.accessLog.SetResponseBytes(int64(lenConn.Len))
			pendingResponse.accessLog.Finish("")
		}
	}()

	for {
		currentAccessLog := accessLog
		if currentAccessLog == nil {
			currentAccessLog = newHTTPAccessLogRecord(r, remoteAddr, host, lk.Host, false)
			currentAccessLog.SetRequestBytes(estimateHTTPAccessLogRequestBytes(r))
		} else {
			currentAccessLog.SetTarget(lk.Host)
			accessLog = nil
		}
		//if the cache start and the request is in the cache list, return the cache
		if s.useCache {
			if v, ok := s.cache.Get(filepath.Join(host.Host, r.URL.Path)); ok {
				n, err := c.Write(v.([]byte))
				if err != nil {
					currentAccessLog.Finish(err.Error())
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
		accessLogs = append(accessLogs, currentAccessLog)
		common.ChangeHostAndHeader(r, host.HostChange, host.HeaderChange, c.Conn.RemoteAddr().String())
		logs.Trace("Request: %s %s://%s%s, remote addr %s, remote target %s", r.Method, r.URL.Scheme, r.Host, r.URL.Path, c.RemoteAddr().String(), lk.Host)
		//write
		lenConn = conn.NewLenConn(connClient)
		//lenConn = conn.LenConn
		if err := r.Write(lenConn); err != nil {
			currentAccessLog.SetRequestBytes(int64(lenConn.Len))
			currentAccessLog.Finish(err.Error())
			logs.Error("Request write error:", err)
			break
		}
		currentAccessLog.SetRequestBytes(int64(lenConn.Len))
		pendingResponses <- pendingHTTPResponseLog{request: r, accessLog: currentAccessLog}
		host.Client.Flow.Add(int64(lenConn.Len), int64(lenConn.Len))

	readReq:
		//read req from connection
		r, err = http.ReadRequest(bufio.NewReader(c))
		if err != nil {
			for _, pendingAccessLog := range accessLogs {
				pendingAccessLog.Finish(err.Error())
			}
			break
		}
		r.URL.Scheme = scheme
		//What happened ，Why one character less???
		r.Method = resetReqMethod(r.Method)
		if hostTmp, err := file.GetDb().GetInfoByHost(r.Host, r); err != nil {
			s.finishHTTPNotFoundAccessLogWithBytes(r, remoteAddr, estimateHTTPAccessLogRequestBytes(r), 0, err.Error())
			if isEmptyRequestHost(r) {
				break
			}
			logs.Notice("The url %s://%s%s can't be parsed!", r.URL.Scheme, r.Host, r.RequestURI)
			break
		} else if host != hostTmp {
			host = hostTmp
			isReset = true
			close(pendingResponses)
			connClient.Close()
			wg.Wait()
			goto reset
		}
	}
	close(pendingResponses)
	wg.Wait()
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
	accessLog.SetStatusCode(http.StatusNotFound)
	accessLog.SetRequestBytes(requestBytes)
	accessLog.SetResponseBytes(responseBytes)
	accessLog.Finish(errText)
}

func (s *httpServer) connectionFailResponseBytes() int64 {
	return int64(len(common.ConnectionFailBytes) + len(s.errorContent))
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
