package fileserver

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestBrowserListsDirectory(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "hello.txt"), []byte("hello"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "sub"), 0755); err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "http://example.com/", nil)
	NewBrowser(root, "").ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "文件浏览") || strings.Contains(body, "<h1>当前路径</h1>") || !strings.Contains(body, "hello.txt") || !strings.Contains(body, "sub/") {
		t.Fatalf("directory listing missing expected entries: %s", body)
	}
}

func TestBrowserRejectsTraversal(t *testing.T) {
	root := t.TempDir()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "http://example.com/..%2fsecret.txt", nil)

	NewBrowser(root, "").ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestBrowserRedirectKeepsStripPrefix(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "sub"), 0755); err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "http://example.com/web/sub?x=1", nil)
	NewBrowser(root, "/web/").ServeHTTP(rec, req)

	if rec.Code != http.StatusMovedPermanently {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusMovedPermanently)
	}
	if location := rec.Header().Get("Location"); location != "http://example.com/web/sub/?x=1" {
		t.Fatalf("Location = %q", location)
	}
}

func TestBrowserRendersClickableBreadcrumbs(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "sub", "nested"), 0755); err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "http://example.com/files/sub/nested/", nil)
	NewBrowser(root, "/files/").ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	body := rec.Body.String()
	for _, want := range []string{
		`<nav class="path" aria-label="当前位置">`,
		`<a class="crumb root" href="../../">/</a>`,
		`<a class="crumb" href="../">sub</a>`,
		`<span class="crumb current">nested</span>`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("breadcrumb missing %q in: %s", want, body)
		}
	}
}

func TestBrowserRendersConfiguredBrowseURLAndCopyAddress(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "报告 2026.txt"), []byte("hello"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "private"), 0755); err != nil {
		t.Fatal(err)
	}
	handler := NewBrowserWithOptions(root, "", BrowserOptions{BrowseURL: "https://files.example.com/download/"})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "http://internal.example/", nil))
	body := rec.Body.String()
	for _, want := range []string{
		`<span class="browse-base">https://files.example.com/download</span>`,
		`data-copy-url="https://files.example.com/download/报告%202026.txt"`,
		`data-copy-toast`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("browse page missing %q in: %s", want, body)
		}
	}
	if strings.Contains(body, `data-copy-url="https://files.example.com/download/private/"`) {
		t.Fatalf("directories should not render copy address buttons: %s", body)
	}
	if !strings.Contains(body, `>链接</button>`) || strings.Contains(body, `>复制地址</button>`) {
		t.Fatalf("copy link button label is incorrect: %s", body)
	}
}

func TestEscapeBrowseSegmentPreservesChineseAndEscapesReservedCharacters(t *testing.T) {
	const input = "报告 100%#?.txt"
	const want = "报告%20100%25%23%3F.txt"
	if got := escapeBrowseSegment(input); got != want {
		t.Fatalf("escapeBrowseSegment(%q) = %q, want %q", input, got, want)
	}
}

func TestNormalizeBrowseURL(t *testing.T) {
	for _, test := range []struct {
		input string
		want  string
		ok    bool
	}{
		{"", "", true},
		{" https://files.example.com/download/ ", "https://files.example.com/download", true},
		{"http://127.0.0.1:8080/", "http://127.0.0.1:8080", true},
		{"/relative", "", false},
		{"javascript:alert(1)", "", false},
		{"https://user:pass@example.com/files", "", false},
		{"https://example.com/files?token=secret", "", false},
	} {
		got, err := NormalizeBrowseURL(test.input)
		if (err == nil) != test.ok || got != test.want {
			t.Fatalf("NormalizeBrowseURL(%q) = %q, %v; want %q, ok=%v", test.input, got, err, test.want, test.ok)
		}
	}
}

func TestNormalizeRootDefault(t *testing.T) {
	if got := NormalizeRoot(""); got != DefaultRoot {
		t.Fatalf("NormalizeRoot empty = %q, want %q", got, DefaultRoot)
	}
}

func TestNormalizeRootPreservesMsysDrivePath(t *testing.T) {
	if got := NormalizeRoot("/d/tmp"); got != "/d/tmp" {
		t.Fatalf("NormalizeRoot /d/tmp = %q, want /d/tmp", got)
	}
	if got := NormalizeRoot(`\d\tmp`); got != "/d/tmp" {
		t.Fatalf("NormalizeRoot \\\\d\\\\tmp = %q, want /d/tmp", got)
	}
	if runtime.GOOS == "windows" {
		if got := filesystemRoot("/d/tmp"); got != `D:\tmp` {
			t.Fatalf("filesystemRoot /d/tmp = %q, want D:\\tmp", got)
		}
	}
}

