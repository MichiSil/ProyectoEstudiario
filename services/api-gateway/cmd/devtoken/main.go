// Command devtoken genera un JWT de prueba para usar el sistema en local
// mientras users-service no emita tokens (inicio de sesión, RF-02).
//
// Firma con el mismo JWT_SECRET que valida el api-gateway:
//
//	cd services/api-gateway
//	JWT_SECRET=<el de tu .env> go run ./cmd/devtoken -sub estudiante-1
//
// Imprime sólo el token, para usarlo así:
//
//	TOKEN=$(JWT_SECRET=... go run ./cmd/devtoken -sub estudiante-1)
//	curl -H "Authorization: Bearer $TOKEN" http://localhost:8080/api/planner/sessions
//
// Es sólo para desarrollo: en la nube el JWT_SECRET no se comparte y los
// tokens los emite users-service.
package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/MichiSil/ProyectoEstudiario/services/api-gateway/internal/auth"
)

func main() {
	sub := flag.String("sub", "", "id del usuario (obligatorio)")
	role := flag.String("role", auth.RoleStudent, "rol: estudiante o admin")
	ttl := flag.Duration("ttl", time.Hour, "vigencia del token")
	flag.Parse()

	if *sub == "" {
		fmt.Fprintln(os.Stderr, "falta -sub (el id del usuario)")
		os.Exit(2)
	}
	if *role != auth.RoleStudent && *role != auth.RoleAdmin {
		fmt.Fprintln(os.Stderr, "-role tiene que ser estudiante o admin")
		os.Exit(2)
	}

	token, err := auth.Sign(os.Getenv("JWT_SECRET"), *sub, *role, *ttl)
	if err != nil {
		fmt.Fprintln(os.Stderr, "no se pudo firmar el token:", err)
		os.Exit(1)
	}
	fmt.Println(token)
}
