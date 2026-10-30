// Package service es la capa de reglas de forum-service: publicación,
// moderación, calificaciones y búsqueda de resúmenes (SPEC 7), y el registro y
// la consulta de documentos de otros sistemas (SPEC 11.1, ADR-014).
package service

import (
	"bytes"
	"context"
	"crypto/rand"
	"fmt"
	"io"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/MichiSil/ProyectoEstudiario/services/forum-service/internal/model"
)

// MaxFileSize es el tamaño máximo de un PDF (RN-20).
const MaxFileSize = 10 << 20

// PDFContentType es el único tipo de archivo aceptado (RN-20).
const PDFContentType = "application/pdf"

// Repository es lo que la capa de servicio necesita de la persistencia.
type Repository interface {
	Create(ctx context.Context, s model.Summary) error
	Get(ctx context.Context, id string) (model.Summary, error)
	ListByUser(ctx context.Context, userID string, page, size int) (model.Page, error)
	ListByConsumer(ctx context.Context, consumer string, status model.Status, page, size int) (model.Page, error)
	ListPending(ctx context.Context, page, size int) (model.Page, error)
	Moderate(ctx context.Context, id string, mod model.Moderation) error
	Delete(ctx context.Context, id, by string, at time.Time, event model.OutboxEvent) error
	Rate(ctx context.Context, summaryID, userID string, score int, at time.Time) (model.Rating, error)
	ReserveIdempotency(ctx context.Context, rec model.IdempotencyRecord) (model.IdempotencyRecord, bool, error)
	ClaimStaleIdempotency(ctx context.Context, rec model.IdempotencyRecord, olderThan time.Time) (bool, error)
	CompleteIdempotency(ctx context.Context, id, documentID string) error
	ReleaseIdempotency(ctx context.Context, id string) error
}

// SummarySearch busca entre los resúmenes aprobados (RF-19, RF-20). Hoy la
// implementa MongoDB; la de Solr (ADR-006) la reemplaza sin tocar el servicio.
type SummarySearch interface {
	Search(ctx context.Context, q model.SearchQuery) (model.Page, error)
}

// FileStore guarda los PDF (MinIO, ADR-003).
type FileStore interface {
	Put(ctx context.Context, key string, r io.Reader, size int64, contentType string) error
	Get(ctx context.Context, key string) (io.ReadCloser, error)
	Delete(ctx context.Context, key string) error
}

// Service implementa los casos de uso del foro y de la capacidad externa.
type Service struct {
	Repo   Repository
	Search SummarySearch
	Files  FileStore
	Now    func() time.Time
	NewID  func() string
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}

func (s *Service) newID() string {
	if s.NewID != nil {
		return s.NewID()
	}
	return strings.ToLower(rand.Text())
}

// Viewer es quien hace el pedido, para decidir qué puede ver.
//   - Estudiante o administrador del foro: UserID (y Admin si es administrador).
//   - Sistema externo: Consumer, el consumidor de la API key.
type Viewer struct {
	UserID   string
	Admin    bool
	Consumer string
}

// canSee aplica la visibilidad de RN-24 y RN-39: un eliminado no lo ve nadie;
// un aprobado lo ve cualquiera; uno pendiente o rechazado lo ve su autor (el
// estudiante o la API key que lo registró) y, en el foro, un administrador.
func canSee(v Viewer, s model.Summary) bool {
	switch {
	case !s.Visible():
		return false
	case s.Status == model.StatusApproved:
		return true
	case v.UserID != "" && v.Admin:
		return true
	case v.UserID != "" && s.Author.UserID == v.UserID:
		return true
	case v.Consumer != "" && s.Author.Consumer == v.Consumer:
		return true
	}
	return false
}

// ---------- Publicación ----------

// NewDocument son los datos de un resumen o documento nuevo.
type NewDocument struct {
	Title       string
	Description string
	Category    string
	Tags        string // separadas por comas, como en el formulario
	Author      model.Author
	Source      model.Source
}

// File es el archivo subido, ya leído (como máximo MaxFileSize + 1 bytes).
type File struct {
	Name        string
	ContentType string
	Data        []byte
}

// CheckPDF valida RN-20: como máximo 10 MB y que sea un PDF. Se verifica el
// tipo declarado y también el contenido (un PDF empieza con "%PDF-"), para
// que no alcance con cambiarle la extensión a otro archivo.
func CheckPDF(f File) error {
	if len(f.Data) > MaxFileSize {
		return model.ErrFileTooLarge
	}
	ct := strings.ToLower(strings.TrimSpace(strings.Split(f.ContentType, ";")[0]))
	if ct != PDFContentType || !bytes.HasPrefix(f.Data, []byte("%PDF-")) {
		return model.ErrUnsupportedFileType
	}
	return nil
}

// Publish valida y guarda un documento nuevo, que queda pendiente (RN-19).
// Primero sube el PDF a MinIO y recién después crea el documento en MongoDB
// (ADR-003): nunca queda un documento sin archivo. Si falla la creación, se
// intenta borrar el archivo; si eso también falla, queda huérfano y lo borra
// la limpieza periódica.
func (s *Service) Publish(ctx context.Context, in NewDocument, f File) (model.Summary, error) {
	doc, err := s.prepare(in, f)
	if err != nil {
		return model.Summary{}, err
	}
	if err := s.Files.Put(ctx, doc.File.Key, bytes.NewReader(f.Data), int64(len(f.Data)), PDFContentType); err != nil {
		return model.Summary{}, err
	}
	if err := s.Repo.Create(ctx, doc); err != nil {
		_ = s.Files.Delete(context.WithoutCancel(ctx), doc.File.Key)
		return model.Summary{}, err
	}
	return doc, nil
}

