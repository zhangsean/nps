package controllers

import (
	"testing"

	"ehang.io/nps/lib/file"
)

func TestValidateTunnelUploadNormalizesBrowseURL(t *testing.T) {
	tunnel := &file.Tunnel{
		Mode:      "file",
		BrowseURL: " https://files.example.com/download/ ",
	}
	if message := validateTunnelUpload(tunnel); message != "" {
		t.Fatalf("valid browse URL rejected: %s", message)
	}
	if tunnel.BrowseURL != "https://files.example.com/download" {
		t.Fatalf("browse URL = %q", tunnel.BrowseURL)
	}
}

func TestValidateTunnelUploadRejectsInvalidBrowseURL(t *testing.T) {
	tunnel := &file.Tunnel{Mode: "file", BrowseURL: "/internal/files"}
	if message := validateTunnelUpload(tunnel); message == "" {
		t.Fatal("relative browse URL should be rejected")
	}
}
