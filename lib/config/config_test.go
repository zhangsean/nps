package config

import (
	"log"
	"regexp"
	"testing"
)

func TestReg(t *testing.T) {
	content := `
[common]
server=127.0.0.1:8284
tp=tcp
vkey=123
[web2]
host=www.baidu.com
host_change=www.sina.com
target=127.0.0.1:8080,127.0.0.1:8082
header_cookkile=122123
header_user-Agent=122123
[web2]
host=www.baidu.com
host_change=www.sina.com
target=127.0.0.1:8080,127.0.0.1:8082
header_cookkile="122123"
header_user-Agent=122123
[tunnel1]
type=udp
target=127.0.0.1:8080
port=9001
compress=snappy
crypt=true
u=1
p=2
[tunnel2]
type=tcp
target=127.0.0.1:8080
port=9001
compress=snappy
crypt=true
u=1
p=2
`
	re, err := regexp.Compile(`\[.+?\]`)
	if err != nil {
		t.Fail()
	}
	log.Println(re.FindAllString(content, -1))
}

func TestDealCommon(t *testing.T) {
	s := `server_addr=127.0.0.1:8284
conn_type=tcp
vkey=123
cip_url=http://127.0.0.1/ip?token=a=b
cip_interval=60`
	c := dealCommon(s)
	if c.Server != "127.0.0.1:8284" {
		t.Fatalf("expected server 127.0.0.1:8284, got %q", c.Server)
	}
	if c.Tp != "tcp" {
		t.Fatalf("expected tp tcp, got %q", c.Tp)
	}
	if c.VKey != "123" {
		t.Fatalf("expected vkey 123, got %q", c.VKey)
	}
	if c.CipUrl != "http://127.0.0.1/ip?token=a=b" {
		t.Fatalf("expected cip_url with query, got %q", c.CipUrl)
	}
	if c.CipInterval != 60 {
		t.Fatalf("expected cip_interval 60, got %d", c.CipInterval)
	}
}

func TestGetTitleContent(t *testing.T) {
	s := "[common]"
	if getTitleContent(s) != "common" {
		t.Fail()
	}
}
