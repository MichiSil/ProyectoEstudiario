package cache

// Test de integración contra un Memcached real. Se ejecuta sólo si está
// definida FORUM_TEST_MEMCACHED_ADDR (ver "Tests de forum-service" en el README).

import (
	"crypto/rand"
	"errors"
	"os"
	"testing"
	"time"
)

func TestIntegracionMemcached(t *testing.T) {
	addr := os.Getenv("FORUM_TEST_MEMCACHED_ADDR")
	if addr == "" {
		t.Skip("FORUM_TEST_MEMCACHED_ADDR no definida: se omite el test de integración con Memcached")
	}
	c := NewMemcached(addr)
	key := "test:" + rand.Text()
	t.Cleanup(func() { _ = c.Delete(key) })

	if _, err := c.Get(key); !errors.Is(err, ErrCacheMiss) {
		t.Fatalf("Get de una clave que no está = %v, se esperaba ErrCacheMiss", err)
	}
	if err := c.Set(key, []byte("hola"), time.Minute); err != nil {
		t.Fatal(err)
	}
	if got, err := c.Get(key); err != nil || string(got) != "hola" {
		t.Fatalf("Get = %q, %v", got, err)
	}
	if err := c.Delete(key); err != nil {
		t.Fatal(err)
	}
	if err := c.Delete(key); err != nil {
		t.Fatalf("borrar una clave que ya no está no es un error: %v", err)
	}
	if _, err := c.Get(key); !errors.Is(err, ErrCacheMiss) {
		t.Fatalf("Get después de borrar = %v", err)
	}

	// Un TTL menor a un segundo no puede quedar en 0 ("no vence nunca").
	if err := c.Set(key, []byte("corto"), 500*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	time.Sleep(2100 * time.Millisecond)
	if _, err := c.Get(key); !errors.Is(err, ErrCacheMiss) {
		t.Fatalf("Get de una clave vencida = %v, se esperaba ErrCacheMiss", err)
	}
}
