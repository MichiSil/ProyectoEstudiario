package repository

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/MichiSil/ProyectoEstudiario/services/users-service/internal/model"
)

// ---------- Vinculación con el Club (RF-28, ADR-009) ----------

// CreateClubLink guarda la vinculación si el usuario no tenía una y devuelve
// la que quedó guardada: la nueva o la que ya existía.
func (p *Postgres) CreateClubLink(ctx context.Context, l model.ClubLink) (model.ClubLink, error) {
	err := p.run(ctx, func(ctx context.Context) error {
		_, err := p.pool.Exec(ctx,
			`INSERT INTO club_links (user_id, external_user_id, event_id, status, created_at, updated_at)
			 VALUES ($1, $2, $3, $4, $5, $5) ON CONFLICT (user_id) DO NOTHING`,
			l.UserID, l.ExternalUserID, l.EventID, l.Status, l.CreatedAt)
		return err
	})
	if err != nil {
		return model.ClubLink{}, err
	}
	return p.GetClubLink(ctx, l.UserID)
}

func (p *Postgres) GetClubLink(ctx context.Context, userID string) (l model.ClubLink, err error) {
	err = p.run(ctx, func(ctx context.Context) error {
		err := p.pool.QueryRow(ctx,
			`SELECT user_id, external_user_id, event_id, status, created_at, updated_at FROM club_links WHERE user_id = $1`, userID).
			Scan(&l.UserID, &l.ExternalUserID, &l.EventID, &l.Status, &l.CreatedAt, &l.UpdatedAt)
		if errors.Is(err, pgx.ErrNoRows) {
			return model.ErrNotFound
		}
		return err
	})
	return l, err
}

// ActivateClubLink marca la vinculación como completa. Devuelve true sólo la
// primera vez, para que los puntos pendientes se acrediten una sola vez.
func (p *Postgres) ActivateClubLink(ctx context.Context, userID string, at time.Time) (changed bool, err error) {
	err = p.run(ctx, func(ctx context.Context) error {
		tag, err := p.pool.Exec(ctx,
			`UPDATE club_links SET status = 'vinculada', updated_at = $2 WHERE user_id = $1 AND status = 'pendiente'`, userID, at)
		changed = tag.RowsAffected() == 1
		return err
	})
	return changed, err
}

// PendingClubLinks devuelve las vinculaciones que esperan la confirmación del Club.
func (p *Postgres) PendingClubLinks(ctx context.Context, limit int) ([]model.ClubLink, error) {
	var out []model.ClubLink
	err := p.run(ctx, func(ctx context.Context) error {
		rows, err := p.pool.Query(ctx,
			`SELECT user_id, external_user_id, event_id, status, created_at, updated_at
			 FROM club_links WHERE status = 'pendiente' ORDER BY updated_at LIMIT $1`, limit)
		if err != nil {
			return err
		}
		out, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (l model.ClubLink, err error) {
			err = r.Scan(&l.UserID, &l.ExternalUserID, &l.EventID, &l.Status, &l.CreatedAt, &l.UpdatedAt)
			return l, err
		})
		return err
	})
	return out, err
}

// ---------- Puntos (RN-42, RN-43) ----------

// AddClubCredit guarda los puntos a acreditar. Si ya existían con el mismo
// eventId no hace nada y devuelve false: así un resumen suma una sola vez (RN-42).
func (p *Postgres) AddClubCredit(ctx context.Context, c model.ClubCredit) (added bool, err error) {
	err = p.run(ctx, func(ctx context.Context) error {
		tag, err := p.pool.Exec(ctx,
			`INSERT INTO club_credits (event_id, user_id, reference, points, reason, status, created_at)
			 VALUES ($1, $2, $3, $4, $5, 'pendiente', $6) ON CONFLICT (event_id) DO NOTHING`,
			c.EventID, c.UserID, c.Reference, c.Points, c.Reason, c.CreatedAt)
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23503" { // foreign_key_violation: el usuario no existe
			return model.ErrNotFound
		}
		added = tag.RowsAffected() == 1
		return err
	})
	return added, err
}

