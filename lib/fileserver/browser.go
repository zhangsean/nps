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

type BrowserOptions struct {
	AllowUpload    bool
	UploadPassword string
}

type Browser struct {
	root           string
	stripPrefix    string
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

	action := r.URL.Query().Get("action")
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
		http.Error(w, "upload password required", http.StatusForbidden)
		return
	}

	switch action {
	case "mkdir":
		if !b.handleMkdir(w, r, dirPath) {
			return
		}
	case "upload":
		if !b.handleUpload(w, r, dirPath) {
			return
		}
	case "delete":
		if !b.handleDelete(w, r, dirPath) {
			return
		}
	default:
		http.Error(w, "unsupported action", http.StatusBadRequest)
		return
	}
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
		http.Error(w, "invalid password", http.StatusForbidden)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     uploadAuthCookie,
		Value:    b.authToken(),
		Path:     b.cookiePath(),
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
	redirectToDirectory(w, r)
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

func (b *Browser) authToken() string {
	sum := sha256.Sum256([]byte(b.uploadPassword + "\x00" + b.root))
	return fmt.Sprintf("%x", sum)
}

func (b *Browser) isAuthorized(r *http.Request) bool {
	cookie, err := r.Cookie(uploadAuthCookie)
	if err != nil {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(cookie.Value), []byte(b.authToken())) == 1
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

	fmt.Fprintf(w, "<!doctype html><html><head><meta charset=\"utf-8\"><title>Index of %s</title>", html.EscapeString(cleanPath))
	fmt.Fprint(w, "<style>body{font-family:Arial,sans-serif;margin:24px;color:#222}h1{font-size:22px}table{border-collapse:collapse;width:100%;max-width:1100px}th,td{border-bottom:1px solid #ddd;padding:8px;text-align:left}th{background:#f6f6f6}.name{width:50%}.muted{color:#777}.panel{margin:16px 0;padding:12px;border:1px solid #ddd;background:#fafafa;max-width:1100px}.panel form{display:inline-flex;gap:8px;margin:4px 12px 4px 0;align-items:center}.danger{color:#c00}.btn{cursor:pointer}</style>")
	fmt.Fprintf(w, "</head><body><h1>Index of %s</h1>", html.EscapeString(cleanPath))
	b.renderUploadPanel(w, canManage)
	fmt.Fprint(w, "<table><thead><tr><th class=\"name\">Name</th><th>Size</th><th>Modified</th>")
	if canManage {
		fmt.Fprint(w, "<th>Operation</th>")
	}
	fmt.Fprint(w, "</tr></thead><tbody>")

	if cleanPath != "/" {
		fmt.Fprint(w, "<tr><td><a href=\"../\">../</a></td><td class=\"muted\">-</td><td class=\"muted\">-</td>")
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
		if entry.IsDir() {
			displayName += "/"
			href += "/"
			size = "-"
		}
		fmt.Fprintf(w, "<tr><td><a href=\"%s\">%s</a></td><td>%s</td><td>%s</td>",
			href,
			html.EscapeString(displayName),
			html.EscapeString(size),
			html.EscapeString(info.ModTime().Format("2006-01-02 15:04:05")),
		)
		if canManage {
			fmt.Fprintf(w, "<td><form method=\"post\" action=\"?action=delete\" onsubmit=\"return confirm('Delete this entry?')\"><input type=\"hidden\" name=\"name\" value=\"%s\"><button class=\"btn danger\" type=\"submit\">Delete</button></form></td>",
				html.EscapeString(name),
			)
		}
		fmt.Fprint(w, "</tr>")
	}

	fmt.Fprint(w, "</tbody></table></body></html>")
}

func (b *Browser) renderUploadPanel(w http.ResponseWriter, canManage bool) {
	if !b.uploadEnabled() {
		return
	}
	fmt.Fprint(w, "<div class=\"panel\">")
	if !canManage {
		fmt.Fprint(w, "<form method=\"post\" action=\"?action=login\"><label>Upload password</label><input type=\"password\" name=\"password\" autocomplete=\"current-password\"><button class=\"btn\" type=\"submit\">Login</button></form>")
		fmt.Fprint(w, "</div>")
		return
	}
	fmt.Fprint(w, "<form method=\"post\" action=\"?action=mkdir\"><label>New folder</label><input type=\"text\" name=\"name\" required><button class=\"btn\" type=\"submit\">Create</button></form>")
	fmt.Fprint(w, "<form method=\"post\" action=\"?action=upload\" enctype=\"multipart/form-data\"><label>Upload</label><input type=\"file\" name=\"files\" multiple required><button class=\"btn\" type=\"submit\">Upload</button></form>")
	fmt.Fprint(w, "<form method=\"post\" action=\"?action=logout\"><button class=\"btn\" type=\"submit\">Logout</button></form>")
	fmt.Fprint(w, "</div>")
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
