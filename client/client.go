package client

import (
	"bufio"
	"bytes"
	"ehang.io/nps/lib/nps_mux"
	"errors"
	"io"
	"math/rand"
	"net"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/astaxie/beego/logs"
	"github.com/xtaci/kcp-go"

	"ehang.io/nps/lib/common"
	"ehang.io/nps/lib/config"
	"ehang.io/nps/lib/conn"
	"ehang.io/nps/lib/crypt"
)

var targetConnectRetrySleep = time.Sleep

type TRPClient struct {
	svrAddr        string
	bridgeConnType string
	proxyUrl       string
	vKey           string
	cip            string
	cipUrl         string
	cipInterval    time.Duration
	lastCip        string
	p2pAddr        map[string]string
	tunnel         *nps_mux.Mux
	signal         *conn.Conn
	ticker         *time.Ticker
	closeCh        chan struct{}
	cnf            *config.Config
	disconnectTime int
	once           sync.Once
	cipMu          sync.Mutex
}

const (
	DefaultCipURL      = "http://www.3322.org/dyndns/getip"
	DefaultCipInterval = 3600
	minCipInterval     = 30
)

var ipv4Pattern = regexp.MustCompile(`\b(?:\d{1,3}\.){3}\d{1,3}\b`)

// new client
func NewRPClient(svraddr string, vKey string, bridgeConnType string, proxyUrl string, cnf *config.Config, disconnectTime int, cip string, cipUrl string, cipInterval int) *TRPClient {
	var displayIP string
	if cipInterval <= 0 {
		cipInterval = DefaultCipInterval
	}
	if cipInterval < minCipInterval {
		cipInterval = minCipInterval
	}
	displayIP = strings.TrimSpace(cip)
	cipUrl = strings.TrimSpace(cipUrl)
	if cipUrl == "" {
		cipUrl = DefaultCipURL
	}
	return &TRPClient{
		svrAddr:        svraddr,
		p2pAddr:        make(map[string]string, 0),
		vKey:           vKey,
		bridgeConnType: bridgeConnType,
		proxyUrl:       proxyUrl,
		cip:            displayIP,
		cipUrl:         cipUrl,
		cipInterval:    time.Duration(cipInterval) * time.Second,
		cnf:            cnf,
		disconnectTime: disconnectTime,
		closeCh:        make(chan struct{}),
		once:           sync.Once{},
	}
}

var NowStatus int
var CloseClient bool

// start
func (s *TRPClient) Start() {
	CloseClient = false
retry:
	if CloseClient {
		return
	}
	NowStatus = 0
	c, err := NewConn(s.bridgeConnType, s.vKey, s.svrAddr, common.WORK_MAIN, s.proxyUrl)
	if err != nil {
		logs.Error("The connection server failed and will be reconnected in five seconds, error", err.Error())
		time.Sleep(time.Second * 5)
		goto retry
	}
	if c == nil {
		logs.Error("Error data from server, and will be reconnected in five seconds")
		time.Sleep(time.Second * 5)
		goto retry
	}
	logs.Info("Successful connection with server %s", s.svrAddr)
	s.signal = c
	if s.cip != "" {
		s.reportCip(s.cip)
	}
	if s.cipUrl != "" {
		go s.watchCip()
	}
	//monitor the connection
	go s.ping()
	//start a channel connection
	go s.newChan()
	//start health check if the it's open
	if s.cnf != nil && len(s.cnf.Healths) > 0 {
		go heathCheck(s.cnf.Healths, s.signal)
	}
	NowStatus = 1
	//msg connection, eg udp
	s.handleMain()
}

func (s *TRPClient) reportCip(addr string) bool {
	rawAddr := addr
	addr, ok := common.NormalizeClientDisplayAddr(addr)
	if !ok {
		logs.Warn("cip %s is invalid, ignore it", rawAddr)
		return false
	}
	s.cipMu.Lock()
	if s.lastCip == addr {
		s.cipMu.Unlock()
		return true
	}
	s.cipMu.Unlock()
	if _, err := s.signal.SendHealthInfo(common.CIP_PREFIX+addr, "1"); err != nil {
		logs.Warn("report cip %s error: %s", addr, err.Error())
		return false
	}
	s.cipMu.Lock()
	s.lastCip = addr
	s.cipMu.Unlock()
	logs.Info("report cip %s success", addr)
	return true
}

