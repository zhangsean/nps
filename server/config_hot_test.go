package server

import (
	"reflect"
	"sync"
	"testing"
	"time"

	"ehang.io/nps/bridge"
	"ehang.io/nps/server/proxy"
)

func TestPrepareConfigApplyClassifiesAndValidates(t *testing.T) {
	values := requiredTestConfig()
	values["disconnect_timeout"] = "90"
	values["client_connect_timeout_seconds"] = "4"
	values["target_connect_timeout_seconds"] = "6"
	values["target_connect_retry_count"] = "3"
	values["target_connect_retry_interval_ms"] = "750"
	values["upstream_response_timeout_seconds"] = "12"
	values["allow_local_proxy"] = "true"
	values["global_black_ip_list"] = "192.0.2.10,2001:db8::10"
	plan, err := PrepareConfigApply(values, []string{"bridge_port", "allow_local_proxy", "global_black_ip_list", "new_future_key", "target_connect_timeout_seconds"})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(plan.HotApplied, []string{"allow_local_proxy", "global_black_ip_list", "target_connect_timeout_seconds"}) {
		t.Fatalf("hot keys = %#v", plan.HotApplied)
	}
	if !reflect.DeepEqual(plan.RestartRequired, []string{"bridge_port"}) || !reflect.DeepEqual(plan.Deferred, []string{"new_future_key"}) {
		t.Fatalf("restart=%#v deferred=%#v", plan.RestartRequired, plan.Deferred)
	}
}

func TestConfigApplyPlanUpdatesRuntimeBridgeAndHTTPConfig(t *testing.T) {
	previousBridge := Bridge
	previousTimeout := proxy.UpstreamResponseTimeout()
	previousBlackIps := proxy.GlobalBlackIpList()
	t.Cleanup(func() {
		Bridge = previousBridge
		proxy.SetUpstreamResponseTimeout(previousTimeout)
		proxy.SetGlobalBlackIpList(previousBlackIps)
	})
	Bridge = bridge.NewTunnel(0, "tcp", false, &sync.Map{}, 60, 2, 2, 2, 0)
	values := requiredTestConfig()
	values["disconnect_timeout"] = "90"
	values["client_connect_timeout_seconds"] = "4"
	values["target_connect_timeout_seconds"] = "6"
	values["target_connect_retry_count"] = "3"
	values["target_connect_retry_interval_ms"] = "750"
	values["upstream_response_timeout_seconds"] = "12"
	values["global_black_ip_list"] = "192.0.2.10"
	plan, err := PrepareConfigApply(values, []string{"disconnect_timeout", "client_connect_timeout_seconds", "target_connect_timeout_seconds", "target_connect_retry_count", "target_connect_retry_interval_ms", "upstream_response_timeout_seconds", "global_black_ip_list"})
	if err != nil {
		t.Fatal(err)
	}
	if err := plan.Apply(); err != nil {
		t.Fatal(err)
	}
	runtimeConfig := Bridge.RuntimeConfig()
	if runtimeConfig.DisconnectTime != 90 || runtimeConfig.ClientConnectTimeout != 4*time.Second || runtimeConfig.TargetConnectTimeout != 6*time.Second || runtimeConfig.TargetConnectRetryCount != 3 || runtimeConfig.TargetConnectRetryInterval != 750*time.Millisecond {
		t.Fatalf("bridge runtime config = %#v", runtimeConfig)
	}
	if got := proxy.UpstreamResponseTimeout(); got != 12*time.Second {
		t.Fatalf("upstream timeout = %s", got)
	}
	if !proxy.IsGlobalBlackIp("192.0.2.10:443") || proxy.IsGlobalBlackIp("192.0.2.11:443") {
		t.Fatal("global IP blacklist was not applied")
	}
}

func TestPrepareConfigApplyRejectsInvalidValues(t *testing.T) {
	values := requiredTestConfig()
	values["target_connect_retry_count"] = "-1"
	if _, err := PrepareConfigApply(values, []string{"target_connect_retry_count"}); err == nil {
		t.Fatal("negative retry count should be rejected")
	}
	values = requiredTestConfig()
	values["allow_local_proxy"] = "sometimes"
	if _, err := PrepareConfigApply(values, []string{"allow_local_proxy"}); err == nil {
		t.Fatal("invalid boolean should be rejected")
	}
	delete(values, "web_password")
	if _, err := PrepareConfigApply(values, []string{"web_password"}); err == nil {
		t.Fatal("missing web password should be rejected")
	}
	values = requiredTestConfig()
	values["global_black_ip_list"] = "192.0.2.10,not-an-ip"
	if _, err := PrepareConfigApply(values, []string{"global_black_ip_list"}); err == nil {
		t.Fatal("invalid global IP blacklist should be rejected")
	}
}

func requiredTestConfig() map[string]string {
	return map[string]string{
		"bridge_type":  "tcp",
		"bridge_port":  "8024",
		"web_port":     "8080",
		"web_username": "admin",
		"web_password": "secret",
	}
}
