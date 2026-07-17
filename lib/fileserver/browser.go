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
  width: 120px;
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
	AllowBrowse    bool
	BrowsePassword string
	AllowUpload    bool
	UploadPassword string
}

type Browser struct {
	root           string
	stripPrefix    string
	allowBrowse    bool
	browsePassword string
	allowUpload    bool
	uploadPassword string
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
	return &Browser{
		root:           filesystemRoot(root),
		stripPrefix:    normalizeStripPrefix(stripPrefix),
		allowBrowse:    options.AllowBrowse,
		browsePassword: strings.TrimSpace(options.BrowsePassword),
		allowUpload:    options.AllowUpload,
		uploadPassword: strings.TrimSpace(options.UploadPassword),
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
	if !ok {
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
		if !ok {
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
	if !ok {
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
	renderBreadcrumbs(w, cleanPath)
	fmt.Fprint(w, "</div>")
	b.renderHeaderActions(w, r, canManage)
	fmt.Fprint(w, "</header>")
	renderNotice(w, r)
	b.renderUploadPanel(w, canManage)
	fmt.Fprint(w, "<section class=\"table-wrap\"><table><thead><tr><th class=\"name\">名称</th><th>大小</th><th>修改时间</th>")
	if canManage {
		fmt.Fprint(w, "<th class=\"ops\">操作</th>")
	}
	fmt.Fprint(w, "</tr></thead><tbody>")

	if cleanPath != "/" {
		fmt.Fprint(w, "<tr><td><a class=\"entry-link\" href=\"../\"><span class=\"kind\">上级</span><span class=\"entry-name\">../</span></a></td><td class=\"muted\">-</td><td class=\"muted\">-</td>")
		if canManage {
			fmt.Fprint(w, "<td class=\"muted\">-</td>")
		}
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
		if canManage {
			fmt.Fprintf(w, "<td><form class=\"delete-form\" method=\"post\" action=\"?action=delete\"><input type=\"hidden\" name=\"name\" value=\"%s\"><button class=\"btn danger\" type=\"button\" data-confirm-delete data-entry-name=\"%s\">删除</button></form></td>",
				html.EscapeString(name),
				html.EscapeString(name),
			)
		}
		fmt.Fprint(w, "</tr>")
	}

	if len(entries) == 0 {
		colspan := 3
		if canManage {
			colspan = 4
		}
		fmt.Fprintf(w, "<tr><td class=\"empty\" colspan=\"%d\">当前目录为空</td></tr>", colspan)
	}

	fmt.Fprint(w, "</tbody></table></section>")
	if canManage {
		renderManagementModals(w)
	}
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

func renderBreadcrumbs(w http.ResponseWriter, cleanPath string) {
	segments := pathSegments(cleanPath)
	fmt.Fprint(w, "<nav class=\"path\" aria-label=\"当前位置\">")
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
	fmt.Fprint(w, "<form class=\"inline-form action-form\" method=\"post\" action=\"?action=upload\" enctype=\"multipart/form-data\"><div class=\"field\"><label>上传文件</label><input type=\"file\" name=\"files\" multiple required></div><button class=\"btn\" type=\"submit\">上传</button></form>")
	fmt.Fprint(w, "<div class=\"inline-form action-form side-action\"><button class=\"btn secondary\" type=\"button\" data-open-mkdir>新建文件夹</button></div>")
	fmt.Fprint(w, "</div></section>")
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
