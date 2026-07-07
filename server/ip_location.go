package server

import (
	"encoding/json"
	"fmt"
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
)

const defaultIpLocationApi = "http://ip-api.com/json/%s?fields=status,message,country,regionName,city,query&lang=zh-CN"

type ipLocationCacheEntry struct {
	Region  string
	Expires time.Time
}

type ipLocationResponse struct {
	Status     string `json:"status"`
	Message    string `json:"message"`
	Country    string `json:"country"`
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

func enrichClientAddrRegion(c *file.Client) {
	c.AddrRegion = ""
	ip := strings.TrimSpace(c.Addr)
	if !isPublicGeoIP(ip) || !beego.AppConfig.DefaultBool("ip_location", true) {
		return
	}
	if region := getCachedIpRegion(ip); region != "" {
		c.AddrRegion = region
	}
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

	var data ipLocationResponse
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		logs.Warn("decode ip location %s error: %s", ip, err.Error())
		return "", false
	}
	if data.Status != "success" {
		logs.Warn("query ip location %s failed: %s", ip, data.Message)
		return "", false
	}
	region := strings.Join(nonEmptyStrings(data.Country, data.RegionName, data.City), " ")
	return region, region != ""
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
