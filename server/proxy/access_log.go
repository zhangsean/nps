package proxy

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"ehang.io/nps/lib/common"
	"ehang.io/nps/lib/file"
	"github.com/astaxie/beego"
	"github.com/astaxie/beego/logs"
)

type httpAccessLogEntry struct {
	Timestamp      string `json:"timestamp"`
	Method         string `json:"method"`
	Scheme         string `json:"scheme,omitempty"`
	Host           string `json:"host,omitempty"`
	URL            string `json:"url"`
	RemoteAddr     string `json:"remote_addr,omitempty"`
	HostID         int    `json:"host_id,omitempty"`
	ClientID       int    `json:"client_id,omitempty"`
	Target         string `json:"target,omitempty"`
	StatusCode     int    `json:"status_code,omitempty"`
	RequestBytes   int64  `json:"request_bytes,omitempty"`
	ResponseBytes  int64  `json:"response_bytes,omitempty"`
	DurationMS     int64  `json:"duration_ms"`
	TLSPassthrough bool   `json:"tls_passthrough,omitempty"`
	Error          string `json:"error,omitempty"`
	ErrorType      string `json:"error_type,omitempty"`
}

type httpAccessLogRecord struct {
	entry httpAccessLogEntry
	start time.Time
	path  string
	once  sync.Once
}

type httpAccessLogReadCounterConn struct {
	net.Conn
	bytes int64
}

var httpAccessLog = struct {
	once     sync.Once
	mu       sync.Mutex
	file     *os.File
	path     string
	maxSize  int64
	size     int64
	backups  int
	writeCh  chan []byte
	maskKeys map[string]struct{}
	excludes []string
	hosts    []string
	errors   []string
	errTypes map[string]struct{}
	minMs    int64
	fields   map[string]struct{}
	initErr  error
	disabled bool
}{}

var httpAccessLogGzipMu sync.Mutex

func newHTTPAccessLogReadCounterConn(c net.Conn, initialBytes int64) *httpAccessLogReadCounterConn {
	return &httpAccessLogReadCounterConn{Conn: c, bytes: initialBytes}
}

func (c *httpAccessLogReadCounterConn) Read(p []byte) (n int, err error) {
	n, err = c.Conn.Read(p)
	if n > 0 {
		atomic.AddInt64(&c.bytes, int64(n))
	}
	return n, err
}

func (c *httpAccessLogReadCounterConn) Bytes() int64 {
	return atomic.LoadInt64(&c.bytes)
}

func newHTTPAccessLogRecord(r *http.Request, remoteAddr string, host *file.Host, target string, tlsPassthrough bool) *httpAccessLogRecord {
	_ = getHTTPAccessLogFile()
	start := time.Now()
	entry := httpAccessLogEntry{
		Timestamp:      start.Format("2006-01-02 15:04:05.000"),
		RemoteAddr:     remoteAddr,
		Target:         target,
		TLSPassthrough: tlsPassthrough,
	}
	if r != nil {
		entry.Method = r.Method
		entry.Host = r.Host
		if r.URL != nil {
			entry.Scheme = r.URL.Scheme
			entry.URL = requestLogURL(r.URL)
		}
	}
	if entry.URL == "" {
		entry.URL = "/"
	}
	path := "/"
	if r != nil && r.URL != nil {
		path = requestLogPath(r.URL)
	}
	if host != nil {
		entry.HostID = host.Id
		if host.Client != nil {
			entry.ClientID = host.Client.Id
		}
	}
	return &httpAccessLogRecord{entry: entry, start: start, path: path}
}

func (record *httpAccessLogRecord) SetStatusCode(statusCode int) {
	if record != nil {
		record.entry.StatusCode = statusCode
	}
}

func (record *httpAccessLogRecord) SetTarget(target string) {
	if record != nil {
		record.entry.Target = target
	}
}

func (record *httpAccessLogRecord) SetRequestBytes(requestBytes int64) {
	if record != nil {
		record.entry.RequestBytes = requestBytes
	}
}

func (record *httpAccessLogRecord) SetResponseBytes(responseBytes int64) {
	if record != nil {
		record.entry.ResponseBytes = responseBytes
	}
}

func (record *httpAccessLogRecord) Finish(errText string) {
	if record == nil {
		return
	}
	record.once.Do(func() {
		record.entry.DurationMS = time.Since(record.start).Milliseconds()
		record.entry.Error = errText
		record.entry.ErrorType = classifyHTTPAccessLogRecordError(record)
		if shouldSkipHTTPAccessLog(record) {
			return
		}
		line, err := buildHTTPAccessLogLine(record.entry)
		if err != nil {
			logs.Warn("build http access log error: %s", err.Error())
			return
		}
		writeHTTPAccessLog(line)
	})
}

