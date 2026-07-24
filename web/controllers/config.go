package controllers

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"net/http"
	"path/filepath"
	"strings"
	"sync"

	"ehang.io/nps/lib/common"
	"ehang.io/nps/lib/npsconfig"
	"ehang.io/nps/server"
	"github.com/astaxie/beego"
)

const configCSRFSessionKey = "nps_config_csrf"

var configSaveMu sync.Mutex

type ConfigController struct {
	BaseController
}

type configSaveResponse struct {
	Status          int      `json:"status"`
	Message         string   `json:"msg"`
	Changed         []string `json:"changed,omitempty"`
	HotApplied      []string `json:"hot_applied,omitempty"`
	RestartRequired []string `json:"restart_required,omitempty"`
	Deferred        []string `json:"deferred,omitempty"`
	CSRFToken       string   `json:"csrf_token,omitempty"`
}

func (s *ConfigController) Index() {
	s.requireConfigAdmin(false)
	s.Redirect(beego.AppConfig.String("web_base_url")+"/global/index", http.StatusFound)
}

func (s *BaseController) loadRuntimeConfigData() error {
	path := npsConfigurationPath()
	document, err := npsconfig.Load(path)
	if err != nil {
		return err
	}
	token := newConfigCSRFToken()
	s.SetSession(configCSRFSessionKey, token)
	s.Data["configPath"] = path
	s.Data["configContent"] = document.Content
	s.Data["csrfToken"] = token
	s.Data["hotConfigKeys"] = strings.Join(server.HotConfigKeys(), "\n")
	s.Data["restartConfigKeys"] = strings.Join(server.RestartConfigKeys(), "\n")
	return nil
}

func (s *ConfigController) Save() {
	s.requireConfigAdmin(true)
	s.Ctx.Output.Header("Cache-Control", "no-store")
	if s.Ctx.Request.Method != http.MethodPost {
		s.writeConfigJSON(http.StatusMethodNotAllowed, configSaveResponse{Status: 0, Message: "method not allowed"})
		return
	}
	if !s.validConfigCSRFToken(s.GetString("csrf_token")) {
		s.writeConfigJSON(http.StatusForbidden, configSaveResponse{Status: 0, Message: "配置页面已过期，请刷新后重试"})
		return
	}

	configSaveMu.Lock()
	defer configSaveMu.Unlock()
	path := npsConfigurationPath()
	before, err := npsconfig.Load(path)
	if err != nil {
		s.writeConfigJSON(http.StatusInternalServerError, configSaveResponse{Status: 0, Message: "读取 nps.conf 失败：" + err.Error()})
		return
	}
	after, err := npsconfig.Parse(s.GetString("content"))
	if err != nil {
		s.writeConfigJSON(http.StatusBadRequest, configSaveResponse{Status: 0, Message: "配置校验失败：" + err.Error()})
		return
	}
	changed := npsconfig.ChangedKeys(before, after)
	plan, err := server.PrepareConfigApply(after.Values, changed)
	if err != nil {
		s.writeConfigJSON(http.StatusBadRequest, configSaveResponse{Status: 0, Message: "配置校验失败：" + err.Error()})
		return
	}
	if err := npsconfig.Save(path, after.Content); err != nil {
		s.writeConfigJSON(http.StatusInternalServerError, configSaveResponse{Status: 0, Message: "保存 nps.conf 失败：" + err.Error()})
		return
	}
	if err := plan.Apply(); err != nil {
		_ = npsconfig.Save(path, before.Content)
		s.writeConfigJSON(http.StatusInternalServerError, configSaveResponse{Status: 0, Message: "热加载失败，配置文件已回滚：" + err.Error()})
		return
	}

	newToken := newConfigCSRFToken()
	s.SetSession(configCSRFSessionKey, newToken)
	message := "配置已保存"
	if len(changed) == 0 {
		message = "配置内容未发生变化"
	} else if len(plan.RestartRequired) == 0 && len(plan.Deferred) == 0 {
		message = "配置已保存并热加载，现有监听和连接未中断"
	} else {
		message = "可热更新配置已立即生效；其余配置已保存并标记状态"
	}
	s.writeConfigJSON(http.StatusOK, configSaveResponse{
		Status:          1,
		Message:         message,
		Changed:         changed,
		HotApplied:      plan.HotApplied,
		RestartRequired: plan.RestartRequired,
		Deferred:        plan.Deferred,
		CSRFToken:       newToken,
	})
}

func (s *BaseController) requireConfigAdmin(jsonResponse bool) {
	admin, ok := s.GetSession("isAdmin").(bool)
	if ok && admin {
		return
	}
	if jsonResponse {
		s.writeConfigJSON(http.StatusForbidden, configSaveResponse{Status: 0, Message: "仅管理员可以修改运行配置"})
		return
	}
	s.Ctx.Output.SetStatus(http.StatusForbidden)
	s.Ctx.WriteString("forbidden")
	s.StopRun()
}

func (s *ConfigController) validConfigCSRFToken(provided string) bool {
	stored, ok := s.GetSession(configCSRFSessionKey).(string)
	if !ok || stored == "" || len(stored) != len(provided) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(stored), []byte(provided)) == 1
}

func (s *BaseController) writeConfigJSON(status int, response configSaveResponse) {
	s.Ctx.Output.SetStatus(status)
	s.Data["json"] = response
	s.ServeJSON()
	s.StopRun()
}

func npsConfigurationPath() string {
	path := filepath.Join(common.GetRunPath(), "conf", "nps.conf")
	if absolutePath, err := filepath.Abs(path); err == nil {
		return absolutePath
	}
	return path
}

func newConfigCSRFToken() string {
	buffer := make([]byte, 24)
	if _, err := rand.Read(buffer); err != nil {
		return ""
	}
	return hex.EncodeToString(buffer)
}
