package server

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"ehang.io/nps/lib/file"
	"github.com/astaxie/beego"
	"github.com/astaxie/beego/logs"
	"golang.org/x/net/html"
)

const defaultIpLocationApi = "https://ip.cn/ip/%s.html"

type ipLocationCacheEntry struct {
	Region  string
	Expires time.Time
}

type ipLocationResponse struct {
	Status     string `json:"status"`
	Message    string `json:"message"`
	RegionName string `json:"regionName"`
	City       string `json:"city"`
	Query      string `json:"query"`
}

var (
	ipLocationCache        sync.Map
	ipLocationHTTPClient   = &http.Client{Timeout: 5 * time.Second}
	ipLocationRateLimitMu  sync.Mutex
	ipLocationBlockedUntil time.Time
)

func enrichClientRegion(c *file.Client) {
	ip := strings.TrimSpace(c.Addr)
	if !isPublicGeoIP(ip) || !beego.AppConfig.DefaultBool("ip_location", true) {
		saveClientRegion(c, "", "")
		return
	}
	if c.ClientRegion != "" && c.ClientIp == "" {
		saveClientRegion(c, c.ClientRegion, ip)
		return
	}
	if c.ClientRegion != "" && c.ClientIp == ip {
		return
	}
	if c.ClientIp != ip {
		saveClientRegion(c, "", "")
	}
	if region := getCachedIpRegion(ip); region != "" {
		saveClientRegion(c, region, ip)
	}
}

func saveClientRegion(c *file.Client, region, ip string) {
	if c.ClientRegion == region && c.ClientIp == ip {
		return
	}
	c.ClientRegion = region
	c.ClientIp = ip
	file.GetDb().JsonDb.StoreClientsToJsonFile()
}

func getCachedIpRegion(ip string) string {
	if v, ok := ipLocationCache.Load(ip); ok {
		entry := v.(ipLocationCacheEntry)
		if entry.Expires.After(time.Now()) {
			return entry.Region
		}
		ipLocationCache.Delete(ip)
	}

	region, ok := fetchIpRegion(ip)
	cacheMinutes := beego.AppConfig.DefaultInt("ip_location_fail_cache_minutes", 60)
	if ok {
		cacheMinutes = beego.AppConfig.DefaultInt("ip_location_cache_hours", 24) * 60
	}
	if cacheMinutes > 0 {
		ipLocationCache.Store(ip, ipLocationCacheEntry{
			Region:  region,
			Expires: time.Now().Add(time.Duration(cacheMinutes) * time.Minute),
		})
	}
	return region
}

func RefreshClientRegion(c *file.Client) (string, error) {
	ip := strings.TrimSpace(c.Addr)
	if !beego.AppConfig.DefaultBool("ip_location", true) {
		return "", errors.New("ip location is disabled")
	}
	if !isPublicGeoIP(ip) {
		saveClientRegion(c, "", "")
		return "", nil
	}
	ipLocationCache.Delete(ip)
	region, ok := fetchIpRegion(ip)
	if !ok || region == "" {
		return "", errors.New("refresh ip location failed")
	}
	saveClientRegion(c, region, ip)
	return region, nil
}

func fetchIpRegion(ip string) (string, bool) {
	if isIpLocationBlocked() {
		return "", false
	}

	api := beego.AppConfig.DefaultString("ip_location_api", defaultIpLocationApi)
	requestUrl := buildIpLocationUrl(api, ip)
	req, err := http.NewRequest(http.MethodGet, requestUrl, nil)
	if err != nil {
		logs.Warn("create ip location request error: %s", err.Error())
		return "", false
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; nps-ip-location/1.0)")

	resp, err := ipLocationHTTPClient.Do(req)
	if err != nil {
		logs.Warn("query ip location %s error: %s", ip, err.Error())
		return "", false
	}
	defer resp.Body.Close()
	updateIpLocationRateLimit(resp.Header)

	if resp.StatusCode != http.StatusOK {
		logs.Warn("query ip location %s http status: %d", ip, resp.StatusCode)
		return "", false
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1024*1024))
	if err != nil {
		logs.Warn("read ip location %s response error: %s", ip, err.Error())
		return "", false
	}
	region, ok := parseIpRegion(resp.Header.Get("Content-Type"), body)
	if !ok {
		logs.Warn("parse ip location %s response failed", ip)
		return "", false
	}
	return region, true
}

