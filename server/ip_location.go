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
	"sync/atomic"
	"time"
	"unicode/utf8"

	"ehang.io/nps/lib/file"
	"github.com/astaxie/beego"
	"github.com/astaxie/beego/logs"
	"golang.org/x/net/html"
	"golang.org/x/text/encoding/simplifiedchinese"
)

const (
	defaultIpLocationApi  = "https://ip.cn/ip/%s.html"
	defaultIpLocationUA   = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"
	ipLocationBodyMaxSize = 1024 * 1024
)

// builtinIpLocationApis 是内置的多接口列表，查询时轮询使用，单个接口失败后进入冷却。
var builtinIpLocationApis = []string{
	"https://ip.cn/ip/%s.html",
	"https://opendata.baidu.com/api.php?query=%s&co=&resource_id=6006&oe=utf8&ie=utf8&format=json",
	"https://whois.pconline.com.cn/ipJson.jsp?ip=%s&json=true",
	"https://api.ip.sb/geoip/%s",
	"https://ipinfo.io/%s/geo",
	"http://ip-api.com/json/%s?lang=zh-CN",
	"https://ipwho.is/%s",
}

var ipLocationRegionKeys = []string{"regionName", "region_name", "regionname", "region", "prov", "pro", "province", "state"}

var ipLocationCityKeys = []string{"city", "city_name", "cityname"}

type ipLocationCacheEntry struct {
	Region  string
	Expires time.Time
}

type ipLocationApiState struct {
	CooldownUntil time.Time
	LastError     string
}

