package fileserver

import (
	"fmt"
	"html"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

const DefaultRoot = "/files"

type Browser struct {
	root        string
	stripPrefix string
}

func NormalizeRoot(root string) string {
	root = strings.TrimSpace(root)
	if root == "" {
		return DefaultRoot
	}
	if filepath.Clean(root) == filepath.Clean(DefaultRoot) {
		return DefaultRoot
	}
	return filepath.Clean(root)
}

func NewBrowser(root string, stripPrefix string) http.Handler {
	return &Browser{
		root:        NormalizeRoot(root),
		stripPrefix: normalizeStripPrefix(stripPrefix),
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
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
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

	fmt.Fprintf(w, "<!doctype html><html><head><meta charset=\"utf-8\"><title>Index of %s</title>", html.EscapeString(cleanPath))
	fmt.Fprint(w, "<style>body{font-family:Arial,sans-serif;margin:24px;color:#222}h1{font-size:22px}table{border-collapse:collapse;width:100%;max-width:960px}th,td{border-bottom:1px solid #ddd;padding:8px;text-align:left}th{background:#f6f6f6}.name{width:55%}.muted{color:#777}</style>")
	fmt.Fprintf(w, "</head><body><h1>Index of %s</h1><table><thead><tr><th class=\"name\">Name</th><th>Size</th><th>Modified</th></tr></thead><tbody>", html.EscapeString(cleanPath))

	if cleanPath != "/" {
		fmt.Fprint(w, "<tr><td><a href=\"../\">../</a></td><td class=\"muted\">-</td><td class=\"muted\">-</td></tr>")
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
		fmt.Fprintf(w, "<tr><td><a href=\"%s\">%s</a></td><td>%s</td><td>%s</td></tr>",
			href,
			html.EscapeString(displayName),
			html.EscapeString(size),
			html.EscapeString(info.ModTime().Format("2006-01-02 15:04:05")),
		)
	}

	fmt.Fprint(w, "</tbody></table></body></html>")
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
	return os.MkdirAll(NormalizeRoot(root), 0755)
}
