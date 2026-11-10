package service

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/MichiSil/ProyectoEstudiario/services/users-service/internal/club"
	"github.com/MichiSil/ProyectoEstudiario/services/users-service/internal/model"
)

// memClubRepo guarda usuarios, vinculaciones y puntos en memoria, con la
// misma unicidad que la base: un crédito por eventId (RN-42).
type memClubRepo struct {
	mu      sync.Mutex
	users   map[string]model.User
	links   map[string]model.ClubLink
	credits map[string]model.ClubCredit
}

func newMemClubRepo(users ...model.User) *memClubRepo {
	r := &memClubRepo{users: map[string]model.User{}, links: map[string]model.ClubLink{}, credits: map[string]model.ClubCredit{}}
	for _, u := range users {
		r.users[u.ID] = u
	}
	return r
}

func (r *memClubRepo) GetByID(_ context.Context, id string) (model.User, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	u, ok := r.users[id]
	if !ok {
		return model.User{}, model.ErrNotFound
	}
	return u, nil
}

func (r *memClubRepo) CreateClubLink(_ context.Context, l model.ClubLink) (model.ClubLink, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if old, ok := r.links[l.UserID]; ok {
		return old, nil
	}
	l.UpdatedAt = l.CreatedAt
	r.links[l.UserID] = l
	return l, nil
}

func (r *memClubRepo) GetClubLink(_ context.Context, userID string) (model.ClubLink, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	l, ok := r.links[userID]
	if !ok {
		return model.ClubLink{}, model.ErrNotFound
	}
	return l, nil
}

func (r *memClubRepo) ActivateClubLink(_ context.Context, userID string, at time.Time) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	l, ok := r.links[userID]
	if !ok || l.Status != model.ClubLinkPending {
		return false, nil
	}
	l.Status, l.UpdatedAt = model.ClubLinkActive, at
	r.links[userID] = l
	return true, nil
}

func (r *memClubRepo) PendingClubLinks(_ context.Context, _ int) ([]model.ClubLink, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []model.ClubLink
	for _, l := range r.links {
		if l.Status == model.ClubLinkPending {
			out = append(out, l)
		}
	}
	return out, nil
}

func (r *memClubRepo) AddClubCredit(_ context.Context, c model.ClubCredit) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.users[c.UserID]; !ok {
		return false, model.ErrNotFound
	}
	if _, ok := r.credits[c.EventID]; ok {
		return false, nil
	}
	c.Status = model.CreditPending
	r.credits[c.EventID] = c
	return true, nil
}

func (r *memClubRepo) pending(match func(model.ClubCredit) bool) []model.ClubCredit {
	var out []model.ClubCredit
	for _, c := range r.credits {
		if c.Status == model.CreditPending && match(c) {
			out = append(out, c)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].EventID < out[j].EventID })
	return out
}

func (r *memClubRepo) PendingClubCredits(_ context.Context, userID string) ([]model.ClubCredit, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.pending(func(c model.ClubCredit) bool { return c.UserID == userID }), nil
}

func (r *memClubRepo) PendingCreditsOfLinkedUsers(_ context.Context, _ int) ([]model.ClubCredit, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.pending(func(c model.ClubCredit) bool { return r.links[c.UserID].Status == model.ClubLinkActive }), nil
}

func (r *memClubRepo) SetClubCreditResult(_ context.Context, eventID, status, movementID, _ string, _ time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	c := r.credits[eventID]
	if c.Status == model.CreditPending {
		c.Status, c.MovementID = status, movementID
		r.credits[eventID] = c
	}
	return nil
}

// fakeClub simula el Club: deduplica las acreditaciones por eventId, como el
// real, y se puede "caer" o dejar vinculaciones sin completar.
type fakeClub struct {
	mu         sync.Mutex
	down       bool
	delay      time.Duration // demora de las lecturas (saldo y movimientos)
	autoLink   bool // las vinculaciones se completan al instante
	registered map[string]bool
	completed  map[string]bool
	credited   map[string]int // eventId → puntos
	calls      int
}

func newFakeClub() *fakeClub {
	return &fakeClub{registered: map[string]bool{}, completed: map[string]bool{}, credited: map[string]int{}}
}

func (f *fakeClub) fail() error {
	f.calls++
	if f.down {
		return &club.Error{Kind: club.Unavailable, Err: errors.New("caído")}
	}
	return nil
}

func (f *fakeClub) Register(_ context.Context, r club.Registration) (club.Receipt, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.fail(); err != nil {
		return club.Receipt{}, err
	}
	f.registered[r.EventID] = true
	if f.autoLink {
		f.completed[r.EventID] = true
	}
	return club.Receipt{EventID: r.EventID, ExternalUserID: r.ExternalUserID, Completed: f.completed[r.EventID]}, nil
}

