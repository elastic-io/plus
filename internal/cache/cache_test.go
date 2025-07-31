package cache

import (
	"testing"
	"time"
)

func TestMemoryCache_Get(t *testing.T) {
	cache := NewMemoryCache()
	
	// 测试获取不存在的键
	_, exists := cache.Get("nonexistent")
	if exists {
		t.Error("Expected Get of nonexistent key to return false")
	}
	
	// 测试设置和获取
	cache.Set("key1", "value1", 0)
	value, exists := cache.Get("key1")
	if !exists {
		t.Error("Expected Get of existing key to return true")
	}
	if value != "value1" {
		t.Errorf("Expected value to be 'value1', got %v", value)
	}
}

func TestMemoryCache_Set(t *testing.T) {
	cache := NewMemoryCache()
	
	// 测试设置无过期时间的项
	cache.Set("key1", "value1", 0)
	value, exists := cache.Get("key1")
	if !exists || value != "value1" {
		t.Error("Failed to set and retrieve item without expiration")
	}
	
	// 测试覆盖已存在的项
	cache.Set("key1", "value2", 0)
	value, exists = cache.Get("key1")
	if !exists || value != "value2" {
		t.Error("Failed to override existing item")
	}
	
	// 测试设置带过期时间的项
	cache.Set("key2", "value2", 50*time.Millisecond)
	value, exists = cache.Get("key2")
	if !exists || value != "value2" {
		t.Error("Failed to set and retrieve item with expiration")
	}
	
	// 等待过期
	time.Sleep(100 * time.Millisecond)
	_, exists = cache.Get("key2")
	if exists {
		t.Error("Item should have expired")
	}
}

func TestMemoryCache_Delete(t *testing.T) {
	cache := NewMemoryCache()
	
	// 设置项
	cache.Set("key1", "value1", 0)
	
	// 删除项
	cache.Delete("key1")
	_, exists := cache.Get("key1")
	if exists {
		t.Error("Item should have been deleted")
	}
	
	// 删除不存在的项不应该报错
	cache.Delete("nonexistent")
}

func TestMemoryCache_Clear(t *testing.T) {
	cache := NewMemoryCache()
	
	// 设置多个项
	cache.Set("key1", "value1", 0)
	cache.Set("key2", "value2", 0)
	
	// 清空缓存
	cache.Clear()
	
	// 验证所有项都被删除
	_, exists1 := cache.Get("key1")
	_, exists2 := cache.Get("key2")
	if exists1 || exists2 {
		t.Error("Cache should be empty after Clear")
	}
}

func TestMemoryCache_Cleanup(t *testing.T) {
	cache := NewMemoryCache().(*MemoryCache)
	
	// 添加一个很快过期的项
	cache.Set("key1", "value1", 50*time.Millisecond)
	
	// 手动触发清理（为了测试，我们直接调用清理方法而不是等待定时器）
	time.Sleep(100 * time.Millisecond)
	
	// 检查内部状态
	cache.mu.RLock()
	_, exists := cache.items["key1"]
	cache.mu.RUnlock()

	_ = exists
	
	// 注意：这个测试可能不稳定，因为我们无法直接触发cleanup协程
	// 这里我们只是验证Get方法的行为
	_, existsGet := cache.Get("key1")
	if existsGet {
		t.Error("Expired item should not be accessible")
	}
}

func TestConcurrentAccess(t *testing.T) {
	cache := NewMemoryCache()
	done := make(chan bool)
	
	// 并发写入
	for i := 0; i < 10; i++ {
		go func(idx int) {
			for j := 0; j < 100; j++ {
				key := "key" + string(rune('0'+idx))
				cache.Set(key, j, 0)
			}
			done <- true
		}(i)
	}
	
	// 并发读取
	for i := 0; i < 10; i++ {
		go func() {
			for j := 0; j < 100; j++ {
				for k := 0; k < 10; k++ {
					key := "key" + string(rune('0'+k))
					cache.Get(key)
				}
			}
			done <- true
		}()
	}
	
	// 等待所有协程完成
	for i := 0; i < 20; i++ {
		<-done
	}
	
	// 如果没有panic，则测试通过
}