// Package auth resuelve en el api-gateway quién hace cada pedido (ADR-001).
//
// Los servicios internos confían en los encabezados de identidad que les llegan
// (X-User-Id, X-User-Role, X-User-Name y X-User-Email). Por eso esos
// encabezados los completa sólo el gateway: lo que mande el cliente con esos
// nombres se descarta siempre.
package auth

import "net/http"

// Encabezados de identidad que el gateway completa para los servicios internos.
// El nombre y el mail van codificados con url.QueryEscape, porque un encabezado
// HTTP no admite tildes ni ñ: el servicio los decodifica con url.QueryUnescape.
const (
	UserIDHeader    = "X-User-Id"
	UserRoleHeader  = "X-User-Role"
	UserNameHeader  = "X-User-Name"
	UserEmailHeader = "X-User-Email"
)

// identityHeaders son los encabezados que un cliente nunca puede fijar por su
// cuenta: así nadie puede publicar a nombre de otro.
var identityHeaders = []string{UserIDHeader, UserRoleHeader, UserNameHeader, UserEmailHeader}

// StripIdentity borra los encabezados de identidad que manda el cliente antes de
// seguir. Sin esto, cualquiera podría hacerse pasar por otro estudiante mandando
// X-User-Id con el id ajeno (RN-03, CA-02).
func StripIdentity(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r = r.Clone(r.Context())
		for _, h := range identityHeaders {
			r.Header.Del(h)
		}
		next.ServeHTTP(w, r)
	})
}
