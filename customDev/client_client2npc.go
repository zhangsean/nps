package customDev

import (
	"ehang.io/nps/lib/version"
	"fmt"
	"github.com/astaxie/beego/logs"
	"net"
	"os"
	"strings"
)

func clientConnHandler(c net.Conn) {
	buf := make([]byte, 1024)

	for {
		if c == nil {
			logs.Error("无效的 socket 连接")
			return
		}

		cnt, err := c.Read(buf)
		//3.2 数据读尽、读取错误 关闭 socket 连接
		if cnt == 0 || err != nil {
			c.Close()
			break
		}

		inStr := strings.TrimSpace(string(buf[0:cnt]))
		cInputs := strings.Split(inStr, " ")
		//获取 客户端输入第一条命令
		fCommand := cInputs[0]

		//fmt.Println("客户端传输->" + fCommand)

		switch fCommand {
		case IS_USING:
			*TunnelIsUsing = true
		case NOT_USING:
			*TunnelIsUsing = false
		case YOU_CAN_RESRART:
			*NoticedRestart = true
		case TIME_OVER:
			logs.Warning("npc 连接远程服务器 nps 超时")
			*TimeOver = true
		case VKEY_WRONG:
			logs.Error(fmt.Sprintf("Validation key %s incorrect", CNF.CommonConfig.VKey))
		case VERSION_WRONG:
			logs.Error("The npc does not match the nps version. The current core version of the npc is", version.GetVersion())
		case TCP_WITH_NPS_FAILED:
			logs.Error("npc 至 nps 的 tcp 建立失败")

		default:
			//c.Write([]byte("服务器端回复:this command is not in my list\n"))
		}
	}
}

// 开启serverSocket
func ClientTcpServer() {
	//1.监听端口
	server, err := net.Listen("tcp", "127.0.0.1:8005")

	if err != nil {
		logs.Error("开启socket服务失败, 端口 8005 可能被占用")
		os.Exit(-1)
	}

	logs.Info("开启 Tcp Server")

	for {
		//2.接收来自 client 的连接,会阻塞
		conn, err := server.Accept()

		if err != nil {
			logs.Debug("连接出错")
		}

		client2NpcConn = conn

		clientConnHandler(conn)
	}

}

// DisLive 让 npc 通知远程 nps, 本机将要断开连接
func DisLive() {
	if client2NpcConn == nil {
		return
	}
	client2NpcConn.Write([]byte(DIS_LIVE1))
}
