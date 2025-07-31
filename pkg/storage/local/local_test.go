package local

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"plus/pkg/storage"
	"strings"
	"testing"
)

func setupTestDir(t *testing.T) (string, func()) {
	// 创建临时测试目录
	tempDir, err := os.MkdirTemp("", "localstorage-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}

	// 返回清理函数
	cleanup := func() {
		os.RemoveAll(tempDir)
	}

	return tempDir, cleanup
}

func TestStore(t *testing.T) {
	tempDir, cleanup := setupTestDir(t)
	defer cleanup()

	localStorage := NewLocalStorage(tempDir)
	ctx := context.Background()

	// 测试存储文件
	content := []byte("test content")
	err := localStorage.Store(ctx, "test.txt", bytes.NewReader(content))
	if err != nil {
		t.Fatalf("Failed to store file: %v", err)
	}

	// 验证文件是否正确存储
	storedContent, err := os.ReadFile(filepath.Join(tempDir, "test.txt"))
	if err != nil {
		t.Fatalf("Failed to read stored file: %v", err)
	}

	if !bytes.Equal(storedContent, content) {
		t.Errorf("Stored content doesn't match: got %s, want %s", storedContent, content)
	}

	// 测试存储到子目录
	err = localStorage.Store(ctx, "subdir/test.txt", bytes.NewReader(content))
	if err != nil {
		t.Fatalf("Failed to store file in subdirectory: %v", err)
	}

	// 验证子目录文件是否正确存储
	storedContent, err = os.ReadFile(filepath.Join(tempDir, "subdir/test.txt"))
	if err != nil {
		t.Fatalf("Failed to read stored file from subdirectory: %v", err)
	}

	if !bytes.Equal(storedContent, content) {
		t.Errorf("Stored content in subdirectory doesn't match: got %s, want %s", storedContent, content)
	}
}

func TestGet(t *testing.T) {
	tempDir, cleanup := setupTestDir(t)
	defer cleanup()

	localStorage := NewLocalStorage(tempDir)
	ctx := context.Background()

	// 创建测试文件
	testContent := []byte("test content")
	testPath := "test.txt"
	fullPath := filepath.Join(tempDir, testPath)
	
	// 确保目录存在
	err := os.MkdirAll(filepath.Dir(fullPath), 0755)
	if err != nil {
		t.Fatalf("Failed to create directory: %v", err)
	}
	
	err = os.WriteFile(fullPath, testContent, 0644)
	if err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// 测试获取文件
	reader, err := localStorage.Get(ctx, testPath)
	if err != nil {
		t.Fatalf("Failed to get file: %v", err)
	}
	defer reader.Close()

	content, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("Failed to read file content: %v", err)
	}

	if !bytes.Equal(content, testContent) {
		t.Errorf("Retrieved content doesn't match: got %s, want %s", content, testContent)
	}

	// 测试获取不存在的文件
	_, err = localStorage.Get(ctx, "nonexistent.txt")
	if !os.IsNotExist(err) {
		t.Errorf("Expected os.IsNotExist error for nonexistent file, got: %v", err)
	}
}

func TestDelete(t *testing.T) {
	tempDir, cleanup := setupTestDir(t)
	defer cleanup()

	localStorage := NewLocalStorage(tempDir)
	ctx := context.Background()

	// 创建测试文件
	testPath := "test.txt"
	fullPath := filepath.Join(tempDir, testPath)
	
	err := os.WriteFile(fullPath, []byte("test content"), 0644)
	if err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// 测试删除文件
	err = localStorage.Delete(ctx, testPath)
	if err != nil {
		t.Fatalf("Failed to delete file: %v", err)
	}

	// 验证文件是否已删除
	_, err = os.Stat(fullPath)
	if !os.IsNotExist(err) {
		t.Errorf("File should not exist after deletion")
	}

	// 测试删除目录
	testDir := "testdir"
	fullDirPath := filepath.Join(tempDir, testDir)
	
	err = os.MkdirAll(fullDirPath, 0755)
	if err != nil {
		t.Fatalf("Failed to create test directory: %v", err)
	}
	
	err = os.WriteFile(filepath.Join(fullDirPath, "file.txt"), []byte("test"), 0644)
	if err != nil {
		t.Fatalf("Failed to create test file in directory: %v", err)
	}

	err = localStorage.Delete(ctx, testDir)
	if err != nil {
		t.Fatalf("Failed to delete directory: %v", err)
	}

	// 验证目录是否已删除
	_, err = os.Stat(fullDirPath)
	if !os.IsNotExist(err) {
		t.Errorf("Directory should not exist after deletion")
	}
}

