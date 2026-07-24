package fileserver

import (
	"crypto/sha256"
	"crypto/subtle"
	"fmt"
	"html"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

const DefaultRoot = "/files"
const uploadAuthCookie = "nps_file_upload_auth"
const browseAuthCookie = "nps_file_browse_auth"

const directoryPageStyle = `
:root {
  color-scheme: light;
  --bg: #f4f6f8;
  --surface: #ffffff;
  --surface-soft: #f8fafc;
  --line: #d9e1ea;
  --line-strong: #c4ceda;
  --text: #18212f;
  --muted: #66758a;
  --accent: #0f766e;
  --accent-strong: #115e59;
  --danger: #b42318;
  --danger-bg: #fff1f0;
  --shadow: 0 18px 45px rgba(21, 32, 43, .09);
}
* { box-sizing: border-box; }
[hidden] { display: none !important; }
body {
  margin: 0;
  min-height: 100vh;
  background:
    linear-gradient(135deg, rgba(15, 118, 110, .08), transparent 34rem),
    var(--bg);
  color: var(--text);
  font-family: "Segoe UI", "Microsoft YaHei", "Helvetica Neue", sans-serif;
  font-size: 14px;
}
a {
  color: inherit;
  text-decoration: none;
}
a:hover {
  color: var(--accent-strong);
}
.page {
  width: min(1180px, calc(100% - 32px));
  margin: 0 auto;
  padding: 28px 0 42px;
}
.header {
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto;
  gap: 12px 18px;
  align-items: start;
  margin-bottom: 18px;
}
h1 {
  margin: 0;
  font-size: clamp(24px, 2.8vw, 34px);
  line-height: 1.05;
  font-weight: 780;
  letter-spacing: 0;
}
.path-row {
  grid-column: 1 / -1;
  grid-row: 2;
  display: block;
}
.path {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 6px;
  min-height: 52px;
  max-width: 100%;
  padding: 10px 16px;
  border: 1px solid rgba(15, 118, 110, .35);
  border-radius: 8px;
  background: rgba(255, 255, 255, .9);
  color: var(--accent-strong);
  font-family: Consolas, "Cascadia Mono", monospace;
  font-size: clamp(18px, 2.2vw, 28px);
  font-weight: 800;
  line-height: 1.2;
  box-shadow: 0 12px 28px rgba(15, 118, 110, .12);
  overflow-wrap: anywhere;
}
.crumb {
  display: inline-flex;
  align-items: center;
  min-height: 32px;
  border-radius: 6px;
  padding: 0 7px;
}
a.crumb:hover {
  background: #e4f2f0;
}
.crumb.root {
  min-width: 32px;
  justify-content: center;
}
.crumb.current {
  background: #e4f2f0;
  color: var(--accent-strong);
}
.crumb-sep {
  color: #8aa3a0;
  font-size: .8em;
  font-weight: 700;
}
.browse-base {
  display: inline-flex;
  align-items: center;
  min-height: 32px;
  padding: 0 3px 0 7px;
  color: #334155;
  font-size: .72em;
  font-weight: 750;
  letter-spacing: -.02em;
  overflow-wrap: anywhere;
}
.login-corner {
  grid-column: 2;
  grid-row: 1;
  display: inline-flex;
  gap: 8px;
  align-items: flex-start;
  justify-self: end;
  align-self: start;
  z-index: 5;
}
.login-corner form {
  display: inline-flex;
}
.login-menu {
  position: relative;
}
.login-trigger {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  height: 38px;
  min-width: 72px;
  border: 1px solid var(--line-strong);
  border-radius: 8px;
  background: var(--surface);
  color: var(--text);
  font-weight: 750;
  cursor: pointer;
  box-shadow: 0 10px 24px rgba(21, 32, 43, .06);
}
.login-trigger:hover {
  background: var(--surface-soft);
}
.login-trigger::-webkit-details-marker {
  display: none;
}
.login-menu summary {
  list-style: none;
}
.login-popover {
  position: absolute;
  top: 48px;
  right: 0;
  width: 280px;
  padding: 14px;
  border: 1px solid var(--line);
  border-radius: 8px;
  background: var(--surface);
  box-shadow: var(--shadow);
}
.login-popover .inline-form {
  display: grid;
}
.login-popover input[type="password"] {
  width: 100%;
}
.login-message {
  margin: 0 0 10px;
  padding: 9px 10px;
  border: 1px solid #fed7aa;
  border-radius: 7px;
  background: #fff7ed;
  color: #9a3412;
  font-size: 13px;
  line-height: 1.35;
}
.notice {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  margin: 0 0 14px;
  padding: 11px 14px;
  border: 1px solid rgba(15, 118, 110, .22);
  border-radius: 8px;
  background: #ecfdf5;
  color: #065f46;
  font-weight: 700;
  box-shadow: 0 10px 24px rgba(15, 118, 110, .08);
}
.notice-mark {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 22px;
  height: 22px;
  border-radius: 999px;
  background: #0f766e;
  color: #fff;
  font-size: 13px;
  font-weight: 900;
}
.notice-text {
  flex: 1 1 auto;
}
.panel,
.table-wrap {
  border: 1px solid var(--line);
  border-radius: 8px;
  background: var(--surface);
  box-shadow: var(--shadow);
}
.panel {
  display: grid;
  gap: 16px;
  margin: 16px 0;
  padding: 18px 20px 20px;
}
.panel-head {
  display: flex;
  gap: 14px;
  align-items: center;
  justify-content: space-between;
}
.panel-title {
  color: var(--muted);
  font-size: 12px;
  font-weight: 700;
  letter-spacing: .08em;
  text-transform: uppercase;
}
.panel-subtitle {
  margin-top: 4px;
  color: var(--text);
  font-size: 18px;
  font-weight: 760;
}
form {
  margin: 0;
}
.panel-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 14px;
  align-items: end;
  padding-top: 12px;
  border-top: 1px solid var(--line);
}
.inline-form {
  display: inline-flex;
  gap: 10px;
  align-items: flex-end;
}
.action-form {
  min-width: 0;
}
.panel-actions > form.action-form {
  flex: 1 1 620px;
}
.upload-form {
  display: grid;
  gap: 14px;
  width: 100%;
}
.upload-pick-row {
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto auto;
  gap: 10px;
  align-items: end;
}
.resume-hint {
  margin: 0;
  padding: 10px 12px;
  border-left: 3px solid #d97706;
  border-radius: 0 7px 7px 0;
  background: #fffbeb;
  color: #92400e;
  font-size: 13px;
  line-height: 1.5;
}
.transfer-console {
  display: grid;
  gap: 12px;
  padding: 14px;
  border: 1px solid rgba(15, 118, 110, .22);
  border-radius: 8px;
  background: linear-gradient(135deg, #f1faf8, #f8fafc 68%);
}
.transfer-head,
.transfer-stats,
.transfer-job-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}
.transfer-title {
  font-size: 13px;
  font-weight: 800;
  letter-spacing: .04em;
}
.transfer-percent {
  color: var(--accent-strong);
  font-family: Consolas, "Cascadia Mono", monospace;
  font-size: 18px;
  font-weight: 850;
}
.progress-track {
  height: 10px;
  overflow: hidden;
  border-radius: 999px;
  background: #dce9e7;
  box-shadow: inset 0 1px 2px rgba(15, 23, 42, .1);
}
.progress-value {
  width: 0;
  height: 100%;
  border-radius: inherit;
  background: linear-gradient(90deg, #0f766e, #14b8a6);
  box-shadow: 0 0 12px rgba(20, 184, 166, .34);
  transition: width .18s ease;
}
.transfer-stats {
  justify-content: flex-start;
  flex-wrap: wrap;
  color: var(--muted);
  font-family: Consolas, "Cascadia Mono", monospace;
  font-size: 12px;
}
.transfer-jobs {
  display: grid;
  gap: 8px;
}
.transfer-job {
  display: grid;
  gap: 7px;
  padding-top: 9px;
  border-top: 1px solid rgba(15, 118, 110, .14);
}
.transfer-job-name {
  min-width: 0;
  overflow: hidden;
  font-weight: 700;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.transfer-job-state {
  flex: 0 0 auto;
  color: var(--muted);
  font-size: 12px;
  font-weight: 700;
}
.transfer-job .progress-track {
  height: 5px;
}
.action-form .field {
  flex: 1 1 auto;
  min-width: 0;
}
.side-action {
  flex: 0 0 auto;
  justify-content: flex-end;
}
.logout-form {
  flex: 0 0 auto;
}
.field {
  display: grid;
  gap: 8px;
}
label {
  color: var(--muted);
  font-size: 12px;
  font-weight: 650;
}
input[type="text"],
input[type="password"],
input[type="file"] {
  height: 42px;
  max-width: 100%;
  border: 1px solid var(--line-strong);
  border-radius: 7px;
  background: var(--surface-soft);
  color: var(--text);
  font: inherit;
}
input[type="text"],
input[type="password"] {
  width: 100%;
  padding: 8px 10px;
}
input[type="file"] {
  width: 100%;
  padding: 4px 10px 4px 4px;
  line-height: 32px;
}
input[type="file"]::file-selector-button {
  height: 32px;
  margin-right: 12px;
  border: 1px solid rgba(15, 118, 110, .25);
  border-radius: 6px;
  padding: 0 13px;
  background: #e4f2f0;
  color: var(--accent-strong);
  font: inherit;
  font-weight: 750;
  cursor: pointer;
}
input[type="file"]::file-selector-button:hover {
  background: #d4ebe8;
  border-color: rgba(15, 118, 110, .45);
}
input:focus {
  outline: 2px solid rgba(15, 118, 110, .22);
  border-color: var(--accent);
}
.btn {
  height: 42px;
  border: 1px solid transparent;
  border-radius: 7px;
  padding: 0 14px;
  background: var(--accent);
  color: #fff;
  font: inherit;
  font-weight: 700;
  cursor: pointer;
}
.btn:hover {
  background: var(--accent-strong);
}
.btn.secondary {
  border-color: var(--line-strong);
  background: var(--surface);
  color: var(--text);
}
.btn.secondary:hover {
  background: var(--surface-soft);
}
.panel .btn.secondary {
  border-color: rgba(15, 118, 110, .28);
  background: #e4f2f0;
  color: var(--accent-strong);
}
.panel .btn.secondary:hover {
  border-color: rgba(15, 118, 110, .45);
  background: #d4ebe8;
}
.btn.danger {
  height: 34px;
  padding: 0 10px;
  border-color: #ffd1cc;
  background: var(--danger-bg);
  color: var(--danger);
}
.btn.danger:hover {
  border-color: #ffaaa2;
  background: #ffe5e1;
}
.btn.copy {
  height: 34px;
  padding: 0 10px;
  border-color: rgba(15, 118, 110, .24);
  background: #edf9f7;
  color: var(--accent-strong);
}
.btn.copy:hover {
  border-color: rgba(15, 118, 110, .42);
  background: #dff3ef;
}
.table-wrap {
  overflow: hidden;
}
table {
  width: 100%;
  border-collapse: collapse;
}
th,
td {
  padding: 13px 16px;
  border-bottom: 1px solid var(--line);
  text-align: left;
  vertical-align: middle;
}
th {
  background: #edf3f7;
  color: var(--muted);
  font-size: 12px;
  font-weight: 750;
  letter-spacing: .05em;
  text-transform: uppercase;
}
tbody tr {
  transition: background .15s ease;
}
tbody tr:hover {
  background: #f7fbfb;
}
tbody tr:last-child td {
  border-bottom: 0;
}
.name {
  width: 56%;
}
.entry-link {
  display: inline-flex;
  gap: 10px;
  align-items: center;
  max-width: 100%;
  font-weight: 700;
}
.kind {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 42px;
  height: 24px;
  border-radius: 6px;
  background: #e4f2f0;
  color: var(--accent-strong);
  font-size: 11px;
  font-weight: 800;
  letter-spacing: .04em;
}
.kind.file {
  background: #eef2f7;
  color: #475467;
}
.entry-name {
  overflow-wrap: anywhere;
}
.muted {
  color: var(--muted);
}
.ops {
  width: 190px;
}
.entry-actions {
  display: inline-flex;
  flex-wrap: wrap;
  gap: 7px;
}
.delete-form {
  display: inline-flex;
}
.modal-backdrop[hidden] {
  display: none;
}
.modal-backdrop {
  position: fixed;
  inset: 0;
  z-index: 20;
  display: grid;
  place-items: center;
  padding: 20px;
  background: rgba(15, 23, 42, .28);
}
.modal {
  width: min(360px, 100%);
  border: 1px solid var(--line);
  border-radius: 8px;
  background: var(--surface);
  box-shadow: 0 18px 48px rgba(15, 23, 42, .2);
}
.modal-body {
  display: grid;
  gap: 10px;
  padding: 16px 18px 12px;
}
.modal-title {
  margin: 0;
  color: var(--text);
  font-size: 17px;
  font-weight: 780;
}
.modal-desc {
  margin: 0;
  color: var(--muted);
  line-height: 1.55;
}
.modal-entry {
  color: var(--text);
  font-weight: 800;
  overflow-wrap: anywhere;
}
.modal-actions {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
  padding: 0 18px 16px;
}
.modal-actions .btn {
  width: auto;
  min-width: 72px;
  height: 36px;
  padding: 0 13px;
}
.empty {
  padding: 34px 16px;
  color: var(--muted);
  text-align: center;
}
.copy-toast {
  position: fixed;
  right: 22px;
  bottom: 22px;
  z-index: 40;
  max-width: min(420px, calc(100vw - 32px));
  padding: 11px 14px;
  border: 1px solid rgba(15, 118, 110, .25);
  border-radius: 8px;
  background: #0f3d3a;
  color: #fff;
  font-weight: 700;
  box-shadow: 0 18px 40px rgba(15, 23, 42, .22);
}
@media (max-width: 760px) {
  .page {
    width: min(100% - 20px, 1180px);
    padding-top: 18px;
  }
  .header {
    grid-template-columns: minmax(0, 1fr) auto;
    align-items: start;
  }
  .login-popover {
    width: min(280px, calc(100vw - 20px));
  }
  .panel-actions,
  .inline-form {
    display: grid;
    width: 100%;
  }
  .upload-pick-row {
    grid-template-columns: 1fr;
  }
  .panel-head {
    align-items: stretch;
  }
  .side-action {
    justify-content: stretch;
  }
  .modal-actions {
    display: grid;
  }
  input[type="text"],
  input[type="password"],
  input[type="file"],
  .btn {
    width: 100%;
  }
  .field {
    width: 100%;
  }
  .table-wrap {
    overflow-x: auto;
  }
  table {
    min-width: 680px;
  }
}
`

type BrowserOptions struct {
	BrowseURL      string
	AllowBrowse    bool
	BrowsePassword string
	AllowUpload    bool
	UploadPassword string
}

type Browser struct {
	root           string
	stripPrefix    string
	browseURL      string
	allowBrowse    bool
	browsePassword string
	allowUpload    bool
	uploadPassword string
	uploads        *uploadManager
}

func NormalizeBrowseURL(rawURL string) (string, error) {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return "", nil
	}
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return "", fmt.Errorf("browse address must be an absolute HTTP or HTTPS URL")
	}
	if parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", fmt.Errorf("browse address cannot contain credentials, query parameters, or fragments")
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/")
	return strings.TrimRight(parsed.String(), "/"), nil
}

