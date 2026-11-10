package server

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/astaxie/beego"
	"golang.org/x/text/encoding/simplifiedchinese"
)

func TestIsPublicGeoIP(t *testing.T) {
	tests := []struct {
		ip   string
		want bool
	}{
		{ip: "36.22.237.47", want: true},
		{ip: "172.18.0.2", want: false},
		{ip: "192.168.1.1", want: false},
		{ip: "127.0.0.1", want: false},
		{ip: "2001:4860:4860::8888", want: true},
		{ip: "not-an-ip", want: false},
	}
	for _, tt := range tests {
		if got := isPublicGeoIP(tt.ip); got != tt.want {
			t.Fatalf("isPublicGeoIP(%q) = %v, want %v", tt.ip, got, tt.want)
		}
	}
}

func TestBuildIpLocationUrl(t *testing.T) {
	tests := []struct {
		name string
		api  string
		ip   string
		want string
	}{
		{
			name: "printf placeholder",
			api:  "https://example.com/json/%s?lang=zh-CN",
			ip:   "36.22.237.47",
			want: "https://example.com/json/36.22.237.47?lang=zh-CN",
		},
		{
			name: "named placeholder",
			api:  "https://example.com/json/{ip}?lang=zh-CN",
			ip:   "36.22.237.47",
			want: "https://example.com/json/36.22.237.47?lang=zh-CN",
		},
		{
			name: "invalid template falls back",
			api:  "https://example.com/json",
			ip:   "36.22.237.47",
			want: "https://ip.cn/ip/36.22.237.47.html",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := buildIpLocationUrl(tt.api, tt.ip); got != tt.want {
				t.Fatalf("url = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestParseIpCnRegion(t *testing.T) {
	body := []byte(`
<table>
<tbody>
<tr><th><span>您查询的IP</span></th><td><span>36.22.237.47</span></td></tr>
<tr>
<th><span>所在地理位置</span></th>
<td><span style="display:inline-block;min-width: 150px;">中国 浙江 杭州</span><a>查看区县</a></td>
</tr>
</tbody>
</table>`)
	region, ok := parseIpRegion("text/html; charset=utf-8", body)
	if !ok {
		t.Fatal("parseIpRegion ok = false, want true")
	}
	if region != "浙江 杭州" {
		t.Fatalf("region = %q, want %q", region, "浙江 杭州")
	}
}

func TestParseJsonIpRegionOmitsCountry(t *testing.T) {
	region, ok := parseIpRegion("application/json", []byte(`{"status":"success","country":"中国","regionName":"浙江","city":"杭州","query":"47.96.89.55"}`))
	if !ok {
		t.Fatal("parseIpRegion ok = false, want true")
	}
	if region != "浙江 杭州" {
		t.Fatalf("region = %q, want %q", region, "浙江 杭州")
	}
}

func TestParseJsonIpRegionVariants(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
		ok   bool
	}{
		{
			name: "ip-api",
			body: `{"status":"success","country":"中国","regionName":"浙江","city":"杭州"}`,
			want: "浙江 杭州",
			ok:   true,
		},
		{
			name: "ipinfo",
			body: `{"ip":"47.96.89.55","city":"Hangzhou","region":"Zhejiang","country":"CN"}`,
			want: "Zhejiang Hangzhou",
			ok:   true,
		},
		{
			name: "ip.sb",
			body: `{"region":"Zhejiang","city":"Hangzhou","country":"China"}`,
			want: "Zhejiang Hangzhou",
			ok:   true,
		},
		{
			name: "baidu opendata",
			body: `{"status":"0","data":[{"location":"浙江省杭州市 阿里云","origip":"47.96.89.55"}]}`,
			want: "浙江 杭州",
			ok:   true,
		},
		{
			name: "pconline",
			body: `{"ip":"47.96.89.55","pro":"浙江省","city":"杭州市","addr":"浙江省杭州市 电信"}`,
			want: "浙江 杭州",
			ok:   true,
		},
		{
			name: "ipwho.is",
			body: `{"ip":"47.96.89.55","success":true,"region":"Zhejiang Sheng","city":"Hangzhou"}`,
			want: "Zhejiang Sheng Hangzhou",
			ok:   true,
		},
		{
			name: "ip-api failure",
			body: `{"status":"fail","message":"private range"}`,
			ok:   false,
		},
		{
			name: "taobao rate limited",
			body: `{"msg":"the request over max qps for user","code":4}`,
			ok:   false,
		},
		{
			name: "empty body",
			body: ``,
			ok:   false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			region, ok := parseIpRegion("application/json", []byte(tt.body))
			if ok != tt.ok {
				t.Fatalf("parseIpRegion ok = %v, want %v", ok, tt.ok)
			}
			if region != tt.want {
				t.Fatalf("region = %q, want %q", region, tt.want)
			}
		})
	}
}

func TestParseIpRegionDecodesGbk(t *testing.T) {
	body, err := simplifiedchinese.GBK.NewEncoder().Bytes([]byte(`{"pro":"浙江省","city":"杭州市"}`))
	if err != nil {
		t.Fatalf("encode gbk: %s", err)
	}
	region, ok := parseIpRegion("text/html; charset=GBK", body)
	if !ok {
		t.Fatal("parseIpRegion ok = false, want true")
	}
	if region != "浙江 杭州" {
		t.Fatalf("region = %q, want %q", region, "浙江 杭州")
	}
}

func TestParseIpLocationApis(t *testing.T) {
	apis := parseIpLocationApis(" https://a.example/ip/%s ,\nhttps://b.example/ip/{ip}\ninvalid-api,  ")
	if len(apis) != 2 {
		t.Fatalf("apis = %v, want 2 valid entries", apis)
	}
	if apis[0] != "https://a.example/ip/%s" || apis[1] != "https://b.example/ip/{ip}" {
		t.Fatalf("apis = %v", apis)
	}
}

func TestFetchIpRegionFromIpCnHtml(t *testing.T) {
	resetIpLocationTestState(t)
	setIpLocationConfig(t, "ip_location_min_interval_seconds", "0")
	setIpLocationConfig(t, "ip_location_api_cooldown_minutes", "10")
	setIpLocationConfig(t, "ip_location_max_attempts", "2")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("User-Agent"); got == "" {
			t.Fatal("User-Agent is empty")
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`
<table>
<tr><th><span>所在地理位置</span></th><td><span>中国 浙江 杭州</span></td></tr>
</table>`))
	}))
	defer server.Close()

	setIpLocationConfig(t, "ip_location_apis", server.URL+"/ip/{ip}.html")
	region, ok := fetchIpRegion("36.22.237.47", false)
	if !ok {
		t.Fatal("fetchIpRegion ok = false, want true")
	}
	if region != "浙江 杭州" {
		t.Fatalf("region = %q, want %q", region, "浙江 杭州")
	}
}

