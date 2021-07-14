package customDev

import (
	"fmt"
	"github.com/astaxie/beego"
	"github.com/astaxie/beego/logs"
	"net"
	"os"
	"strings"
)

func npsConnHandler(c net.Conn) {
	buf := make([]byte, 1024)
	for {
		if c == nil {
			logs.Error("无效的 socket 连接")
			return
		}

		cnt, err := c.Read(buf)
		// 数据读尽、读取错误 关闭 socket 连接
		if cnt == 0 || err != nil {
			c.Close()
			break
		}

		inStr := strings.TrimSpace(string(buf[0:cnt]))
		cInputs := strings.Split(inStr, " ")
		//获取 客户端输入第一条命令
		fCommand := cInputs[0]

		ip := GetIpByStr(c.RemoteAddr().String())

		//println(fCommand)

		switch fCommand {
		case PING:
			renewFreshIp(ip)
			if findClientByIp(ip) {
				c.Write([]byte(ALIVE)) // 告诉npc通道还在线
			}

		case DIS_LIVE2:
			// 客户端断开信号
			setIpExpired(ip)

			logs.Debug(fmt.Sprintf("收到客户端 %s 断开请求", ip))
			c.Write([]byte(ROGER_DISLIVE))

		default:
			//c.Write([]byte("服务器端回复" + fCommand + "\n"))
		}
	}
}

// 开启serverSocket
func NpsTcpServer() {
	tcpPort, _ := beego.AppConfig.Int("nps_tcp_port")
	server, err := net.Listen("tcp", fmt.Sprintf(":%d", tcpPort))

	if err != nil {
		logs.Error(fmt.Sprintf("开启socket服务失败, 端口 %d 可能被占用", tcpPort))
		os.Exit(-1)
	}

	logs.Info(fmt.Sprintf("NPS 开启 Tcp Server, 端口 %d", tcpPort))

	for {
		conn, err := server.Accept()

		if err != nil {
			logs.Debug("连接出错")
		}

		//并发模式 接收来自客户端的连接请求，一个连接 建立一个 conn，服务器资源有可能耗尽 BIO模式
		go npsConnHandler(conn)
	}
}
