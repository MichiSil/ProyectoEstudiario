package cache

import (
	"errors"
	"testing"
	"time"
)

func TestLocalGuardaLeeYBorra(t *testing.T) {
	c := NewLocal()
	if _, err := c.Get("k"); !errors.Is(err, ErrCacheMiss) {
		t.Fatalf("Get de una clave que no está = %v, se esperaba ErrCacheMiss", err)
	}
	if err := c.Set("k", []byte("v"), time.Minute); err != nil {
		t.Fatal(err)
	}
	if got, err := c.Get("k"); err != nil || string(got) != "v" {
		t.Fatalf("Get = %q, %v", got, err)
	}
	if err := c.Delete("k"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Get("k"); !errors.Is(err, ErrCacheMiss) {
		t.Fatalf("Get después de borrar = %v", err)
	}
}

func TestLocalVence(t *testing.T) {
	c := NewLocal()
	_ = c.Set("k", []byte("v"), 20*time.Millisecond)
	time.Sleep(40 * time.Millisecond)
	if _, err := c.Get("k"); !errors.Is(err, ErrCacheMiss) {
		t.Fatalf("Get de una clave vencida = %v, se esperaba ErrCacheMiss", err)
	}
}

func TestNulaNoGuardaNada(t *testing.T) {
	c := NewNoop()
	_ = c.Set("k", []byte("v"), time.Minute)
	if _, err := c.Get("k"); !errors.Is(err, ErrCacheMiss) {
		t.Fatalf("Get = %v, se esperaba ErrCacheMiss", err)
	}
}

func TestNewEligePorNombre(t *testing.T) {
	for provider, want := range map[string]string{"": "none", "none": "none", "local": "local", "memcached": "memcached"} {
		c, err := New(provider, "memcached:11211")
		if err != nil || c.Name() != want {
			t.Errorf("New(%q) = %v, %v; se esperaba %s", provider, c, err, want)
		}
	}
	if _, err := New("redis", ""); err == nil {
		t.Error("un proveedor desconocido tiene que dar error")
	}
	if _, err := New("memcached", ""); err == nil {
		t.Error("memcached sin dirección tiene que dar error")
	}
}
