// Package system implementa los puertos de reloj e identificadores.
package system

import (
	"crypto/rand"
	"strings"
	"time"
)

type Clock struct{}

func (Clock) Now() time.Time { return time.Now() }

// IDs genera identificadores aleatorios de 26 caracteres.
type IDs struct{}

func (IDs) NewID() string { return strings.ToLower(rand.Text()) }
