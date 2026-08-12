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

func TestNewTunnelFormDefaultsToEditTemplateShape(t *testing.T) {
	tunnel := newTunnelForm(7, "tcp")
	if tunnel.Id != 0 {
		t.Fatalf("new tunnel id = %d, want 0", tunnel.Id)
	}
	if tunnel.Mode != "tcp" {
		t.Fatalf("new tunnel mode = %q, want tcp", tunnel.Mode)
	}
	if tunnel.Client == nil || tunnel.Client.Id != 7 {
		t.Fatalf("new tunnel client = %#v, want id 7", tunnel.Client)
	}
	if tunnel.Target == nil {
		t.Fatal("new tunnel target should be initialized for edit template rendering")
	}
	if tunnel.ServerIp != "0.0.0.0" {
		t.Fatalf("new tunnel server ip = %q, want 0.0.0.0", tunnel.ServerIp)
	}
}

func TestTunnelEditTemplateKeepsAddCloneTitlesSeparate(t *testing.T) {
	content, err := os.ReadFile(filepath.Join("..", "views", "index", "edit.html"))
	if err != nil {
		t.Fatal(err)
	}
	source := string(content)
	if !strings.Contains(source, `{{if .is_add}}add{{else}}{{if eq 0 .t.Id}}clone{{else}}edit{{end}}{{end}}`) {
		t.Fatal("edit template should render add, clone, and edit titles from the shared form")
	}
	if !strings.Contains(source, `submitform('{{if eq 0 .t.Id}}add{{else}}edit{{end}}'`) {
		t.Fatal("shared edit template should still submit new or cloned tunnels to the add endpoint")
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
