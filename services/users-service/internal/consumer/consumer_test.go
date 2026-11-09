package consumer

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/MichiSil/ProyectoEstudiario/services/users-service/internal/club"
	"github.com/MichiSil/ProyectoEstudiario/services/users-service/internal/mail"
	"github.com/MichiSil/ProyectoEstudiario/services/users-service/internal/model"
	"github.com/MichiSil/ProyectoEstudiario/services/users-service/internal/service"
)

var quiet = slog.New(slog.NewTextHandler(io.Discard, nil))

type fakeAwarder struct {
	got []service.ApprovedSummary
	err error
}

func (f *fakeAwarder) AwardApprovedSummary(_ context.Context, s service.ApprovedSummary) error {
	f.got = append(f.got, s)
	return f.err
}

// approved arma un summary.approved como lo publica forum-service.
func approved(extra string) []byte {
	return []byte(`{"eventId":"ev-1","eventType":"summary.approved","eventVersion":1,"occurredAt":"2026-11-09T18:00:00Z",
		"producer":"forum-service","correlationId":"c-1","data":{"summaryId":"s1","title":"Análisis","subject":"Análisis",
		"source":"estudiario","author":{"id":"u1","name":"Ana","email":"ana@x.com"},"moderatedBy":"admin",
		"moderatedAt":"2026-11-09T18:00:00Z","notifyAuthor":true` + extra + `}}`)
}

func newBenefits(a Awarder) *Benefits {
	return &Benefits{Club: a, Logger: quiet, Delays: []time.Duration{time.Millisecond, time.Millisecond}, Jitter: func() float64 { return 0.5 }}
}

func TestResumenAprobadoSumaPuntos(t *testing.T) {
	a := &fakeAwarder{}
	d := newBenefits(a).Handle(context.Background(), Message{Body: approved(`,"awardBenefits":true`)})
	if d.Action != Ack || len(a.got) != 1 || a.got[0] != (service.ApprovedSummary{SummaryID: "s1", AuthorID: "u1"}) {
		t.Fatalf("decisión = %+v, llamadas = %+v", d, a.got)
	}
}

func TestDocumentoExternoNoSumaPuntos(t *testing.T) {
	a := &fakeAwarder{}
	d := newBenefits(a).Handle(context.Background(), Message{Body: approved(`,"awardBenefits":false`)})
	if d.Action != Ack || len(a.got) != 0 {
		t.Fatalf("decisión = %+v, llamadas = %d", d, len(a.got))
	}
}

func TestClubCaidoSeReintentaYDespuesVaALaDLQ(t *testing.T) {
	a := &fakeAwarder{err: &club.Error{Kind: club.Unavailable}}
	h := newBenefits(a)
	if d := h.Handle(context.Background(), Message{Body: approved(""), Retries: 0}); d.Action != Retry || d.Delay <= 0 {
		t.Fatalf("primer intento: %+v", d)
	}
	if d := h.Handle(context.Background(), Message{Body: approved(""), Retries: 2}); d.Action != DeadLetter {
		t.Fatalf("reintentos agotados: %+v", d)
	}
}

func TestMensajeInvalidoVaDirectoALaDLQ(t *testing.T) {
	a := &fakeAwarder{}
	for _, body := range []string{`no es json`, `{"eventId":"x","eventType":"otro","eventVersion":1,"data":{}}`,
		`{"eventId":"x","eventType":"summary.approved","eventVersion":2,"data":{}}`} {
		if d := newBenefits(a).Handle(context.Background(), Message{Body: []byte(body)}); d.Action != DeadLetter {
			t.Errorf("%s → %+v", body, d)
		}
	}
	if len(a.got) != 0 {
		t.Fatal("procesó un mensaje inválido")
	}
}

func TestAutorInexistenteVaALaDLQ(t *testing.T) {
	d := newBenefits(&fakeAwarder{err: model.ErrNotFound}).Handle(context.Background(), Message{Body: approved("")})
	if d.Action != DeadLetter {
		t.Fatalf("decisión = %+v", d)
	}
}

func TestAPIKeyRechazadaNoSeReintenta(t *testing.T) {
	d := newBenefits(&fakeAwarder{err: &club.Error{Kind: club.Misconfigured, Status: 401}}).Handle(context.Background(), Message{Body: approved("")})
	if d.Action != Ack {
		t.Fatalf("decisión = %+v: los puntos quedan pendientes y reintentar no sirve", d)
	}
}

type fakeCreator struct {
	got []service.ExternalAuthor
	err error
}

func (f *fakeCreator) CreateExternalAuthor(_ context.Context, in service.ExternalAuthor) error {
	f.got = append(f.got, in)
	return f.err
}

func externalRegistered(email string) []byte {
	return []byte(`{"eventId":"ev-2","eventType":"document.external-registered","eventVersion":1,"occurredAt":"2026-11-09T18:00:00Z",
		"producer":"forum-service","correlationId":"c-2","data":{"documentId":"d1","title":"Guía de Bariloche",
		"source":"agencia_de_viajes","author":{"name":"Lucía","email":"` + email + `","externalId":"v-1"}}}`)
}

func newExternalAuthors(c AccountCreator) *ExternalAuthors {
	return &ExternalAuthors{Accounts: c, Logger: quiet, Delays: []time.Duration{time.Millisecond}, Jitter: func() float64 { return 0.5 }}
}

func TestDocumentoExternoCreaLaCuentaDelAutor(t *testing.T) {
	c := &fakeCreator{}
	d := newExternalAuthors(c).Handle(context.Background(), Message{Body: externalRegistered("lucia@viajes.com")})
	want := service.ExternalAuthor{EventID: "ev-2", Name: "Lucía", Email: "lucia@viajes.com", Source: "agencia_de_viajes", DocumentTitle: "Guía de Bariloche"}
	if d.Action != Ack || len(c.got) != 1 || c.got[0] != want {
		t.Fatalf("decisión = %+v, llamadas = %+v", d, c.got)
	}
}

func TestDocumentoExternoSinMailNoCreaCuenta(t *testing.T) {
	c := &fakeCreator{}
	if d := newExternalAuthors(c).Handle(context.Background(), Message{Body: externalRegistered("")}); d.Action != Ack || len(c.got) != 0 {
		t.Fatalf("decisión = %+v, llamadas = %d", d, len(c.got))
	}
}

func TestErroresDeLaCuentaDelAutor(t *testing.T) {
	cases := []struct {
		err  error
		want Action
	}{
		{errors.New("mailpit caído"), Retry},
		{errors.Join(mail.ErrPermanent, errors.New("550")), DeadLetter},
		{&model.ValidationError{Fields: []model.FieldError{{Field: "author.email"}}}, DeadLetter},
	}
	for _, tc := range cases {
		d := newExternalAuthors(&fakeCreator{err: tc.err}).Handle(context.Background(), Message{Body: externalRegistered("lucia@viajes.com")})
		if d.Action != tc.want {
			t.Errorf("%v → %+v, quería %v", tc.err, d, tc.want)
		}
	}
}
