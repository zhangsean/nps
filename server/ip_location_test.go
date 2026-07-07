package server

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/astaxie/beego"
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

func TestFetchIpRegionFromIpCnHtml(t *testing.T) {
	oldClient := ipLocationHTTPClient
	oldApi := beego.AppConfig.String("ip_location_api")
	defer func() {
		ipLocationHTTPClient = oldClient
		beego.AppConfig.Set("ip_location_api", oldApi)
	}()

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

	ipLocationHTTPClient = server.Client()
	beego.AppConfig.Set("ip_location_api", server.URL+"/ip/{ip}.html")

	region, ok := fetchIpRegion("36.22.237.47")
	if !ok {
		t.Fatal("fetchIpRegion ok = false, want true")
	}
	if region != "浙江 杭州" {
		t.Fatalf("region = %q, want %q", region, "浙江 杭州")
	}
}