// PendingClubCredits devuelve los puntos sin acreditar de un usuario, los más viejos primero.
func (p *Postgres) PendingClubCredits(ctx context.Context, userID string) ([]model.ClubCredit, error) {
	return p.credits(ctx, `WHERE status = 'pendiente' AND user_id = $1 ORDER BY created_at`, userID)
}

// PendingCreditsOfLinkedUsers devuelve puntos sin acreditar de usuarios ya
// vinculados: los que quedaron así porque el Club no respondió (RN-43).
func (p *Postgres) PendingCreditsOfLinkedUsers(ctx context.Context, limit int) ([]model.ClubCredit, error) {
	return p.credits(ctx,
		`WHERE status = 'pendiente' AND user_id IN (SELECT user_id FROM club_links WHERE status = 'vinculada')
		 ORDER BY created_at LIMIT $1`, limit)
}

// SetClubCreditResult deja la acreditación como acreditada o rechazada.
func (p *Postgres) SetClubCreditResult(ctx context.Context, eventID, status, movementID, lastError string, at time.Time) error {
	return p.run(ctx, func(ctx context.Context) error {
		_, err := p.pool.Exec(ctx,
			`UPDATE club_credits SET status = $2, movement_id = NULLIF($3, ''), last_error = NULLIF($4, ''),
			        credited_at = CASE WHEN $2 = 'acreditado' THEN $5 ELSE credited_at END
			 WHERE event_id = $1 AND status = 'pendiente'`,
			eventID, status, movementID, lastError, at)
		return err
	})
}

func (p *Postgres) credits(ctx context.Context, where string, arg any) ([]model.ClubCredit, error) {
	var out []model.ClubCredit
	err := p.run(ctx, func(ctx context.Context) error {
		rows, err := p.pool.Query(ctx,
			`SELECT event_id, user_id, reference, points, reason, status, COALESCE(movement_id, ''), created_at, credited_at
			 FROM club_credits `+where, arg)
		if err != nil {
			return err
		}
		out, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (c model.ClubCredit, err error) {
			err = r.Scan(&c.EventID, &c.UserID, &c.Reference, &c.Points, &c.Reason, &c.Status, &c.MovementID, &c.CreatedAt, &c.CreditedAt)
			return c, err
		})
		return err
	})
	return out, err
}

// ---------- Eventos procesados (ADR-005, 3.4) ----------

// GetProcessedEvent devuelve el estado de un evento, o "" si nunca se procesó.
func (p *Postgres) GetProcessedEvent(ctx context.Context, eventID string) (status, userID string, err error) {
	err = p.run(ctx, func(ctx context.Context) error {
		err := p.pool.QueryRow(ctx,
			`SELECT status, COALESCE(user_id, '') FROM processed_events WHERE event_id = $1`, eventID).Scan(&status, &userID)
		if errors.Is(err, pgx.ErrNoRows) {
			status, userID = "", ""
			return nil
		}
		return err
	})
	return status, userID, err
}

// SaveProcessedEvent guarda o actualiza el estado de un evento.
func (p *Postgres) SaveProcessedEvent(ctx context.Context, eventID, eventType, status, userID string, at time.Time) error {
	return p.run(ctx, func(ctx context.Context) error {
		_, err := p.pool.Exec(ctx,
			`INSERT INTO processed_events (event_id, event_type, status, user_id, processed_at)
			 VALUES ($1, $2, $3, NULLIF($4, ''), $5)
			 ON CONFLICT (event_id) DO UPDATE SET status = EXCLUDED.status, user_id = COALESCE(EXCLUDED.user_id, processed_events.user_id), processed_at = EXCLUDED.processed_at`,
			eventID, eventType, status, userID, at)
		return err
	})
}
