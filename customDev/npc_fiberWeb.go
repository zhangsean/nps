package customDev

import (
	"ehang.io/nps/lib/goroutine"
	"github.com/gofiber/fiber/v2"
)

func FiberNPC() {
	app := fiber.New()

	app.Get("/api/isUsing", isUsing)

	_ = app.Listen(":8005")
}

// tunnel 是否使用中
func isUsing(c *fiber.Ctx) (err error) {
	if goroutine.CopyConnsPool.Running() > 0 {
		c.Append("isUsing", "1")
	} else {
		c.Append("isUsing", "0")
	}

	return
}