func (s *TRPClient) watchCip() {
	s.fetchAndReportCip()
	ticker := time.NewTicker(s.cipInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			s.fetchAndReportCip()
		case <-s.closeCh:
			return
		}
	}
}

func (s *TRPClient) fetchAndReportCip() {
	addr, err := fetchPublicCip(s.cipUrl)
	if err != nil {
		logs.Warn("query cip from %s error: %s", s.cipUrl, err.Error())
		return
	}
	s.reportCip(addr)
}

func fetchPublicCip(cipUrl string) (string, error) {
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(cipUrl)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return "", errors.New("unexpected status " + resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 512))
	if err != nil {
		return "", err
	}
	return extractPublicCipResponse(string(body))
}

func extractPublicCipResponse(body string) (string, error) {
	for _, match := range ipv4Pattern.FindAllString(body, -1) {
		addr, ok := common.NormalizeClientDisplayAddr(match)
		if ok {
			return addr, nil
		}
	}
	return "", errors.New("invalid cip response")
}

// handle main connection
func (s *TRPClient) handleMain() {
	for {
		flags, err := s.signal.ReadFlag()
		if err != nil {
			logs.Error("Accept server data error %s, end this service", err.Error())
			break
		}
		switch flags {
		case common.NEW_UDP_CONN:
			//read server udp addr and password
			if lAddr, err := s.signal.GetShortLenContent(); err != nil {
				logs.Warn(err)
				return
			} else if pwd, err := s.signal.GetShortLenContent(); err == nil {
				var localAddr string
				//The local port remains unchanged for a certain period of time
				if v, ok := s.p2pAddr[crypt.Md5(string(pwd)+strconv.Itoa(int(time.Now().Unix()/100)))]; !ok {
					tmpConn, err := common.GetLocalUdpAddr()
					if err != nil {
						logs.Error(err)
						return
					}
					localAddr = tmpConn.LocalAddr().String()
				} else {
					localAddr = v
				}
				go s.newUdpConn(localAddr, string(lAddr), string(pwd))
			}
		}
	}
	s.Close()
}

func (s *TRPClient) newUdpConn(localAddr, rAddr string, md5Password string) {
	var localConn net.PacketConn
	var err error
	var remoteAddress string
	if remoteAddress, localConn, err = handleP2PUdp(localAddr, rAddr, md5Password, common.WORK_P2P_PROVIDER); err != nil {
		logs.Error(err)
		return
	}
	l, err := kcp.ServeConn(nil, 150, 3, localConn)
	if err != nil {
		logs.Error(err)
		return
	}
	logs.Trace("start local p2p udp listen, local address", localConn.LocalAddr().String())
	for {
		udpTunnel, err := l.AcceptKCP()
		if err != nil {
			logs.Error(err)
			l.Close()
			return
		}
		if udpTunnel.RemoteAddr().String() == string(remoteAddress) {
			conn.SetUdpSession(udpTunnel)
			logs.Trace("successful connection with client ,address %s", udpTunnel.RemoteAddr().String())
			//read link info from remote
			conn.Accept(nps_mux.NewMux(udpTunnel, s.bridgeConnType, s.disconnectTime), func(c net.Conn) {
				go s.handleChan(c)
			})
			break
		}
	}
}

// pmux tunnel
func (s *TRPClient) newChan() {
	tunnel, err := NewConn(s.bridgeConnType, s.vKey, s.svrAddr, common.WORK_CHAN, s.proxyUrl)
	if err != nil {
		logs.Error("connect to ", s.svrAddr, "error:", err)
		return
	}
	s.tunnel = nps_mux.NewMux(tunnel.Conn, s.bridgeConnType, s.disconnectTime)
	for {
		src, err := s.tunnel.Accept()
		if err != nil {
			logs.Warn(err)
			s.Close()
			break
		}
		go s.handleChan(src)
	}
}