func buildHTTPAccessLogLine(entry httpAccessLogEntry) ([]byte, error) {
	if entry.URL == "" {
		entry.URL = "/"
	}
	fields := httpAccessLog.fields
	var buf bytes.Buffer
	buf.WriteByte('{')
	first := true
	add := func(name string, value interface{}, force bool) {
		if len(fields) > 0 {
			if _, ok := fields[name]; !ok {
				return
			}
		}
		if force {
			writeHTTPAccessLogField(&buf, &first, name, value)
			return
		}
		switch v := value.(type) {
		case string:
			if v != "" {
				writeHTTPAccessLogField(&buf, &first, name, value)
			}
		case int:
			if v != 0 {
				writeHTTPAccessLogField(&buf, &first, name, value)
			}
		case int64:
			if v != 0 {
				writeHTTPAccessLogField(&buf, &first, name, value)
			}
		case bool:
			if v {
				writeHTTPAccessLogField(&buf, &first, name, value)
			}
		default:
			if value != nil {
				writeHTTPAccessLogField(&buf, &first, name, value)
			}
		}
	}
	add("timestamp", entry.Timestamp, true)
	add("method", entry.Method, true)
	add("scheme", entry.Scheme, false)
	add("host", entry.Host, false)
	add("url", entry.URL, true)
	add("remote_addr", entry.RemoteAddr, false)
	add("host_id", entry.HostID, false)
	add("client_id", entry.ClientID, false)
	add("target", entry.Target, false)
	add("status_code", entry.StatusCode, false)
	add("duration_ms", entry.DurationMS, true)
	add("request_bytes", entry.RequestBytes, false)
	add("response_bytes", entry.ResponseBytes, false)
	add("tls_passthrough", entry.TLSPassthrough, false)
	add("error", entry.Error, false)
	add("error_type", entry.ErrorType, false)
	buf.WriteByte('}')
	return buf.Bytes(), nil
}

func writeHTTPAccessLogField(buf *bytes.Buffer, first *bool, name string, value interface{}) {
	if !*first {
		buf.WriteByte(',')
	}
	*first = false
	nameJSON, _ := encodeHTTPAccessLogJSONValue(name)
	valueJSON, _ := encodeHTTPAccessLogJSONValue(value)
	buf.Write(nameJSON)
	buf.WriteByte(':')
	buf.Write(valueJSON)
}

func encodeHTTPAccessLogJSONValue(value interface{}) ([]byte, error) {
	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return nil, err
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}

func buildHTTPAccessLogLineFromRequest(r *http.Request, remoteAddr string, host *file.Host, target string, now time.Time) ([]byte, error) {
	record := newHTTPAccessLogRecord(r, remoteAddr, host, target, false)
	record.start = now
	record.entry.Timestamp = now.Format("2006-01-02 15:04:05.000")
	return buildHTTPAccessLogLine(record.entry)
}

func requestLogURL(u *url.URL) string {
	if u == nil {
		return "/"
	}
	path := requestLogPath(u)
	rawQuery := maskHTTPAccessLogRawQuery(u.RawQuery)
	if rawQuery != "" {
		return path + "?" + rawQuery
	}
	return path
}

func requestLogPath(u *url.URL) string {
	if u == nil {
		return "/"
	}
	path := u.EscapedPath()
	if path == "" {
		path = u.Path
	}
	if path == "" {
		path = "/"
	}
	return path
}

func estimateHTTPAccessLogRequestBytes(r *http.Request) int64 {
	if r == nil {
		return 0
	}
	uri := r.RequestURI
	if uri == "" && r.URL != nil {
		uri = r.URL.RequestURI()
	}
	if uri == "" {
		uri = "/"
	}
	proto := r.Proto
	if proto == "" {
		proto = "HTTP/1.1"
	}
	n := int64(len(r.Method) + 1 + len(uri) + 1 + len(proto) + 2)
	host := r.Host
	if host == "" && r.URL != nil {
		host = r.URL.Host
	}
	if host != "" && r.Header.Get("Host") == "" {
		n += int64(len("Host") + 2 + len(host) + 2)
	}
	if r.ContentLength > 0 && r.Header.Get("Content-Length") == "" {
		n += int64(len("Content-Length") + 2 + len(strconv.FormatInt(r.ContentLength, 10)) + 2)
	}
	for key, values := range r.Header {
		for _, value := range values {
			n += int64(len(key) + 2 + len(value) + 2)
		}
	}
	n += 2
	if r.ContentLength > 0 {
		n += r.ContentLength
	}
	return n
}

