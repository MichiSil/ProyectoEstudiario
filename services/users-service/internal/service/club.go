package service

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/MichiSil/ProyectoEstudiario/services/users-service/internal/club"
	"github.com/MichiSil/ProyectoEstudiario/services/users-service/internal/model"
)

// Valores del programa de beneficios (RF-28).
const (
	PointsPerApprovedSummary = 50
	approvedSummaryReason    = "Resumen aprobado en Estudiario"
	movementsShown           = 10
	sweepBatch               = 50
)

// Avisos de GET /me/club para el frontend.
const (
	WarningClubUnavailable = "CLUB_UNAVAILABLE"
)

// ClubProvider es lo que el servicio usa del Club de Beneficios. Lo implementa
// el adaptador club.Client; los tests usan uno falso.
type ClubProvider interface {
	Register(ctx context.Context, r club.Registration) (club.Receipt, error)
	Registration(ctx context.Context, eventID string) (club.Receipt, error)
	Credit(ctx context.Context, externalUserID string, c club.Credit) (club.Movement, error)
	Balance(ctx context.Context, externalUserID string) (club.Balance, error)
	Movements(ctx context.Context, externalUserID string, size int) ([]club.Movement, error)
}

// ClubRepository es la persistencia de la vinculación y de los puntos.
type ClubRepository interface {
	GetByID(ctx context.Context, id string) (model.User, error)
	CreateClubLink(ctx context.Context, l model.ClubLink) (model.ClubLink, error)
	GetClubLink(ctx context.Context, userID string) (model.ClubLink, error)
	ActivateClubLink(ctx context.Context, userID string, at time.Time) (bool, error)
	PendingClubLinks(ctx context.Context, limit int) ([]model.ClubLink, error)
	AddClubCredit(ctx context.Context, c model.ClubCredit) (bool, error)
	PendingClubCredits(ctx context.Context, userID string) ([]model.ClubCredit, error)
	PendingCreditsOfLinkedUsers(ctx context.Context, limit int) ([]model.ClubCredit, error)
	SetClubCreditResult(ctx context.Context, eventID, status, movementID, lastError string, at time.Time) error
}

// Club implementa la integración con el Club de Beneficios del Grupo 1:
//
//   - el estudiante vincula su cuenta (Link);
//   - cuando un admin aprueba un resumen suyo, gana puntos (AwardApprovedSummary,
//     desde el evento summary.approved);
//   - ve su saldo y sus movimientos (Status).
//
// La moderación nunca depende del Club (RN-43): los puntos se guardan primero
// en la base y se acreditan cuando se puede, con el eventId como clave, así
// nunca se acreditan dos veces (RN-42).
type Club struct {
	Repo     ClubRepository
	Provider ClubProvider
	Logger   *slog.Logger
	Now      func() time.Time
}

func (c *Club) now() time.Time {
	if c.Now != nil {
		return c.Now().UTC()
	}
	return time.Now().UTC()
}

// externalIDs son los ids que se mandan al Club para un usuario. Son fijos:
// repetir la vinculación no crea otra cuenta (el Club deduplica por eventId).
func externalIDs(userID string) (externalUserID, eventID string) {
	return "estudiario-" + userID, "vinculo-" + userID
}

// ClubStatus es lo que ve el usuario en su perfil.
type ClubStatus struct {
	Status        string // "sin_vincular", "pendiente" o "vinculada"
	Balance       *int   // nil si no está vinculada o el Club no respondió
	PendingPoints int    // puntos ganados que todavía no se acreditaron
	Movements     []club.Movement
	Warning       string // WarningClubUnavailable si el Club no respondió
}

// Link pide al Club vincular la cuenta del usuario. Si el Club no responde, la
// vinculación queda pendiente y el barrido la vuelve a mandar (Sweep): el
// usuario no tiene que reintentar.
func (c *Club) Link(ctx context.Context, userID string) (ClubStatus, error) {
	u, err := c.Repo.GetByID(ctx, userID)
	if err != nil {
		return ClubStatus{}, err
	}
	extID, eventID := externalIDs(u.ID)
	link, err := c.Repo.CreateClubLink(ctx, model.ClubLink{
		UserID: u.ID, ExternalUserID: extID, EventID: eventID,
		Status: model.ClubLinkPending, CreatedAt: c.now(),
	})
	if err != nil {
		return ClubStatus{}, err
	}
	st := ClubStatus{Status: link.Status}
	if link.Status == model.ClubLinkActive {
		return c.Status(ctx, userID)
	}
	receipt, err := c.Provider.Register(ctx, club.Registration{Name: u.Name, Email: u.Email, ExternalUserID: extID, EventID: eventID})
	switch {
	case errors.Is(err, club.ErrUnavailable):
		st.Warning = WarningClubUnavailable
		return c.withPending(ctx, userID, st)
	case err != nil:
		return ClubStatus{}, err
	}
	if receipt.Completed {
		if err := c.activate(ctx, userID); err != nil {
			return ClubStatus{}, err
		}
		return c.Status(ctx, userID)
	}
	return c.withPending(ctx, userID, st)
}

