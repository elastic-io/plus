package utils

import (
	"strings"
	"testing"
)

func TestIsValidRepoName(t *testing.T) {
	testCases := []struct {
		name     string
		expected bool
		desc     string
	}{
		// 有效的仓库名称
		{"oe-release", true, "简单名称"},
		{"oe-release/x86_64", true, "两级路径"},
		{"oe-release/x86_64/python", true, "三级路径"},
		{"centos7/base/updates", true, "多级路径"},
		{"ubuntu_20_04/main", true, "包含下划线"},
		{"repo-123/arch-456", true, "包含数字和连字符"},

		// 无效的仓库名称
		{"", false, "空字符串"},
		{"/oe-release", false, "以斜杠开头"},
		{"oe-release/", false, "以斜杠结尾"},
		{"oe-release//x86_64", false, "连续斜杠"},
		{"oe-release/x86_64/", false, "末尾斜杠"},
		{"/oe-release/x86_64", false, "开头斜杠"},
		{"oe-release/x86_64//python", false, "中间连续斜杠"},
		{"oe-release/x86@64", false, "包含特殊字符"},
		{"oe release/x86_64", false, "包含空格"},
		{"oe-release/" + strings.Repeat("a", 51), false, "路径段过长"},
		{strings.Repeat("a", 101), false, "总长度过长"},
	}

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			result := IsValidRepoName(tc.name)
			if result != tc.expected {
				t.Errorf("isValidRepoName(%q) = %v, expected %v (%s)",
					tc.name, result, tc.expected, tc.desc)
			}
		})
	}
}