func TestFetchIpRegionRotatesApisAndCoolsDownFailedApi(t *testing.T) {
	resetIpLocationTestState(t)
	setIpLocationConfig(t, "ip_location_min_interval_seconds", "0")
	setIpLocationConfig(t, "ip_location_api_cooldown_minutes", "10")
	setIpLocationConfig(t, "ip_location_max_attempts", "2")

	var firstCalls, secondCalls int32
	first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&firstCalls, 1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer first.Close()
	second := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&secondCalls, 1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"success","regionName":"浙江","city":"杭州"}`))
	}))
	defer second.Close()

	setIpLocationConfig(t, "ip_location_apis", first.URL+"/ip/%s,"+second.URL+"/ip/%s")

	region, ok := fetchIpRegion("36.22.237.47", false)
	if !ok || region != "浙江 杭州" {
		t.Fatalf("fetchIpRegion = %q, %v, want 浙江 杭州, true", region, ok)
	}
	if got := atomic.LoadInt32(&firstCalls); got != 1 {
		t.Fatalf("first api calls = %d, want 1", got)
	}
	if got := atomic.LoadInt32(&secondCalls); got != 1 {
		t.Fatalf("second api calls = %d, want 1", got)
	}

	// 再次从第一个接口开始，应直接跳过处于冷却期的接口。
	atomic.StoreUint32(&ipLocationRoundRobin, 0)
	region, ok = fetchIpRegion("36.22.237.47", false)
	if !ok || region != "浙江 杭州" {
		t.Fatalf("fetchIpRegion = %q, %v, want 浙江 杭州, true", region, ok)
	}
	if got := atomic.LoadInt32(&firstCalls); got != 1 {
		t.Fatalf("first api calls after cooldown = %d, want 1", got)
	}
	if got := atomic.LoadInt32(&secondCalls); got != 2 {
		t.Fatalf("second api calls after cooldown = %d, want 2", got)
	}
}

func resetIpLocationTestState(t *testing.T) {
	t.Helper()
	ipLocationCache.Range(func(key, _ interface{}) bool {
		ipLocationCache.Delete(key)
		return true
	})
	ipLocationApiStateMu.Lock()
	ipLocationApiStates = map[string]*ipLocationApiState{}
	ipLocationApiStateMu.Unlock()
	atomic.StoreUint32(&ipLocationRoundRobin, 0)
	ipLocationSlotMu.Lock()
	ipLocationLastRequest = time.Time{}
	ipLocationSlotMu.Unlock()
	t.Cleanup(func() {
		ipLocationApiStateMu.Lock()
		ipLocationApiStates = map[string]*ipLocationApiState{}
		ipLocationApiStateMu.Unlock()
		atomic.StoreUint32(&ipLocationRoundRobin, 0)
		ipLocationSlotMu.Lock()
		ipLocationLastRequest = time.Time{}
		ipLocationSlotMu.Unlock()
	})
}

func setIpLocationConfig(t *testing.T, key, value string) {
	t.Helper()
	old := beego.AppConfig.String(key)
	if err := beego.AppConfig.Set(key, value); err != nil {
		t.Fatalf("set %s: %s", key, err)
	}
	t.Cleanup(func() {
		_ = beego.AppConfig.Set(key, old)
	})
}
