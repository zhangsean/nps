package npsconfig

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"
)

func TestParseAndChangedKeys(t *testing.T) {
	before, err := Parse("# keep me\ntarget_connect_timeout_seconds=2\nallow_local_proxy=true\n")
	if err != nil {
		t.Fatal(err)
	}
	after, err := Parse("# keep me\ntarget_connect_timeout_seconds=5\nallow_multi_ip=true\n")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"allow_local_proxy", "allow_multi_ip", "target_connect_timeout_seconds"}
	if got := ChangedKeys(before, after); !reflect.DeepEqual(got, want) {
		t.Fatalf("ChangedKeys() = %#v, want %#v", got, want)
	}
	if after.Values["target_connect_timeout_seconds"] != "5" {
		t.Fatalf("parsed value = %q", after.Values["target_connect_timeout_seconds"])
	}
}

func TestParseNormalizesLineEndingsAndRejectsNUL(t *testing.T) {
	document, err := Parse("log_level=7\r\n")
	if err != nil {
		t.Fatal(err)
	}
	if document.Content != "log_level=7\n" {
		t.Fatalf("normalized content = %q", document.Content)
	}
	if _, err := Parse("web_password=bad\x00value\n"); err == nil {
		t.Fatal("NUL byte should be rejected")
	}
}

func TestSaveCreatesBackupAndPreservesMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nps.conf")
	if err := os.WriteFile(path, []byte("log_level=7\n"), 0640); err != nil {
		t.Fatal(err)
	}
	if err := Save(path, "log_level=5"); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "log_level=5\n" {
		t.Fatalf("saved content = %q", content)
	}
	backup, err := os.ReadFile(path + ".bak")
	if err != nil {
		t.Fatal(err)
	}
	if string(backup) != "log_level=7\n" {
		t.Fatalf("backup content = %q", backup)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0640 && runtime.GOOS != "windows" {
		t.Fatalf("saved mode = %o", info.Mode().Perm())
	}
}