func NormalizeRoot(root string) string {
	root = strings.TrimSpace(root)
	if root == "" {
		return DefaultRoot
	}
	if isBackslashDrivePath(root) {
		return path.Clean(strings.ReplaceAll(root, `\`, "/"))
	}
	if strings.HasPrefix(root, "/") {
		cleanRoot := path.Clean(root)
		if cleanRoot == DefaultRoot {
			return DefaultRoot
		}
		return cleanRoot
	}
	if filepath.Clean(root) == filepath.Clean(DefaultRoot) {
		return DefaultRoot
	}
	return filepath.Clean(root)
}

func filesystemRoot(root string) string {
	root = NormalizeRoot(root)
	if runtime.GOOS == "windows" && isMsysDrivePath(root) {
		if len(root) == 2 {
			return strings.ToUpper(root[1:2]) + `:\`
		}
		return strings.ToUpper(root[1:2]) + ":" + filepath.FromSlash(root[2:])
	}
	return filepath.Clean(root)
}

func isMsysDrivePath(root string) bool {
	return len(root) >= 2 &&
		root[0] == '/' &&
		((root[1] >= 'a' && root[1] <= 'z') || (root[1] >= 'A' && root[1] <= 'Z')) &&
		(len(root) == 2 || root[2] == '/')
}

func isBackslashDrivePath(root string) bool {
	return len(root) >= 2 &&
		root[0] == '\\' &&
		((root[1] >= 'a' && root[1] <= 'z') || (root[1] >= 'A' && root[1] <= 'Z')) &&
		(len(root) == 2 || root[2] == '\\' || root[2] == '/')
}

func NewBrowser(root string, stripPrefix string) http.Handler {
	return NewBrowserWithOptions(root, stripPrefix, BrowserOptions{})
}

func NewBrowserWithOptions(root string, stripPrefix string, options BrowserOptions) http.Handler {
	filesystemPath := filesystemRoot(root)
	browseURL, _ := NormalizeBrowseURL(options.BrowseURL)
	return &Browser{
		root:           filesystemPath,
		stripPrefix:    normalizeStripPrefix(stripPrefix),
		browseURL:      browseURL,
		allowBrowse:    options.AllowBrowse,
		browsePassword: strings.TrimSpace(options.BrowsePassword),
		allowUpload:    options.AllowUpload,
		uploadPassword: strings.TrimSpace(options.UploadPassword),
		uploads:        newUploadManager(filesystemPath),
	}
}

func normalizeStripPrefix(prefix string) string {
	prefix = strings.TrimSpace(prefix)
	if prefix == "" || prefix == "/" {
		return ""
	}
	if !strings.HasPrefix(prefix, "/") {
		prefix = "/" + prefix
	}
	return strings.TrimRight(prefix, "/") + "/"
}

func (b *Browser) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		b.handlePost(w, r)
		return
	}

	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD, POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	requestPath, ok := b.stripRequestPath(w, r)
	if !ok {
		return
	}

	if b.browseEnabled() && !b.isBrowseAuthorized(r) {
		b.renderBrowseLogin(w, r)
		return
	}

	name, cleanPath, ok := b.resolve(requestPath)
	if !ok {
		http.NotFound(w, r)
		return
	}

	info, err := os.Stat(name)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	if !info.IsDir() {
		http.ServeFile(w, r, name)
		return
	}

	if !strings.HasSuffix(requestPath, "/") {
		redirectURL := *r.URL
		redirectURL.Path = r.URL.Path + "/"
		http.Redirect(w, r, redirectURL.String(), http.StatusMovedPermanently)
		return
	}

	b.renderDirectory(w, r, name, cleanPath)
}

func (b *Browser) handlePost(w http.ResponseWriter, r *http.Request) {
	requestPath, ok := b.stripRequestPath(w, r)
	if !ok {
		return
	}

	action := r.URL.Query().Get("action")
	if action == "browse_login" {
		b.handleBrowseLogin(w, r)
		return
	}
	if action == "browse_logout" {
		b.clearBrowseAuthCookie(w)
		b.clearAuthCookie(w)
		redirectToDirectory(w, r)
		return
	}
	if action == "login" {
		b.handleLogin(w, r)
		return
	}
	if b.browseEnabled() && !b.isBrowseAuthorized(r) {
		http.Error(w, "browse password required", http.StatusForbidden)
		return
	}

	dirPath, _, ok := b.resolve(requestPath)
	if !ok {
		http.NotFound(w, r)
		return
	}
	info, err := os.Stat(dirPath)
	if err != nil || !info.IsDir() {
		http.NotFound(w, r)
		return
	}

	if action == "" {
		if err := r.ParseMultipartForm(64 << 20); err != nil && err != http.ErrNotMultipart {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		action = r.FormValue("action")
	}

	if action == "login" {
		b.handleLogin(w, r)
		return
	}
	if action == "logout" {
		b.clearAuthCookie(w)
		redirectToDirectory(w, r)
		return
	}
	if !b.uploadEnabled() {
		http.Error(w, "upload management is disabled", http.StatusForbidden)
		return
	}
	if !b.isAuthorized(r) {
		http.Error(w, "management password required", http.StatusForbidden)
		return
	}

	switch action {
	case "mkdir":
		if !b.handleMkdir(w, r, dirPath) {
			return
		}
		redirectToDirectoryNotice(w, r, "mkdir")
	case "upload":
		if !b.handleUpload(w, r, dirPath) {
			return
		}
		redirectToDirectoryNotice(w, r, "upload")
	case "upload_status":
		b.handleUploadStatus(w, r, dirPath)
	case "upload_chunk":
		b.handleUploadChunk(w, r, dirPath)
	case "delete":
		if !b.handleDelete(w, r, dirPath) {
			return
		}
		redirectToDirectoryNotice(w, r, "delete")
	default:
		http.Error(w, "unsupported action", http.StatusBadRequest)
		return
	}
}

func (b *Browser) handleBrowseLogin(w http.ResponseWriter, r *http.Request) {
	if !b.browseEnabled() {
		http.Error(w, "browse password is disabled", http.StatusForbidden)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	password := r.FormValue("password")
	if b.uploadEnabled() && subtle.ConstantTimeCompare([]byte(password), []byte(b.uploadPassword)) == 1 {
		b.setBrowseAuthCookie(w)
		b.setAuthCookie(w)
		redirectToDirectory(w, r)
		return
	}
	if subtle.ConstantTimeCompare([]byte(password), []byte(b.browsePassword)) != 1 {
		redirectToBrowseLoginError(w, r)
		return
	}
	b.setBrowseAuthCookie(w)
	redirectToDirectory(w, r)
}

func (b *Browser) handleLogin(w http.ResponseWriter, r *http.Request) {
	if !b.uploadEnabled() {
		http.Error(w, "upload management is disabled", http.StatusForbidden)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if subtle.ConstantTimeCompare([]byte(r.FormValue("password")), []byte(b.uploadPassword)) != 1 {
		redirectToLoginError(w, r)
		return
	}
	if b.browseEnabled() {
		b.setBrowseAuthCookie(w)
	}
	b.setAuthCookie(w)
	redirectToDirectory(w, r)
}

func (b *Browser) setAuthCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     uploadAuthCookie,
		Value:    b.authToken(),
		Path:     b.cookiePath(),
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
}

func (b *Browser) setBrowseAuthCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     browseAuthCookie,
		Value:    b.browseAuthToken(),
		Path:     b.cookiePath(),
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
}

func (b *Browser) handleMkdir(w http.ResponseWriter, r *http.Request, dirPath string) bool {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return false
	}
	name, ok := safeEntryName(r.FormValue("name"))
	if !ok || isUploadStateEntry(name) {
		http.Error(w, "invalid directory name", http.StatusBadRequest)
		return false
	}
	if err := os.Mkdir(filepath.Join(dirPath, name), 0755); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return false
	}
	return true
}

func (b *Browser) handleUpload(w http.ResponseWriter, r *http.Request, dirPath string) bool {
	if err := r.ParseMultipartForm(64 << 20); err != nil {
		http.Error(w, "bad upload request", http.StatusBadRequest)
		return false
	}
	files := r.MultipartForm.File["files"]
	if len(files) == 0 {
		files = r.MultipartForm.File["file"]
	}
	if len(files) == 0 {
		http.Error(w, "no file uploaded", http.StatusBadRequest)
		return false
	}
	for _, header := range files {
		name, ok := safeEntryName(filepath.Base(header.Filename))
		if !ok || isUploadStateEntry(name) {
			http.Error(w, "invalid file name", http.StatusBadRequest)
			return false
		}
		if err := saveUploadedFile(filepath.Join(dirPath, name), header); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return false
		}
	}
	return true
}

func (b *Browser) handleDelete(w http.ResponseWriter, r *http.Request, dirPath string) bool {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return false
	}
	name, ok := safeEntryName(r.FormValue("name"))
	if !ok || isUploadStateEntry(name) {
		http.Error(w, "invalid file name", http.StatusBadRequest)
		return false
	}
	target := filepath.Join(dirPath, name)
	if err := b.ensureWithinRoot(target); err != nil {
		http.Error(w, "invalid file path", http.StatusBadRequest)
		return false
	}
	if err := os.RemoveAll(target); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return false
	}
	return true
}

func saveUploadedFile(dst string, header *multipart.FileHeader) error {
	src, err := header.Open()
	if err != nil {
		return err
	}
	defer src.Close()

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, src)
	return err
}

func (b *Browser) stripRequestPath(w http.ResponseWriter, r *http.Request) (string, bool) {
	if b.stripPrefix == "" {
		return r.URL.Path, true
	}

	prefix := strings.TrimRight(b.stripPrefix, "/")
	if r.URL.Path == prefix {
		redirectURL := *r.URL
		redirectURL.Path = prefix + "/"
		http.Redirect(w, r, redirectURL.String(), http.StatusMovedPermanently)
		return "", false
	}
	if !strings.HasPrefix(r.URL.Path, prefix+"/") {
		http.NotFound(w, r)
		return "", false
	}
	return "/" + strings.TrimPrefix(r.URL.Path, prefix+"/"), true
}

func (b *Browser) resolve(requestPath string) (string, string, bool) {
	cleanPath := path.Clean("/" + requestPath)
	if isUploadStatePath(cleanPath) {
		return "", cleanPath, false
	}
	if cleanPath == "/" {
		return b.root, cleanPath, true
	}

	rel := strings.TrimPrefix(cleanPath, "/")
	name := filepath.Join(b.root, filepath.FromSlash(rel))
	relative, err := filepath.Rel(b.root, name)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(os.PathSeparator)) {
		return "", cleanPath, false
	}
	return name, cleanPath, true
}

func (b *Browser) uploadEnabled() bool {
	return b.allowUpload && b.uploadPassword != ""
}

func (b *Browser) browseEnabled() bool {
	return b.allowBrowse && b.browsePassword != ""
}

func (b *Browser) authToken() string {
	sum := sha256.Sum256([]byte(b.uploadPassword + "\x00" + b.root))
	return fmt.Sprintf("%x", sum)
}

func (b *Browser) browseAuthToken() string {
	sum := sha256.Sum256([]byte("browse\x00" + b.browsePassword + "\x00" + b.root))
	return fmt.Sprintf("%x", sum)
}

func (b *Browser) isAuthorized(r *http.Request) bool {
	cookie, err := r.Cookie(uploadAuthCookie)
	if err != nil {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(cookie.Value), []byte(b.authToken())) == 1
}

func (b *Browser) isBrowseAuthorized(r *http.Request) bool {
	cookie, err := r.Cookie(browseAuthCookie)
	if err != nil {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(cookie.Value), []byte(b.browseAuthToken())) == 1
}

func (b *Browser) cookiePath() string {
	if b.stripPrefix == "" {
		return "/"
	}
	return b.stripPrefix
}

func (b *Browser) clearAuthCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     uploadAuthCookie,
		Value:    "",
		Path:     b.cookiePath(),
		MaxAge:   -1,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
}

func (b *Browser) clearBrowseAuthCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     browseAuthCookie,
		Value:    "",
		Path:     b.cookiePath(),
		MaxAge:   -1,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
}

func (b *Browser) ensureWithinRoot(name string) error {
	relative, err := filepath.Rel(b.root, name)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(os.PathSeparator)) {
		return fmt.Errorf("path is outside root")
	}
	return nil
}

func safeEntryName(name string) (string, bool) {
	name = strings.TrimSpace(name)
	if name == "" || name == "." || name == ".." || strings.ContainsRune(name, 0) {
		return "", false
	}
	if strings.ContainsAny(name, `/\`) {
		return "", false
	}
	for _, character := range name {
		if character < 0x20 || character == 0x7f {
			return "", false
		}
	}
	return name, true
}

func redirectToDirectory(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, r.URL.Path, http.StatusSeeOther)
}

func redirectToDirectoryNotice(w http.ResponseWriter, r *http.Request, notice string) {
	redirectURL := *r.URL
	query := url.Values{}
	query.Set("notice", notice)
	redirectURL.RawQuery = query.Encode()
	http.Redirect(w, r, redirectURL.String(), http.StatusSeeOther)
}

func redirectToLoginError(w http.ResponseWriter, r *http.Request) {
	redirectURL := *r.URL
	query := redirectURL.Query()
	query.Del("action")
	query.Set("login_error", "1")
	redirectURL.RawQuery = query.Encode()
	http.Redirect(w, r, redirectURL.String(), http.StatusSeeOther)
}

func redirectToBrowseLoginError(w http.ResponseWriter, r *http.Request) {
	redirectURL := *r.URL
	query := redirectURL.Query()
	query.Del("action")
	query.Set("browse_login_error", "1")
	redirectURL.RawQuery = query.Encode()
	http.Redirect(w, r, redirectURL.String(), http.StatusSeeOther)
}

func (b *Browser) renderDirectory(w http.ResponseWriter, r *http.Request, dirPath string, cleanPath string) {
	entries, err := os.ReadDir(dirPath)
	if err != nil {
		http.Error(w, "cannot read directory", http.StatusInternalServerError)
		return
	}
	if filepath.Clean(dirPath) == filepath.Clean(b.root) {
		visibleEntries := entries[:0]
		for _, entry := range entries {
			if !isUploadStateEntry(entry.Name()) {
				visibleEntries = append(visibleEntries, entry)
			}
		}
		entries = visibleEntries
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].IsDir() != entries[j].IsDir() {
			return entries[i].IsDir()
		}
		return strings.ToLower(entries[i].Name()) < strings.ToLower(entries[j].Name())
	})

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if r.Method == http.MethodHead {
		return
	}
	canManage := b.uploadEnabled() && b.isAuthorized(r)

	fmt.Fprintf(w, "<!doctype html><html><head><meta charset=\"utf-8\"><meta name=\"viewport\" content=\"width=device-width, initial-scale=1\"><link rel=\"icon\" href=\"data:,\"><title>文件浏览 %s</title>", html.EscapeString(cleanPath))
	fmt.Fprintf(w, "<style>%s</style>", directoryPageStyle)
	fmt.Fprint(w, "</head><body><main class=\"page\"><header class=\"header\"><h1>文件浏览</h1><div class=\"path-row\">")
	renderBreadcrumbs(w, cleanPath, b.browseURL)
	fmt.Fprint(w, "</div>")
	b.renderHeaderActions(w, r, canManage)
	fmt.Fprint(w, "</header>")
	renderNotice(w, r)
	b.renderUploadPanel(w, canManage)
	fmt.Fprint(w, "<section class=\"table-wrap\"><table><thead><tr><th class=\"name\">名称</th><th>大小</th><th>修改时间</th><th class=\"ops\">操作</th></tr></thead><tbody>")

	if cleanPath != "/" {
		fmt.Fprint(w, "<tr><td><a class=\"entry-link\" href=\"../\"><span class=\"kind\">上级</span><span class=\"entry-name\">../</span></a></td><td class=\"muted\">-</td><td class=\"muted\">-</td>")
		fmt.Fprint(w, "<td class=\"muted\">-</td>")
		fmt.Fprint(w, "</tr>")
	}

	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			continue
		}
		name := entry.Name()
		displayName := name
		href := url.PathEscape(name)
		size := humanSize(info.Size())
		kind := "文件"
		kindClass := "kind file"
		if entry.IsDir() {
			displayName += "/"
			href += "/"
			size = "-"
			kind = "目录"
			kindClass = "kind"
		}
		fmt.Fprintf(w, "<tr><td><a class=\"entry-link\" href=\"%s\"><span class=\"%s\">%s</span><span class=\"entry-name\">%s</span></a></td><td>%s</td><td>%s</td>",
			href,
			kindClass,
			kind,
			html.EscapeString(displayName),
			html.EscapeString(size),
			html.EscapeString(info.ModTime().Format("2006-01-02 15:04:05")),
		)
		fmt.Fprint(w, "<td><div class=\"entry-actions\">")
		if !entry.IsDir() {
			copyURL := href
			if b.browseURL != "" {
				copyURL = joinBrowseURL(b.browseURL, cleanPath, name)
			}
			fmt.Fprintf(w, "<button class=\"btn copy\" type=\"button\" data-copy-url=\"%s\">链接</button>", html.EscapeString(copyURL))
		}
		if canManage {
			fmt.Fprintf(w, "<form class=\"delete-form\" method=\"post\" action=\"?action=delete\"><input type=\"hidden\" name=\"name\" value=\"%s\"><button class=\"btn danger\" type=\"button\" data-confirm-delete data-entry-name=\"%s\">删除</button></form>",
				html.EscapeString(name),
				html.EscapeString(name),
			)
		}
		if entry.IsDir() && !canManage {
			fmt.Fprint(w, "<span class=\"muted\">-</span>")
		}
		fmt.Fprint(w, "</div></td>")
		fmt.Fprint(w, "</tr>")
	}

	if len(entries) == 0 {
		fmt.Fprint(w, "<tr><td class=\"empty\" colspan=\"4\">当前目录为空</td></tr>")
	}

	fmt.Fprint(w, "</tbody></table></section>")
	if canManage {
		renderManagementModals(w)
	}
	renderCopyTools(w)
	fmt.Fprint(w, "</main></body></html>")
}

func (b *Browser) renderBrowseLogin(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if r.Method == http.MethodHead {
		return
	}
	message := ""
	if r.URL.Query().Get("browse_login_error") == "1" {
		message = "<p class=\"login-message\">浏览密码不正确，请确认后再试。</p>"
	}
	fmt.Fprint(w, "<!doctype html><html><head><meta charset=\"utf-8\"><meta name=\"viewport\" content=\"width=device-width, initial-scale=1\"><link rel=\"icon\" href=\"data:,\"><title>文件浏览登录</title>")
	fmt.Fprintf(w, "<style>%s</style>", directoryPageStyle)
	fmt.Fprintf(w, "</head><body><main class=\"page\"><section class=\"panel\"><div class=\"panel-head\"><div><div class=\"panel-title\">文件浏览</div><div class=\"panel-subtitle\">请输入浏览密码</div></div></div>%s<form class=\"inline-form\" method=\"post\" action=\"?action=browse_login\"><input type=\"text\" name=\"username\" value=\"file-browser\" autocomplete=\"username\" hidden><div class=\"field\"><label>浏览密码</label><input type=\"password\" name=\"password\" autocomplete=\"current-password\" autofocus required></div><button class=\"btn\" type=\"submit\">登录</button></form></section></main></body></html>", message)
}

func renderNotice(w http.ResponseWriter, r *http.Request) {
	message := noticeMessage(r.URL.Query().Get("notice"))
	if message == "" {
		return
	}
	fmt.Fprintf(w, "<div class=\"notice\" role=\"status\"><span class=\"notice-mark\">✓</span><span class=\"notice-text\">%s</span></div>", html.EscapeString(message))
	fmt.Fprint(w, `<script>
(function () {
  if (!window.history || !window.URL) return;
  var url = new URL(window.location.href);
  if (!url.searchParams.has('notice')) return;
  url.searchParams.delete('notice');
  window.history.replaceState(null, '', url.pathname + url.search + url.hash);
})();
</script>`)
}

func noticeMessage(notice string) string {
	switch notice {
	case "upload":
		return "文件上传成功，列表已刷新。"
	case "mkdir":
		return "文件夹创建成功。"
	case "delete":
		return "已删除所选项目。"
	default:
		return ""
	}
}

func renderBreadcrumbs(w http.ResponseWriter, cleanPath string, browseURL string) {
	segments := pathSegments(cleanPath)
	fmt.Fprint(w, "<nav class=\"path\" aria-label=\"当前位置\">")
	if browseURL != "" {
		fmt.Fprintf(w, "<span class=\"browse-base\">%s</span>", html.EscapeString(browseURL))
	}
	if len(segments) == 0 {
		fmt.Fprint(w, "<span class=\"crumb root current\">/</span>")
		fmt.Fprint(w, "</nav>")
		return
	}

	rootHref := strings.Repeat("../", len(segments))
	fmt.Fprintf(w, "<a class=\"crumb root\" href=\"%s\">/</a>", rootHref)
	for i, segment := range segments {
		if i > 0 {
			fmt.Fprint(w, "<span class=\"crumb-sep\">/</span>")
		}
		if i == len(segments)-1 {
			fmt.Fprintf(w, "<span class=\"crumb current\">%s</span>", html.EscapeString(segment))
			continue
		}
		href := strings.Repeat("../", len(segments)-i-1)
		fmt.Fprintf(w, "<a class=\"crumb\" href=\"%s\">%s</a>", href, html.EscapeString(segment))
	}
	fmt.Fprint(w, "</nav>")
}

func pathSegments(cleanPath string) []string {
	cleanPath = path.Clean("/" + cleanPath)
	if cleanPath == "/" {
		return nil
	}
	return strings.Split(strings.Trim(cleanPath, "/"), "/")
}

func joinBrowseURL(baseURL string, cleanPath string, entryName string) string {
	segments := pathSegments(cleanPath)
	if entryName != "" {
		segments = append(segments, entryName)
	}
	encoded := make([]string, 0, len(segments))
	for _, segment := range segments {
		encoded = append(encoded, escapeBrowseSegment(segment))
	}
	return strings.TrimRight(baseURL, "/") + "/" + strings.Join(encoded, "/")
}

func escapeBrowseSegment(segment string) string {
	var escaped strings.Builder
	for _, character := range segment {
		if character > 127 {
			escaped.WriteRune(character)
			continue
		}
		escaped.WriteString(url.PathEscape(string(character)))
	}
	return escaped.String()
}

func (b *Browser) renderHeaderActions(w http.ResponseWriter, r *http.Request, canManage bool) {
	showManageLogin := b.uploadEnabled() && !canManage
	showBrowseLogout := b.browseEnabled() && b.isBrowseAuthorized(r)
	if !showManageLogin && !showBrowseLogout {
		return
	}
	fmt.Fprint(w, "<div class=\"login-corner\">")
	if showManageLogin {
		b.renderLoginButton(w, r)
	}
	if showBrowseLogout {
		fmt.Fprint(w, "<form method=\"post\" action=\"?action=browse_logout\"><button class=\"login-trigger\" type=\"submit\">退出浏览</button></form>")
	}
	fmt.Fprint(w, "</div>")
}

func (b *Browser) renderLoginButton(w http.ResponseWriter, r *http.Request) {
	openAttr := ""
	message := ""
	if r.URL.Query().Get("login_error") == "1" {
		openAttr = " open"
		message = "<p class=\"login-message\">管理密码不正确，请确认后再试。</p>"
	}
	fmt.Fprintf(w, "<details class=\"login-menu\"%s><summary class=\"login-trigger\">管理登录</summary><div class=\"login-popover\">%s<form class=\"inline-form\" method=\"post\" action=\"?action=login\"><input type=\"text\" name=\"username\" value=\"file-upload\" autocomplete=\"username\" hidden><div class=\"field\"><label>管理密码</label><input type=\"password\" name=\"password\" autocomplete=\"current-password\"></div><button class=\"btn\" type=\"submit\">登录</button></form></div></details>", openAttr, message)
}

func (b *Browser) renderUploadPanel(w http.ResponseWriter, canManage bool) {
	if !b.uploadEnabled() || !canManage {
		return
	}
	fmt.Fprint(w, "<section class=\"panel\"><div class=\"panel-head\"><div><div class=\"panel-title\">上传维护</div><div class=\"panel-subtitle\">管理当前目录</div></div>")
	fmt.Fprint(w, "<form class=\"logout-form\" method=\"post\" action=\"?action=logout\"><button class=\"btn secondary\" type=\"submit\">退出登录</button></form></div>")
	fmt.Fprint(w, "<div class=\"panel-actions\">")
	fmt.Fprint(w, `<form class="action-form upload-form" data-upload-form method="post" action="?action=upload" enctype="multipart/form-data">
<p class="resume-hint" data-resume-hint hidden></p>
<div class="upload-pick-row">
<div class="field"><label>选择文件 · 支持分片上传与断点续传</label><input type="file" name="files" multiple required data-upload-input></div>
<button class="btn" type="submit" data-upload-start>开始上传</button>
<button class="btn secondary" type="button" data-upload-pause hidden>暂停</button>
</div>
<div class="transfer-console" data-upload-console hidden>
<div class="transfer-head"><span class="transfer-title" data-upload-title>准备传输</span><span class="transfer-percent" data-upload-percent>0%</span></div>
<div class="progress-track" role="progressbar" aria-label="总上传进度" aria-valuemin="0" aria-valuemax="100" data-upload-overall><div class="progress-value" data-upload-overall-value></div></div>
<div class="transfer-stats"><span data-upload-bytes>0 B / 0 B</span><span data-upload-speed>0 B/s</span><span data-upload-eta>剩余时间 --</span></div>
<div class="transfer-jobs" data-upload-jobs></div>
</div>
</form>`)
	fmt.Fprint(w, "<div class=\"inline-form action-form side-action\"><button class=\"btn secondary\" type=\"button\" data-open-mkdir>新建文件夹</button></div>")
	fmt.Fprint(w, "</div></section>")
	renderUploadScript(w)
}

func renderUploadScript(w http.ResponseWriter) {
	fmt.Fprint(w, `<script>
(function () {
  var form = document.querySelector('[data-upload-form]');
  if (!form || !window.XMLHttpRequest || !window.Promise || !window.URL) return;
  var input = form.querySelector('[data-upload-input]');
  var startButton = form.querySelector('[data-upload-start]');
  var pauseButton = form.querySelector('[data-upload-pause]');
  var consoleBox = form.querySelector('[data-upload-console]');
  var resumeHint = form.querySelector('[data-resume-hint]');
  var title = form.querySelector('[data-upload-title]');
  var percent = form.querySelector('[data-upload-percent]');
  var overall = form.querySelector('[data-upload-overall]');
  var overallValue = form.querySelector('[data-upload-overall-value]');
  var bytesText = form.querySelector('[data-upload-bytes]');
  var speedText = form.querySelector('[data-upload-speed]');
  var etaText = form.querySelector('[data-upload-eta]');
  var jobsBox = form.querySelector('[data-upload-jobs]');
  var chunkSize = 8 * 1024 * 1024;
  var maxRetries = 5;
  var jobs = [];
  var running = false;
  var paused = false;
  var activeRequest = null;
  var activeLoaded = 0;
  var runStartedAt = 0;
  var storageKey = 'nps-file-upload:' + window.location.origin + window.location.pathname;

  function formatBytes(value) {
    if (!isFinite(value) || value <= 0) return '0 B';
    var units = ['B', 'KB', 'MB', 'GB', 'TB'];
    var index = Math.min(Math.floor(Math.log(value) / Math.log(1024)), units.length - 1);
    var number = value / Math.pow(1024, index);
    return (index === 0 ? Math.round(number) : number.toFixed(number >= 100 ? 0 : number >= 10 ? 1 : 2)) + ' ' + units[index];
  }

  function formatDuration(seconds) {
    if (!isFinite(seconds) || seconds < 0) return '--';
    if (seconds < 60) return Math.max(1, Math.ceil(seconds)) + ' 秒';
    if (seconds < 3600) return Math.ceil(seconds / 60) + ' 分钟';
    return (seconds / 3600).toFixed(1) + ' 小时';
  }

  function uploadID(file) {
    var value = file.name + '\u0000' + file.size + '\u0000' + file.lastModified;
    var seeds = [2166136261, 2246822507, 3266489909, 668265263];
    return seeds.map(function (seed) {
      var hash = seed >>> 0;
      for (var index = 0; index < value.length; index++) {
        hash ^= value.charCodeAt(index);
        hash = Math.imul(hash, 16777619) >>> 0;
      }
      return ('00000000' + hash.toString(16)).slice(-8);
    }).join('');
  }

  function endpoint(action, job, offset) {
    var target = new URL(window.location.href);
    target.search = '';
    target.hash = '';
    target.searchParams.set('action', action);
    target.searchParams.set('upload_id', job.id);
    target.searchParams.set('name', job.file.name);
    target.searchParams.set('size', String(job.file.size));
    target.searchParams.set('last_modified', String(job.file.lastModified || 0));
    if (typeof offset === 'number') target.searchParams.set('offset', String(offset));
    return target.toString();
  }

  function request(action, job, offset, body, onProgress) {
    return new Promise(function (resolve, reject) {
      var xhr = new XMLHttpRequest();
      activeRequest = xhr;
      xhr.open('POST', endpoint(action, job, offset), true);
      xhr.setRequestHeader('Accept', 'application/json');
      if (body) xhr.setRequestHeader('Content-Type', 'application/octet-stream');
      if (xhr.upload && onProgress) {
        xhr.upload.onprogress = function (event) {
          if (event.lengthComputable) onProgress(event.loaded);
        };
      }
      xhr.onload = function () {
        activeRequest = null;
        var payload = {};
        try { payload = JSON.parse(xhr.responseText || '{}'); } catch (error) { payload = {}; }
        if (xhr.status >= 200 && xhr.status < 300) {
          resolve(payload);
          return;
        }
        var failure = new Error(payload.error || ('上传请求失败，HTTP ' + xhr.status));
        failure.status = xhr.status;
        if (typeof payload.offset === 'number') failure.offset = payload.offset;
        reject(failure);
      };
      xhr.onerror = function () {
        activeRequest = null;
        reject(new Error('网络连接中断'));
      };
      xhr.onabort = function () {
        activeRequest = null;
        var error = new Error('上传已暂停');
        error.paused = true;
        reject(error);
      };
      xhr.send(body || null);
    });
  }

  function sleep(milliseconds) {
    return new Promise(function (resolve) { window.setTimeout(resolve, milliseconds); });
  }

  function createJob(file) {
    var row = document.createElement('div');
    row.className = 'transfer-job';
    var head = document.createElement('div');
    head.className = 'transfer-job-head';
    var name = document.createElement('span');
    name.className = 'transfer-job-name';
    name.textContent = file.name;
    var state = document.createElement('span');
    state.className = 'transfer-job-state';
    state.textContent = '等待上传';
    var track = document.createElement('div');
    track.className = 'progress-track';
    var value = document.createElement('div');
    value.className = 'progress-value';
    track.appendChild(value);
    head.appendChild(name);
    head.appendChild(state);
    row.appendChild(head);
    row.appendChild(track);
    jobsBox.appendChild(row);
    return { file: file, id: uploadID(file), offset: 0, initialOffset: 0, statusLoaded: false, complete: false, row: row, state: state, value: value };
  }

  function rebuildJobs() {
    jobs = [];
    jobsBox.innerHTML = '';
    Array.prototype.forEach.call(input.files || [], function (file) { jobs.push(createJob(file)); });
    consoleBox.hidden = jobs.length === 0;
    activeLoaded = 0;
    updateOverall();
  }

  function updateJob(job, inFlight, label) {
    var current = Math.min(job.file.size, job.offset + (inFlight || 0));
    var jobPercent = job.file.size === 0 ? (job.complete ? 100 : 0) : current / job.file.size * 100;
    job.value.style.width = Math.max(0, Math.min(100, jobPercent)).toFixed(2) + '%';
    if (label) job.state.textContent = label;
  }

  function updateOverall(activeJob) {
    var total = jobs.reduce(function (sum, job) { return sum + job.file.size; }, 0);
    var uploaded = jobs.reduce(function (sum, job) { return sum + Math.min(job.offset, job.file.size); }, 0);
    if (activeJob) uploaded += Math.min(activeLoaded, Math.max(0, activeJob.file.size - activeJob.offset));
    var ratio = total === 0 ? (jobs.length && jobs.every(function (job) { return job.complete; }) ? 1 : 0) : uploaded / total;
    var value = Math.max(0, Math.min(100, ratio * 100));
    overallValue.style.width = value.toFixed(2) + '%';
    overall.setAttribute('aria-valuenow', String(Math.round(value)));
    percent.textContent = Math.round(value) + '%';
    bytesText.textContent = formatBytes(uploaded) + ' / ' + formatBytes(total);
    var effective = jobs.reduce(function (sum, job) {
      return sum + Math.max(0, job.offset - job.initialOffset);
    }, 0) + (activeJob ? activeLoaded : 0);
    var elapsed = runStartedAt ? Math.max(0.25, (Date.now() - runStartedAt) / 1000) : 0;
    var speed = elapsed ? effective / elapsed : 0;
    speedText.textContent = formatBytes(speed) + '/s';
    etaText.textContent = '剩余时间 ' + (speed > 0 ? formatDuration((total - uploaded) / speed) : '--');
  }

  function loadPending() {
    if (!window.localStorage) return [];
    try {
      var pending = JSON.parse(window.localStorage.getItem(storageKey) || '[]');
      var cutoff = Date.now() - 7 * 24 * 60 * 60 * 1000;
      pending = pending.filter(function (item) { return item.updatedAt >= cutoff; });
      window.localStorage.setItem(storageKey, JSON.stringify(pending));
      return pending;
    } catch (error) {
      return [];
    }
  }

  function savePending() {
    if (!window.localStorage) return;
    if (!jobs.length) return;
    var pending = jobs.filter(function (job) { return !job.complete; }).map(function (job) {
      return { name: job.file.name, size: job.file.size, lastModified: job.file.lastModified || 0, offset: job.offset, updatedAt: Date.now() };
    });
    try {
      if (pending.length) window.localStorage.setItem(storageKey, JSON.stringify(pending));
      else window.localStorage.removeItem(storageKey);
    } catch (error) {}
  }

  function showPendingHint() {
    var pending = loadPending();
    if (!pending.length) return;
    resumeHint.textContent = '检测到 ' + pending.length + ' 个未完成上传；重新选择同一文件后，将从服务端已保存的分片继续。';
    resumeHint.hidden = false;
  }

  async function sendChunk(job, blob) {
    for (var attempt = 0; attempt < maxRetries; attempt++) {
      if (paused) {
        var pausedError = new Error('上传已暂停');
        pausedError.paused = true;
        throw pausedError;
      }
      try {
        return await request('upload_chunk', job, job.offset, blob, function (loaded) {
          activeLoaded = loaded;
          updateJob(job, loaded, '上传中 ' + formatBytes(job.offset + loaded) + ' / ' + formatBytes(job.file.size));
          updateOverall(job);
        });
      } catch (error) {
        activeLoaded = 0;
        updateOverall();
        if (error.paused) throw error;
        if (error.status === 409 && typeof error.offset === 'number') {
          return { offset: error.offset, complete: error.offset === job.file.size };
        }
        if (attempt === maxRetries - 1) throw error;
        var wait = Math.min(10000, Math.pow(2, attempt) * 1000);
        updateJob(job, 0, '网络异常，' + Math.ceil(wait / 1000) + ' 秒后重试 ' + (attempt + 2) + '/' + maxRetries);
        await sleep(wait);
      }
    }
  }

  async function fetchStatus(job) {
    for (var attempt = 0; attempt < maxRetries; attempt++) {
      try {
        return await request('upload_status', job, null, null, null);
      } catch (error) {
        if (error.paused || (error.status >= 400 && error.status < 500 && error.status !== 408 && error.status !== 429)) throw error;
        if (attempt === maxRetries - 1) throw error;
        var wait = Math.min(10000, Math.pow(2, attempt) * 1000);
        updateJob(job, 0, '状态查询失败，' + Math.ceil(wait / 1000) + ' 秒后重试');
        await sleep(wait);
      }
    }
  }

  async function uploadJob(job) {
    updateJob(job, 0, '检查断点');
    var status = await fetchStatus(job);
    job.offset = Math.max(0, Math.min(job.file.size, Number(status.offset) || 0));
    job.initialOffset = job.offset;
    job.statusLoaded = true;
    job.complete = Boolean(status.complete);
    activeLoaded = 0;
    if (job.complete) {
      updateJob(job, 0, '已存在 · 完成');
      updateOverall();
      savePending();
      return;
    }
    if (job.offset > 0) updateJob(job, 0, '从 ' + formatBytes(job.offset) + ' 继续');
    while (job.offset < job.file.size) {
      var end = Math.min(job.file.size, job.offset + chunkSize);
      var response = await sendChunk(job, job.file.slice(job.offset, end));
      job.offset = Math.max(0, Math.min(job.file.size, Number(response.offset) || 0));
      job.complete = Boolean(response.complete) || job.offset === job.file.size;
      activeLoaded = 0;
      updateJob(job, 0, job.complete ? '上传完成' : '已保存 ' + formatBytes(job.offset));
      updateOverall();
      savePending();
    }
    if (job.file.size === 0) {
      job.complete = true;
      updateJob(job, 0, '上传完成');
    }
  }

  async function runUploads() {
    if (running || !jobs.length) return;
    running = true;
    paused = false;
    runStartedAt = Date.now();
    input.disabled = true;
    startButton.disabled = true;
    startButton.textContent = '上传中';
    pauseButton.hidden = false;
    title.textContent = '分片传输中 · 每片 8 MB';
    try {
      for (var index = 0; index < jobs.length; index++) {
        if (!jobs[index].complete) await uploadJob(jobs[index]);
      }
      title.textContent = '全部上传完成';
      percent.textContent = '100%';
      overallValue.style.width = '100%';
      savePending();
      window.setTimeout(function () {
        var target = new URL(window.location.href);
        target.search = '';
        target.searchParams.set('notice', 'upload');
        window.location.href = target.toString();
      }, 650);
    } catch (error) {
      if (error.paused || paused) {
        title.textContent = '上传已暂停 · 可继续';
      } else {
        title.textContent = '上传中断 · ' + error.message;
      }
      savePending();
    } finally {
      running = false;
      activeLoaded = 0;
      input.disabled = false;
      startButton.disabled = false;
      startButton.textContent = jobs.some(function (job) { return !job.complete; }) ? '继续上传' : '上传完成';
      pauseButton.hidden = true;
      updateOverall();
    }
  }

  input.addEventListener('change', function () {
    if (running) return;
    rebuildJobs();
    resumeHint.hidden = true;
    startButton.textContent = '开始上传';
  });
  form.addEventListener('submit', function (event) {
    event.preventDefault();
    if (!jobs.length) rebuildJobs();
    runUploads();
  });
  pauseButton.addEventListener('click', function () {
    paused = true;
    if (activeRequest) activeRequest.abort();
  });
  window.addEventListener('beforeunload', function () { savePending(); });
  showPendingHint();
})();
</script>`)
}

func renderManagementModals(w http.ResponseWriter) {
	fmt.Fprint(w, `<div class="modal-backdrop" data-modal="mkdir" hidden>
<div class="modal" role="dialog" aria-modal="true" aria-labelledby="mkdir-title">
<form method="post" action="?action=mkdir">
<div class="modal-body">
<h2 class="modal-title" id="mkdir-title">新建文件夹</h2>
<p class="modal-desc">在当前目录下创建一个新的文件夹。</p>
<div class="field"><label>文件夹名称</label><input type="text" name="name" autocomplete="off" required></div>
</div>
<div class="modal-actions"><button class="btn secondary" type="button" data-close-modal>取消</button><button class="btn" type="submit">确定</button></div>
</form>
</div>
</div>`)
	fmt.Fprint(w, `<div class="modal-backdrop" data-modal="delete" hidden>
<div class="modal" role="dialog" aria-modal="true" aria-labelledby="delete-title">
<div class="modal-body">
<h2 class="modal-title" id="delete-title">删除确认</h2>
<p class="modal-desc">确认删除 <span class="modal-entry" data-delete-entry></span> 吗？删除后不可恢复。</p>
</div>
<div class="modal-actions"><button class="btn secondary" type="button" data-close-modal>取消</button><button class="btn danger" type="button" data-delete-submit>删除</button></div>
</div>
</div>`)
	fmt.Fprint(w, `<script>
(function () {
  var activeDeleteForm = null;
  function openModal(modal) {
    if (!modal) return;
    modal.hidden = false;
    var input = modal.querySelector('input');
    if (input) window.setTimeout(function () { input.focus(); input.select(); }, 0);
  }
  function closeModal(modal) {
    if (!modal) return;
    modal.hidden = true;
    if (modal.getAttribute('data-modal') === 'delete') activeDeleteForm = null;
  }
  var mkdirModal = document.querySelector('[data-modal="mkdir"]');
  var deleteModal = document.querySelector('[data-modal="delete"]');
  var deleteEntry = document.querySelector('[data-delete-entry]');
  document.querySelectorAll('[data-open-mkdir]').forEach(function (button) {
    button.addEventListener('click', function () { openModal(mkdirModal); });
  });
  document.querySelectorAll('[data-confirm-delete]').forEach(function (button) {
    button.addEventListener('click', function () {
      activeDeleteForm = button.closest('form');
      if (deleteEntry) deleteEntry.textContent = button.getAttribute('data-entry-name') || '';
      openModal(deleteModal);
    });
  });
  document.querySelectorAll('[data-close-modal]').forEach(function (button) {
    button.addEventListener('click', function () { closeModal(button.closest('.modal-backdrop')); });
  });
  document.querySelectorAll('.modal-backdrop').forEach(function (backdrop) {
    backdrop.addEventListener('click', function (event) {
      if (event.target === backdrop) closeModal(backdrop);
    });
  });
  var deleteSubmit = document.querySelector('[data-delete-submit]');
  if (deleteSubmit) {
    deleteSubmit.addEventListener('click', function () {
      if (activeDeleteForm) activeDeleteForm.submit();
    });
  }
  document.addEventListener('keydown', function (event) {
    if (event.key !== 'Escape') return;
    document.querySelectorAll('.modal-backdrop:not([hidden])').forEach(closeModal);
  });
})();
</script>`)
}

func renderCopyTools(w http.ResponseWriter) {
	fmt.Fprint(w, `<div class="copy-toast" role="status" aria-live="polite" data-copy-toast hidden></div>
<script>
(function () {
  var toast = document.querySelector('[data-copy-toast]');
  var toastTimer = null;
  function showToast(message) {
    if (!toast) return;
    toast.textContent = message;
    toast.hidden = false;
    window.clearTimeout(toastTimer);
    toastTimer = window.setTimeout(function () { toast.hidden = true; }, 1800);
  }
  function fallbackCopy(value) {
    var input = document.createElement('textarea');
    input.value = value;
    input.setAttribute('readonly', 'readonly');
    input.style.position = 'fixed';
    input.style.opacity = '0';
    document.body.appendChild(input);
    input.select();
    var copied = false;
    try { copied = document.execCommand('copy'); } catch (error) { copied = false; }
    document.body.removeChild(input);
    if (!copied) window.prompt('请复制文件地址', value);
    return copied;
  }
  document.querySelectorAll('[data-copy-url]').forEach(function (button) {
    button.addEventListener('click', function () {
      var configured = button.getAttribute('data-copy-url') || '';
      var value = /^https?:\/\//i.test(configured) ? configured : new URL(configured, window.location.href).href;
      var original = button.textContent;
      function done() {
        button.textContent = '已复制';
        showToast('文件地址已复制');
        window.setTimeout(function () { button.textContent = original; }, 1500);
      }
      if (navigator.clipboard && window.isSecureContext) {
        navigator.clipboard.writeText(value).then(done, function () {
          fallbackCopy(value);
          done();
        });
      } else {
        fallbackCopy(value);
        done();
      }
    });
  });
})();
</script>`)
}

func humanSize(size int64) string {
	const unit = 1024
	if size < unit {
		return fmt.Sprintf("%d B", size)
	}
	div, exp := int64(unit), 0
	for n := size / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(size)/float64(div), "KMGTPE"[exp])
}

func EnsureRoot(root string) error {
	return os.MkdirAll(filesystemRoot(root), 0755)
}