var (
	ipLocationCache      sync.Map
	ipLocationHTTPClient = &http.Client{Timeout: 5 * time.Second}

	ipLocationApiStateMu sync.Mutex
	ipLocationApiStates  = map[string]*ipLocationApiState{}
	ipLocationRoundRobin uint32

	ipLocationSlotMu      sync.Mutex
	ipLocationLastRequest time.Time

	ipLocationQueue     = make(chan string, 256)
	ipLocationQueueOnce sync.Once
	ipLocationPending   sync.Map
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
	region, cached := getCachedIpRegion(ip)
	if !cached {
		// 列表查询不再同步等待第三方接口，改为后台按最小间隔慢慢补齐，避免页面加载时集中请求被拉黑。
		enqueueIpRegionLookup(ip)
		return
	}
	if region != "" {
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

func getCachedIpRegion(ip string) (string, bool) {
	if v, ok := ipLocationCache.Load(ip); ok {
		entry := v.(ipLocationCacheEntry)
		if entry.Expires.After(time.Now()) {
			return entry.Region, true
		}
		ipLocationCache.Delete(ip)
	}
	return "", false
}

func storeIpLocationCache(ip, region string, ok bool) {
	minutes := ipLocationFailCacheMinutes()
	if ok && region != "" {
		minutes = ipLocationCacheHours() * 60
	}
	if minutes > 0 {
		ipLocationCache.Store(ip, ipLocationCacheEntry{
			Region:  region,
			Expires: time.Now().Add(time.Duration(minutes) * time.Minute),
		})
	}
	if ok && region != "" {
		applyIpRegionToClients(ip, region)
	}
}

func applyIpRegionToClients(ip, region string) {
	db := file.GetDb()
	if db == nil || db.JsonDb == nil {
		return
	}
	db.JsonDb.Clients.Range(func(_, value interface{}) bool {
		c, ok := value.(*file.Client)
		if !ok || strings.TrimSpace(c.Addr) != ip {
			return true
		}
		saveClientRegion(c, region, ip)
		return true
	})
}

func enqueueIpRegionLookup(ip string) {
	if ip == "" {
		return
	}
	ipLocationQueueOnce.Do(func() {
		go ipLocationWorker()
	})
	if _, loaded := ipLocationPending.LoadOrStore(ip, struct{}{}); loaded {
		return
	}
	select {
	case ipLocationQueue <- ip:
	default:
		ipLocationPending.Delete(ip)
	}
}

func ipLocationWorker() {
	for ip := range ipLocationQueue {
		if beego.AppConfig.DefaultBool("ip_location", true) {
			region, ok := fetchIpRegion(ip, true)
			storeIpLocationCache(ip, region, ok)
		}
		ipLocationPending.Delete(ip)
	}
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
	region, ok := fetchIpRegion(ip, false)
	if !ok || region == "" {
		storeIpLocationCache(ip, "", false)
		return "", errors.New("refresh ip location failed: all ip location apis are cooling down or unavailable")
	}
	storeIpLocationCache(ip, region, true)
	saveClientRegion(c, region, ip)
	return region, nil
}

func fetchIpRegion(ip string, background bool) (string, bool) {
	apis := ipLocationApis()
	if len(apis) == 0 {
		return "", false
	}
	attempts := ipLocationMaxAttempts()
	if attempts > len(apis) {
		attempts = len(apis)
	}
	start := int(atomic.AddUint32(&ipLocationRoundRobin, 1)-1) % len(apis)
	tried := 0
	lastErr := "no available ip location api"
	for offset := 0; offset < len(apis) && tried < attempts; offset++ {
		api := apis[(start+offset)%len(apis)]
		if ipLocationApiCoolingDown(api) {
			lastErr = "all ip location apis are cooling down"
			continue
		}
		tried++
		if background {
			waitIpLocationRequestSlot()
		}
		region, err := fetchIpRegionFromApi(api, ip)
		if err != nil {
			ipLocationMarkApiFailed(api, err)
			logs.Warn("query ip location %s via %s failed: %s", ip, ipLocationApiHost(api), err.Error())
			lastErr = err.Error()
			continue
		}
		ipLocationMarkApiSuccess(api)
		return region, true
	}
	logs.Warn("query ip location %s failed: %s", ip, lastErr)
	return "", false
}

func ipLocationApis() []string {
	if raw := strings.TrimSpace(beego.AppConfig.String("ip_location_apis")); raw != "" {
		return parseIpLocationApis(raw)
	}
	apis := make([]string, 0, len(builtinIpLocationApis)+1)
	if custom := strings.TrimSpace(beego.AppConfig.String("ip_location_api")); custom != "" && custom != defaultIpLocationApi && isValidIpLocationApi(custom) {
		apis = append(apis, custom)
	}
	for _, api := range builtinIpLocationApis {
		if isValidIpLocationApi(api) {
			apis = append(apis, api)
		}
	}
	return apis
}

func parseIpLocationApis(raw string) []string {
	fields := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == '\n' || r == '\r' || r == ';'
	})
	apis := make([]string, 0, len(fields))
	for _, field := range fields {
		api := strings.TrimSpace(field)
		if isValidIpLocationApi(api) {
			apis = append(apis, api)
		}
	}
	return apis
}

func isValidIpLocationApi(api string) bool {
	return strings.Contains(api, "%s") || strings.Contains(api, "{ip}")
}

func ipLocationApiHost(api string) string {
	parsed, err := url.Parse(buildIpLocationUrl(api, "127.0.0.1"))
	if err != nil {
		return api
	}
	return parsed.Host
}

