package customDev

import (
	"ehang.io/nps/newgocommand"
	"github.com/astaxie/beego/logs"
	"regexp"
	"strings"
	"syscall"
	"time"
)

var (
	PGidPppoe int
)

func ChangeIP(latestAccessServer *time.Time) {
	if PppoeStop() {
		for i := 1; i <= 8; i++ {
			time.Sleep(1 * time.Second)
			if pppoeStatus() == "off" {
				// 等待直到断开拨号
				break
			}
		}

		time.Sleep(2 * time.Second)

		if PppoeStart() {
			for i := 1; i <= 8; i++ {
				time.Sleep(1 * time.Second)
				if pppoeStatus() == "on" {
					// 等待直到拨号成功
					*latestAccessServer = time.Now()
					break
				}
			}
		}
	}
}

func PppoeStart() (result bool) {
	_, success := cmd("/usr/sbin/pppoe-start")

	if success == false {
		time.Sleep(time.Second)
		return
	}

	logs.Info("pppoe start")
	return true
}

func PppoeStop() (result bool) {
	_, success := cmd("/usr/sbin/pppoe-stop")

	if success == false {
		time.Sleep(time.Second)
		return
	}

	logs.Info("pppoe stop")
	return true
}

func pppoeStatus() (status string) {
	out, success := cmd("/usr/sbin/pppoe-status")
	if success == false {
		logs.Error("pppoe-status failed")
		return
	}

	if strings.Contains(out, "Link is up and running") {
		return "on"
	} else if strings.Contains(out, "Link is down") {
		return "off"
	}

	logs.Error("pppoe-status return unexpect: ", out)

	pppDown, _ := regexp.MatchString(`ppp\d* is down`, out)
	if pppDown || strings.Contains(out, "Cannot find") {
		PppoeStop()
		logs.Warning("pause pppoe for 1 minute")
		time.Sleep(time.Minute)
	}
	return
}

func cmd(command string) (result string, success bool) {
	defer func() {
		if err := recover(); err != nil {
			logs.Error("Command 发生严重错误", err)
		}
	}()

	var cmd, out, err = newgocommand.NewCommand().Exec(command)

	if cmd != nil {
		PGidPppoe, err = syscall.Getpgid(cmd.Process.Pid)
		if err == nil {
			errKill := syscall.Kill(-PGidPppoe, 15) // note the minus sign
			if errKill != nil {
				logs.Error("Kill 发生错误", errKill)
			}
		}

		_ = cmd.Wait()
	}

	if err != nil {
		logs.Error("执行命令 %s, 发生错误 %s", command, err)
		return
	}

	return out, true
}
