package metrics

import (
	"sync/atomic"
	"time"
)

type Metrics struct {
	RequestCount   int64
	UploadCount    int64
	DownloadCount  int64
	ErrorCount     int64
	ResponseTime   int64
	ActiveRequests int64
}

var GlobalMetrics = &Metrics{}

func IncrementRequests() {
	atomic.AddInt64(&GlobalMetrics.RequestCount, 1)
}

func IncrementUploads() {
	atomic.AddInt64(&GlobalMetrics.UploadCount, 1)
}

func IncrementDownloads() {
	atomic.AddInt64(&GlobalMetrics.DownloadCount, 1)
}

func IncrementErrors() {
	atomic.AddInt64(&GlobalMetrics.ErrorCount, 1)
}

func RecordResponseTime(duration time.Duration) {
	atomic.StoreInt64(&GlobalMetrics.ResponseTime, duration.Milliseconds())
}

func IncrementActiveRequests() {
	atomic.AddInt64(&GlobalMetrics.ActiveRequests, 1)
}

func DecrementActiveRequests() {
	atomic.AddInt64(&GlobalMetrics.ActiveRequests, -1)
}

func GetMetrics() Metrics {
	return Metrics{
		RequestCount:   atomic.LoadInt64(&GlobalMetrics.RequestCount),
		UploadCount:    atomic.LoadInt64(&GlobalMetrics.UploadCount),
		DownloadCount:  atomic.LoadInt64(&GlobalMetrics.DownloadCount),
		ErrorCount:     atomic.LoadInt64(&GlobalMetrics.ErrorCount),
		ResponseTime:   atomic.LoadInt64(&GlobalMetrics.ResponseTime),
		ActiveRequests: atomic.LoadInt64(&GlobalMetrics.ActiveRequests),
	}
}