func TestListWithOptions(t *testing.T) {
	tempDir, cleanup := setupTestDir(t)
	defer cleanup()

	localStorage := NewLocalStorage(tempDir)
	ctx := context.Background()

	// 创建测试目录结构
	dirs := []string{
		"dir1",
		"dir1/subdir1",
		"dir1/subdir2",
		"dir2",
	}
	
	for _, dir := range dirs {
		err := os.MkdirAll(filepath.Join(tempDir, dir), 0755)
		if err != nil {
			t.Fatalf("Failed to create directory %s: %v", dir, err)
		}
	}

	// 创建测试文件
	files := map[string]string{
		"file1.txt":             "content1",
		"file2.md":              "content2",
		"dir1/file3.txt":        "content3",
		"dir1/file4.md":         "content4",
		"dir1/subdir1/file5.txt": "content5",
		"dir2/file6.txt":        "content6",
	}
	
	for path, content := range files {
		err := os.WriteFile(filepath.Join(tempDir, path), []byte(content), 0644)
		if err != nil {
			t.Fatalf("Failed to create file %s: %v", path, err)
		}
	}

	// 创建仓库目录结构
	repoDir := filepath.Join(tempDir, "repo")
	err := os.MkdirAll(filepath.Join(repoDir, "repodata"), 0755)
	if err != nil {
		t.Fatalf("Failed to create repo directory: %v", err)
	}

	// 测试列出根目录
	fileInfos, err := localStorage.ListWithOptions(ctx, "", storage.ListOptions{IncludeDirs: true})
	if err != nil {
		t.Fatalf("Failed to list files: %v", err)
	}

	// 验证根目录文件数量（2个文件 + 3个目录）
	expectedCount := 5
	if len(fileInfos) != expectedCount {
		t.Errorf("Expected %d files/dirs, got %d", expectedCount, len(fileInfos))
	}

	// 测试深度限制
	fileInfos, err = localStorage.ListWithOptions(ctx, "", storage.ListOptions{
		IncludeDirs: true,
		MaxDepth:    0,
	})
	if err != nil {
		t.Fatalf("Failed to list files with depth limit: %v", err)
	}

	// 验证深度限制（只有根目录的文件和目录）
	expectedCount = 5
	if len(fileInfos) != expectedCount {
		t.Errorf("Expected %d files/dirs with depth 0, got %d", expectedCount, len(fileInfos))
	}

	// 测试扩展名过滤
	fileInfos, err = localStorage.ListWithOptions(ctx, "", storage.ListOptions{
		Extensions: []string{".txt"},
	})
	if err != nil {
		t.Fatalf("Failed to list files with extension filter: %v", err)
	}

	// 验证只返回 .txt 文件
	for _, info := range fileInfos {
		if !info.IsDir && !strings.HasSuffix(strings.ToLower(info.Name), ".txt") {
			t.Errorf("Expected only .txt files, got %s", info.Name)
		}
	}

	// 测试不包含目录
	fileInfos, err = localStorage.ListWithOptions(ctx, "", storage.ListOptions{
		IncludeDirs: false,
	})
	if err != nil {
		t.Fatalf("Failed to list files without directories: %v", err)
	}

	// 验证没有目录
	for _, info := range fileInfos {
		if info.IsDir {
			t.Errorf("Expected no directories, got directory: %s", info.Name)
		}
	}

	// 测试仓库检测
	fileInfos, err = localStorage.ListWithOptions(ctx, "", storage.ListOptions{
		IncludeDirs: true,
	})
	if err != nil {
		t.Fatalf("Failed to list files for repo detection: %v", err)
	}

	// 验证仓库标记
	foundRepo := false
	for _, info := range fileInfos {
		if info.Name == "repo" && info.IsRepo {
			foundRepo = true
			break
		}
	}
	if !foundRepo {
		t.Errorf("Failed to detect repository directory")
	}
}

func TestExists(t *testing.T) {
	tempDir, cleanup := setupTestDir(t)
	defer cleanup()

	localStorage := NewLocalStorage(tempDir)
	ctx := context.Background()

	// 创建测试文件
	testPath := "test.txt"
	fullPath := filepath.Join(tempDir, testPath)
	
	err := os.WriteFile(fullPath, []byte("test content"), 0644)
	if err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// 测试文件存在
	exists, err := localStorage.Exists(ctx, testPath)
	if err != nil {
		t.Fatalf("Failed to check if file exists: %v", err)
	}
	if !exists {
		t.Errorf("File should exist")
	}

	// 测试文件不存在
	exists, err = localStorage.Exists(ctx, "nonexistent.txt")
	if err != nil {
		t.Fatalf("Failed to check if file exists: %v", err)
	}
	if exists {
		t.Errorf("File should not exist")
	}
}