func maskHTTPAccessLogRawQuery(rawQuery string) string {
	if rawQuery == "" || len(httpAccessLog.maskKeys) == 0 {
		return rawQuery
	}
	parts := strings.Split(rawQuery, "&")
	for i, part := range parts {
		key := part
		if idx := strings.Index(part, "="); idx >= 0 {
			key = part[:idx]
		}
		unescapedKey, err := url.QueryUnescape(key)
		if err != nil {
			unescapedKey = key
		}
		if _, ok := httpAccessLog.maskKeys[strings.ToLower(unescapedKey)]; ok {
			parts[i] = key + "=***"
		}
	}
	return strings.Join(parts, "&")
}

func shouldSkipHTTPAccessLog(record *httpAccessLogRecord) bool {
	if httpAccessLog.disabled {
		return true
	}
	if httpAccessLog.minMs > 0 && record.entry.DurationMS < httpAccessLog.minMs {
		return true
	}
	for _, pattern := range httpAccessLog.hosts {
		if matchHTTPAccessLogHost(pattern, record.entry.Host) {
			return true
		}
	}
	for _, pattern := range httpAccessLog.excludes {
		if matchHTTPAccessLogPath(pattern, record.path) {
			return true
		}
	}
	for _, pattern := range httpAccessLog.errors {
		if matchHTTPAccessLogError(pattern, record.entry.Error) {
			return true
		}
	}
	if len(httpAccessLog.errTypes) > 0 && record.entry.ErrorType != "" {
		if _, ok := httpAccessLog.errTypes[strings.ToLower(record.entry.ErrorType)]; ok {
			return true
		}
	}
	return false
}

func matchHTTPAccessLogPath(pattern, path string) bool {
	if pattern == "" {
		return false
	}
	if strings.HasSuffix(pattern, "*") {
		return strings.HasPrefix(path, strings.TrimSuffix(pattern, "*"))
	}
	return path == pattern
}

func matchHTTPAccessLogHost(pattern, host string) bool {
	pattern = strings.ToLower(strings.TrimSpace(pattern))
	host = strings.ToLower(strings.TrimSpace(host))
	if pattern == "" || host == "" {
		return false
	}
	if matchHTTPAccessLogWildcard(pattern, host) {
		return true
	}
	if !strings.Contains(pattern, ":") {
		hostOnly := stripHTTPAccessLogHostPort(host)
		if hostOnly != host && matchHTTPAccessLogWildcard(pattern, hostOnly) {
			return true
		}
	}
	return false
}

func stripHTTPAccessLogHostPort(host string) string {
	if host == "" {
		return host
	}
	if strings.HasPrefix(host, "[") {
		if h, _, err := net.SplitHostPort(host); err == nil {
			return strings.Trim(h, "[]")
		}
		return strings.Trim(host, "[]")
	}
	if strings.Count(host, ":") == 1 {
		if h, _, err := net.SplitHostPort(host); err == nil {
			return h
		}
	}
	return host
}

func matchHTTPAccessLogError(pattern, errText string) bool {
	pattern = strings.ToLower(strings.TrimSpace(pattern))
	errText = strings.ToLower(strings.TrimSpace(errText))
	if pattern == "" || errText == "" {
		return false
	}
	return matchHTTPAccessLogWildcard(pattern, errText)
}

func matchHTTPAccessLogWildcard(pattern, value string) bool {
	if !strings.Contains(pattern, "*") {
		return pattern == value
	}
	parts := strings.Split(pattern, "*")
	pos := 0
	for i, part := range parts {
		if part == "" {
			continue
		}
		idx := strings.Index(value[pos:], part)
		if idx < 0 {
			return false
		}
		if i == 0 && !strings.HasPrefix(pattern, "*") && idx != 0 {
			return false
		}
		pos += idx + len(part)
	}
	last := parts[len(parts)-1]
	return strings.HasSuffix(pattern, "*") || last == "" || strings.HasSuffix(value, last)
}

func classifyHTTPAccessLogError(errText string) string {
	if errText == "" {
		return ""
	}
	lower := strings.ToLower(errText)
	switch {
	case strings.Contains(lower, "host could not be parsed"):
		return "host_parse_error"
	case strings.Contains(lower, "timeout"):
		return "timeout"
	case strings.Contains(lower, "broken pipe"),
		strings.Contains(lower, "connection reset"),
		strings.Contains(lower, "forcibly closed"),
		strings.Contains(lower, "use of closed network connection"),
		lower == "eof",
		strings.Contains(lower, "unexpected eof"):
		return "client_closed"
	case strings.Contains(lower, "read response"),
		strings.Contains(lower, "bad gateway"),
		strings.Contains(lower, "connect"):
		return "upstream_error"
	default:
		return "proxy_error"
	}
}

