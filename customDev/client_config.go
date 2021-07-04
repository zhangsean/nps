package customDev

import (
	"ehang.io/nps/lib/common"
	"ehang.io/nps/lib/config"
	"github.com/astaxie/beego/logs"
	"os"
)

var CNF config.Config

func ReadConfig() {
	configPath := common.GetConfigPath()

	cnf, err := config.NewConfig(configPath)
	if err != nil || cnf.CommonConfig == nil {
		logs.Error("Config file %s loading error %s", configPath, err)
		os.Exit(0)
	}
	CNF = *cnf
	logs.Info("Loading configuration file %s successfully", configPath)
}
