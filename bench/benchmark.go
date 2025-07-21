package main

import (
	"fmt"
	"net/http"
	"sync"
	"time"
)

func benchmarkDownload(url string, concurrent int, requests int) {
	var wg sync.WaitGroup
	start := time.Now()

	for i := 0; i < concurrent; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < requests/concurrent; j++ {
				resp, err := http.Get(url)
				if err == nil {
					resp.Body.Close()
				}
			}
		}()
	}

	wg.Wait()
	duration := time.Since(start)

	fmt.Printf("完成 %d 个请求，耗时: %v\n", requests, duration)
	fmt.Printf("QPS: %.2f\n", float64(requests)/duration.Seconds())
}