func classifyHTTPAccessLogRecordError(record *httpAccessLogRecord) string {
	if record == nil {
		return ""
	}
	errorType := classifyHTTPAccessLogError(record.entry.Error)
	if errorType != "host_parse_error" {
		return errorType
	}
	if strings.TrimSpace(record.entry.Host) != "" {
		return "host_not_matched"
	}
	return errorType
}

func parseHTTPAccessLogSet(s string) map[string]struct{} {
	items := parseHTTPAccessLogList(s)
	if len(items) == 0 {
		return nil
	}
	result := make(map[string]struct{}, len(items))
	for _, item := range items {
		result[strings.ToLower(item)] = struct{}{}
	}
	return result
}

func parseHTTPAccessLogList(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	rawItems := strings.Split(s, ",")
	items := make([]string, 0, len(rawItems))
	for _, item := range rawItems {
		item = strings.TrimSpace(item)
		if item != "" {
			items = append(items, item)
		}
	}
	return items
}

func writeHTTPAccessLog(line []byte) {
	if getHTTPAccessLogFile() == nil || httpAccessLog.writeCh == nil {
		return
	}
	line = append(line, '\n')
	select {
	case httpAccessLog.writeCh <- line:
	default:
		logs.Warn("http access log channel is full, drop log")
	}
}

func writeHTTPAccessLogWorker() {
	for line := range httpAccessLog.writeCh {
		writeHTTPAccessLogLineLocked(line)
	}
}

func writeHTTPAccessLogLineLocked(line []byte) {
	httpAccessLog.mu.Lock()
	defer httpAccessLog.mu.Unlock()
	if err := rotateHTTPAccessLogIfNeededLocked(int64(len(line))); err != nil {
		logs.Warn("rotate http access log error: %s", err.Error())
	}
	if httpAccessLog.file == nil {
		return
	}
	if _, err := httpAccessLog.file.Write(line); err != nil {
		logs.Warn("write http access log error: %s", err.Error())
		return
	}
	httpAccessLog.size += int64(len(line))
}

func getHTTPAccessLogFile() *os.File {
	httpAccessLog.once.Do(func() {
		logPath := strings.TrimSpace(beego.AppConfig.String("http_access_log_path"))
		if logPath == "" {
			httpAccessLog.disabled = true
			return
		}
		if !filepath.IsAbs(logPath) {
			logPath = filepath.Join(common.GetRunPath(), logPath)
		}
		if err := os.MkdirAll(filepath.Dir(logPath), 0755); err != nil {
			httpAccessLog.initErr = err
			logs.Warn("create http access log dir error: %s", err.Error())
			return
		}
		f, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
		if err != nil {
			httpAccessLog.initErr = err
			logs.Warn("open http access log %s error: %s", logPath, err.Error())
			return
		}
		info, err := f.Stat()
		if err != nil {
			_ = f.Close()
			httpAccessLog.initErr = err
			logs.Warn("stat http access log %s error: %s", logPath, err.Error())
			return
		}
		maxSizeMB := beego.AppConfig.DefaultInt("http_access_log_max_size_mb", 100)
		if maxSizeMB < 0 {
			maxSizeMB = 0
		}
		backups := beego.AppConfig.DefaultInt("http_access_log_max_backups", 3)
		if backups < 0 {
			backups = 0
		}
		httpAccessLog.file = f
		httpAccessLog.path = logPath
		httpAccessLog.maxSize = int64(maxSizeMB) * 1024 * 1024
		httpAccessLog.size = info.Size()
		httpAccessLog.backups = backups
		httpAccessLog.maskKeys = parseHTTPAccessLogSet(beego.AppConfig.String("http_access_log_mask_query_keys"))
		httpAccessLog.excludes = parseHTTPAccessLogList(beego.AppConfig.String("http_access_log_exclude_paths"))
		httpAccessLog.hosts = parseHTTPAccessLogList(beego.AppConfig.String("http_access_log_exclude_hosts"))
		httpAccessLog.errors = parseHTTPAccessLogList(beego.AppConfig.String("http_access_log_exclude_errors"))
		httpAccessLog.errTypes = parseHTTPAccessLogSet(beego.AppConfig.String("http_access_log_exclude_error_types"))
		httpAccessLog.minMs = int64(beego.AppConfig.DefaultInt("http_access_log_min_duration_ms", 0))
		httpAccessLog.fields = parseHTTPAccessLogSet(beego.AppConfig.String("http_access_log_fields"))
		httpAccessLog.writeCh = make(chan []byte, 4096)
		go writeHTTPAccessLogWorker()
	})
	if httpAccessLog.disabled || httpAccessLog.initErr != nil {
		return nil
	}
	return httpAccessLog.file
}

