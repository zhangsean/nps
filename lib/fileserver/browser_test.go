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
	if !strings.Contains(body, "Index of /") || !strings.Contains(body, "hello.txt") || !strings.Contains(body, "sub/") {
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
	if !strings.Contains(rec.Body.String(), "Upload password") {
		t.Fatalf("upload login panel missing: %s", rec.Body.String())
	}

	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, formRequest("http://example.com/?action=mkdir", url.Values{"name": {"blocked"}}, nil))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("unauthorized mkdir status = %d, want %d", rec.Code, http.StatusForbidden)
	}

	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, formRequest("http://example.com/?action=login", url.Values{"password": {"secret"}}, nil))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("login status = %d, want %d", rec.Code, http.StatusSeeOther)
	}
	cookie := rec.Result().Cookies()[0]

	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, formRequest("http://example.com/?action=mkdir", url.Values{"name": {"created"}}, cookie))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("mkdir status = %d, want %d", rec.Code, http.StatusSeeOther)
	}
	if info, err := os.Stat(filepath.Join(root, "created")); err != nil || !info.IsDir() {
		t.Fatalf("created directory missing, info=%v err=%v", info, err)
	}

	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, uploadRequest("http://example.com/?action=upload", "uploaded.txt", "hello", cookie))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("upload status = %d, want %d", rec.Code, http.StatusSeeOther)
	}
	if content, err := os.ReadFile(filepath.Join(root, "uploaded.txt")); err != nil || string(content) != "hello" {
		t.Fatalf("uploaded file content = %q err=%v", string(content), err)
	}

	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, formRequest("http://example.com/?action=delete", url.Values{"name": {"uploaded.txt"}}, cookie))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("delete status = %d, want %d", rec.Code, http.StatusSeeOther)
	}
	if _, err := os.Stat(filepath.Join(root, "uploaded.txt")); !os.IsNotExist(err) {
		t.Fatalf("uploaded file still exists, err=%v", err)
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