func (s *TRPClient) handleChan(src net.Conn) {
	lk, err := conn.NewConn(src).GetLinkInfo()
	if err != nil || lk == nil {
		src.Close()
		logs.Error("get connection info from server error ", err)
		return
	}
	//host for target processing
	lk.Host = common.FormatAddress(lk.Host)
	lk.TargetHosts = formatTargetHosts(lk.Host, lk.TargetHosts)
	//if Conn type is http, read the request and log
	if lk.ConnType == "http" {
		if targetConn, err := dialTargetsWithRetry(common.CONN_TCP, lk.TargetHosts, lk.Option.Timeout, lk.Option.RetryCount, lk.Option.RetryInterval); err != nil {
			logs.Warn("connect to %s error %s", lk.Host, err.Error())
			src.Close()
		} else {
			srcConn := conn.GetConn(src, lk.Crypt, lk.Compress, nil, false)
			go func() {
				common.CopyBuffer(srcConn, targetConn)
				srcConn.Close()
				targetConn.Close()
			}()
			for {
				if r, err := http.ReadRequest(bufio.NewReader(srcConn)); err != nil {
					logs.Error("http read error:", err.Error())
					srcConn.Close()
					targetConn.Close()
					break
				} else {
					remoteAddr := strings.TrimSpace(r.Header.Get("X-Forwarded-For"))
					if len(remoteAddr) == 0 {
						remoteAddr = r.RemoteAddr
					}
					logs.Trace("http request, method %s, host %s, url %s, remote address %s", r.Method, r.Host, r.URL.Path, remoteAddr)
					r.Write(targetConn)
				}
			}
		}
		return
	}
	if lk.ConnType == "udp5" {
		logs.Trace("new %s connection with the goal of %s, remote address:%s", lk.ConnType, lk.Host, lk.RemoteAddr)
		s.handleUdp(src)
	}
	//connect to target if conn type is tcp or udp
	if targetConn, err := dialTargetsWithRetry(lk.ConnType, lk.TargetHosts, lk.Option.Timeout, lk.Option.RetryCount, lk.Option.RetryInterval); err != nil {
		logs.Warn("connect to %s error %s", lk.Host, err.Error())
		src.Close()
	} else {
		logs.Trace("new %s connection with the goal of %s, remote address:%s", lk.ConnType, lk.Host, lk.RemoteAddr)
		conn.CopyWaitGroup(src, targetConn, lk.Crypt, lk.Compress, nil, nil, false, nil, nil)
	}
}

func dialTargetWithRetry(connType string, targetHost string, timeout time.Duration, retryCount int, retryInterval time.Duration) (targetConn net.Conn, err error) {
	return dialTargetsWithRetry(connType, []string{targetHost}, timeout, retryCount, retryInterval)
}

func dialTargetsWithRetry(connType string, targetHosts []string, timeout time.Duration, retryCount int, retryInterval time.Duration) (targetConn net.Conn, err error) {
	targetHosts = formatTargetHosts("", targetHosts)
	if len(targetHosts) == 0 {
		return nil, net.InvalidAddrError("empty target host")
	}
	attempts := retryCount + 1
	if attempts < 1 {
		attempts = 1
	}
	for attempt := 1; attempt <= attempts; attempt++ {
		targetHost := targetHosts[(attempt-1)%len(targetHosts)]
		targetConn, err = net.DialTimeout(connType, targetHost, timeout)
		if err == nil {
			if attempt > 1 {
				logs.Info("target connect retry success, conn type %s, target %s, attempt %d/%d", connType, targetHost, attempt, attempts)
			}
			return targetConn, nil
		}
		if attempt == attempts {
			return nil, err
		}
		delay := randomTargetConnectRetryDelay(retryInterval)
		if delay > 0 {
			logs.Warn("target connect failed, conn type %s, target %s, attempt %d/%d, timeout %s, retry after %s, error %s", connType, targetHost, attempt, attempts, timeout, delay, err.Error())
			targetConnectRetrySleep(delay)
		} else {
			logs.Warn("target connect failed, conn type %s, target %s, attempt %d/%d, timeout %s, retry next, error %s", connType, targetHost, attempt, attempts, timeout, err.Error())
		}
	}
	return nil, err
}

