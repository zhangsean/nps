package customDev

import (
	"ehang.io/nps/lib/file"
	"ehang.io/nps/server"
	"fmt"
	"github.com/astaxie/beego"
	"github.com/gofiber/fiber/v2"
	"net/url"
	"strconv"
	"time"
)

func FiberServer() {
	time.Sleep(2 * time.Second)
	fiberPort, _ := beego.AppConfig.Int("fiber_web_port")
	if fiberPort <= 0 || CheckPort(fiberPort) == 0 {
		panic("fiber web port is unavailable, please check the 'fiber_web_port' in nps.conf")
	}

	go cleanExpired()

	app := fiber.New()

	setupRoutes(app)

	_ = app.Listen(fmt.Sprintf(":%d", fiberPort))
}

// Set Routes
func setupRoutes(app *fiber.App) {
	// set handler for index page
	app.Get("/api/heartbeat", heartbeat)
	app.Get("/api/rate", rate)
	//app.Get("/api/freePort", getFreePort)
	app.Get("/api/randHttpProxy/:amount?", randHttpProxy)
}

// 连通性检查
func heartbeat(c *fiber.Ctx) (err error) {
	ip := c.IP()
	renewFreshIp(ip)

	// 通过 head 来反馈数据
	//c.Append("IP", ip)
	vkey := c.Query("vkey")
	c.Append("Alive", findClientByVkey(vkey))
	return
}

// 返回一个空闲可用端口, 注意防火墙开启端口
func getFreePort(c *fiber.Ctx) (err error) {
	availablePort := strconv.Itoa(FindFreePort())

	c.Append("Port", availablePort)
	return
}

// RandHttpProxy 返回代理
func randHttpProxy(c *fiber.Ctx) error {
	result := getProxy(c)

	return c.JSON(result)
}

func getProxy(c *fiber.Ctx) (result map[string]interface{}) {
	needAmount, _ := strconv.Atoi(c.Params("amount")) // 客户端需要代理数量

	// 通过 nps 服务端内置的列队来获取代理开放的端口, 种类 httpProxy
	//list, cnt := server.GetClientList(0, 100, "", "", "", 0)  // 客户端列表
	listTmp, _ := server.GetTunnel(0, 100, "httpProxy", 0, "") // 隧道列表

	// 丢弃离线和不活跃的代理
	var aliveList []*file.Tunnel
	for _, item := range listTmp {
		// 离线
		if !item.Client.IsConnect {
			continue
		}

		// 不活跃
		if !isFresh(item.Client.Addr) {
			continue
		}

		aliveList = append(aliveList, item)
	}

	if len(aliveList) <= 0 {
		// 没有任何可用代理
		return nil
	}

	if needAmount <= 0 {
		needAmount = 1
	}

	var (
		chooseList []*file.Tunnel
		m          = map[string]interface{}{"proxies": []Proxy{}}
		p          []*Proxy
	)

	// 随机获取N个代理
	chooseList = RandChooseByNums(aliveList, needAmount)

	u, _ := url.Parse(c.BaseURL()) // 服务器地址

	// 合成 json, 代理是私人代理, 所以需要提供帐号密码授权
	for _, item := range chooseList {
		tmp := Proxy{fmt.Sprintf("http://%s:%d", u.Hostname(), item.Port), item.Client.Cnf.U, item.Client.Cnf.P}
		p = append(p, &tmp)
	}

	m["proxies"] = p

	return m
}

// 代理通道是否正在传输数据，如果正在使用告诉客户端暂时不要换IP
func rate(c *fiber.Ctx) (err error) {
	ip := c.IP()

	setIpExpired(ip) // 此代理准备换IP，不再给api接口调用

	list, num := server.GetClientList(0, 10000, "", "", "", 0)

	if num <= 0 {
		return
	}

	// 从客户端列表里面找到对应客户端ID
	for _, item := range list {
		if item.Addr == ip {
			c.Append("Rate", fmt.Sprintf("%d", item.Rate.NowRate))
		}
	}
	return
}
