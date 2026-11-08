package common

import (
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"

	"github.com/mozillazg/go-pinyin"
)

// pinyinToken 表示待匹配文本中的一个字符：ch 为小写原字符（用于字面匹配），
// readings 为该字符的全部读音（小写、无声调，多音字可能有多个），
// hanzi 标记是否为收录读音的汉字（只有汉字才参与首字母匹配）。
type pinyinToken struct {
	ch       rune
	readings []string
	hanzi    bool
}

var (
	pinyinTokensMu    sync.RWMutex
	pinyinTokensCache = make(map[string][]pinyinToken)
	pinyinTokensLimit = 8192
)

// PinyinMatch 判断 text 能否匹配搜索词 query，找不到时返回 false。
// 支持原文包含（忽略大小写）、全拼、拼音首字母以及汉字/拼音混合输入，
// 例如 "zhangsan"、"zs"、"z三" 均可匹配 "张三"；多音字按多种读音匹配。
// 空的 query 或 text 一律视为不匹配。
func PinyinMatch(query, text string) bool {
	if query == "" || text == "" {
		return false
	}
	if strings.Contains(strings.ToLower(text), strings.ToLower(query)) {
		return true
	}
	return pinyinQueryMatch(pinyinTokensFor(text), []rune(strings.ToLower(query)))
}

// pinyinTokensFor 返回 text 的拼音标记，结果按 text 缓存，备注等字段在列表搜索时会被反复匹配。
func pinyinTokensFor(text string) []pinyinToken {
	pinyinTokensMu.RLock()
	tokens, ok := pinyinTokensCache[text]
	pinyinTokensMu.RUnlock()
	if ok {
		return tokens
	}
	tokens = buildPinyinTokens(text)
	pinyinTokensMu.Lock()
	if len(pinyinTokensCache) >= pinyinTokensLimit {
		pinyinTokensCache = make(map[string][]pinyinToken, pinyinTokensLimit)
	}
	pinyinTokensCache[text] = tokens
	pinyinTokensMu.Unlock()
	return tokens
}

func buildPinyinTokens(text string) []pinyinToken {
	args := pinyin.NewArgs()
	args.Heteronym = true
	tokens := make([]pinyinToken, 0, utf8.RuneCountInString(text))
	for _, r := range text {
		lower := unicode.ToLower(r)
		token := pinyinToken{ch: lower}
		if unicode.Is(unicode.Han, r) {
			token.hanzi = true
			for _, py := range pinyin.SinglePinyin(r, args) {
				py = strings.ToLower(py)
				if py != "" && py != string(lower) {
					token.readings = append(token.readings, py)
				}
			}
		} else {
			token.readings = []string{string(lower)}
		}
		if len(token.readings) == 0 {
			// 字典未收录的汉字按原字符参与字面匹配
			token.readings = []string{string(lower)}
			token.hanzi = false
		}
		tokens = append(tokens, token)
	}
	return tokens
}

// pinyinQueryMatch 按“子串包含”语义匹配 query：从 tokens 的任意位置开始，
// 依次消耗 query，每个字符可以按原字符、完整读音或读音首字母匹配。
func pinyinQueryMatch(tokens []pinyinToken, query []rune) bool {
	if len(query) == 0 {
		return false
	}
	first := query[0]
	for start := 0; start < len(tokens); start++ {
		token := tokens[start]
		if token.ch != first && !pinyinTokenStartsWith(token, first) {
			continue
		}
		if pinyinMatchFrom(tokens, start, query) {
			return true
		}
	}
	return false
}

// firstRune 返回字符串的首个字符，空串返回 0。
func firstRune(s string) rune {
	for _, r := range s {
		return r
	}
	return 0
}

// pinyinTokenStartsWith 判断字符的任一读音是否以 r 开头（含首字母）。
func pinyinTokenStartsWith(token pinyinToken, r rune) bool {
	for _, py := range token.readings {
		if firstRune(py) == r {
			return true
		}
	}
	return false
}

func pinyinMatchFrom(tokens []pinyinToken, start int, query []rune) bool {
	// visited[i][j] 表示“已消耗 query 前 i 个字符、正要匹配 tokens[j]”的状态，
	// 走过的失败状态不再重复展开。
	visited := make([][]bool, len(query)+1)
	var walk func(qPos, tPos int) bool
	walk = func(qPos, tPos int) bool {
		if qPos == len(query) {
			return true
		}
		if tPos == len(tokens) {
			return false
		}
		if visited[qPos] == nil {
			visited[qPos] = make([]bool, len(tokens)+1)
		}
		if visited[qPos][tPos] {
			return false
		}
		visited[qPos][tPos] = true

		token := tokens[tPos]
		// 按原字符匹配
		if token.ch == query[qPos] && walk(qPos+1, tPos+1) {
			return true
		}
		for _, py := range token.readings {
			pyLen := utf8.RuneCountInString(py)
			remain := len(query) - qPos
			// 完整读音匹配
			if pyLen <= remain && string(query[qPos:qPos+pyLen]) == py && walk(qPos+pyLen, tPos+1) {
				return true
			}
			// 读音前缀匹配：剩余 query 恰好是某个读音的前缀，支持边输入边搜（如 "kaihe" 匹配 "开黑"）
			if pyLen > remain && string(query[qPos:]) == string([]rune(py)[:remain]) {
				return true
			}
			// 拼音首字母匹配（仅汉字）
			if token.hanzi && firstRune(py) == query[qPos] && walk(qPos+1, tPos+1) {
				return true
			}
		}
		return false
	}
	return walk(0, start)
}
