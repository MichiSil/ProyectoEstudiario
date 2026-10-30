// Package mail manda los mails por SMTP. En local el servidor es Mailpit, que
// los guarda y los muestra en http://localhost:8025 en lugar de mandarlos.
package mail

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"mime"
	"mime/quotedprintable"
	"net"
	"net/mail"
	"net/smtp"
	"net/textproto"
	"time"

	"github.com/MichiSil/ProyectoEstudiario/services/notification-service/internal/consumer"
	"github.com/MichiSil/ProyectoEstudiario/services/notification-service/internal/event"
)

// SMTP manda mails a un servidor SMTP sin autenticación (Mailpit). Toda la
// conversación respeta el deadline del contexto (10 s, ADR-005, 3.4).
//
// Errores (ADR-005, 3.5):
//   - el servidor no responde, corta la conexión o responde 4xx: transitorio;
//   - responde 5xx (por ejemplo, la dirección no existe) o la dirección es
//     inválida: permanente (event.ErrPermanent).
type SMTP struct {
	Addr    string // host:puerto
	From    string // por ejemplo "Estudiario <no-reply@estudiario.local>"
	HelloAs string // nombre que se presenta en EHLO; por defecto "notification-service"
	Now     func() time.Time
	dialer  net.Dialer
}

// Send manda el mail.
func (s *SMTP) Send(ctx context.Context, m consumer.Mail) error {
	from, err := s.from()
	if err != nil {
		return err
	}
	to, err := mail.ParseAddress(m.To)
	if err != nil {
		return event.Permanent("dirección de mail inválida %q: %v", m.To, err)
	}
	to.Name = m.ToName

	conn, err := s.dialer.DialContext(ctx, "tcp", s.Addr)
	if err != nil {
		return fmt.Errorf("conectar al servidor de mail: %w", err)
	}
	defer conn.Close()
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}
	host, _, _ := net.SplitHostPort(s.Addr)
	c, err := smtp.NewClient(conn, host)
	if err != nil {
		return classify("saludo del servidor de mail", err)
	}
	defer c.Close()

	hello := s.HelloAs
	if hello == "" {
		hello = "notification-service"
	}
	if err := c.Hello(hello); err != nil {
		return classify("EHLO", err)
	}
	if err := c.Mail(from.Address); err != nil {
		return classify("MAIL FROM", err)
	}
	if err := c.Rcpt(to.Address); err != nil {
		return classify("RCPT TO", err)
	}
	w, err := c.Data()
	if err != nil {
		return classify("DATA", err)
	}
	if _, err := w.Write(s.message(from, to, m)); err != nil {
		return classify("enviar el mensaje", err)
	}
	if err := w.Close(); err != nil {
		return classify("fin del mensaje", err)
	}
	_ = c.Quit()
	return nil
}

func (s *SMTP) from() (*mail.Address, error) {
	a, err := mail.ParseAddress(s.From)
	if err != nil {
		return nil, fmt.Errorf("MAIL_FROM inválido %q: %w", s.From, err)
	}
	return a, nil
}

// message arma el mail en texto plano UTF-8. El Message-ID sale del eventId,
// así cada decisión de moderación se reconoce en el servidor de mail.
func (s *SMTP) message(from, to *mail.Address, m consumer.Mail) []byte {
	now := time.Now
	if s.Now != nil {
		now = s.Now
	}
	var b bytes.Buffer
	header := func(k, v string) { fmt.Fprintf(&b, "%s: %s\r\n", k, v) }
	header("From", from.String())
	header("To", to.String())
	header("Subject", mime.QEncoding.Encode("utf-8", m.Subject))
	header("Date", now().Format(time.RFC1123Z))
	if m.MessageID != "" {
		header("Message-ID", "<"+m.MessageID+"@estudiario.local>")
	}
	header("MIME-Version", "1.0")
	header("Content-Type", `text/plain; charset="utf-8"`)
	header("Content-Transfer-Encoding", "quoted-printable")
	b.WriteString("\r\n")
	qp := quotedprintable.NewWriter(&b)
	_, _ = qp.Write(bytes.ReplaceAll([]byte(m.Body), []byte("\n"), []byte("\r\n")))
	_ = qp.Close()
	return b.Bytes()
}

// classify marca como permanentes las respuestas 5xx del servidor. Las 4xx y
// los errores de red son transitorios.
func classify(step string, err error) error {
	var tp *textproto.Error
	if errors.As(err, &tp) && tp.Code >= 500 {
		return event.Permanent("%s: el servidor de mail respondió %d %s", step, tp.Code, tp.Msg)
	}
	return fmt.Errorf("%s: %w", step, err)
}
