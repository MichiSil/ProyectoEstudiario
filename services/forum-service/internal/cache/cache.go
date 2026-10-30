// Package cache es la caché trabajada en clase: la interfaz Cache con tres
// implementaciones, Memcached (la que corre en docker compose), local (en la
// memoria del proceso, para desarrollar sin contenedores) y nula (no guarda
// nada: para los tests y para medir "sin caché"). ADR-003, ADR-007.
//
// Origen: https://github.com/gerasalinas7/DesarrolloDeSoftware2026, carpeta
// claseCache-2026/internal/cache. Es un paquete internal de otro módulo y no
// se puede importar, así que está copiado. Cambios respecto del original:
//   - se completaron los ejercicios 2 y 3 (Get, Set y Delete de la versión
//     local y de Memcached);
//   - Memcached tiene un timeout de 100 ms por operación (ADR-010);
//   - New elige la implementación por nombre (CACHE_PROVIDER).
package cache

import (
	"errors"
	"fmt"
	"time"
)

var ErrCacheMiss = errors.New("cache miss")

type Cache interface {
	Name() string
	Get(key string) ([]byte, error)
	Set(key string, val []byte, ttl time.Duration) error
	Delete(key string) error
}

// New devuelve la caché de nombre provider: "memcached" (en addr), "local" o
// "none" (la nula).
func New(provider, addr string) (Cache, error) {
	switch provider {
	case "memcached":
		if addr == "" {
			return nil, errors.New("la caché memcached necesita MEMCACHED_ADDR")
		}
		return NewMemcached(addr), nil
	case "local":
		return NewLocal(), nil
	case "none", "":
		return NewNoop(), nil
	}
	return nil, fmt.Errorf("CACHE_PROVIDER desconocido: %q (memcached, local o none)", provider)
}