func fetchIpRegionFromApi(api, ip string) (string, error) {
	requestUrl := buildIpLocationUrl(api, ip)
	req, err := http.NewRequest(http.MethodGet, requestUrl, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", defaultIpLocationUA)
	req.Header.Set("Accept", "application/json, text/html;q=0.9, */*;q=0.8")
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.8")

	resp, err := ipLocationHTTPClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if cooldown := ipLocationHeaderCooldown(resp.Header); cooldown > 0 {
		ipLocationBlockApi(api, cooldown)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("http status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, ipLocationBodyMaxSize))
	if err != nil {
		return "", err
	}
	region, ok := parseIpRegion(resp.Header.Get("Content-Type"), body)
	if !ok || region == "" {
		return "", errors.New("parse response failed")
	}
	return region, nil
}

func parseIpRegion(contentType string, body []byte) (string, bool) {
	body = decodeIpLocationCharset(contentType, body)
	text := strings.TrimSpace(string(body))
	if text == "" {
		return "", false
	}
	if strings.Contains(strings.ToLower(contentType), "json") || strings.HasPrefix(text, "{") {
		return parseJsonIpRegion(body)
	}
	return parseIpCnRegion(body)
}

func decodeIpLocationCharset(contentType string, body []byte) []byte {
	ct := strings.ToLower(contentType)
	if !strings.Contains(ct, "gbk") && !strings.Contains(ct, "gb2312") && !strings.Contains(ct, "gb18030") {
		return body
	}
	decoded, err := simplifiedchinese.GBK.NewDecoder().Bytes(body)
	if err != nil {
		return body
	}
	return decoded
}

func parseJsonIpRegion(body []byte) (string, bool) {
	var root map[string]interface{}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	if err := decoder.Decode(&root); err != nil {
		return "", false
	}
	if !ipLocationJsonSucceeded(root) {
		return "", false
	}
	candidates := []map[string]interface{}{root}
	for _, key := range []string{"data", "result", "geo"} {
		if nested := asJsonObject(root[key]); nested != nil {
			candidates = append(candidates, nested)
		}
	}
	for _, candidate := range candidates {
		if region := extractJsonIpRegion(candidate); region != "" {
			return region, true
		}
	}
	return "", false
}

func ipLocationJsonSucceeded(root map[string]interface{}) bool {
	if success, ok := root["success"].(bool); ok && !success {
		return false
	}
	if status, ok := root["status"].(string); ok {
		status = strings.ToLower(strings.TrimSpace(status))
		if status != "" && status != "success" && status != "ok" && status != "0" {
			return false
		}
	}
	if code, exists := root["code"]; exists {
		switch value := code.(type) {
		case json.Number:
			return value.String() == "0"
		case float64:
			return value == 0
		case string:
			value = strings.ToLower(strings.TrimSpace(value))
			return value == "" || value == "0" || value == "success" || value == "ok"
		case nil:
			return true
		default:
			return false
		}
	}
	return true
}

func asJsonObject(value interface{}) map[string]interface{} {
	switch v := value.(type) {
	case map[string]interface{}:
		return v
	case []interface{}:
		if len(v) > 0 {
			if first, ok := v[0].(map[string]interface{}); ok {
				return first
			}
		}
	}
	return nil
}

func extractJsonIpRegion(values map[string]interface{}) string {
	region := cleanJsonIpRegion(firstJsonString(values, ipLocationRegionKeys...))
	city := cleanJsonIpRegion(firstJsonString(values, ipLocationCityKeys...))
	if region == "" && city == "" {
		// 百度 opendata 接口使用 location 返回“省+市+运营商”，如“浙江省杭州市 阿里云”。
		return locationToIpRegion(firstJsonString(values, "location"))
	}
	if region == "" {
		return city
	}
	if city == "" || strings.Contains(region, city) {
		return region
	}
	return region + " " + city
}

func firstJsonString(values map[string]interface{}, keys ...string) string {
	for _, key := range keys {
		if value, ok := values[key].(string); ok {
			value = strings.TrimSpace(value)
			if value != "" {
				return value
			}
		}
	}
	return ""
}

func locationToIpRegion(location string) string {
	fields := strings.Fields(normalizeSpace(location))
	if len(fields) == 0 {
		return ""
	}
	token := fields[0]
	for _, separator := range []string{"特别行政区", "自治区", "省", "市"} {
		if idx := strings.Index(token, separator); idx > 0 && idx+len(separator) < len(token) {
			left := cleanRegionField(token[:idx+len(separator)])
			right := cleanRegionField(token[idx+len(separator):])
			if left != "" && right != "" {
				return left + " " + right
			}
		}
	}
	return cleanRegionField(token)
}

func cleanJsonIpRegion(value string) string {
	value = normalizeIpRegion(value)
	if value == "" {
		return ""
	}
	fields := strings.Fields(value)
	for i, field := range fields {
		fields[i] = cleanRegionField(field)
	}
	return strings.Join(nonEmptyStrings(fields...), " ")
}

func cleanRegionField(value string) string {
	value = strings.TrimSpace(value)
	for _, suffix := range []string{"特别行政区", "自治区", "省", "市"} {
		if strings.HasSuffix(value, suffix) && utf8.RuneCountInString(value) > utf8.RuneCountInString(suffix) {
			return strings.TrimSuffix(value, suffix)
		}
	}
	return value
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

func ipLocationCacheHours() int {
	return beego.AppConfig.DefaultInt("ip_location_cache_hours", 168)
}

func ipLocationFailCacheMinutes() int {
	return beego.AppConfig.DefaultInt("ip_location_fail_cache_minutes", 360)
}

func ipLocationApiCooldown() time.Duration {
	return time.Duration(beego.AppConfig.DefaultInt("ip_location_api_cooldown_minutes", 10)) * time.Minute
}

func ipLocationMinInterval() time.Duration {
	return time.Duration(beego.AppConfig.DefaultInt("ip_location_min_interval_seconds", 5)) * time.Second
}

func ipLocationMaxAttempts() int {
	attempts := beego.AppConfig.DefaultInt("ip_location_max_attempts", 2)
	if attempts < 1 {
		attempts = 1
	}
	if attempts > 10 {
		attempts = 10
	}
	return attempts
}

func ipLocationApiCoolingDown(api string) bool {
	ipLocationApiStateMu.Lock()
	defer ipLocationApiStateMu.Unlock()
	state := ipLocationApiStates[api]
	return state != nil && time.Now().Before(state.CooldownUntil)
}

func ipLocationMarkApiFailed(api string, err error) {
	cooldown := ipLocationApiCooldown()
	if cooldown <= 0 {
		return
	}
	ipLocationBlockApi(api, cooldown)
	ipLocationApiStateMu.Lock()
	if state := ipLocationApiStates[api]; state != nil {
		state.LastError = err.Error()
	}
	ipLocationApiStateMu.Unlock()
}

func ipLocationBlockApi(api string, cooldown time.Duration) {
	if cooldown <= 0 {
		return
	}
	until := time.Now().Add(cooldown)
	ipLocationApiStateMu.Lock()
	state := ipLocationApiStates[api]
	if state == nil {
		state = &ipLocationApiState{}
		ipLocationApiStates[api] = state
	}
	if until.After(state.CooldownUntil) {
		state.CooldownUntil = until
	}
	ipLocationApiStateMu.Unlock()
}

func ipLocationMarkApiSuccess(api string) {
	ipLocationApiStateMu.Lock()
	delete(ipLocationApiStates, api)
	ipLocationApiStateMu.Unlock()
}

func ipLocationHeaderCooldown(header http.Header) time.Duration {
	if header.Get("X-Rl") == "0" {
		if ttl, err := strconv.Atoi(header.Get("X-Ttl")); err == nil && ttl > 0 {
			return time.Duration(ttl) * time.Second
		}
		return time.Minute
	}
	if retryAfter := strings.TrimSpace(header.Get("Retry-After")); retryAfter != "" {
		if seconds, err := strconv.Atoi(retryAfter); err == nil && seconds > 0 {
			return time.Duration(seconds) * time.Second
		}
	}
	return 0
}

func waitIpLocationRequestSlot() {
	interval := ipLocationMinInterval()
	if interval <= 0 {
		return
	}
	ipLocationSlotMu.Lock()
	defer ipLocationSlotMu.Unlock()
	if !ipLocationLastRequest.IsZero() {
		if wait := interval - time.Since(ipLocationLastRequest); wait > 0 {
			time.Sleep(wait)
		}
	}
	ipLocationLastRequest = time.Now()
}
