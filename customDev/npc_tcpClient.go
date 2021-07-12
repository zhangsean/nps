package customDev

import (
	"fmt"
	"net"
	"time"
)

func cConnHandler(c net.Conn) {
	//缓存 conn 中的数据
	buf := make([]byte, 1024)

	for {
		time.Sleep(time.Second)
		//客户端请求数据写入 conn，并传输
		c.Write([]byte("alive"))
		//服务器端返回的数据写入空buf
		cnt, err := c.Read(buf)

		if err != nil {
			fmt.Printf("客户端读取数据失败 %s\n", err)
			continue
		}

		//回显服务器端回传的信息
		fmt.Print("服务器端回复" + string(buf[0:cnt]))
	}
}

func ClientSocket() {
	conn, err := net.Dial("tcp", "127.0.0.1:8005")
	if err != nil {
		fmt.Println("客户端建立连接失败")
		return
	}

	cConnHandler(conn)
}