func parseIpRegion(contentType string, body []byte) (string, bool) {
	text := strings.TrimSpace(string(body))
	if strings.Contains(strings.ToLower(contentType), "json") || strings.HasPrefix(text, "{") {
		return parseJsonIpRegion(body)
	}
	return parseIpCnRegion(body)
}

func parseJsonIpRegion(body []byte) (string, bool) {
	var data ipLocationResponse
	if err := json.Unmarshal(body, &data); err != nil {
		return "", false
	}
	if data.Status != "success" {
		return "", false
	}
	region := normalizeIpRegion(strings.Join(nonEmptyStrings(data.RegionName, data.City), " "))
	return region, region != ""
}

func parseIpCnRegion(body []byte) (string, bool) {
	doc, err := html.Parse(bytes.NewReader(body))
	if err != nil {
		return "", false
	}
	if region := findIpCnTableRegion(doc); region != "" {
		return region, true
	}
	return "", false
}

func findIpCnTableRegion(n *html.Node) string {
	if n.Type == html.ElementNode && n.Data == "tr" {
		th := findDirectElement(n, "th")
		td := findDirectElement(n, "td")
		if th != nil && td != nil && strings.Contains(normalizeSpace(nodeText(th)), "所在地理位置") {
			if span := findDescendantElement(td, "span"); span != nil {
				return normalizeIpRegion(nodeText(span))
			}
			return normalizeIpRegion(nodeText(td))
		}
	}
	for child := n.FirstChild; child != nil; child = child.NextSibling {
		if region := findIpCnTableRegion(child); region != "" {
			return region
		}
	}
	return ""
}

func findDirectElement(n *html.Node, tag string) *html.Node {
	for child := n.FirstChild; child != nil; child = child.NextSibling {
		if child.Type == html.ElementNode && child.Data == tag {
			return child
		}
	}
	return nil
}

func findDescendantElement(n *html.Node, tag string) *html.Node {
	if n.Type == html.ElementNode && n.Data == tag {
		return n
	}
	for child := n.FirstChild; child != nil; child = child.NextSibling {
		if result := findDescendantElement(child, tag); result != nil {
			return result
		}
	}
	return nil
}

func nodeText(n *html.Node) string {
	if n.Type == html.TextNode {
		return n.Data
	}
	var b strings.Builder
	for child := n.FirstChild; child != nil; child = child.NextSibling {
		b.WriteString(nodeText(child))
		b.WriteString(" ")
	}
	return b.String()
}

func normalizeIpRegion(region string) string {
	fields := strings.Fields(normalizeSpace(region))
	if len(fields) > 0 && fields[0] == "中国" {
		fields = fields[1:]
	}
	return strings.Join(fields, " ")
}

func normalizeSpace(value string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
}

func buildIpLocationUrl(api, ip string) string {
	escapedIP := url.PathEscape(ip)
	if strings.Contains(api, "%s") {
		return fmt.Sprintf(api, escapedIP)
	}
	if strings.Contains(api, "{ip}") {
		return strings.ReplaceAll(api, "{ip}", escapedIP)
	}
	return fmt.Sprintf(defaultIpLocationApi, escapedIP)
}

func isPublicGeoIP(ipStr string) bool {
	ip := net.ParseIP(ipStr)
	if ip == nil {
		return false
	}
	return ip.IsGlobalUnicast() && !ip.IsPrivate() && !ip.IsLoopback() && !ip.IsUnspecified()
}

func nonEmptyStrings(values ...string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			result = append(result, value)
		}
	}
	return result
}

func isIpLocationBlocked() bool {
	ipLocationRateLimitMu.Lock()
	defer ipLocationRateLimitMu.Unlock()
	return time.Now().Before(ipLocationBlockedUntil)
}

func updateIpLocationRateLimit(header http.Header) {
	if header.Get("X-Rl") != "0" {
		return
	}
	ttl, err := strconv.Atoi(header.Get("X-Ttl"))
	if err != nil || ttl <= 0 {
		ttl = 60
	}
	ipLocationRateLimitMu.Lock()
	ipLocationBlockedUntil = time.Now().Add(time.Duration(ttl) * time.Second)
	ipLocationRateLimitMu.Unlock()
}
