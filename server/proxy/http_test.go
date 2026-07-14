package proxy

import (
	"compress/gzip"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"ehang.io/nps/lib/common"
	"ehang.io/nps/lib/file"
)

func TestHandleTunnelingHealthCheck(t *testing.T) {
	server := &httpServer{
		BaseServer: BaseServer{errorContent: []byte("nps 404")},
	}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "http://127.0.0.1/health", nil)

	server.handleTunneling(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected health check status %d, got %d", http.StatusOK, recorder.Code)
	}
	if recorder.Body.String() != "ok" {
		t.Fatalf("expected health check body ok, got %q", recorder.Body.String())
	}
}

func TestWriteHttpNotFound(t *testing.T) {
	server := &httpServer{
		BaseServer: BaseServer{errorContent: []byte("nps 404")},
	}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "http://unmatched.invalid/", nil)

	server.writeHttpNotFound(recorder, request)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("expected unmatched host status %d, got %d", http.StatusNotFound, recorder.Code)
	}
}

func TestConnectionFailResponseBytes(t *testing.T) {
	server := &httpServer{
		BaseServer: BaseServer{errorContent: []byte("nps 404")},
	}
	want := int64(len(common.ConnectionFailBytes) + len("nps 404"))
	if got := server.connectionFailResponseBytes(); got != want {
		t.Fatalf("unexpected connection fail response bytes %d, want %d", got, want)
	}
}

func TestBuildHTTPAccessLogLine(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "http://example.com/api/list?keyword=nps&page=1", nil)
	request.RequestURI = "/api/list?keyword=nps&page=1"
	request.RemoteAddr = "10.0.0.1:12345"
	host := &file.Host{
		Id:     12,
		Client: &file.Client{Id: 34},
	}

	line, err := buildHTTPAccessLogLineFromRequest(request, getRequestRemoteAddr(request, ""), host, "127.0.0.1:8080", time.Date(2026, 7, 13, 10, 11, 12, 13*int(time.Millisecond), time.Local))
	if err != nil {
		t.Fatalf("build log line error: %v", err)
	}

	var entry httpAccessLogEntry
	if err := json.Unmarshal(line, &entry); err != nil {
		t.Fatalf("log line is not json: %v", err)
	}
	if entry.Timestamp != "2026-07-13 10:11:12.013" {
		t.Fatalf("unexpected timestamp %q", entry.Timestamp)
	}
	if entry.URL != "/api/list?keyword=nps&page=1" {
		t.Fatalf("unexpected url %q", entry.URL)
	}
	if entry.RemoteAddr != "10.0.0.1:12345" {
		t.Fatalf("unexpected remote addr %q", entry.RemoteAddr)
	}
	if entry.HostID != 12 || entry.ClientID != 34 {
		t.Fatalf("unexpected host/client id %d/%d", entry.HostID, entry.ClientID)
	}
	if strings.Contains(string(line), "request_uri") {
		t.Fatalf("request_uri should not be logged: %s", string(line))
	}
	if strings.Contains(string(line), `"query"`) {
		t.Fatalf("query should be appended to url instead of logged separately: %s", string(line))
	}
	if strings.Contains(string(line), `"path"`) {
		t.Fatalf("path should be logged as url: %s", string(line))
	}
}

func TestBuildHTTPAccessLogLineForUnmatchedHost(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "http://missing.example.com/not-found?id=1", nil)
	request.RequestURI = "/not-found?id=1"
	request.RemoteAddr = "10.0.0.3:12345"
	record := newHTTPAccessLogRecord(request, getRequestRemoteAddr(request, ""), nil, "", false)
	record.entry.Timestamp = "2026-07-13 10:11:12.013"
	record.entry.StatusCode = http.StatusNotFound
	record.entry.DurationMS = 7
	record.entry.Error = "host not matched"
	record.entry.ErrorType = classifyHTTPAccessLogError(record.entry.Error)

	line, err := buildHTTPAccessLogLine(record.entry)
	if err != nil {
		t.Fatalf("build log line error: %v", err)
	}

	var entry httpAccessLogEntry
	if err := json.Unmarshal(line, &entry); err != nil {
		t.Fatalf("log line is not json: %v", err)
	}
	if entry.StatusCode != http.StatusNotFound {
		t.Fatalf("unexpected status code %d", entry.StatusCode)
	}
	if entry.Host != "missing.example.com" {
		t.Fatalf("unexpected host %q", entry.Host)
	}
	if entry.URL != "/not-found?id=1" {
		t.Fatalf("unexpected url %q", entry.URL)
	}
	if entry.HostID != 0 || entry.ClientID != 0 {
		t.Fatalf("unmatched host should not include host/client id, got %d/%d", entry.HostID, entry.ClientID)
	}
}

