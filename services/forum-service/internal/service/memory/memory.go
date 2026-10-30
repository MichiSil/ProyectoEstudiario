// Package memory tiene implementaciones en memoria del repositorio, la
// búsqueda y el almacenamiento de archivos, para los tests unitarios y el test
// de contrato. Respetan las mismas reglas que las reales: la moderación es
// condicional (RN-23), hay una calificación por estudiante (RN-30) y una
// reserva por clave de idempotencia (RN-40).
package memory

import (
	"bytes"
	"context"
	"io"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/MichiSil/ProyectoEstudiario/services/forum-service/internal/model"
)

// Store guarda resúmenes, calificaciones e idempotencia. También implementa
// la búsqueda (SummarySearch) sobre los mismos datos.
type Store struct {
	mu          sync.Mutex
	summaries   map[string]model.Summary
	ratings     map[string]map[string]int // summaryId → userId → puntaje
	idempotency map[string]model.IdempotencyRecord
	// Err, si no es nil, hace fallar todas las operaciones (por ejemplo, con
	// model.ErrUnavailable para simular la base caída).
	Err error
}

func NewStore() *Store {
	return &Store{
		summaries:   map[string]model.Summary{},
		ratings:     map[string]map[string]int{},
		idempotency: map[string]model.IdempotencyRecord{},
	}
}

func (s *Store) Ping(context.Context) error { return s.Err }

func (s *Store) Create(_ context.Context, doc model.Summary) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.Err != nil {
		return s.Err
	}
	s.summaries[doc.ID] = doc
	return nil
}

func (s *Store) Get(_ context.Context, id string) (model.Summary, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.Err != nil {
		return model.Summary{}, s.Err
	}
	doc, ok := s.summaries[id]
	if !ok {
		return model.Summary{}, model.ErrNotFound
	}
	return doc, nil
}

func (s *Store) ListByUser(_ context.Context, userID string, page, size int) (model.Page, error) {
	return s.list(func(d model.Summary) bool { return d.Author.UserID == userID && d.Visible() },
		func(a, b model.Summary) bool { return a.CreatedAt.After(b.CreatedAt) }, page, size)
}

func (s *Store) ListByConsumer(_ context.Context, consumer string, status model.Status, page, size int) (model.Page, error) {
	return s.list(func(d model.Summary) bool {
		return d.Author.Consumer == consumer && d.Visible() && (status == "" || d.Status == status)
	}, func(a, b model.Summary) bool { return a.CreatedAt.After(b.CreatedAt) }, page, size)
}

func (s *Store) ListPending(_ context.Context, page, size int) (model.Page, error) {
	return s.list(func(d model.Summary) bool { return d.Status == model.StatusPending },
		func(a, b model.Summary) bool { return a.CreatedAt.Before(b.CreatedAt) }, page, size)
}

func (s *Store) list(match func(model.Summary) bool, less func(a, b model.Summary) bool, page, size int) (model.Page, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.Err != nil {
		return model.Page{}, s.Err
	}
	var all []model.Summary
	for _, d := range s.summaries {
		if match(d) {
			all = append(all, d)
		}
	}
	sort.SliceStable(all, func(i, j int) bool { return less(all[i], all[j]) })
	return paginate(all, page, size), nil
}

func paginate(all []model.Summary, page, size int) model.Page {
	p := model.Page{Page: page, Size: size, Total: len(all), Items: []model.Summary{}}
	from := (page - 1) * size
	if from < len(all) {
		p.Items = all[from:min(from+size, len(all))]
	}
	return p
}

// Moderate cambia el estado sólo si está pendiente, como la actualización
// condicional de MongoDB.
func (s *Store) Moderate(_ context.Context, id string, mod model.Moderation) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.Err != nil {
		return s.Err
	}
	doc, ok := s.summaries[id]
	switch {
	case !ok || !doc.Visible():
		return model.ErrNotFound
	case doc.Status != model.StatusPending:
		return model.ErrAlreadyModerated
	}
	doc.Status, doc.ModeratedBy, doc.UpdatedAt = mod.To, mod.By, mod.At
	at := mod.At
	doc.ModeratedAt = &at
	if mod.To == model.StatusApproved {
		doc.PublishedAt = &at
	} else {
		doc.RejectionReason = mod.Reason
	}
	doc.Outbox = append(append([]model.OutboxEvent{}, doc.Outbox...), mod.Event)
	s.summaries[id] = doc
	return nil
}

