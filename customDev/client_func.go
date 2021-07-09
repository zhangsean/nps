package customDev

import (
	"fmt"
	"github.com/astaxie/beego/logs"
	"github.com/gofiber/fiber/v2"
	"github.com/parnurzeal/gorequest"
	"regexp"
	"time"
)

// ClientWeb 启动客户端的web功能
func ClientWeb(port int) {
	if CheckPort(port) == 0 {
		panic(fmt.Sprintf("fiber web port %d is unavailable", port))
	}
	app := fiber.New()

	// status 返回 6 表示通道正常
	app.Get("/api/6", func(c *fiber.Ctx) error {
		return c.SendStatus(6)
	})

	_ = app.Listen(fmt.Sprintf(":%d", port))
}

func RunHeartbeat(serverApiHost string, serverApiPort int, latestAccessServer *time.Time, readyRestart *bool) {
	var (
		request = gorequest.New()
	)

	for {
		time.Sleep(2 * time.Second)

		if *readyRestart == true {
			continue
		}

		resp, _, errs := request.Head(fmt.Sprintf("http://%s:%d/api/heartbeat?vkey=%s", serverApiHost, serverApiPort, CNF.CommonConfig.VKey)).
			Timeout(10 * time.Second).
			End()

		if err := ErrAndStatus(errs, resp); err != nil {
			logs.Error("代理池的API无法访问: %s", err)
			continue
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