func TestBuildHTTPSAccessLogLineForUnmatchedHost(t *testing.T) {
	request := buildHttpsRequest("missing.example.com")
	record := newHTTPAccessLogRecord(request, "10.0.0.4:44321", nil, "", true)
	record.entry.Timestamp = "2026-07-13 10:11:12.014"
	record.entry.StatusCode = http.StatusNotFound
	record.entry.RequestBytes = 128
	record.entry.DurationMS = 5
	record.entry.Error = "host not matched"
	record.entry.ErrorType = classifyHTTPAccessLogError(record.entry.Error)

	line, err := buildHTTPAccessLogLine(record.entry)
	if err != nil {
		t.Fatalf("build log line error: %v", err)
	}

	var entry httpAccessLogEntry
	if err := json.Unmarshal(line, &entry); err != nil {
		t.Fatalf("log line is not json: %v", err)
	}
	if entry.Method != http.MethodConnect {
		t.Fatalf("unexpected method %q", entry.Method)
	}
	if entry.Scheme != "https" || entry.Host != "missing.example.com" {
		t.Fatalf("unexpected scheme/host %q/%q", entry.Scheme, entry.Host)
	}
	if entry.StatusCode != http.StatusNotFound {
		t.Fatalf("unexpected status code %d", entry.StatusCode)
	}
	if entry.RequestBytes != 128 {
		t.Fatalf("unexpected request bytes %d", entry.RequestBytes)
	}
	if entry.HostID != 0 || entry.ClientID != 0 {
		t.Fatalf("unmatched host should not include host/client id, got %d/%d", entry.HostID, entry.ClientID)
	}
}

func TestBuildHTTPSPassthroughAccessLogLine(t *testing.T) {
	request := buildHttpsRequest("secure.example.com")
	host := &file.Host{
		Id:     56,
		Client: &file.Client{Id: 78},
	}

	record := newHTTPAccessLogRecord(request, "10.0.0.2:44321", host, "127.0.0.1:8443", true)
	record.entry.DurationMS = 123
	line, err := buildHTTPAccessLogLine(record.entry)
	if err != nil {
		t.Fatalf("build log line error: %v", err)
	}

	var entry httpAccessLogEntry
	if err := json.Unmarshal(line, &entry); err != nil {
		t.Fatalf("log line is not json: %v", err)
	}
	if entry.Method != http.MethodConnect {
		t.Fatalf("unexpected method %q", entry.Method)
	}
	if entry.Scheme != "https" || entry.Host != "secure.example.com" {
		t.Fatalf("unexpected scheme/host %q/%q", entry.Scheme, entry.Host)
	}
	if entry.URL != "/" {
		t.Fatalf("unexpected url %q", entry.URL)
	}
	if !entry.TLSPassthrough {
		t.Fatalf("expected tls passthrough flag")
	}
	if entry.DurationMS != 123 {
		t.Fatalf("unexpected duration %d", entry.DurationMS)
	}
}