func (f *fakeClub) Registration(_ context.Context, eventID string) (club.Receipt, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.fail(); err != nil {
		return club.Receipt{}, err
	}
	if !f.registered[eventID] {
		return club.Receipt{}, &club.Error{Kind: club.NotLinked, Status: 404}
	}
	return club.Receipt{EventID: eventID, ExternalUserID: "x", Completed: f.completed[eventID]}, nil
}

func (f *fakeClub) Credit(_ context.Context, _ string, c club.Credit) (club.Movement, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.fail(); err != nil {
		return club.Movement{}, err
	}
	f.credited[c.EventID] = c.Points // mismo eventId: no suma de nuevo
	return club.Movement{ID: "mv-" + c.EventID, Points: c.Points}, nil
}

func (f *fakeClub) total() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, p := range f.credited {
		n += p
	}
	return n
}

// wait simula un Club lento: espera delay o hasta que venza ctx.
func (f *fakeClub) wait(ctx context.Context) error {
	select {
	case <-time.After(f.delay):
		return nil
	case <-ctx.Done():
		return &club.Error{Kind: club.Unavailable, Err: ctx.Err()}
	}
}

func (f *fakeClub) Balance(ctx context.Context, _ string) (club.Balance, error) {
	if err := f.wait(ctx); err != nil {
		return club.Balance{}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.fail(); err != nil {
		return club.Balance{}, err
	}
	n := 0
	for _, p := range f.credited {
		n += p
	}
	return club.Balance{Points: n}, nil
}

func (f *fakeClub) Movements(ctx context.Context, _ string, _ int) ([]club.Movement, error) {
	if err := f.wait(ctx); err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.fail(); err != nil {
		return nil, err
	}
	return []club.Movement{}, nil
}

var ana = model.User{ID: "u1", Name: "Ana", Email: "ana@x.com", Role: model.RoleStudent}

func newClub(repo *memClubRepo, provider *fakeClub) *Club {
	return &Club{Repo: repo, Provider: provider, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
}

// CA-35: un resumen aprobado suma una sola vez, aunque el evento llegue repetido (RN-42).
func TestResumenAprobadoSumaUnaSolaVez(t *testing.T) {
	repo, provider := newMemClubRepo(ana), newFakeClub()
	provider.autoLink = true
	c, ctx := newClub(repo, provider), context.Background()
	if _, err := c.Link(ctx, ana.ID); err != nil {
		t.Fatal(err)
	}
	for range 3 {
		if err := c.AwardApprovedSummary(ctx, ApprovedSummary{SummaryID: "s1", AuthorID: ana.ID}); err != nil {
			t.Fatal(err)
		}
	}
	if provider.total() != PointsPerApprovedSummary || len(repo.credits) != 1 {
		t.Fatalf("puntos en el Club = %d, créditos = %d", provider.total(), len(repo.credits))
	}
	if repo.credits["resumen-aprobado-s1"].Status != model.CreditDone {
		t.Errorf("estado = %s", repo.credits["resumen-aprobado-s1"].Status)
	}
}

// Sin vincular, los puntos quedan pendientes y se acreditan al completar la vinculación.
func TestPuntosAntesDeVincularSeAcreditanAlVincular(t *testing.T) {
	repo, provider := newMemClubRepo(ana), newFakeClub()
	c, ctx := newClub(repo, provider), context.Background()
	if err := c.AwardApprovedSummary(ctx, ApprovedSummary{SummaryID: "s1", AuthorID: ana.ID}); err != nil {
		t.Fatal(err)
	}
	if provider.calls != 0 {
		t.Fatal("llamó al Club sin que el usuario esté vinculado")
	}
	st, err := c.Link(ctx, ana.ID)
	if err != nil || st.Status != model.ClubLinkPending || st.PendingPoints != PointsPerApprovedSummary {
		t.Fatalf("estado = %+v, err = %v", st, err)
	}
	provider.completed["vinculo-u1"] = true // la persona confirmó el mail del Club
	st, err = c.Status(ctx, ana.ID)
	if err != nil || st.Status != model.ClubLinkActive || st.PendingPoints != 0 || st.Balance == nil || *st.Balance != PointsPerApprovedSummary {
		t.Fatalf("estado = %+v, err = %v", st, err)
	}
}

// CA-36 / RN-43: con el Club caído, los puntos se guardan y el barrido los
// acredita cuando vuelve, una sola vez.
func TestClubCaidoElBarridoAcreditaCuandoVuelve(t *testing.T) {
	repo, provider := newMemClubRepo(ana), newFakeClub()
	provider.autoLink = true
	c, ctx := newClub(repo, provider), context.Background()
	if _, err := c.Link(ctx, ana.ID); err != nil {
		t.Fatal(err)
	}
	provider.down = true
	err := c.AwardApprovedSummary(ctx, ApprovedSummary{SummaryID: "s1", AuthorID: ana.ID})
	if !errors.Is(err, club.ErrUnavailable) {
		t.Fatalf("err = %v: el consumidor tiene que saber que el Club no respondió", err)
	}
	if repo.credits["resumen-aprobado-s1"].Status != model.CreditPending {
		t.Fatal("los puntos no quedaron pendientes")
	}
	c.Sweep(ctx) // sigue caído: no pasa nada
	provider.down = false
	c.Sweep(ctx)
	c.Sweep(ctx)
	if provider.total() != PointsPerApprovedSummary || repo.credits["resumen-aprobado-s1"].Status != model.CreditDone {
		t.Fatalf("puntos = %d, estado = %s", provider.total(), repo.credits["resumen-aprobado-s1"].Status)
	}
}

// Si el Club está caído al vincular, queda pendiente y el barrido la vuelve a mandar.
func TestVincularConElClubCaido(t *testing.T) {
	repo, provider := newMemClubRepo(ana), newFakeClub()
	c, ctx := newClub(repo, provider), context.Background()
	provider.down = true
	st, err := c.Link(ctx, ana.ID)
	if err != nil || st.Status != model.ClubLinkPending || st.Warning != WarningClubUnavailable {
		t.Fatalf("estado = %+v, err = %v", st, err)
	}
	provider.down, provider.autoLink = false, true
	c.Sweep(ctx)
	if repo.links[ana.ID].Status != model.ClubLinkActive {
		t.Fatalf("la vinculación sigue %s", repo.links[ana.ID].Status)
	}
}

// ADR-009, 5: si el Club no responde, el perfil se muestra con el aviso.
func TestEstadoConElClubCaidoTraeAviso(t *testing.T) {
	repo, provider := newMemClubRepo(ana), newFakeClub()
	provider.autoLink = true
	c, ctx := newClub(repo, provider), context.Background()
	if _, err := c.Link(ctx, ana.ID); err != nil {
		t.Fatal(err)
	}
	provider.down = true
	st, err := c.Status(ctx, ana.ID)
	if err != nil || st.Warning != WarningClubUnavailable || st.Balance != nil || st.Status != model.ClubLinkActive {
		t.Fatalf("estado = %+v, err = %v", st, err)
	}
}

func TestSinVincularNoLlamaAlClub(t *testing.T) {
	repo, provider := newMemClubRepo(ana), newFakeClub()
	st, err := newClub(repo, provider).Status(context.Background(), ana.ID)
	if err != nil || st.Status != "sin_vincular" || provider.calls != 0 {
		t.Fatalf("estado = %+v, err = %v, llamadas = %d", st, err, provider.calls)
	}
}

func TestAutorInexistenteEsNoEncontrado(t *testing.T) {
	err := newClub(newMemClubRepo(), newFakeClub()).AwardApprovedSummary(context.Background(), ApprovedSummary{SummaryID: "s1", AuthorID: "nadie"})
	if !errors.Is(err, model.ErrNotFound) {
		t.Fatalf("err = %v", err)
	}
}

// Con el Club lento, el perfil responde dentro de su tope con el aviso, y el
// saldo y los movimientos se piden a la vez (no se suman las dos demoras).
func TestClubLentoRespondeATiempoConAviso(t *testing.T) {
	repo, provider := newMemClubRepo(ana), newFakeClub()
	provider.autoLink = true
	c, ctx := newClub(repo, provider), context.Background()
	if _, err := c.Link(ctx, ana.ID); err != nil {
		t.Fatal(err)
	}
	c.StatusBudget = 150 * time.Millisecond

	provider.delay = 100 * time.Millisecond // cada llamada entra en el tope, las dos juntas no
	started := time.Now()
	st, err := c.Status(ctx, ana.ID)
	if err != nil || st.Warning != "" || st.Balance == nil {
		t.Fatalf("estado = %+v, err = %v: las dos lecturas tenían que ir en paralelo", st, err)
	}
	if time.Since(started) > 140*time.Millisecond {
		t.Errorf("tardó %v: las lecturas no fueron en paralelo", time.Since(started))
	}

	provider.delay = time.Second
	started = time.Now()
	st, err = c.Status(ctx, ana.ID)
	if err != nil || st.Warning != WarningClubUnavailable {
		t.Fatalf("estado = %+v, err = %v", st, err)
	}
	if time.Since(started) > 300*time.Millisecond {
		t.Errorf("tardó %v: no respetó el tope", time.Since(started))
	}
}