func rotateHTTPAccessLogIfNeededLocked(incoming int64) error {
	if httpAccessLog.file == nil || httpAccessLog.maxSize <= 0 || httpAccessLog.path == "" {
		return nil
	}
	if httpAccessLog.size+incoming <= httpAccessLog.maxSize {
		return nil
	}
	if err := httpAccessLog.file.Close(); err != nil {
		return err
	}
	httpAccessLogGzipMu.Lock()
	gzipMuLocked := true
	defer func() {
		if gzipMuLocked {
			httpAccessLogGzipMu.Unlock()
		}
	}()
	if httpAccessLog.backups > 0 {
		_ = os.Remove(rotatedHTTPAccessLogPath(httpAccessLog.path, httpAccessLog.backups))
		_ = os.Remove(rotatedHTTPAccessLogGzipPath(httpAccessLog.path, httpAccessLog.backups))
		for i := httpAccessLog.backups - 1; i >= 1; i-- {
			src := rotatedHTTPAccessLogPath(httpAccessLog.path, i)
			dst := rotatedHTTPAccessLogPath(httpAccessLog.path, i+1)
			if common.FileExists(src) {
				_ = os.Rename(src, dst)
			}
			srcGzip := rotatedHTTPAccessLogGzipPath(httpAccessLog.path, i)
			dstGzip := rotatedHTTPAccessLogGzipPath(httpAccessLog.path, i+1)
			if common.FileExists(srcGzip) {
				_ = os.Rename(srcGzip, dstGzip)
			}
		}
		if common.FileExists(httpAccessLog.path) {
			rotated := rotatedHTTPAccessLogPath(httpAccessLog.path, 1)
			_ = os.Remove(rotated)
			_ = os.Remove(rotatedHTTPAccessLogGzipPath(httpAccessLog.path, 1))
			if err := os.Rename(httpAccessLog.path, rotated); err != nil {
				return err
			}
			gzipHTTPAccessLogFileAsyncLocked(rotated)
			gzipMuLocked = false
		}
	} else {
		_ = os.Remove(httpAccessLog.path)
	}
	f, err := os.OpenFile(httpAccessLog.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		httpAccessLog.file = nil
		return err
	}
	httpAccessLog.file = f
	httpAccessLog.size = 0
	return nil
}

func rotatedHTTPAccessLogPath(path string, index int) string {
	return path + "." + strconv.Itoa(index)
}

func rotatedHTTPAccessLogGzipPath(path string, index int) string {
	return rotatedHTTPAccessLogPath(path, index) + ".gz"
}

func gzipHTTPAccessLogFileAsync(path string) {
	httpAccessLogGzipMu.Lock()
	gzipHTTPAccessLogFileAsyncLocked(path)
}

func gzipHTTPAccessLogFileAsyncLocked(path string) {
	go func() {
		defer httpAccessLogGzipMu.Unlock()
		if err := gzipHTTPAccessLogFile(path); err != nil {
			logs.Warn("gzip http access log %s error: %s", path, err.Error())
		}
	}()
}

func gzipHTTPAccessLogFile(path string) error {
	src, err := os.Open(path)
	if err != nil {
		return err
	}

	dstPath := path + ".gz"
	tmpPath := dstPath + ".tmp"
	dst, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0644)
	if err != nil {
		_ = src.Close()
		return err
	}
	gzipWriter := gzip.NewWriter(dst)
	_, copyErr := io.Copy(gzipWriter, src)
	closeErr := gzipWriter.Close()
	fileCloseErr := dst.Close()
	srcCloseErr := src.Close()
	if copyErr != nil {
		_ = os.Remove(tmpPath)
		return copyErr
	}
	if closeErr != nil {
		_ = os.Remove(tmpPath)
		return closeErr
	}
	if fileCloseErr != nil {
		_ = os.Remove(tmpPath)
		return fileCloseErr
	}
	if srcCloseErr != nil {
		_ = os.Remove(tmpPath)
		return srcCloseErr
	}
	if err := os.Rename(tmpPath, dstPath); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	return os.Remove(path)
}
