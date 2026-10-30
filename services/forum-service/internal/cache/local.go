package cache

import (
	"time"

	"github.com/karlseguin/ccache"
)

type Local struct {
	client *ccache.Cache
}

func NewLocal() *Local {
	return &Local{client: ccache.New(ccache.Configure())}
}

func (l *Local) Name() string {
	return "local"
}

func (l *Local) Get(key string) ([]byte, error) {
	item := l.client.Get(key)
	if item == nil || item.Expired() {
		return nil, ErrCacheMiss
	}
	return item.Value().([]byte), nil
}

func (l *Local) Set(key string, val []byte, ttl time.Duration) error {
	l.client.Set(key, val, ttl)
	return nil
}

func (l *Local) Delete(key string) error {
	l.client.Delete(key)
	return nil
}