func TestBuildHTTPAccessLogLineWithStatusCode(t *testing.T) {
	entry := httpAccessLogEntry{
		Timestamp:     "2026-07-13 10:11:12.013",
		Method:        http.MethodPost,
		Scheme:        "http",
		Host:          "example.com",
		URL:           "/api/save?debug=true",
		StatusCode:    http.StatusCreated,
		RequestBytes:  256,
		ResponseBytes: 128,
	}

	line, err := buildHTTPAccessLogLine(entry)
	if err != nil {
		t.Fatalf("build log line error: %v", err)
	}

	var got httpAccessLogEntry
	if err := json.Unmarshal(line, &got); err != nil {
		t.Fatalf("log line is not json: %v", err)
	}
	if got.Method != http.MethodPost {
		t.Fatalf("unexpected method %q", got.Method)
	}
	if got.URL != "/api/save?debug=true" {
		t.Fatalf("unexpected url %q", got.URL)
	}
	if got.StatusCode != http.StatusCreated {
		t.Fatalf("unexpected status code %d", got.StatusCode)
	}
	if got.RequestBytes != 256 {
		t.Fatalf("unexpected request bytes %d", got.RequestBytes)
	}
	if got.ResponseBytes != 128 {
		t.Fatalf("unexpected response bytes %d", got.ResponseBytes)
	}
}

func TestEstimateHTTPAccessLogRequestBytes(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "http://example.com/api/save?id=1", strings.NewReader("hello"))
	request.RequestURI = "/api/save?id=1"
	request.Header = make(http.Header)
	request.Header.Set("Content-Type", "text/plain")
	request.Header.Set("X-Test", "abc")

	got := estimateHTTPAccessLogRequestBytes(request)
	want := int64(len("POST /api/save?id=1 HTTP/1.1\r\n") +
		len("Host: example.com\r\n") +
		len("Content-Length: 5\r\n") +
		len("Content-Type: text/plain\r\n") +
		len("X-Test: abc\r\n") +
		len("\r\n") +
		len("hello"))
	if got != want {
		t.Fatalf("unexpected estimated request bytes %d, want %d", got, want)
	}
}

func TestHTTPAccessLogMaskQueryKeys(t *testing.T) {
	oldMaskKeys := httpAccessLog.maskKeys
	httpAccessLog.maskKeys = map[string]struct{}{
		"token":    {},
		"password": {},
	}
	t.Cleanup(func() {
		httpAccessLog.maskKeys = oldMaskKeys
	})

	u, err := url.Parse("/api/list?id=1&token=abc&password=secret&name=tom")
	if err != nil {
		t.Fatalf("parse url error: %v", err)
	}
	got := requestLogURL(u)
	want := "/api/list?id=1&token=***&password=***&name=tom"
	if got != want {
		t.Fatalf("unexpected masked url %q, want %q", got, want)
	}
}

func TestBuildHTTPAccessLogLineWithFieldWhitelist(t *testing.T) {
	oldFields := httpAccessLog.fields
	httpAccessLog.fields = map[string]struct{}{
		"method":        {},
		"url":           {},
		"request_bytes": {},
		"duration_ms":   {},
	}
	t.Cleanup(func() {
		httpAccessLog.fields = oldFields
	})

	line, err := buildHTTPAccessLogLine(httpAccessLogEntry{
		Timestamp:    "2026-07-13 10:11:12.013",
		Method:       http.MethodGet,
		Host:         "example.com",
		URL:          "/api/list",
		RequestBytes: 64,
		DurationMS:   9,
	})
	if err != nil {
		t.Fatalf("build log line error: %v", err)
	}
	var got map[string]interface{}
	if err := json.Unmarshal(line, &got); err != nil {
		t.Fatalf("log line is not json: %v", err)
	}
	if len(got) != 4 || got["method"] != http.MethodGet || got["url"] != "/api/list" || got["request_bytes"] != float64(64) {
		t.Fatalf("unexpected whitelisted fields: %v", got)
	}
	if _, ok := got["host"]; ok {
		t.Fatalf("host should not be logged when it is not in whitelist: %v", got)
	}
}

