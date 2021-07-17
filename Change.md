
# CHANGE
    go customDev.FiberServer()
    go customDev.NpsTcpServer()

	go customDev.Npc2Nps()
	go customDev.Npc2Client()

	customDev.VkeyWrong()

	customDev.VersionWrong()

	customDev.RemoteNpsIP = customDev.GetIpByStr(cnf.CommonConfig.Server)

    LastConnectTime time.Time // 上次客户端成功建立连接的时间

    v.LastConnectTime = time.Now() // 记录客户端存活时间
---
    if (row.LastConnectTime === 0) {
    return '<span class="badge badge-primary">从未连接</span>'
    }
    // 在列表页显示存活或者长期离线提示
    let now = Date.parse(new Date())/1000;
    let offMinute = parseInt((now - row.LastConnectTime)/60)
    let msg
    
    if (offMinute < 10) {
    msg = offMinute +'分钟'
    } else {
    msg = '<span style=color:red>' + offMinute +'分钟</span>'
    }
    
    return '<span class="badge badge-badge" langtag="word-offline"></span> ' + msg
# TEST CODE
	content := []byte("测试1\n测试2\n")
	_ = ioutil.WriteFile("/home/pgshow/Desktop/nps/cmd/npc/111.txt", content, 0644)

# CAUTION
1.任务在1分钟内最好