func TestUploadManagementFlow(t *testing.T) {
	root := t.TempDir()
	handler := NewBrowserWithOptions(root, "", BrowserOptions{
		AllowUpload:    true,
		UploadPassword: "secret",
	})

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "http://example.com/", nil))
	body := rec.Body.String()
	if !strings.Contains(body, "login-trigger") || strings.Contains(body, "<section class=\"panel\">") {
		t.Fatalf("unauthorized upload UI should only show compact login: %s", body)
	}

	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, formRequest("http://example.com/?action=mkdir", url.Values{"name": {"blocked"}}, nil))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("unauthorized mkdir status = %d, want %d", rec.Code, http.StatusForbidden)
	}

	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, formRequest("http://example.com/?action=login", url.Values{"password": {"wrong"}}, nil))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("invalid login status = %d, want %d", rec.Code, http.StatusSeeOther)
	}
	if location := rec.Header().Get("Location"); !strings.Contains(location, "login_error=1") {
		t.Fatalf("invalid login location = %q, want login_error=1", location)
	}

	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "http://example.com/?login_error=1", nil))
	if !strings.Contains(rec.Body.String(), "管理密码不正确") || !strings.Contains(rec.Body.String(), "login-menu\" open") {
		t.Fatalf("login error prompt missing: %s", rec.Body.String())
	}

	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, formRequest("http://example.com/?action=login", url.Values{"password": {"secret"}}, nil))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("login status = %d, want %d", rec.Code, http.StatusSeeOther)
	}
	cookie := rec.Result().Cookies()[0]

	rec = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "http://example.com/", nil)
	req.AddCookie(cookie)
	handler.ServeHTTP(rec, req)
	body = rec.Body.String()
	if !strings.Contains(body, "上传维护") || !strings.Contains(body, "data-open-mkdir") || !strings.Contains(body, "data-upload-overall") || !strings.Contains(body, "upload_chunk") || strings.Contains(body, "confirm(") {
		t.Fatalf("authorized upload panel missing expected controls: %s", body)
	}
	if strings.Index(body, `action="?action=upload"`) > strings.Index(body, `data-open-mkdir`) {
		t.Fatalf("upload action should appear before mkdir button: %s", body)
	}

	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, formRequest("http://example.com/?action=mkdir", url.Values{"name": {"created"}}, cookie))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("mkdir status = %d, want %d", rec.Code, http.StatusSeeOther)
	}
	if location := rec.Header().Get("Location"); !strings.Contains(location, "notice=mkdir") {
		t.Fatalf("mkdir location = %q, want notice=mkdir", location)
	}
	if info, err := os.Stat(filepath.Join(root, "created")); err != nil || !info.IsDir() {
		t.Fatalf("created directory missing, info=%v err=%v", info, err)
	}
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "http://example.com/?notice=mkdir", nil)
	req.AddCookie(cookie)
	handler.ServeHTTP(rec, req)
	if !strings.Contains(rec.Body.String(), "文件夹创建成功") {
		t.Fatalf("mkdir notice missing: %s", rec.Body.String())
	}

	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, uploadRequest("http://example.com/?action=upload", "uploaded.txt", "hello", cookie))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("upload status = %d, want %d", rec.Code, http.StatusSeeOther)
	}
	if location := rec.Header().Get("Location"); !strings.Contains(location, "notice=upload") {
		t.Fatalf("upload location = %q, want notice=upload", location)
	}
	if content, err := os.ReadFile(filepath.Join(root, "uploaded.txt")); err != nil || string(content) != "hello" {
		t.Fatalf("uploaded file content = %q err=%v", string(content), err)
	}
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "http://example.com/?notice=upload", nil)
	req.AddCookie(cookie)
	handler.ServeHTTP(rec, req)
	if !strings.Contains(rec.Body.String(), "文件上传成功") {
		t.Fatalf("upload notice missing: %s", rec.Body.String())
	}

	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, formRequest("http://example.com/?action=delete", url.Values{"name": {"uploaded.txt"}}, cookie))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("delete status = %d, want %d", rec.Code, http.StatusSeeOther)
	}
	if location := rec.Header().Get("Location"); !strings.Contains(location, "notice=delete") {
		t.Fatalf("delete location = %q, want notice=delete", location)
	}
	if _, err := os.Stat(filepath.Join(root, "uploaded.txt")); !os.IsNotExist(err) {
		t.Fatalf("uploaded file still exists, err=%v", err)
	}
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "http://example.com/?notice=delete", nil)
	req.AddCookie(cookie)
	handler.ServeHTTP(rec, req)
	if !strings.Contains(rec.Body.String(), "已删除所选项目") {
		t.Fatalf("delete notice missing: %s", rec.Body.String())
	}
}

