package fileserver

import (
	"bytes"
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
	if !strings.Contains(body, "上传维护") || !strings.Contains(body, "data-open-mkdir") || strings.Contains(body, "confirm(") {
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
