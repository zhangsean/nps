package common

import "testing"

func TestPinyinMatch(t *testing.T) {
	cases := []struct {
		query string
		text  string
		want  bool
	}{
		// 原文包含（忽略大小写）
		{"张三", "张三的隧道", true},
		{"abc", "ABC公司", true},
		{"1.2", "192.168.1.2:8080", true},
		{"d盘", "D盘文件", true},
		// 全拼
		{"zhangsan", "张三", true},
		{"zhang", "张三", true},
		{"zhangs", "张三", true},
		{"kaihei", "开黑工具", true},
		{"kaihe", "开黑工具", true},
		{"zhejiang", "浙江省杭州市", true},
		{"changsha", "长沙银行", true},
		{"ZHANGSAN", "张三", true},
		// 拼音首字母
		{"zs", "张三", true},
		{"khgj", "开黑工具", true},
		{"zj", "浙江省杭州市", true},
		{"hz", "浙江省杭州市", true},
		{"fwq", "Web服务器", true},
		{"cs", "长沙银行", true},
		{"k", "开黑", true},
		// 汉字/拼音混合
		{"z三", "张三", true},
		{"开h", "开黑工具", true},
		{"k黑", "开黑工具", true},
		{"开黑gj", "开黑工具", true},
		// 多音字
		{"cq", "重庆市", true},
		{"chongqing", "重庆市", true},
		// 空值
		{"", "张三", false},
		{"zs", "", false},
		// 不匹配
		{"wangwu", "张三", false},
		{"kx", "开黑工具", false},
		{"abc", "张三", false},
		{"ss", "三", false},
	}
	for _, c := range cases {
		if got := PinyinMatch(c.query, c.text); got != c.want {
			t.Errorf("PinyinMatch(%q, %q) = %v, want %v", c.query, c.text, got, c.want)
		}
	}
}
