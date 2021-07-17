package main

import (
	"ehang.io/nps/customDev"
	"fmt"
	"github.com/astaxie/beego/logs"
	"os/exec"
	"syscall"
	"time"
)

var (
	ClientIpExpiry = 60   // Adsl拨号间隔（秒）
	serverApiPort  = 8002 // 远程服务器 fiber web 的端口
)

func main() {
	logs.Reset()
	logs.EnableFuncCallDepth(true)
	logs.SetLogFuncCallDepth(3)

	customDev.ClientInit()

	go npcRunningStatus()

	for {
		runNPC()

		if *customDev.ServerAccessFailTimes >= 5 {
			logs.Warning("server maybe down, try after 5 minutes later")
			customDev.PppoeStop()
			time.Sleep(5 * time.Minute)
		}

		customDev.ChangeIP()
		time.Sleep(time.Second)
	}
}

func runNPC() {
	defer func() {
		if err := recover(); err != nil {
			logs.Error("Command 发生严重错误", err)
		}
	}()

	// ./npc -server=1.1.1.1:8024 -vkey=客户端的密钥
	npcCmd := fmt.Sprintf("-server=%s:%d -vkey=%s", *customDev.ServerApiHost, serverApiPort, customDev.CNF.CommonConfig.VKey)
	//cmd := exec.Command("/home/pgshow/Desktop/nps/cmd/npc/npc", npcCmd)
	cmd := exec.Command("./npc", npcCmd)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	err := cmd.Start()
	if err != nil {
		logs.Error("Start 发生错误", err)
		goto End
	}

	*customDev.GidNpc, err = syscall.Getpgid(cmd.Process.Pid)
	if err != nil {
		goto End
	}

	ipExpiryCheck()

	err = syscall.Kill(-*customDev.GidNpc, 15)
	if err != nil {
		logs.Error("Kill 发生错误", err)
		goto End
	} // note the minus sign

End:
	_ = cmd.Wait()
}

// 判断IP是否过期
func ipExpiryCheck() {
	var (
		accessNpsTimeOver int // 无法访问服务器超时几秒
		msgNoticed        bool
	)

	*customDev.NoticedRestart = false

	// 1.若30秒没有连上服务器会直接重拨，2.若60秒倒计时完成会进入正常重拨流程
	for i := 0; i <= ClientIpExpiry; i++ {
		if *customDev.TimeOver {
			//for {
			//	if *customDev.TunnelIsUsing == false {
			//		break
			//	}
			//
			//	logs.Info("Npc to Nps overtime but tunnel still using")
			//	customDev.DisLive() // 通知断开
			//	time.Sleep(time.Second)
			//}

			*customDev.ServerAccessFailTimes += 1
			goto End
		}
		time.Sleep(time.Second)
	}

	*customDev.ServerAccessFailTimes = 0
	customDev.DisLive() // 通知断开

	// 等待客户端数据传输完毕才能够重拨
	for {
		time.Sleep(time.Second)
		// 等待 npc 没有传输任务时，通知服务器该代理暂停服务，然后进入拨号
		if *customDev.TunnelIsUsing == false {
			goto TellServer
		}

		if !msgNoticed {
			logs.Info("Pppoe restart is waiting for tunnel transferring data")
			msgNoticed = true
		}
	}

TellServer:
	for {
		// 是否已经成功通知服务端我即将离线
		if *customDev.NoticedRestart {
			logs.Debug("NPS got my disLive request")
			goto End
		}

		accessNpsTimeOver += 1
		if accessNpsTimeOver >= 8 {
			break
		}

		logs.Debug("Waiting for Heartbeat tell server my disLive")
		time.Sleep(time.Second)
	}

End:
}

func npcRunningStatus() {
	var running bool
	for {
		time.Sleep(500 * time.Millisecond)
		if customDev.IsRunning(*customDev.GidNpc) {
			if !running {
				logs.Info("npc 正在运行中")
				running = true
			}
		} else {
			if running {
				logs.Warning("npc 已经停止运行")
				running = false
			}
		}
	}
}
