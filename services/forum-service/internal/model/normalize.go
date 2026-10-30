package model

import (
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// NormalizeKey pasa un texto a la forma con la que se compara en los filtros:
// minúsculas, sin tildes y con un solo espacio entre palabras. Así "Análisis
// Matemático II" y "analisis matematico ii" son la misma categoría.
func NormalizeKey(s string) string {
	var b strings.Builder
	for _, r := range norm.NFD.String(strings.ToLower(s)) {
		if unicode.Is(unicode.Mn, r) { // marcas de tilde, diéresis, etc.
			continue
		}
		b.WriteRune(r)
	}
	return strings.Join(strings.Fields(b.String()), " ")
}
