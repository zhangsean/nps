package fileserver

import (
	"net/http"
	"net/http/httptest"
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
