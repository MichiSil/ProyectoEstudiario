package consumer

import (
	"fmt"
	"strings"

	"github.com/MichiSil/ProyectoEstudiario/services/notification-service/internal/event"
)

// Compose arma el mail al autor: "tu resumen fue publicado" (RF-23) o "tu
// resumen fue rechazado", con el motivo (RF-24).
func Compose(ev event.Event) Mail {
	d := ev.Data
	greeting := "Hola"
	if name := strings.TrimSpace(d.Author.Name); name != "" {
		greeting = "Hola, " + name
	}
	subject := ""
	if d.Subject != "" {
		subject = fmt.Sprintf(" de %s", d.Subject)
	}

	var b strings.Builder
	m := Mail{MessageID: ev.EventID, To: d.Author.Email, ToName: d.Author.Name}
	switch ev.EventType {
	case event.TypeApproved:
		m.Subject = "Tu resumen fue publicado"
		fmt.Fprintf(&b, "%s:\n\n", greeting)
		fmt.Fprintf(&b, "Tu resumen \"%s\"%s fue aprobado y ya está publicado en el foro de Estudiario.\n", d.Title, subject)
		b.WriteString("Los demás estudiantes ya lo pueden encontrar y calificar.\n")
	default:
		m.Subject = "Tu resumen fue rechazado"
		fmt.Fprintf(&b, "%s:\n\n", greeting)
		fmt.Fprintf(&b, "Tu resumen \"%s\"%s fue rechazado por la moderación del foro de Estudiario.\n\n", d.Title, subject)
		fmt.Fprintf(&b, "Motivo: %s\n\n", d.Reason)
		b.WriteString("Podés corregirlo y volver a subirlo.\n")
	}
	b.WriteString("\n— Estudiario\n")
	m.Body = b.String()
	return m
}
