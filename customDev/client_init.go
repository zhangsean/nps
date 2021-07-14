package customDev

import (
	"github.com/astaxie/beego/logs"
	"net"
	"os"
	"os/signal"
	"syscall"
)

var (
	GidNpc                = new(int)
	NoticedRestart        = new(bool)
	TunnelIsUsing         = new(bool)
	ServerAccessFailTimes = new(int)    // 记录因无法访问服务器而连续重拨的次数
	TimeOver              = new(bool)   // npc无法访问nps超时
	ServerApiHost         = new(string) // 远程服务器的IP
	client2NpcConn        net.Conn      // client 和 npc 之间的 conn
)

func ClientInit() {
	ReadConfig() // 从npc.conf 读取配置
	serverIp := GetIpByStr(CNF.CommonConfig.Server)
	ServerApiHost = &serverIp

	go ClientTcpServer()

	//创建监听退出chan
	c := make(chan os.Signal)
	//监听指定信号 ctrl+c kill
	signal.Notify(c, syscall.SIGHUP, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT)

	go func() {
		for s := range c {
			switch s {
			case syscall.SIGHUP, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT:
				exitFunc()
			}
		}
	}()

	PppoeStart()
}

func exitFunc() {
	print("\n")
	logs.Warning("开始退出...")
	logs.Warning("执行清理...")
	_ = syscall.Kill(-*GidNpc, 15)
	_ = syscall.Kill(-PGidPppoe, 15)
	logs.Warning("结束退出...")
	os.Exit(0)
}
