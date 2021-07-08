package customDev

import (
	"ehang.io/nps/server"
	"github.com/huandu/go-clone"
	"sync"
	"time"
)

var (
	freshIps = make(map[string]time.Time, 1000)
	mutex    sync.RWMutex
)

// 更新客户端访问时间
func renewFreshIp(ip string) {
	mutex.Lock()
	freshIps[ip] = time.Now()
	//print("renewed", ip)
	mutex.Unlock()
}

// 删除长时间没有访问 api 的客户端
func cleanExpired() {
	for {
		mutex.RLock()
		tmp := clone.Clone(freshIps).(map[string]time.Time)
		mutex.RUnlock()

		for ip, updateTime := range tmp {
			if time.Now().Sub(updateTime) >= time.Duration(10)*time.Second {
				mutex.Lock()
				delete(freshIps, ip)
				mutex.Unlock()
			}
		}
		time.Sleep(60 * time.Second)
	}
}

// 是否活跃客户端,6秒内有访问api
func isFresh(ip string) bool {
	mutex.RLock()
	tmp := clone.Clone(freshIps).(map[string]time.Time)
	mutex.RUnlock()

	for tmpIp, renewTime := range tmp {
		if ip == tmpIp {
			if time.Now().Sub(renewTime) <= time.Duration(6)*time.Second {
				return true
			}
			break
		}
	}
	return false
}

// 在列表里找到对应vKey的客户端
func findClientByVkey(vKey string) (exist string) {
	list, num := server.GetClientList(0, 10000, "", "", "", 0)

	if num <= 0 {
		return "0"
	}

	// 从客户端列表里面找到对应客户端ID
	for _, item := range list {
		if item.VerifyKey == vKey {
			return "1"
		}
	}
	return "0"
}
