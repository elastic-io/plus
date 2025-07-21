package utils

import (
	"encoding/json"
	"io"
	"regexp"
	"strings"
)

// 验证仓库名称
func IsValidRepoName(name string) bool {
	// 允许字母、数字、连字符、下划线和斜杠
	// 斜杠用于表示层级结构，如 oe-release/x86_64 或 oe-release/x86_64/python
	matched, _ := regexp.MatchString(`^[a-zA-Z0-9_/-]+$`, name)

	// 基本长度检查
	if !matched || len(name) == 0 || len(name) > 256 {
		return false
	}

	// 不能以斜杠开头或结尾
	if strings.HasPrefix(name, "/") || strings.HasSuffix(name, "/") {
		return false
	}

	// 不能有连续的斜杠
	if strings.Contains(name, "//") {
		return false
	}

	// 验证每个路径段
	segments := strings.Split(name, "/")
	for _, segment := range segments {
		// 每个段不能为空，且只能包含字母、数字、连字符和下划线
		if len(segment) == 0 || len(segment) > 50 {
			return false
		}
		segmentMatched, _ := regexp.MatchString(`^[a-zA-Z0-9_-]+$`, segment)
		if !segmentMatched {
			return false
		}
	}

	return true
}

func WriteTo(m json.Marshaler, w io.Writer) (int64, error) {
	b, err := m.MarshalJSON()
	if err != nil {
		return -1, err
	}
	n, err := w.Write(b)
	if err != nil {
		return int64(n), err
	}
	return int64(n), nil
}
