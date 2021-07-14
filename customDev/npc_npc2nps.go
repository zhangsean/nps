package customDev

import (
	"fmt"
	"github.com/astaxie/beego/logs"
	"net"
	"strings"
	"time"
)

var (
	RemoteNpsIP        string
	npc2npsConn        net.Conn
	latestAccessServer = time.Now() // 最近一次访问服务器成功的时间
	npsTcpPort         = 7999       // 远程服务器 nps 的端口
)

// 心跳
func heatBeat(c net.Conn) {
	for {
		c.Write([]byte(PING))

		time.Sleep(2 * time.Second)
	}
}

func npc2npsHandler(c net.Conn) {
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

		switch fCommand {
		case ALIVE:
			latestAccessServer = time.Now()

		case ROGER_DISLIVE:
			YouCanRestart()
			//content := []byte("测试1\n测试2\n")
			//_ = ioutil.WriteFile("/home/pgshow/Desktop/nps/cmd/npc/111.txt", content, 0644)

		default:
			//c.Write([]byte("服务器端回复" + fCommand + "\n"))
		}
	}
}

func Npc2Nps() {
	go accessNpsTimeOver()

	for {
		// 先等待其他函数拿到远程IP在进行TCP连接
		if RemoteNpsIP == "" {
			time.Sleep(time.Second)
			continue
		}
		conn, err := net.Dial("tcp", fmt.Sprintf("%s:%d", RemoteNpsIP, npsTcpPort))
		if err != nil {
			fmt.Println("客户端建立连接失败")
			TcpWithNpsFailed()
			time.Sleep(3 * time.Second)
			continue
		}

		npc2npsConn = conn

		go heatBeat(conn)
		npc2npsHandler(conn)
	}
}

func accessNpsTimeOver() {
	for {
		// 若25秒没有连上服务器会通知 client 掉线
		if time.Now().Sub(latestAccessServer) >= time.Duration(25)*time.Second {
			NoticeTimeOver()
		}
		time.Sleep(time.Second)
	}
}

// 通知 nps, npc 想要断开连接
func NoticeNpsDisLive() {
	npc2npsConn.Write([]byte(DIS_LIVE2))
}
