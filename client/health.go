package client

import (
	"container/heap"
	"net"
	"net/http"
	"strings"
	"time"

	"ehang.io/nps/lib/conn"
	"ehang.io/nps/lib/file"
	"ehang.io/nps/lib/sheap"
	"github.com/astaxie/beego/logs"
	"github.com/pkg/errors"
)

var isStart bool
var serverConn *conn.Conn

type healthCheckHolder struct {
	health        *file.Health
	nextCheckTime time.Time
}

func (it *healthCheckHolder) Weight() int64 {
	return it.nextCheckTime.UnixNano()
}

func heathCheck(healths []*file.Health, c *conn.Conn) bool {
	serverConn = c
	if isStart {
		for _, v := range healths {
			v.HealthMap = make(map[string]int)
		}
		return true
	}
	isStart = true
	h := &sheap.Heap{}
	for _, v := range healths {
		if v.HealthMaxFail > 0 && v.HealthCheckTimeout > 0 && v.HealthCheckInterval > 0 {
			heap.Push(h, &healthCheckHolder{health: v, nextCheckTime: time.Now().Add(time.Duration(v.HealthCheckInterval) * time.Second)})
			v.HealthMap = make(map[string]int)
		}
	}
	go session(h)
	return true
}

func session(h *sheap.Heap) {
	for {
		if h.Len() == 0 {
			logs.Error("health check error")
			break
		}
		v := heap.Pop(h).(*healthCheckHolder)
		rs := v.nextCheckTime.UnixNano() - time.Now().UnixNano()
		if rs > 0 {
			time.Sleep(time.Duration(rs) * time.Nanosecond)
		}

		v.nextCheckTime = time.Now().Add(time.Duration(v.health.HealthCheckInterval) * time.Second)
		//check
		go check(v.health)
		//reset time
		heap.Push(h, v)
	}
}

// work when just one port and many target
func check(t *file.Health) {
	arr := strings.Split(t.HealthCheckTarget, ",")
	var err error
	var rs *http.Response
	for _, v := range arr {
		if t.HealthCheckType == "tcp" {
			var c net.Conn
			c, err = net.DialTimeout("tcp", v, time.Duration(t.HealthCheckTimeout)*time.Second)
			if err == nil {
				c.Close()
			}
		} else {
			client := &http.Client{}
			client.Timeout = time.Duration(t.HealthCheckTimeout) * time.Second
			rs, err = client.Get("http://" + v + t.HttpHealthUrl)
			if err == nil && rs.StatusCode != 200 {
				err = errors.New("status code is not match")
			}
		}
		t.Lock()
		if err != nil {
			t.HealthMap[v] += 1
		} else if t.HealthMap[v] >= t.HealthMaxFail {
			//send recovery add
			serverConn.SendHealthInfo(v, "1")
			t.HealthMap[v] = 0
		}

		if t.HealthMap[v] > 0 && t.HealthMap[v]%t.HealthMaxFail == 0 {
			//send fail remove
			serverConn.SendHealthInfo(v, "0")
		}
		t.Unlock()
	}
}
