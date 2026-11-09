package repository_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/MichiSil/ProyectoEstudiario/services/users-service/internal/model"
)

// RN-42 contra PostgreSQL real: el mismo eventId se guarda una sola vez,
// aunque lleguen varios a la vez, y se acredita una sola vez.
func TestPuntosDelClubUnaSolaVez(t *testing.T) {
	repo, ctx := newRepo(t), context.Background()
	u := newUser()
	if err := repo.Create(ctx, u); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	credit := model.ClubCredit{EventID: "resumen-aprobado-" + u.ID, UserID: u.ID, Reference: u.ID, Points: 50, Reason: "Resumen aprobado en Estudiario", CreatedAt: now}

	const parallel = 5
	added := make(chan bool, parallel)
	for range parallel {
		go func() {
			ok, err := repo.AddClubCredit(ctx, credit)
			if err != nil {
				t.Error(err)
			}
			added <- ok
		}()
	}
	n := 0
	for range parallel {
		if <-added {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("se guardó %d veces", n)
	}

	pending, err := repo.PendingClubCredits(ctx, u.ID)
	if err != nil || len(pending) != 1 || pending[0].Points != 50 {
		t.Fatalf("pendientes = %+v, err = %v", pending, err)
	}
	// Sin vincular, el barrido no los ve.
	if linked, _ := repo.PendingCreditsOfLinkedUsers(ctx, 1000); containsCredit(linked, credit.EventID) {
		t.Fatal("el barrido ve puntos de un usuario sin vincular")
	}

	link, err := repo.CreateClubLink(ctx, model.ClubLink{UserID: u.ID, ExternalUserID: "estudiario-" + u.ID, EventID: "vinculo-" + u.ID, Status: model.ClubLinkPending, CreatedAt: now})
	if err != nil || link.Status != model.ClubLinkPending {
		t.Fatalf("vinculación = %+v, err = %v", link, err)
	}
	if again, _ := repo.CreateClubLink(ctx, model.ClubLink{UserID: u.ID, ExternalUserID: "otro", EventID: "otro", Status: model.ClubLinkPending, CreatedAt: now}); again.ExternalUserID != link.ExternalUserID {
		t.Fatal("una segunda vinculación reemplazó a la primera")
	}
	for i, want := range []bool{true, false} {
		if changed, err := repo.ActivateClubLink(ctx, u.ID, now); err != nil || changed != want {
			t.Fatalf("activar %d: changed = %v, err = %v", i, changed, err)
		}
	}
	if linked, _ := repo.PendingCreditsOfLinkedUsers(ctx, 1000); !containsCredit(linked, credit.EventID) {
		t.Fatal("el barrido no ve los puntos pendientes de un usuario vinculado")
	}

	if err := repo.SetClubCreditResult(ctx, credit.EventID, model.CreditDone, "mv-1", "", now); err != nil {
		t.Fatal(err)
	}
	// Un segundo resultado no pisa al primero.
	if err := repo.SetClubCreditResult(ctx, credit.EventID, model.CreditRejected, "", "x", now); err != nil {
		t.Fatal(err)
	}
	if pending, _ := repo.PendingClubCredits(ctx, u.ID); len(pending) != 0 {
		t.Fatalf("siguen pendientes: %+v", pending)
	}
}

func containsCredit(list []model.ClubCredit, eventID string) bool {
	for _, c := range list {
		if c.EventID == eventID {
			return true
		}
	}
	return false
}

func TestPuntosDeUnUsuarioInexistente(t *testing.T) {
	repo, ctx := newRepo(t), context.Background()
	_, err := repo.AddClubCredit(ctx, model.ClubCredit{EventID: "e-" + newUser().ID, UserID: "no-existe", Reference: "x", Points: 1, Reason: "x", CreatedAt: time.Now()})
	if !errors.Is(err, model.ErrNotFound) {
		t.Fatalf("err = %v", err)
	}
}

func TestEventosProcesadosYOrigenDeLaCuenta(t *testing.T) {
	repo, ctx := newRepo(t), context.Background()
	u := newUser()
	u.Source = "agencia_de_viajes"
	if err := repo.Create(ctx, u); err != nil {
		t.Fatal(err)
	}
	if got, _ := repo.GetByID(ctx, u.ID); got.Source != "agencia_de_viajes" {
		t.Fatalf("source = %q", got.Source)
	}
	if err := repo.UpdatePassword(ctx, u.ID, "$2a$10$otro"); err != nil {
		t.Fatal(err)
	}
	ev := "ev-" + u.ID
	if status, _, _ := repo.GetProcessedEvent(ctx, ev); status != "" {
		t.Fatalf("status = %q", status)
	}
	_ = repo.SaveProcessedEvent(ctx, ev, "document.external-registered", "cuenta_creada", u.ID, time.Now())
	_ = repo.SaveProcessedEvent(ctx, ev, "document.external-registered", "terminado", "", time.Now())
	if status, userID, err := repo.GetProcessedEvent(ctx, ev); status != "terminado" || userID != u.ID || err != nil {
		t.Fatalf("status = %q, userId = %q, err = %v", status, userID, err)
	}
}