func TestHTTPAccessLogSkipAndErrorType(t *testing.T) {
	oldDisabled := httpAccessLog.disabled
	oldMinMs := httpAccessLog.minMs
	oldExcludes := httpAccessLog.excludes
	oldHosts := httpAccessLog.hosts
	httpAccessLog.disabled = false
	httpAccessLog.minMs = 100
	httpAccessLog.excludes = []string{"/health", "/static/*"}
	httpAccessLog.hosts = []string{"localhost", "*.internal.test", "api.example.com:8080"}
	t.Cleanup(func() {
		httpAccessLog.disabled = oldDisabled
		httpAccessLog.minMs = oldMinMs
		httpAccessLog.excludes = oldExcludes
		httpAccessLog.hosts = oldHosts
	})

	record := &httpAccessLogRecord{
		entry: httpAccessLogEntry{DurationMS: 50, URL: "/api"},
		path:  "/api",
	}
	if !shouldSkipHTTPAccessLog(record) {
		t.Fatalf("record below min duration should be skipped")
	}
	record.entry.DurationMS = 101
	record.path = "/static/app.js"
	if !shouldSkipHTTPAccessLog(record) {
		t.Fatalf("excluded static path should be skipped")
	}
	record.path = "/api"
	record.entry.DurationMS = 101
	record.entry.Host = "localhost:1080"
	if !shouldSkipHTTPAccessLog(record) {
		t.Fatalf("excluded host without port should match host with port")
	}
	record.entry.Host = "shop.internal.test"
	if !shouldSkipHTTPAccessLog(record) {
		t.Fatalf("excluded wildcard host should be skipped")
	}
	record.entry.Host = "api.example.com:8080"
	if !shouldSkipHTTPAccessLog(record) {
		t.Fatalf("excluded host with port should be skipped")
	}
	if got := classifyHTTPAccessLogError("write tcp: broken pipe"); got != "client_closed" {
		t.Fatalf("unexpected error type %q", got)
	}
	if got := classifyHTTPAccessLogError("i/o timeout"); got != "timeout" {
		t.Fatalf("unexpected timeout error type %q", got)
	}
}

func TestRotateHTTPAccessLogIfNeededLocked(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "access.log")
	if err := os.WriteFile(logPath, []byte("old line\n"), 0644); err != nil {
		t.Fatalf("write initial log error: %v", err)
	}
	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		t.Fatalf("open log error: %v", err)
	}
	oldState := httpAccessLog
	httpAccessLog.file = f
	httpAccessLog.path = logPath
	httpAccessLog.maxSize = 1
	httpAccessLog.size = int64(len("old line\n"))
	httpAccessLog.backups = 2
	t.Cleanup(func() {
		if httpAccessLog.file != nil {
			_ = httpAccessLog.file.Close()
		}
		httpAccessLog = oldState
	})

	if err := rotateHTTPAccessLogIfNeededLocked(1); err != nil {
		t.Fatalf("rotate log error: %v", err)
	}
	if _, err := httpAccessLog.file.Write([]byte("new line\n")); err != nil {
		t.Fatalf("write new log error: %v", err)
	}
	_ = httpAccessLog.file.Close()
	httpAccessLog.file = nil

	var rotatedFile *os.File
	for i := 0; i < 50; i++ {
		rotatedFile, err = os.Open(logPath + ".1.gz")
		if err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("open rotated gzip log error: %v", err)
	}
	defer rotatedFile.Close()
	gzipReader, err := gzip.NewReader(rotatedFile)
	if err != nil {
		t.Fatalf("create gzip reader error: %v", err)
	}
	rotated, err := io.ReadAll(gzipReader)
	if err != nil {
		t.Fatalf("read rotated gzip log error: %v", err)
	}
	if err := gzipReader.Close(); err != nil {
		t.Fatalf("close gzip reader error: %v", err)
	}
	if string(rotated) != "old line\n" {
		t.Fatalf("unexpected rotated log %q", string(rotated))
	}
	if common.FileExists(logPath + ".1") {
		t.Fatalf("uncompressed rotated log should be removed")
	}
	current, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read current log error: %v", err)
	}
	if string(current) != "new line\n" {
		t.Fatalf("unexpected current log %q", string(current))
	}
}
