package customDev

import (
	"ehang.io/nps/lib/goroutine"
	"fmt"
	"net"
	"strings"
	"time"
)

var npc2clientConn net.Conn
var isBusy bool

func npc2clientHandlerSend(c net.Conn) {
	for {
		if c == nil {
			return
		}

		//客户端请求数据写入 conn，并传输
		if goroutine.CopyConnsPool.Running() > 0 {
			isBusy = true
			c.Write([]byte(IS_USING)) // conn数量大于0，通道使用中
		} else {
			isBusy = false
			c.Write([]byte(NOT_USING)) // conn数量等于0，通道没有任务，可以安全切换IP
		}
		time.Sleep(300 * time.Millisecond)
	}
}

func npc2clientHandler(c net.Conn) {
	//缓存 conn 中的数据
	buf := make([]byte, 1024)

	for {
		//服务器端返回的数据写入空buf
		cnt, err := c.Read(buf)

		if err != nil {
			fmt.Println("客户端读取数据失败 %s", err)
			continue
		}

		inStr := strings.TrimSpace(string(buf[0:cnt]))
		cInputs := strings.Split(inStr, " ")
		fCommand := cInputs[0]

		switch fCommand {
		case DIS_LIVE1:
			// 收到 client 发来的断开命令后转发给远程 nps, 同时不断的告知 client 通道的使用情况
			go npc2clientHandlerSend(c)
			NoticeNpsDisLive()

		default:
			//c.Write([]byte("客户端回复" + fCommand + "\n"))
		}
	}
}

// Npc2Client npc 连接 client
func Npc2Client() {
	conn, err := net.Dial("tcp", "127.0.0.1:8005")
	if err != nil {
		panic("客户端建立连接失败")
	}

	npc2clientConn = conn

	npc2clientHandler(conn)
}

// YouCanRestart 让 npc 通知 client, 远程 nps 已经知道你要重拨了
func YouCanRestart() {
	if npc2clientConn == nil {
		return
	}
	npc2clientConn.Write([]byte(YOU_CAN_RESRART))
}

// NoticeTimeOver npc 和 nps 的心跳超时了
func NoticeTimeOver() {
	if npc2clientConn == nil {
		return
	}
	npc2clientConn.Write([]byte(TIME_OVER))
}

// 告诉 client 密匙错误
func VkeyWrong() {
	if npc2clientConn == nil {
		return
	}
	npc2clientConn.Write([]byte(VKEY_WRONG))
}

func VersionWrong() {
	if npc2clientConn == nil {
		return
	}
	npc2clientConn.Write([]byte(VERSION_WRONG))
}

func TcpWithNpsFailed() {
	if npc2clientConn == nil {
		return
	}
	npc2clientConn.Write([]byte(TCP_WITH_NPS_FAILED))
}
