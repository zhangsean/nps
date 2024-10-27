package version

import (
	"fmt"
	"strconv"
	"strings"
)

const VERSION = "0.26.21"

// Compulsory minimum version, Minimum downward compatibility to this version
func GetVersion() string {
	return "0.26.0"
}

// 对比版本号，v1较大返回1，相等返回0，v1较小返回-1
func CompareVersion(v1, v2 string) int {
	sv1 := strsToSlice(v1)
	sv2 := strsToSlice(v2)
	sv1, sv2 = apeendZreo(sv1, sv2)
	for i := 0; i < len(sv1); i++ {
		i1 := strToInt64(sv1[i])
		i2 := strToInt64(sv2[i])
		if i1 > i2 {
			return 1
		} else if i1 < i2 {
			return -1
		}
	}
	// 退出循环表示版本号相同
	return 0
}

// 补全版本号
func apeendZreo(s1, s2 []string) ([]string, []string) {
	var count int
	if len(s1) > len(s2) {
		count = len(s1) - len(s2)
		for i := 0; i < count; i++ {
			s2 = append(s2, "0")
		}
	}
	if len(s1) < len(s2) {
		count = len(s2) - len(s1)
		for i := 0; i < count; i++ {
			s1 = append(s1, "0")
		}
	}

	return s1, s2
}

func strsToSlice(version string) []string {
	return strings.Split(version, ".")
}

func strToInt64(str string) int64 {
	res, err := strconv.Atoi(str)
	if err != nil {
		fmt.Println("Invalid Number string")
		return -1
	}
	return int64(res)
}
