package server

import (
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"ehang.io/nps/lib/common"
	"ehang.io/nps/lib/file"
	"ehang.io/nps/lib/npsconfig"
	"ehang.io/nps/server/proxy"
	"ehang.io/nps/server/tool"
	"github.com/astaxie/beego"
	"github.com/astaxie/beego/logs"
)

type ConfigApplyMode string

const (
	ConfigApplyHot      ConfigApplyMode = "hot"
	ConfigApplyRestart  ConfigApplyMode = "restart"
	ConfigApplyDeferred ConfigApplyMode = "deferred"
)

var hotConfigKeys = map[string]struct{}{
	"allow_connection_num_limit":        {},
	"allow_flow_limit":                  {},
	"allow_local_proxy":                 {},
	"allow_multi_ip":                    {},
	"allow_ports":                       {},
	"allow_rate_limit":                  {},
	"allow_tunnel_num_limit":            {},
	"allow_user_change_username":        {},
	"allow_user_login":                  {},
	"allow_user_register":               {},
	"auth_crypt_key":                    {},
	"auth_key":                          {},
	"client_connect_timeout_seconds":    {},
	"disconnect_timeout":                {},
	"global_black_ip_list":              {},
	"http_add_origin_header":            {},
	"ip_location":                       {},
	"ip_location_api":                   {},
	"ip_location_cache_hours":           {},
	"ip_location_fail_cache_minutes":    {},
	"open_captcha":                      {},
	"target_connect_retry_count":        {},
	"target_connect_retry_interval_ms":  {},
	"target_connect_timeout_seconds":    {},
	"upstream_response_timeout_seconds": {},
	"web_password":                      {},
	"web_username":                      {},
}

var restartConfigKeys = map[string]struct{}{
	"appname":                             {},
	"bridge_ip":                           {},
	"bridge_port":                         {},
	"bridge_type":                         {},
	"flow_store_interval":                 {},
	"http_access_log_exclude_error_types": {},
	"http_access_log_exclude_errors":      {},
	"http_access_log_exclude_hosts":       {},
	"http_access_log_exclude_paths":       {},
	"http_access_log_fields":              {},
	"http_access_log_mask_query_keys":     {},
	"http_access_log_max_backups":         {},
	"http_access_log_max_size_mb":         {},
	"http_access_log_min_duration_ms":     {},
	"http_access_log_path":                {},
	"http_cache":                          {},
	"http_cache_length":                   {},
	"http_proxy_ip":                       {},
	"http_proxy_port":                     {},
	"https_default_cert_file":             {},
	"https_default_key_file":              {},
	"https_just_proxy":                    {},
	"https_proxy_port":                    {},
	"log_level":                           {},
	"log_path":                            {},
	"public_vkey":                         {},
	"runmode":                             {},
	"system_info_display":                 {},
	"tls_bridge_port":                     {},
	"tls_enable":                          {},
	"web_base_url":                        {},
	"web_cert_file":                       {},
	"web_host":                            {},
	"web_ip":                              {},
	"web_key_file":                        {},
	"web_open_ssl":                        {},
	"web_port":                            {},
}

var booleanHotConfigKeys = map[string]struct{}{
	"allow_connection_num_limit": {},
	"allow_flow_limit":           {},
	"allow_local_proxy":          {},
	"allow_multi_ip":             {},
	"allow_rate_limit":           {},
	"allow_tunnel_num_limit":     {},
	"allow_user_change_username": {},
	"allow_user_login":           {},
	"allow_user_register":        {},
	"http_add_origin_header":     {},
	"ip_location":                {},
	"open_captcha":               {},
}

type ConfigApplyPlan struct {
	Changed         []string
	HotApplied      []string
	RestartRequired []string
	Deferred        []string
	values          map[string]string
	disconnect      int
	clientTimeout   int
	targetTimeout   int
	retryCount      int
	retryInterval   int
	upstreamTimeout int
	globalBlackIps  []string
}

func ConfigKeyApplyMode(key string) ConfigApplyMode {
	key = strings.ToLower(strings.TrimSpace(key))
	if strings.Contains(key, "::") {
		return ConfigApplyDeferred
	}
	if _, ok := hotConfigKeys[key]; ok {
		return ConfigApplyHot
	}
	if _, ok := restartConfigKeys[key]; ok {
		return ConfigApplyRestart
	}
	return ConfigApplyDeferred
}

func HotConfigKeys() []string {
	return sortedConfigKeys(hotConfigKeys)
}

func RestartConfigKeys() []string {
	return sortedConfigKeys(restartConfigKeys)
}

