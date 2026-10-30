package mail

import (
	"bufio"
	"context"
	"io"
	"mime/quotedprintable"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/MichiSil/ProyectoEstudiario/services/notification-service/internal/consumer"
	"github.com/MichiSil/ProyectoEstudiario/services/notification-service/internal/event"
)

// fakeSMTP es un servidor SMTP mínimo que responde rcptCode a RCPT TO y
// guarda el mensaje recibido.
func fakeSMTP(t *testing.T, rcptCode string) (addr string, received chan string) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	received = make(chan string, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		r := bufio.NewReader(conn)
		say := func(s string) { _, _ = io.WriteString(conn, s+"\r\n") }
		say("220 fake ESMTP")
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				return
			}
			cmd := strings.ToUpper(strings.TrimSpace(line))
			switch {
			case strings.HasPrefix(cmd, "EHLO"):
				say("250 fake")
			case strings.HasPrefix(cmd, "MAIL FROM"):
				say("250 ok")
			case strings.HasPrefix(cmd, "RCPT TO"):
				say(rcptCode)
			case cmd == "DATA":
				say("354 go")
				var msg strings.Builder
				for {
					l, err := r.ReadString('\n')
					if err != nil {
						return
					}
					if l == ".\r\n" {
						break
					}
					msg.WriteString(l)
				}
				received <- msg.String()
				say("250 queued")
			case cmd == "QUIT":
				say("221 bye")
				return
			default:
				say("250 ok")
			}
		}
	}()
	return ln.Addr().String(), received
}

func testMail() consumer.Mail {
	return consumer.Mail{
		MessageID: "ev-1", To: "ana@example.com", ToName: "Ana Pérez",
		Subject: "Tu resumen fue rechazado", Body: "Motivo: faltan las páginas 3 a 5.\n",
	}
}

func send(t *testing.T, addr string) error {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	s := &SMTP{Addr: addr, From: "Estudiario <no-reply@estudiario.local>"}
	return s.Send(ctx, testMail())
}

func TestSendMandaElMensaje(t *testing.T) {
	addr, received := fakeSMTP(t, "250 ok")
	if err := send(t, addr); err != nil {
		t.Fatalf("Send: %v", err)
	}
	msg := <-received
	for _, want := range []string{"To: =?utf-8?q?Ana_P=C3=A9rez?= <ana@example.com>", "Message-ID: <ev-1@estudiario.local>", "charset=\"utf-8\""} {
		if !strings.Contains(msg, want) {
			t.Errorf("falta %q en:\n%s", want, msg)
		}
	}
	_, bodyPart, _ := strings.Cut(msg, "\r\n\r\n")
	decoded, _ := io.ReadAll(quotedprintable.NewReader(strings.NewReader(bodyPart)))
	if !strings.Contains(string(decoded), "faltan las páginas 3 a 5") {
		t.Errorf("cuerpo = %q", decoded)
	}
}

func TestSMTP5xxEsPermanente(t *testing.T) {
	addr, _ := fakeSMTP(t, "550 5.1.1 la casilla no existe")
	err := send(t, addr)
	if !event.IsPermanent(err) {
		t.Fatalf("error = %v, se esperaba permanente", err)
	}
}

func TestSMTP4xxEsTransitorio(t *testing.T) {
	addr, _ := fakeSMTP(t, "451 4.3.0 probá más tarde")
	err := send(t, addr)
	if err == nil || event.IsPermanent(err) {
		t.Fatalf("error = %v, se esperaba transitorio", err)
	}
}

func TestServidorCaidoEsTransitorio(t *testing.T) {
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	addr := ln.Addr().String()
	_ = ln.Close() // nadie escucha
	err := send(t, addr)
	if err == nil || event.IsPermanent(err) {
		t.Fatalf("error = %v, se esperaba transitorio", err)
	}
}

func TestServidorQueNoRespondeCortaPorTimeout(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		conn, err := ln.Accept()
		if err == nil {
			defer conn.Close()
			time.Sleep(2 * time.Second) // acepta y nunca saluda
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	started := time.Now()
	err = (&SMTP{Addr: ln.Addr().String(), From: "no-reply@estudiario.local"}).Send(ctx, testMail())
	if err == nil || event.IsPermanent(err) {
		t.Fatalf("error = %v, se esperaba transitorio", err)
	}
	if time.Since(started) > time.Second {
		t.Errorf("tardó %v: no respetó el timeout", time.Since(started))
	}
}

func TestDireccionInvalidaEsPermanente(t *testing.T) {
	s := &SMTP{Addr: "127.0.0.1:1", From: "no-reply@estudiario.local"}
	m := testMail()
	m.To = "no es un mail"
	if err := s.Send(context.Background(), m); !event.IsPermanent(err) {
		t.Fatalf("error = %v, se esperaba permanente", err)
	}
}
