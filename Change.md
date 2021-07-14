
# CHANGE
    go customDev.FiberServer()
    go customDev.NpsTcpServer()

	go customDev.Npc2Nps()
	go customDev.Npc2Client()

	customDev.VkeyWrong()

	customDev.VersionWrong()

	customDev.RemoteNpsIP = customDev.GetIpByStr(cnf.CommonConfig.Server)


# TEST CODE
	content := []byte("测试1\n测试2\n")
	_ = ioutil.WriteFile("/home/pgshow/Desktop/nps/cmd/npc/111.txt", content, 0644)

# CAUTION
1.任务在1分钟内最好