// prepare valida los datos (con los límites del contrato documents-v1) y arma
// el documento.
func (s *Service) prepare(in NewDocument, f File) (model.Summary, error) {
	title := strings.TrimSpace(in.Title)
	category := strings.TrimSpace(in.Category)
	description := strings.TrimSpace(in.Description)
	authorName := strings.TrimSpace(in.Author.Name)

	var fields []model.FieldError
	check := func(field, value string, min, max int, msg string) {
		if n := utf8.RuneCountInString(value); n < min || n > max {
			fields = append(fields, model.FieldError{Field: field, Message: msg})
		}
	}
	check("title", title, 3, 150, "tiene que tener entre 3 y 150 caracteres")
	check("category", category, 2, 120, "tiene que tener entre 2 y 120 caracteres")
	check("description", description, 0, 2000, "tiene como máximo 2000 caracteres")
	check("authorName", authorName, 2, 100, "tiene que tener entre 2 y 100 caracteres")
	check("authorExternalId", in.Author.ExternalID, 0, 100, "tiene como máximo 100 caracteres")
	tags, tagKeys, tagErr := parseTags(in.Tags)
	if tagErr != "" {
		fields = append(fields, model.FieldError{Field: "tags", Message: tagErr})
	}
	if len(fields) > 0 {
		return model.Summary{}, &model.ValidationError{Fields: fields}
	}
	if err := CheckPDF(f); err != nil {
		return model.Summary{}, err
	}

	now, id := s.now(), s.newID()
	author := in.Author
	author.Name = authorName
	return model.Summary{
		ID: id, Title: title, Description: description,
		Category: category, CategoryKey: model.NormalizeKey(category),
		Tags: tags, TagKeys: tagKeys,
		Author: author, Source: in.Source,
		Status: model.StatusPending, // RN-19
		File: model.FileRef{
			Key: id + ".pdf", ContentType: PDFContentType,
			SizeBytes: int64(len(f.Data)), FileName: fileName(f.Name, title),
		},
		CreatedAt: now, UpdatedAt: now,
		Outbox: []model.OutboxEvent{},
	}, nil
}

// parseTags separa las etiquetas por comas: sin espacios a los costados, sin
// repetir, hasta 10 y de 2 a 40 caracteres cada una (contrato documents-v1).
func parseTags(raw string) (tags, keys []string, problem string) {
	tags, keys = []string{}, []string{}
	if utf8.RuneCountInString(raw) > 300 {
		return nil, nil, "tiene como máximo 300 caracteres"
	}
	seen := map[string]bool{}
	for _, t := range strings.Split(raw, ",") {
		t = strings.TrimSpace(t)
		if t == "" {
			continue
		}
		if n := utf8.RuneCountInString(t); n < 2 || n > 40 {
			return nil, nil, "cada etiqueta tiene que tener entre 2 y 40 caracteres"
		}
		k := model.NormalizeKey(t)
		if seen[k] {
			continue
		}
		seen[k] = true
		tags, keys = append(tags, t), append(keys, k)
	}
	if len(tags) > 10 {
		return nil, nil, "se permiten hasta 10 etiquetas"
	}
	return tags, keys, ""
}

// fileName es el nombre sugerido al descargar: el del archivo subido o, si no
// vino, el título.
func fileName(uploaded, title string) string {
	name := strings.TrimSpace(uploaded)
	if name == "" {
		name = title
	}
	name = strings.Map(func(r rune) rune {
		if r == '"' || r == '\\' || r == '/' || r < 32 {
			return '-'
		}
		return r
	}, name)
	if !strings.HasSuffix(strings.ToLower(name), ".pdf") {
		name += ".pdf"
	}
	return name
}

// ---------- Lectura ----------

// Get devuelve un resumen si el viewer lo puede ver; si no, ErrNotFound (no se
// revela que existe).
func (s *Service) Get(ctx context.Context, v Viewer, id string) (model.Summary, error) {
	doc, err := s.Repo.Get(ctx, id)
	if err != nil {
		return model.Summary{}, err
	}
	if !canSee(v, doc) {
		return model.Summary{}, model.ErrNotFound
	}
	return doc, nil
}

// Download abre el PDF de un resumen, con la misma visibilidad que el detalle.
// La descarga pasa siempre por forum-service (ADR-003, ADR-014).
func (s *Service) Download(ctx context.Context, v Viewer, id string) (model.Summary, io.ReadCloser, error) {
	doc, err := s.Get(ctx, v, id)
	if err != nil {
		return model.Summary{}, nil, err
	}
	r, err := s.Files.Get(ctx, doc.File.Key)
	if err != nil {
		return model.Summary{}, nil, err
	}
	return doc, r, nil
}

// newEventID genera el eventId de un evento: un UUID v4 (ADR-005, 3.2), que
// es la clave con la que los consumidores descartan duplicados.
func (s *Service) newEventID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = (b[6] & 0x0f) | 0x40 // versión 4
	b[8] = (b[8] & 0x3f) | 0x80 // variante RFC 4122
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
