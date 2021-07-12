package customDev

import (
	"fmt"
	"github.com/astaxie/beego/logs"
	"net"
	"os"
	"strings"
)

func connHandler(c net.Conn) {
	//1.conn是否有效
	if c == nil {
		logs.Error("无效的 socket 连接")
	}

	//2.新建网络数据流存储结构
	buf := make([]byte, 4096)
	//3.循环读取网络数据流
	for {
		//3.1 网络数据流读入 buffer
		cnt, err := c.Read(buf)
		//3.2 数据读尽、读取错误 关闭 socket 连接
		if cnt == 0 || err != nil {
			c.Close()
			break
		}

		//3.3 根据输入流进行逻辑处理
		//buf数据 -> 去两端空格的string
		inStr := strings.TrimSpace(string(buf[0:cnt]))
		//去除 string 内部空格
		cInputs := strings.Split(inStr, " ")
		//获取 客户端输入第一条命令
		fCommand := cInputs[0]

		fmt.Println("客户端传输->" + fCommand)

		switch fCommand {
		case "ping":
			c.Write([]byte("服务器端回复-> pong\n"))
		case "hello":
			c.Write([]byte("服务器端回复-> world\n"))
		default:
			c.Write([]byte("服务器端回复" + fCommand + "\n"))
		}

		//c.Close() //关闭client端的连接，telnet 被强制关闭

		fmt.Printf("来自 %v 的连接关闭\n", c.RemoteAddr())
	}
}

// 开启serverSocket
func ServerSocket() {
	//1.监听端口
	server, err := net.Listen("tcp", ":8005")

	if err != nil {
		logs.Error("开启socket服务失败, 端口 8005 可能被占用")
		os.Exit(-1)
	}

	logs.Info("正在开启 Tcp Server ...")

	for {
		//2.接收来自 client 的连接,会阻塞
		conn, err := server.Accept()

		if err != nil {
			logs.Debug("连接出错")
		}

		//并发模式 接收来自客户端的连接请求，一个连接 建立一个 conn，服务器资源有可能耗尽 BIO模式
		go connHandler(conn)
	}

}