// Status devuelve el estado de la vinculación, el saldo y los últimos
// movimientos. Si el Club no responde, el resto del perfil se muestra igual
// con el aviso CLUB_UNAVAILABLE (ADR-009, 5).
func (c *Club) Status(ctx context.Context, userID string) (ClubStatus, error) {
	link, err := c.Repo.GetClubLink(ctx, userID)
	if errors.Is(err, model.ErrNotFound) {
		return c.withPending(ctx, userID, ClubStatus{Status: "sin_vincular"})
	}
	if err != nil {
		return ClubStatus{}, err
	}
	if link.Status == model.ClubLinkPending {
		done, err := c.refresh(ctx, link)
		if err != nil {
			return c.withPending(ctx, userID, ClubStatus{Status: link.Status, Warning: WarningClubUnavailable})
		}
		if !done {
			return c.withPending(ctx, userID, ClubStatus{Status: link.Status})
		}
	}
	st := ClubStatus{Status: model.ClubLinkActive}
	bal, err := c.Provider.Balance(ctx, link.ExternalUserID)
	if err != nil {
		c.Logger.WarnContext(ctx, "no se pudo consultar el saldo del Club", "error", err.Error())
		st.Warning = WarningClubUnavailable
		return c.withPending(ctx, userID, st)
	}
	st.Balance = &bal.Points
	if moves, err := c.Provider.Movements(ctx, link.ExternalUserID, movementsShown); err == nil {
		st.Movements = moves
	} else {
		st.Warning = WarningClubUnavailable
	}
	return c.withPending(ctx, userID, st)
}

func (c *Club) withPending(ctx context.Context, userID string, st ClubStatus) (ClubStatus, error) {
	pending, err := c.Repo.PendingClubCredits(ctx, userID)
	if err != nil {
		return ClubStatus{}, err
	}
	for _, p := range pending {
		st.PendingPoints += p.Points
	}
	return st, nil
}

// refresh consulta al Club si la vinculación pendiente ya se completó. Si el
// Club nunca recibió el pedido (404, por ejemplo porque estaba caído al
// vincular), lo vuelve a mandar con el mismo eventId.
func (c *Club) refresh(ctx context.Context, link model.ClubLink) (bool, error) {
	receipt, err := c.Provider.Registration(ctx, link.EventID)
	if errors.Is(err, club.ErrNotLinked) {
		u, uerr := c.Repo.GetByID(ctx, link.UserID)
		if uerr != nil {
			return false, uerr
		}
		receipt, err = c.Provider.Register(ctx, club.Registration{
			Name: u.Name, Email: u.Email, ExternalUserID: link.ExternalUserID, EventID: link.EventID,
		})
	}
	if err != nil {
		return false, err
	}
	if !receipt.Completed {
		return false, nil
	}
	return true, c.activate(ctx, link.UserID)
}

// activate marca la vinculación como completa y acredita los puntos que el
// usuario ganó antes de vincularse.
func (c *Club) activate(ctx context.Context, userID string) error {
	changed, err := c.Repo.ActivateClubLink(ctx, userID, c.now())
	if err != nil {
		return err
	}
	if changed {
		c.Logger.InfoContext(ctx, "cuenta del Club vinculada", "userId", userID)
	}
	pending, err := c.Repo.PendingClubCredits(ctx, userID)
	if err != nil {
		return err
	}
	link, err := c.Repo.GetClubLink(ctx, userID)
	if err != nil {
		return err
	}
	for _, p := range pending {
		// Si el Club se cae a mitad de camino, lo que falte lo acredita el barrido.
		if err := c.credit(ctx, link, p); err != nil {
			return nil
		}
	}
	return nil
}

// ApprovedSummary son los datos de summary.approved que importan acá.
type ApprovedSummary struct {
	SummaryID string
	AuthorID  string
}