func (s *Store) Delete(_ context.Context, id, by string, at time.Time, event model.OutboxEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.Err != nil {
		return s.Err
	}
	doc, ok := s.summaries[id]
	if !ok || !doc.Visible() {
		return model.ErrNotFound
	}
	doc.Status, doc.DeletedAt, doc.ModeratedBy, doc.UpdatedAt = model.StatusDeleted, &at, by, at
	doc.Outbox = append(append([]model.OutboxEvent{}, doc.Outbox...), event)
	s.summaries[id] = doc
	return nil
}

func (s *Store) Rate(_ context.Context, summaryID, userID string, score int, _ time.Time) (model.Rating, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.Err != nil {
		return model.Rating{}, s.Err
	}
	if s.ratings[summaryID] == nil {
		s.ratings[summaryID] = map[string]int{}
	}
	s.ratings[summaryID][userID] = score // reemplaza el anterior (RN-30)
	var r model.Rating
	for _, sc := range s.ratings[summaryID] {
		r.Average += float64(sc)
		r.Count++
	}
	r.Average /= float64(r.Count)
	doc := s.summaries[summaryID]
	doc.Rating = r
	s.summaries[summaryID] = doc
	return r, nil
}

func (s *Store) ReserveIdempotency(_ context.Context, rec model.IdempotencyRecord) (model.IdempotencyRecord, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.Err != nil {
		return model.IdempotencyRecord{}, false, s.Err
	}
	if existing, ok := s.idempotency[rec.ID]; ok {
		return existing, false, nil
	}
	s.idempotency[rec.ID] = rec
	return model.IdempotencyRecord{}, true, nil
}

func (s *Store) ClaimStaleIdempotency(_ context.Context, rec model.IdempotencyRecord, olderThan time.Time) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	existing, ok := s.idempotency[rec.ID]
	if !ok || existing.Done || !existing.CreatedAt.Before(olderThan) {
		return false, nil
	}
	s.idempotency[rec.ID] = rec
	return true, nil
}

func (s *Store) CompleteIdempotency(_ context.Context, id, documentID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec := s.idempotency[id]
	rec.Done, rec.DocumentID = true, documentID
	s.idempotency[id] = rec
	return nil
}

func (s *Store) ReleaseIdempotency(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.idempotency[id].Done {
		delete(s.idempotency, id)
	}
	return nil
}

// Search es una búsqueda simple entre los aprobados: texto contenido en el
// título, la descripción o la categoría, con los mismos filtros y órdenes.
func (s *Store) Search(_ context.Context, q model.SearchQuery) (model.Page, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.Err != nil {
		return model.Page{}, s.Err
	}
	text := model.NormalizeKey(q.Text)
	var all []model.Summary
	for _, d := range s.summaries {
		if d.Status != model.StatusApproved ||
			(q.Category != "" && d.CategoryKey != q.Category) ||
			(q.MinRating > 0 && d.Rating.Average < q.MinRating) ||
			(text != "" && !strings.Contains(model.NormalizeKey(d.Title+" "+d.Description+" "+d.Category), text)) ||
			!hasAll(d.TagKeys, q.Tags) {
			continue
		}
		all = append(all, d)
	}
	sort.SliceStable(all, func(i, j int) bool {
		a, b := all[i], all[j]
		if q.Sort == model.SortRating && a.Rating.Average != b.Rating.Average {
			return a.Rating.Average > b.Rating.Average
		}
		if q.Sort == model.SortRating && a.Rating.Count != b.Rating.Count {
			return a.Rating.Count > b.Rating.Count
		}
		return a.PublishedAt != nil && b.PublishedAt != nil && a.PublishedAt.After(*b.PublishedAt)
	})
	return paginate(all, q.Page, q.Size), nil
}

func hasAll(have, want []string) bool {
	for _, w := range want {
		found := false
		for _, h := range have {
			found = found || h == w
		}
		if !found {
			return false
		}
	}
	return true
}

// Files guarda archivos en memoria. Err, si no es nil, hace fallar Put.
type Files struct {
	mu   sync.Mutex
	data map[string][]byte
	Err  error
}

func NewFiles() *Files { return &Files{data: map[string][]byte{}} }

func (f *Files) Put(_ context.Context, key string, r io.Reader, _ int64, _ string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Err != nil {
		return f.Err
	}
	b, err := io.ReadAll(r)
	f.data[key] = b
	return err
}

func (f *Files) Get(_ context.Context, key string) (io.ReadCloser, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	b, ok := f.data[key]
	if !ok {
		return nil, model.ErrNotFound
	}
	return io.NopCloser(bytes.NewReader(b)), nil
}

func (f *Files) Delete(_ context.Context, key string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.data, key)
	return nil
}

// Has dice si el archivo está guardado.
func (f *Files) Has(key string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	_, ok := f.data[key]
	return ok
}
