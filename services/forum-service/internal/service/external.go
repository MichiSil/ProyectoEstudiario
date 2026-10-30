package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/MichiSil/ProyectoEstudiario/services/forum-service/internal/model"
)

// staleAfter es cuánto se espera a un registro en curso con la misma clave
// antes de considerar que se cortó sin terminar y tomarlo.
const staleAfter = time.Minute

// Register registra un documento de un sistema externo (RF-25). Queda
// pendiente y lo moderan los mismos administradores (RN-38).
//
// La Idempotency-Key, por API key, evita registrarlo dos veces (RN-40):
//   - misma clave y mismo cuerpo: devuelve el documento ya registrado y replayed = true;
//   - misma clave y otro cuerpo: ErrIdempotencyReused;
//   - misma clave mientras el primer pedido se procesa: ErrRequestInProgress.
func (s *Service) Register(ctx context.Context, consumer, idemKey string, in NewDocument, f File) (doc model.Summary, replayed bool, err error) {
	in.Source = model.SourceExternal
	in.Author.Consumer = consumer
	in.Author.UserID = ""
	in.Author.Email = "" // su autor no tiene cuenta en Estudiario (RN-41)

	rec := model.IdempotencyRecord{ID: consumer + ":" + idemKey, RequestHash: requestHash(in, f), CreatedAt: s.now()}
	existing, reserved, err := s.Repo.ReserveIdempotency(ctx, rec)
	if err != nil {
		return model.Summary{}, false, err
	}
	if !reserved {
		switch {
		case existing.RequestHash != rec.RequestHash:
			return model.Summary{}, false, model.ErrIdempotencyReused
		case existing.Done:
			doc, err := s.Get(ctx, Viewer{Consumer: consumer}, existing.DocumentID)
			return doc, true, err
		case existing.CreatedAt.After(s.now().Add(-staleAfter)):
			return model.Summary{}, false, model.ErrRequestInProgress
		}
		// El primer pedido se cortó sin terminar: se toma la reserva.
		claimed, err := s.Repo.ClaimStaleIdempotency(ctx, rec, s.now().Add(-staleAfter))
		if err != nil {
			return model.Summary{}, false, err
		}
		if !claimed {
			return model.Summary{}, false, model.ErrRequestInProgress
		}
	}

	doc, err = s.Publish(ctx, in, f)
	if err != nil {
		// Se libera la clave para que el consumidor pueda reintentar con la misma.
		_ = s.Repo.ReleaseIdempotency(context.WithoutCancel(ctx), rec.ID)
		return model.Summary{}, false, err
	}
	if err := s.Repo.CompleteIdempotency(ctx, rec.ID, doc.ID); err != nil {
		// El documento ya está creado; si no se pudo marcar la clave, un
		// reintento recibe REQUEST_IN_PROGRESS durante un minuto. Se informa el error.
		return model.Summary{}, false, errors.Join(fmt.Errorf("documento %s creado, pero no se pudo registrar la clave de idempotencia", doc.ID), err)
	}
	return doc, false, nil
}

// requestHash resume el cuerpo del registro, para saber si un reintento con la
// misma clave trae el mismo documento.
func requestHash(in NewDocument, f File) string {
	h := sha256.New()
	for _, v := range []string{in.Title, in.Description, in.Category, in.Tags, in.Author.Name, in.Author.ExternalID, f.ContentType} {
		fmt.Fprintf(h, "%d:%s|", len(v), v)
	}
	file := sha256.Sum256(f.Data)
	h.Write(file[:])
	return hex.EncodeToString(h.Sum(nil))
}

// Submissions lista lo registrado con una API key, en cualquier estado (RF-27).
func (s *Service) Submissions(ctx context.Context, consumer string, status model.Status, page, size int) (model.Page, error) {
	return s.Repo.ListByConsumer(ctx, consumer, status, page, size)
}
