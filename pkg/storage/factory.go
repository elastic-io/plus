package storage

import (
	"fmt"
	"log"
)

type StorageType string

const (
	Local StorageType = "local"
	S3    StorageType = "s3"
)

type storageFn func(string) (Storage, error)

type storageCtx struct {
	labels []string
	fn     storageFn
}

var factory = make(map[StorageType]storageCtx)

func Register(st StorageType, fn storageFn, labels ...string) {
	if _, ok := factory[st]; ok {
		return
	}
	factory[st] = storageCtx{
		fn:     fn,
		labels: labels,
	}
	log.Println(factory)
}

func Create(storeType StorageType, path string) (Storage, error) {
	if fn, ok := factory[storeType]; ok {
		return fn.fn(path)
	}
	return nil, fmt.Errorf("unsupported storage type: %s", storeType)
}

func CreateByLable(path string, label string) (Storage, error) {
	for _, fn := range factory {
		for _, l := range fn.labels {
			if l == label {
				return fn.fn(path)
			}
		}
	}
	log.Println("label and storage not fount: ", label, Local, factory)
	if fn, ok := factory[Local]; ok {
		return fn.fn(path)
	}
	return nil, fmt.Errorf("label and storage not fount: %s, %s", label, Local)
}