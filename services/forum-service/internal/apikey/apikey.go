// Package apikey valida las API keys de los sistemas que consumen la capacidad
// publicada (ADR-014). forum-service no conoce las claves: sólo su hash
// SHA-256, configurado por variable de entorno, así una filtración de la
// configuración no expone las claves.
package apikey

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"strings"
)

type entry struct {
	consumer string
	hash     []byte
}

// Keys es el conjunto de claves válidas.
type Keys struct {
	entries []entry
}

// Parse lee la configuración: pares consumidor:hash separados por comas, con
// el hash SHA-256 de la clave en hexadecimal. Por ejemplo:
//
//	FORUM_API_KEYS=grupo4:5e884898da28047151d0e56f8dc6292773603d0d6aabbdd62a11ef721d1542d8
//
// El hash de una clave se calcula con: printf '%s' 'la-clave' | shasum -a 256
func Parse(config string) (*Keys, error) {
	k := &Keys{}
	seen := map[string]bool{}
	for _, item := range strings.Split(config, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		consumer, hexHash, ok := strings.Cut(item, ":")
		consumer = strings.TrimSpace(consumer)
		hash, err := hex.DecodeString(strings.TrimSpace(hexHash))
		if !ok || consumer == "" || err != nil || len(hash) != sha256.Size {
			return nil, fmt.Errorf("FORUM_API_KEYS: %q no tiene el formato consumidor:sha256-en-hex", item)
		}
		if seen[consumer] {
			return nil, fmt.Errorf("FORUM_API_KEYS: el consumidor %q está repetido", consumer)
		}
		seen[consumer] = true
		k.entries = append(k.entries, entry{consumer: consumer, hash: hash})
	}
	return k, nil
}

// Len es la cantidad de claves configuradas.
func (k *Keys) Len() int { return len(k.entries) }

// Lookup devuelve el consumidor dueño de la clave. Compara el hash de la clave
// recibida con todos los configurados en tiempo constante.
func (k *Keys) Lookup(key string) (consumer string, ok bool) {
	if key == "" {
		return "", false
	}
	sum := sha256.Sum256([]byte(key))
	for _, e := range k.entries {
		if subtle.ConstantTimeCompare(sum[:], e.hash) == 1 {
			consumer, ok = e.consumer, true
		}
	}
	return consumer, ok
}
