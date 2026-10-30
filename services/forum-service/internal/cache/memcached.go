package cache

import (
	"errors"
	"time"

	"github.com/bradfitz/gomemcache/memcache"
)

// Timeout es lo máximo que se espera a Memcached en cada operación (ADR-010).
// Si no responde, quien usa la caché lo trata como un fallo y lee de MongoDB.
const Timeout = 100 * time.Millisecond

type Memcached struct {
	client *memcache.Client
}

func NewMemcached(addr string) *Memcached {
	client := memcache.New(addr)
	client.Timeout = Timeout
	client.MaxIdleConns = 32
	return &Memcached{client: client}
}

func (m *Memcached) Name() string {
	return "memcached"
}

func (m *Memcached) Get(key string) ([]byte, error) {
	item, err := m.client.Get(key)
	if errors.Is(err, memcache.ErrCacheMiss) {
		return nil, ErrCacheMiss
	}
	if err != nil {
		return nil, err
	}
	return item.Value, nil
}

func (m *Memcached) Set(key string, val []byte, ttl time.Duration) error {
	exp := int32(ttl / time.Second)
	if ttl > 0 && exp == 0 {
		exp = 1 // en Memcached, 0 es "no vence nunca"
	}
	return m.client.Set(&memcache.Item{Key: key, Value: val, Expiration: exp})
}

func (m *Memcached) Delete(key string) error {
	err := m.client.Delete(key)
	if errors.Is(err, memcache.ErrCacheMiss) {
		return nil // ya no estaba: quedó invalidada igual
	}
	return err
}
