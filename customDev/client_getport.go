package customDev

import (
	"fmt"
	"github.com/astaxie/beego/logs"
	"github.com/parnurzeal/gorequest"
	"io/ioutil"
	"regexp"
	"strconv"
	"time"
)

// GetPort 得到可用port然后写入conf文件
func GetPort(serverApiHost string, serverApiPort int) {
	var port string
	for {
		port = fetchPort(serverApiHost, serverApiPort)
		if port != "" {
			break
		}
		time.Sleep(5 * time.Second)
	}

	data, err := ioutil.ReadFile("conf/npc.conf")

	if err != nil {
		panic(err)
	}

	// 替换配置文件里的端口
	re, _ := regexp.Compile("\\[http\\]\\nmode=httpProxy\\nserver_port=(\\d+)")
	rep := re.ReplaceAllString(string(data), "[http]\nmode=httpProxy\nserver_port="+port)

	err = ioutil.WriteFile("conf/npc.conf", []byte(rep), 0777)
	if err != nil {
		panic(err)
	}
}

// 从服务器获取可用的端口
func fetchPort(serverApiHost string, serverApiPort int) (port string) {
	resp, _, errs := gorequest.New().Head(fmt.Sprintf("http://%s:%d/api/freePort", serverApiHost, serverApiPort)).
		Timeout(30 * time.Second).
		End()

	if err := ErrAndStatus(errs, resp); err != nil {
		logs.Error("无法从服务端获取可用端口: %s", err)
		return
	}

	port = resp.Header.Get("Port")

	if port == "" {
		logs.Error("无法从服务端获取可用端口: ", "port is empty")
	}

	// 判断是否数字
	_, err := strconv.Atoi(port)
	if err != nil {
		return
	}

	return port
}