func TestChunkUploadResumesAcrossBrowserRestart(t *testing.T) {
	root := t.TempDir()
	options := BrowserOptions{AllowUpload: true, UploadPassword: "secret"}
	handler := NewBrowserWithOptions(root, "", options)
	cookie := managementCookie(t, handler, "secret")
	query := url.Values{
		"upload_id":     {"0123456789abcdef0123456789abcdef"},
		"name":          {"resume.txt"},
		"size":          {"11"},
		"last_modified": {"1784822400000"},
	}

	response := chunkUploadAPIRequest(t, handler, cookie, "upload_status", query, nil)
	if response.Offset != 0 || response.Complete {
		t.Fatalf("initial upload status = %#v", response)
	}
	first := []byte("hello")
	query.Set("offset", "0")
	response = chunkUploadAPIRequest(t, handler, cookie, "upload_chunk", query, first)
	if response.Offset != int64(len(first)) || response.Complete {
		t.Fatalf("first chunk response = %#v", response)
	}
	rec := httptest.NewRecorder()
	conflictRequest := httptest.NewRequest(http.MethodPost, "http://example.com/?action=upload_chunk&"+query.Encode(), strings.NewReader("again"))
	conflictRequest.AddCookie(cookie)
	handler.ServeHTTP(rec, conflictRequest)
	conflict := uploadResponse{}
	if err := json.Unmarshal(rec.Body.Bytes(), &conflict); err != nil || rec.Code != http.StatusConflict || conflict.Offset != int64(len(first)) {
		t.Fatalf("offset conflict status=%d response=%#v decodeErr=%v", rec.Code, conflict, err)
	}

	handler = NewBrowserWithOptions(root, "", options)
	cookie = managementCookie(t, handler, "secret")
	query.Del("offset")
	response = chunkUploadAPIRequest(t, handler, cookie, "upload_status", query, nil)
	if response.Offset != int64(len(first)) || response.Complete {
		t.Fatalf("resumed upload status = %#v", response)
	}
	query.Set("offset", "5")
	response = chunkUploadAPIRequest(t, handler, cookie, "upload_chunk", query, []byte(" world"))
	if response.Offset != 11 || !response.Complete {
		t.Fatalf("final chunk response = %#v", response)
	}
	content, err := os.ReadFile(filepath.Join(root, "resume.txt"))
	if err != nil || string(content) != "hello world" {
		t.Fatalf("completed upload content = %q, err=%v", string(content), err)
	}

	query.Del("offset")
	response = chunkUploadAPIRequest(t, handler, cookie, "upload_status", query, nil)
	if response.Offset != 11 || !response.Complete {
		t.Fatalf("idempotent completed status = %#v", response)
	}
	rec = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "http://example.com/", nil)
	req.AddCookie(cookie)
	handler.ServeHTTP(rec, req)
	if strings.Contains(rec.Body.String(), uploadStateDirectory) {
		t.Fatalf("internal upload state directory leaked into listing: %s", rec.Body.String())
	}
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "http://example.com/"+uploadStateDirectory+"/", nil)
	req.AddCookie(cookie)
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("internal upload state status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestBrowsePasswordProtectsDirectoryAndFiles(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "hello.txt"), []byte("hello"), 0644); err != nil {
		t.Fatal(err)
	}
	handler := NewBrowserWithOptions(root, "", BrowserOptions{
		AllowBrowse:    true,
		BrowsePassword: "view",
	})

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "http://example.com/", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("directory status = %d, want %d", rec.Code, http.StatusOK)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "请输入浏览密码") || strings.Contains(body, "管理密码") || strings.Contains(body, "hello.txt") {
		t.Fatalf("unauthorized directory should show browse login only: %s", body)
	}

	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "http://example.com/hello.txt", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("file status = %d, want %d", rec.Code, http.StatusOK)
	}
	body = rec.Body.String()
	if !strings.Contains(body, "请输入浏览密码") || strings.Contains(body, "hello") {
		t.Fatalf("unauthorized direct file request should not return file content: %s", body)
	}

	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, formRequest("http://example.com/?action=browse_login", url.Values{"password": {"wrong"}}, nil))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("invalid browse login status = %d, want %d", rec.Code, http.StatusSeeOther)
	}
	if location := rec.Header().Get("Location"); !strings.Contains(location, "browse_login_error=1") {
		t.Fatalf("invalid browse login location = %q, want browse_login_error=1", location)
	}

	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "http://example.com/?browse_login_error=1", nil))
	if !strings.Contains(rec.Body.String(), "浏览密码不正确") {
		t.Fatalf("browse login error prompt missing: %s", rec.Body.String())
	}

	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, formRequest("http://example.com/?action=browse_login", url.Values{"password": {"view"}}, nil))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("browse login status = %d, want %d", rec.Code, http.StatusSeeOther)
	}
	cookie := namedCookie(rec.Result().Cookies(), browseAuthCookie)
	if cookie == nil {
		t.Fatalf("browse auth cookie missing: %#v", rec.Result().Cookies())
	}

	rec = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "http://example.com/", nil)
	req.AddCookie(cookie)
	handler.ServeHTTP(rec, req)
	if body := rec.Body.String(); !strings.Contains(body, "hello.txt") || !strings.Contains(body, "退出浏览") {
		t.Fatalf("authorized directory listing missing file: %s", body)
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "http://example.com/hello.txt", nil)
	req.AddCookie(cookie)
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || strings.TrimSpace(rec.Body.String()) != "hello" {
		t.Fatalf("authorized file response status=%d body=%q", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	req = formRequest("http://example.com/?action=browse_logout", nil, cookie)
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("browse logout status = %d, want %d", rec.Code, http.StatusSeeOther)
	}
	cleared := namedCookie(rec.Result().Cookies(), browseAuthCookie)
	if cleared == nil || cleared.MaxAge != -1 {
		t.Fatalf("browse logout should clear browse cookie: %#v", rec.Result().Cookies())
	}
}

