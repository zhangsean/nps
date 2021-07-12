package customDev

import (
	"fmt"
	"github.com/astaxie/beego/logs"
	"github.com/parnurzeal/gorequest"
	"regexp"
	"time"
)

func RunHeartbeat(serverApiHost string, serverApiPort int, latestAccessServer *time.Time, needRestart *bool, noticedRestart *bool) {
	var (
		request = gorequest.New()
	)

	for {
		time.Sleep(2 * time.Second)

		//if *needRestart == true {
		//	continue
		//}
		resp, _, errs := request.Head(fmt.Sprintf("http://%s:%d/api/heartbeat?vkey=%s&disLive=%d", serverApiHost, serverApiPort, CNF.CommonConfig.VKey, B2i(*needRestart))).
			Timeout(10 * time.Second).
			End()

		if err := ErrAndStatus(errs, resp); err != nil {
			logs.Error("代理池的API无法访问: %s", err)
			continue
		}

		if *needRestart {
			// 访问服务器接口无错误的情况下就说明已经通知
			*noticedRestart = true
		}

		if resp.Header.Get("Alive") == "1" {
			*latestAccessServer = time.Now()
		}

		//nowIP := resp.Header.Get("ip")
		//if nowIP != "" {
		//	*latestAccessServer = time.Now()
		//}
	}
}

// GetIpByStr 从字符串里提取IP
func GetIpByStr(str string) (ip string) {
	re := regexp.MustCompile(`(((2(5[0-5]|[0-4]\d))|[0-1]?\d{1,2})(\.((2(5[0-5]|[0-4]\d))|[0-1]?\d{1,2})){3})`)
	match := re.FindStringSubmatch(str)
	if match != nil {
		return match[1]
	}
	return
}