// AwardApprovedSummary registra los puntos por un resumen aprobado (RF-28) y,
// si el autor ya está vinculado, los acredita en el Club. Es idempotente: el
// mismo resumen suma una sola vez (RN-42). Devuelve club.ErrUnavailable si el
// Club no respondió, para que el consumidor reintente; los puntos ya quedaron
// guardados como pendientes, así que tampoco se pierden si se agotan los
// reintentos (el barrido los acredita cuando el Club vuelva, RN-43).
func (c *Club) AwardApprovedSummary(ctx context.Context, s ApprovedSummary) error {
	credit := model.ClubCredit{
		EventID:   "resumen-aprobado-" + s.SummaryID,
		UserID:    s.AuthorID,
		Reference: s.SummaryID,
		Points:    PointsPerApprovedSummary,
		Reason:    approvedSummaryReason,
		CreatedAt: c.now(),
	}
	if _, err := c.Repo.AddClubCredit(ctx, credit); err != nil {
		return err
	}
	link, err := c.Repo.GetClubLink(ctx, s.AuthorID)
	if errors.Is(err, model.ErrNotFound) || (err == nil && link.Status != model.ClubLinkActive) {
		c.Logger.InfoContext(ctx, "puntos guardados hasta que el autor vincule su cuenta del Club", "summaryId", s.SummaryID)
		return nil
	}
	if err != nil {
		return err
	}
	pending, err := c.Repo.PendingClubCredits(ctx, s.AuthorID)
	if err != nil {
		return err
	}
	for _, p := range pending {
		if p.EventID == credit.EventID {
			return c.credit(ctx, link, p)
		}
	}
	return nil // ya estaba acreditado: el evento llegó repetido
}

// credit acredita una fila pendiente en el Club y guarda el resultado.
func (c *Club) credit(ctx context.Context, link model.ClubLink, p model.ClubCredit) error {
	mv, err := c.Provider.Credit(ctx, link.ExternalUserID, club.Credit{
		EventID: p.EventID, Points: p.Points, Reason: p.Reason, Reference: p.Reference,
	})
	switch {
	case err == nil:
		c.Logger.InfoContext(ctx, "puntos acreditados en el Club", "eventId", p.EventID, "points", p.Points, "movementId", mv.ID)
		return c.Repo.SetClubCreditResult(ctx, p.EventID, model.CreditDone, mv.ID, "", c.now())
	case errors.Is(err, club.ErrRejected):
		// El Club no lo va a aceptar nunca (por ejemplo, el eventId se usó con
		// otro contenido): se deja registrado y no se insiste.
		c.Logger.ErrorContext(ctx, "el Club rechazó la acreditación", "eventId", p.EventID, "error", err.Error())
		return c.Repo.SetClubCreditResult(ctx, p.EventID, model.CreditRejected, "", err.Error(), c.now())
	default:
		return err
	}
}

// Sweep es el barrido periódico (RN-43): completa las vinculaciones pendientes
// y acredita los puntos que quedaron sin acreditar porque el Club no respondía.
// Corre cada SweepInterval sin intervención manual.
func (c *Club) Sweep(ctx context.Context) {
	links, err := c.Repo.PendingClubLinks(ctx, sweepBatch)
	if err != nil {
		c.Logger.WarnContext(ctx, "barrido del Club: no se pudieron leer las vinculaciones", "error", err.Error())
		return
	}
	for _, l := range links {
		if _, err := c.refresh(ctx, l); errors.Is(err, club.ErrUnavailable) {
			return // el Club sigue caído: se reintenta en el próximo barrido
		}
	}
	credits, err := c.Repo.PendingCreditsOfLinkedUsers(ctx, sweepBatch)
	if err != nil {
		c.Logger.WarnContext(ctx, "barrido del Club: no se pudieron leer los puntos pendientes", "error", err.Error())
		return
	}
	for _, p := range credits {
		link, err := c.Repo.GetClubLink(ctx, p.UserID)
		if err != nil {
			continue
		}
		if err := c.credit(ctx, link, p); errors.Is(err, club.ErrUnavailable) {
			return
		}
	}
}

// SweepInterval es cada cuánto corre el barrido.
const SweepInterval = time.Minute

// RunSweeper corre el barrido hasta que se cancele ctx.
func (c *Club) RunSweeper(ctx context.Context) {
	t := time.NewTicker(SweepInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			c.Sweep(ctx)
		}
	}
}