func formatTargetHosts(primary string, targetHosts []string) []string {
	formatted := make([]string, 0, len(targetHosts)+1)
	seen := make(map[string]struct{}, len(targetHosts)+1)
	add := func(targetHost string) {
		targetHost = strings.TrimSpace(targetHost)
		if targetHost == "" {
			return
		}
		targetHost = common.FormatAddress(targetHost)
		if _, ok := seen[targetHost]; ok {
			return
		}
		seen[targetHost] = struct{}{}
		formatted = append(formatted, targetHost)
	}
	add(primary)
	for _, targetHost := range targetHosts {
		add(targetHost)
	}
	return formatted
}

func randomTargetConnectRetryDelay(maxInterval time.Duration) time.Duration {
	maxMs := maxInterval.Milliseconds()
	if maxMs <= 0 {
		return 0
	}
	return time.Duration(rand.Int63n(maxMs)+1) * time.Millisecond
}

func (s *TRPClient) handleUdp(serverConn net.Conn) {
	// bind a local udp port
	local, err := net.ListenUDP("udp", nil)
	defer serverConn.Close()
	if err != nil {
		logs.Error("bind local udp port error ", err.Error())
		return
	}
	defer local.Close()
	go func() {
		defer serverConn.Close()
		b := common.BufPoolUdp.Get().([]byte)
		defer common.BufPoolUdp.Put(b)
		for {
			n, raddr, err := local.ReadFrom(b)
			if err != nil {
				logs.Error("read data from remote server error", err.Error())
			}
			buf := bytes.Buffer{}
			dgram := common.NewUDPDatagram(common.NewUDPHeader(0, 0, common.ToSocksAddr(raddr)), b[:n])
			dgram.Write(&buf)
			b, err := conn.GetLenBytes(buf.Bytes())
			if err != nil {
				logs.Warn("get len bytes error", err.Error())
				continue
			}
			if _, err := serverConn.Write(b); err != nil {
				logs.Error("write data to remote  error", err.Error())
				return
			}
		}
	}()
	b := common.BufPoolUdp.Get().([]byte)
	defer common.BufPoolUdp.Put(b)
	for {
		n, err := serverConn.Read(b)
		if err != nil {
			logs.Error("read udp data from server error ", err.Error())
			return
		}

		udpData, err := common.ReadUDPDatagram(bytes.NewReader(b[:n]))
		if err != nil {
			logs.Error("unpack data error", err.Error())
			return
		}
		raddr, err := net.ResolveUDPAddr("udp", udpData.Header.Addr.String())
		if err != nil {
			logs.Error("build remote addr err", err.Error())
			continue // drop silently
		}
		_, err = local.WriteTo(udpData.Data, raddr)
		if err != nil {
			logs.Error("write data to remote ", raddr.String(), "error", err.Error())
			return
		}
	}
}

// Whether the monitor channel is closed
func (s *TRPClient) ping() {
	s.ticker = time.NewTicker(time.Second * 5)
loop:
	for {
		select {
		case <-s.ticker.C:
			if s.tunnel != nil && s.tunnel.IsClose {
				s.Close()
				break loop
			}
		}
	}
}

func (s *TRPClient) Close() {
	s.once.Do(s.closing)
}

func (s *TRPClient) closing() {
	CloseClient = true
	NowStatus = 0
	if s.tunnel != nil {
		_ = s.tunnel.Close()
	}
	if s.signal != nil {
		_ = s.signal.Close()
	}
	if s.ticker != nil {
		s.ticker.Stop()
	}
	close(s.closeCh)
}