func sortedConfigKeys(values map[string]struct{}) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func PrepareConfigApply(values map[string]string, changed []string) (*ConfigApplyPlan, error) {
	plan := &ConfigApplyPlan{
		Changed: append([]string(nil), changed...),
		values:  make(map[string]string, len(values)),
	}
	for key, value := range values {
		plan.values[strings.ToLower(key)] = value
	}
	if err := validateRequiredConfig(plan.values); err != nil {
		return nil, err
	}
	for _, key := range changed {
		switch ConfigKeyApplyMode(key) {
		case ConfigApplyHot:
			plan.HotApplied = append(plan.HotApplied, key)
		case ConfigApplyRestart:
			plan.RestartRequired = append(plan.RestartRequired, key)
		default:
			plan.Deferred = append(plan.Deferred, key)
		}
	}

	for key := range booleanHotConfigKeys {
		if value, exists := plan.values[key]; exists && strings.TrimSpace(value) != "" {
			if _, err := strconv.ParseBool(strings.TrimSpace(value)); err != nil {
				return nil, fmt.Errorf("%s must be true or false", key)
			}
		}
	}
	if authCryptKey := strings.TrimSpace(plan.values["auth_crypt_key"]); authCryptKey != "" && len(authCryptKey) != 16 {
		return nil, fmt.Errorf("auth_crypt_key must contain exactly 16 characters")
	}

	var err error
	if plan.disconnect, err = configInt(plan.values, "disconnect_timeout", 60, 1, 86400); err != nil {
		return nil, err
	}
	if plan.clientTimeout, err = configInt(plan.values, "client_connect_timeout_seconds", 2, 1, 3600); err != nil {
		return nil, err
	}
	if plan.targetTimeout, err = configInt(plan.values, "target_connect_timeout_seconds", 2, 1, 3600); err != nil {
		return nil, err
	}
	if plan.retryCount, err = configInt(plan.values, "target_connect_retry_count", 2, 0, 100); err != nil {
		return nil, err
	}
	if plan.retryInterval, err = configInt(plan.values, "target_connect_retry_interval_ms", 0, 0, 3600000); err != nil {
		return nil, err
	}
	if plan.upstreamTimeout, err = configInt(plan.values, "upstream_response_timeout_seconds", 0, 0, 86400); err != nil {
		return nil, err
	}
	if plan.globalBlackIps, err = proxy.ParseGlobalBlackIpList(plan.values["global_black_ip_list"]); err != nil {
		return nil, err
	}
	sort.Strings(plan.HotApplied)
	sort.Strings(plan.RestartRequired)
	sort.Strings(plan.Deferred)
	return plan, nil
}

func validateRequiredConfig(values map[string]string) error {
	for _, key := range []string{"bridge_type", "bridge_port", "web_port", "web_username", "web_password"} {
		if strings.TrimSpace(values[key]) == "" {
			return fmt.Errorf("required configuration %s cannot be empty", key)
		}
	}
	bridgeType := strings.ToLower(strings.TrimSpace(values["bridge_type"]))
	if bridgeType != "tcp" && bridgeType != "kcp" {
		return fmt.Errorf("bridge_type must be tcp or kcp")
	}
	if _, err := configInt(values, "bridge_port", 0, 1, 65535); err != nil {
		return err
	}
	if _, err := configInt(values, "web_port", 0, 0, 65535); err != nil {
		return err
	}
	return nil
}

func configInt(values map[string]string, key string, defaultValue, minimum, maximum int) (int, error) {
	raw := strings.TrimSpace(values[key])
	if raw == "" {
		return defaultValue, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < minimum || value > maximum {
		return 0, fmt.Errorf("%s must be an integer between %d and %d", key, minimum, maximum)
	}
	return value, nil
}

func (plan *ConfigApplyPlan) Apply() error {
	for _, key := range plan.HotApplied {
		if err := beego.AppConfig.Set(key, plan.values[key]); err != nil {
			return fmt.Errorf("apply %s: %w", key, err)
		}
	}
	if Bridge != nil {
		Bridge.UpdateRuntimeConfig(plan.disconnect, plan.clientTimeout, plan.targetTimeout, plan.retryCount, plan.retryInterval)
	}
	proxy.SetUpstreamResponseTimeout(time.Duration(plan.upstreamTimeout) * time.Second)
	proxy.SetGlobalBlackIpList(plan.globalBlackIps)
	tool.SetAllowPorts(plan.values["allow_ports"])
	return nil
}

func InitializeGlobalBlackIpConfig() {
	path := filepath.Join(common.GetRunPath(), "conf", "nps.conf")
	document, err := npsconfig.Load(path)
	if err != nil {
		logs.Error("load global IP blacklist configuration: %s", err)
		return
	}
	value, configured := document.Values["global_black_ip_list"]
	if !configured {
		if legacy := file.GetDb().GetGlobal(); legacy != nil {
			legacyValues := make([]string, 0, len(legacy.BlackIpList))
			for _, item := range legacy.BlackIpList {
				if strings.TrimSpace(item) != "" {
					legacyValues = append(legacyValues, strings.TrimSpace(item))
				}
			}
			if len(legacyValues) > 0 {
				parsedLegacy, parseErr := proxy.ParseGlobalBlackIpList(strings.Join(legacyValues, ","))
				if parseErr != nil {
					logs.Error("migrate global IP blacklist to nps.conf: %s", parseErr)
					return
				}
				value = strings.Join(parsedLegacy, ",")
				content := strings.TrimRight(document.Content, "\n") + "\n\n# Global IP blacklist, comma-separated exact IP addresses\nglobal_black_ip_list=" + value + "\n"
				if saveErr := npsconfig.Save(path, content); saveErr != nil {
					logs.Error("migrate global IP blacklist to nps.conf: %s", saveErr)
				} else {
					_ = beego.AppConfig.Set("global_black_ip_list", value)
					logs.Notice("migrated global IP blacklist from global.json to nps.conf")
				}
			}
		}
	}
	values, err := proxy.ParseGlobalBlackIpList(value)
	if err != nil {
		logs.Error("invalid global_black_ip_list: %s", err)
		return
	}
	proxy.SetGlobalBlackIpList(values)
}
