package controllers

import (
	"path/filepath"
	"testing"
)

func TestNpsConfigurationPathIsAbsolute(t *testing.T) {
	path := npsConfigurationPath()
	if !filepath.IsAbs(path) {
		t.Fatalf("npsConfigurationPath() = %q, want an absolute path", path)
	}
	if filepath.Base(path) != "nps.conf" {
		t.Fatalf("npsConfigurationPath() = %q, want nps.conf", path)
	}
}
