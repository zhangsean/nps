package controllers

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ehang.io/nps/lib/file"
)

func TestTunnelFormsPreserveRenderedClientIDOnInitialLoad(t *testing.T) {
	for _, name := range []string{"add.html", "edit.html"} {
		t.Run(name, func(t *testing.T) {
			content, err := os.ReadFile(filepath.Join("..", "views", "index", name))
			if err != nil {
				t.Fatal(err)
			}
			source := string(content)
			if strings.Contains(source, "cid.val(oldId === undefined ? '' : oldId)") {
				t.Fatal("initial form load still clears the rendered client ID")
			}
			if !strings.Contains(source, "if (oldId !== undefined) {\n                cid.val(oldId)\n            }") {
				t.Fatal("client ID should only be restored after local proxy mode saved an old value")
			}
		})
	}

	content, err := os.ReadFile(filepath.Join("..", "views", "index", "edit.html"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(content), `value="{{.t.Client.Id}}" value="{{.client_id}}"`) {
		t.Fatal("edit form must render a single client ID value")
	}
}

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
