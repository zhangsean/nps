package customDev

import (
	"regexp"
)

// GetIpByStr 从字符串里提取IP
func GetIpByStr(str string) (ip string) {
	re := regexp.MustCompile(`(((2(5[0-5]|[0-4]\d))|[0-1]?\d{1,2})(\.((2(5[0-5]|[0-4]\d))|[0-1]?\d{1,2})){3})`)
	match := re.FindStringSubmatch(str)
	if match != nil {
		return match[1]
	}
	return
}