func TestCreateDir(t *testing.T) {
	tempDir, cleanup := setupTestDir(t)
	defer cleanup()

	localStorage := NewLocalStorage(tempDir)
	ctx := context.Background()

	// 测试创建目录
	testDir := "newdir/subdir"
	err := localStorage.CreateDir(ctx, testDir)
	if err != nil {
		t.Fatalf("Failed to create directory: %v", err)
	}

	// 验证目录是否已创建
	fullPath := filepath.Join(tempDir, testDir)
	info, err := os.Stat(fullPath)
	if err != nil {
		t.Fatalf("Failed to stat directory: %v", err)
	}
	if !info.IsDir() {
		t.Errorf("Expected a directory, got a file")
	}
}

func TestGetPath(t *testing.T) {
	tempDir, cleanup := setupTestDir(t)
	defer cleanup()

	localStorage := NewLocalStorage(tempDir)

	// 测试获取路径
	testPath := "test/path"
	expected := filepath.Join(tempDir, testPath)
	result := localStorage.GetPath(testPath)

	if result != expected {
		t.Errorf("GetPath returned incorrect path: got %s, want %s", result, expected)
	}
}

func TestIsRepoDirectory(t *testing.T) {
	tempDir, cleanup := setupTestDir(t)
	defer cleanup()

	localStorage := NewLocalStorage(tempDir).(*LocalStorage)

	// 创建一个普通目录
	normalDir := filepath.Join(tempDir, "normal")
	err := os.MkdirAll(normalDir, 0755)
	if err != nil {
		t.Fatalf("Failed to create normal directory: %v", err)
	}

	// 创建一个带有 repodata 的目录
	repoDir1 := filepath.Join(tempDir, "repo1")
	repodataDir1 := filepath.Join(repoDir1, "repodata")
	err = os.MkdirAll(repodataDir1, 0755)
	if err != nil {
		t.Fatalf("Failed to create repo directory: %v", err)
	}

	// 创建一个带有 Packages 和 RPM 文件的目录
	repoDir2 := filepath.Join(tempDir, "repo2")
	packagesDir2 := filepath.Join(repoDir2, "Packages")
	err = os.MkdirAll(packagesDir2, 0755)
	if err != nil {
		t.Fatalf("Failed to create packages directory: %v", err)
	}
	err = os.WriteFile(filepath.Join(packagesDir2, "test.rpm"), []byte("rpm content"), 0644)
	if err != nil {
		t.Fatalf("Failed to create RPM file: %v", err)
	}

	// 测试普通目录
	if localStorage.isRepoDirectory(normalDir) {
		t.Errorf("Normal directory should not be detected as repo")
	}

	// 测试带有 repodata 的目录
	if !localStorage.isRepoDirectory(repoDir1) {
		t.Errorf("Directory with repodata should be detected as repo")
	}

	// 测试带有 Packages 和 RPM 文件的目录
	if !localStorage.isRepoDirectory(repoDir2) {
		t.Errorf("Directory with Packages and RPM files should be detected as repo")
	}
}

func TestFileInfoModTime(t *testing.T) {
	tempDir, cleanup := setupTestDir(t)
	defer cleanup()

	localStorage := NewLocalStorage(tempDir)
	ctx := context.Background()

	// 创建测试文件
	testPath := "test.txt"
	fullPath := filepath.Join(tempDir, testPath)
	
	err := os.WriteFile(fullPath, []byte("test content"), 0644)
	if err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// 获取文件信息
	fileInfo, err := os.Stat(fullPath)
	if err != nil {
		t.Fatalf("Failed to get file info: %v", err)
	}
	expectedModTime := fileInfo.ModTime()

	// 列出文件并检查修改时间
	files, err := localStorage.ListWithOptions(ctx, "", storage.ListOptions{})
	if err != nil {
		t.Fatalf("Failed to list files: %v", err)
	}

	var found bool
	for _, file := range files {
		if file.Name == testPath {
			found = true
			// 检查修改时间是否正确
			if !file.ModTime.Equal(expectedModTime) {
				t.Errorf("ModTime doesn't match: got %v, want %v", file.ModTime, expectedModTime)
			}
			break
		}
	}

	if !found {
		t.Errorf("Test file not found in listing")
	}
}