func TestManagementPasswordGrantsBrowseAndManage(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "hello.txt"), []byte("hello"), 0644); err != nil {
		t.Fatal(err)
	}
	handler := NewBrowserWithOptions(root, "", BrowserOptions{
		AllowBrowse:    true,
		BrowsePassword: "view",
		AllowUpload:    true,
		UploadPassword: "admin",
	})

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, formRequest("http://example.com/?action=browse_login", url.Values{"password": {"admin"}}, nil))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("management browse login status = %d, want %d", rec.Code, http.StatusSeeOther)
	}
	cookies := rec.Result().Cookies()
	browseCookie := namedCookie(cookies, browseAuthCookie)
	manageCookie := namedCookie(cookies, uploadAuthCookie)
	if browseCookie == nil || manageCookie == nil {
		t.Fatalf("management browse login should set browse and management cookies: %#v", cookies)
	}

	rec = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "http://example.com/", nil)
	req.AddCookie(browseCookie)
	req.AddCookie(manageCookie)
	handler.ServeHTTP(rec, req)
	body := rec.Body.String()
	for _, want := range []string{"hello.txt", "上传维护", "退出浏览"} {
		if !strings.Contains(body, want) {
			t.Fatalf("management login response missing %q: %s", want, body)
		}
	}
}

func formRequest(target string, values url.Values, cookie *http.Cookie) *http.Request {
	req := httptest.NewRequest(http.MethodPost, target, strings.NewReader(values.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if cookie != nil {
		req.AddCookie(cookie)
	}
	return req
}

func namedCookie(cookies []*http.Cookie, name string) *http.Cookie {
	for _, cookie := range cookies {
		if cookie.Name == name {
			return cookie
		}
	}
	return nil
}

func managementCookie(t *testing.T, handler http.Handler, password string) *http.Cookie {
	t.Helper()
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, formRequest("http://example.com/?action=login", url.Values{"password": {password}}, nil))
	cookie := namedCookie(rec.Result().Cookies(), uploadAuthCookie)
	if cookie == nil {
		t.Fatalf("management cookie missing: %#v", rec.Result().Cookies())
	}
	return cookie
}

func chunkUploadAPIRequest(t *testing.T, handler http.Handler, cookie *http.Cookie, action string, values url.Values, body []byte) uploadResponse {
	t.Helper()
	target := "http://example.com/?action=" + url.QueryEscape(action) + "&" + values.Encode()
	req := httptest.NewRequest(http.MethodPost, target, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/octet-stream")
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	response := uploadResponse{}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode %s response status=%d body=%q: %v", action, rec.Code, rec.Body.String(), err)
	}
	if rec.Code < 200 || rec.Code >= 300 {
		t.Fatalf("%s status=%d response=%#v", action, rec.Code, response)
	}
	return response
}

func uploadRequest(target string, name string, content string, cookie *http.Cookie) *http.Request {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, _ := writer.CreateFormFile("files", name)
	_, _ = part.Write([]byte(content))
	_ = writer.Close()
	req := httptest.NewRequest(http.MethodPost, target, &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	if cookie != nil {
		req.AddCookie(cookie)
	}
	return req
}
