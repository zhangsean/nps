package customDev

import (
	"github.com/astaxie/beego"
)

var (
	freePortsChan = make(chan int, 100000)
)

// 用队列来保障一定时间内不分配冲突的端口
func popPort() int {
	if len(freePortsChan) <= 0 {
		restore()
	}

	return <-freePortsChan
}

// 重新填充
func restore() {
	ServerPortStart, _ := beego.AppConfig.Int("server_port_start")
	ServerPortEnd, _ := beego.AppConfig.Int("server_port_end")
	for i := ServerPortStart; i <= ServerPortEnd; i++ {
		freePortsChan <- i
	}
}

func FindFreePort() (port int) {
	for {
		port = CheckPort(popPort())
		if port > 0 {
			return port
		}
	}
}
