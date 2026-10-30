package service_test

import (
	"context"
	"errors"
	"io"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/MichiSil/ProyectoEstudiario/services/forum-service/internal/model"
	"github.com/MichiSil/ProyectoEstudiario/services/forum-service/internal/service"
	"github.com/MichiSil/ProyectoEstudiario/services/forum-service/internal/service/memory"
)

var (
	ctx     = context.Background()
	admin   = service.Viewer{UserID: "admin-1", Admin: true}
	ana     = service.Viewer{UserID: "ana"}
	beto    = service.Viewer{UserID: "beto"}
	grupo4  = service.Viewer{Consumer: "grupo4"}
	grupo7  = service.Viewer{Consumer: "grupo7"}
	pdf     = service.File{Name: "resumen.pdf", ContentType: "application/pdf", Data: []byte("%PDF-1.4 contenido")}
	uuidV4  = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	student = service.NewDocument{Title: "Integrales dobles", Category: "Análisis II", Tags: "parcial, integrales", Author: model.Author{UserID: "ana", Name: "Ana"}, Source: model.SourceEstudiario}
)

func newService() (*service.Service, *memory.Store, *memory.Files) {
	store, files := memory.NewStore(), memory.NewFiles()
	return &service.Service{Repo: store, Search: store, Files: files}, store, files
}

func publish(t *testing.T, svc *service.Service, in service.NewDocument) model.Summary {
	t.Helper()
	doc, err := svc.Publish(ctx, in, pdf)
	if err != nil {
		t.Fatal(err)
	}
	return doc
}

// ---------- Publicación y PDF (RN-19, RN-20) ----------

func TestPublicarQuedaPendienteConElArchivoGuardado(t *testing.T) {
	svc, store, files := newService()
	doc := publish(t, svc, student)
	if doc.Status != model.StatusPending {
		t.Errorf("estado = %q, se esperaba pendiente (RN-19)", doc.Status)
	}
	if !files.Has(doc.File.Key) {
		t.Error("el PDF no quedó guardado")
	}
	if got, _ := store.Get(ctx, doc.ID); got.CategoryKey != "analisis ii" || len(got.Tags) != 2 {
		t.Errorf("categoryKey = %q, tags = %v", got.CategoryKey, got.Tags)
	}
}

func TestValidacionDelPDF(t *testing.T) {
	big := append([]byte("%PDF-"), make([]byte, service.MaxFileSize)...)
	casos := []struct {
		nombre string
		file   service.File
		want   error
	}{
		{"PDF válido", pdf, nil},
		{"más de 10 MB", service.File{ContentType: "application/pdf", Data: big}, model.ErrFileTooLarge},
		{"tipo declarado distinto", service.File{ContentType: "text/plain", Data: pdf.Data}, model.ErrUnsupportedFileType},
		{"dice PDF pero el contenido no lo es", service.File{ContentType: "application/pdf", Data: []byte("hola")}, model.ErrUnsupportedFileType},
		{"tipo con parámetros", service.File{ContentType: "application/pdf; charset=binary", Data: pdf.Data}, nil},
	}
	for _, c := range casos {
		if err := service.CheckPDF(c.file); !errors.Is(err, c.want) && !(c.want == nil && err == nil) {
			t.Errorf("%s: err = %v, se esperaba %v", c.nombre, err, c.want)
		}
	}
}

func TestSiFallaMinIONoSeCreaElDocumento(t *testing.T) {
	svc, store, files := newService()
	files.Err = model.ErrUnavailable
	if _, err := svc.Publish(ctx, student, pdf); !errors.Is(err, model.ErrUnavailable) {
		t.Fatalf("err = %v, se esperaba ErrUnavailable", err)
	}
	if p, _ := store.ListByUser(ctx, "ana", 1, 10); p.Total != 0 {
		t.Error("se creó un documento sin archivo (ADR-003)")
	}
}

func TestSiFallaMongoSeBorraElArchivo(t *testing.T) {
	svc, store, files := newService()
	svc.NewID = func() string { return "fijo12345" }
	store.Err = model.ErrUnavailable
	if _, err := svc.Publish(ctx, student, pdf); !errors.Is(err, model.ErrUnavailable) {
		t.Fatalf("err = %v, se esperaba ErrUnavailable", err)
	}
	if files.Has("fijo12345.pdf") {
		t.Error("el PDF quedó guardado aunque no se creó el documento")
	}
}

func TestValidacionDeLosCampos(t *testing.T) {
	svc, _, _ := newService()
	in := student
	in.Title, in.Category, in.Tags = "ab", "x", "a,"+strings.Repeat("t", 41)
	_, err := svc.Publish(ctx, in, pdf)
	var verr *model.ValidationError
	if !errors.As(err, &verr) {
		t.Fatalf("err = %v, se esperaba ValidationError", err)
	}
	campos := map[string]bool{}
	for _, f := range verr.Fields {
		campos[f.Field] = true
	}
	for _, c := range []string{"title", "category", "tags"} {
		if !campos[c] {
			t.Errorf("falta el error de %s: %+v", c, verr.Fields)
		}
	}
}

// ---------- Moderación (RN-19 a RN-24) ----------

func TestAprobarRechazarYEliminar(t *testing.T) {
	svc, store, _ := newService()
	doc := publish(t, svc, student)

	if err := svc.Approve(ctx, ana, doc.ID); !errors.Is(err, model.ErrForbidden) {
		t.Errorf("un estudiante aprueba: err = %v, se esperaba ErrForbidden", err)
	}
	if err := svc.Approve(ctx, admin, doc.ID); err != nil {
		t.Fatal(err)
	}
	if err := svc.Approve(ctx, admin, doc.ID); !errors.Is(err, model.ErrAlreadyModerated) {
		t.Errorf("aprobar dos veces: err = %v, se esperaba ErrAlreadyModerated (RN-21)", err)
	}
	if err := svc.Reject(ctx, admin, doc.ID, "tarde"); !errors.Is(err, model.ErrAlreadyModerated) {
		t.Errorf("rechazar un aprobado: err = %v, se esperaba ErrAlreadyModerated (RN-21)", err)
	}
	if err := svc.Delete(ctx, admin, doc.ID); err != nil {
		t.Fatalf("eliminar un aprobado: %v (RF-18)", err)
	}
	if _, err := svc.Get(ctx, admin, doc.ID); !errors.Is(err, model.ErrNotFound) {
		t.Errorf("después de eliminar: err = %v, se esperaba ErrNotFound (RN-24)", err)
	}
	if err := svc.Delete(ctx, admin, doc.ID); !errors.Is(err, model.ErrNotFound) {
		t.Errorf("eliminar dos veces: err = %v, se esperaba ErrNotFound", err)
	}
	got, _ := store.Get(ctx, doc.ID)
	if len(got.Outbox) != 2 || got.Outbox[0].EventType != model.EventApproved || got.Outbox[1].EventType != model.EventDeleted {
		t.Errorf("outbox = %+v, se esperaban summary.approved y summary.deleted", got.Outbox)
	}
}

func TestRechazarExigeMotivo(t *testing.T) {
	svc, store, _ := newService()
	doc := publish(t, svc, student)
	var verr *model.ValidationError
	if err := svc.Reject(ctx, admin, doc.ID, "   "); !errors.As(err, &verr) {
		t.Fatalf("sin motivo: err = %v, se esperaba ValidationError (RN-22, CA-17)", err)
	}
	if err := svc.Reject(ctx, admin, doc.ID, "El PDF está incompleto"); err != nil {
		t.Fatal(err)
	}
	got, _ := store.Get(ctx, doc.ID)
	if got.Status != model.StatusRejected || got.RejectionReason != "El PDF está incompleto" {
		t.Errorf("estado = %q, motivo = %q", got.Status, got.RejectionReason)
	}
}

func TestDosModeracionesSimultaneasSoloUnaTieneEfecto(t *testing.T) {
	svc, store, _ := newService()
	doc := publish(t, svc, student)

	var wg sync.WaitGroup
	errs := make([]error, 20)
	for i := range errs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if i%2 == 0 {
				errs[i] = svc.Approve(ctx, admin, doc.ID)
			} else {
				errs[i] = svc.Reject(ctx, admin, doc.ID, "motivo")
			}
		}(i)
	}
	wg.Wait()

	ok := 0
	for _, err := range errs {
		if err == nil {
			ok++
		} else if !errors.Is(err, model.ErrAlreadyModerated) {
			t.Fatalf("error inesperado: %v", err)
		}
	}
	got, _ := store.Get(ctx, doc.ID)
	if ok != 1 || len(got.Outbox) != 1 {
		t.Fatalf("%d moderaciones con efecto y %d eventos; se esperaba 1 y 1 (RN-23, CA-18)", ok, len(got.Outbox))
	}
}

func TestFormatoDelEventoYMarcaDeExternos(t *testing.T) {
	svc, store, _ := newService()
	est := publish(t, svc, student)
	ext, _, err := svc.Register(ctx, "grupo4", "clave-0001", service.NewDocument{Title: "Guía", Category: "Patagonia", Author: model.Author{Name: "Lucía", ExternalID: "v-1"}}, pdf)
	if err != nil {
		t.Fatal(err)
	}
	_ = svc.Approve(ctx, admin, est.ID)
	_ = svc.Reject(ctx, admin, ext.ID, "no se lee")

	e, _ := store.Get(ctx, est.ID)
	ev := e.Outbox[0]
	if !uuidV4.MatchString(ev.EventID) || ev.EventVersion != 1 || ev.Producer != "forum-service" || ev.OccurredAt.IsZero() {
		t.Errorf("sobre del evento = %+v, se esperaba el formato del ADR-005", ev)
	}
	if ev.Data["summaryId"] != est.ID || ev.Data["notifyAuthor"] != true || ev.Data["awardBenefits"] != true || ev.Data["source"] != "estudiario" {
		t.Errorf("datos del aprobado de un estudiante = %+v", ev.Data)
	}
	x, _ := store.Get(ctx, ext.ID)
	ev = x.Outbox[0]
	if ev.EventType != model.EventRejected || ev.Data["notifyAuthor"] != false || ev.Data["source"] != "externo" || ev.Data["reason"] != "no se lee" {
		t.Errorf("datos del rechazo de un externo = %+v; un externo no genera mail (RN-41)", ev.Data)
	}
}

func TestPendientesDeLaMasViejaALaMasNueva(t *testing.T) {
	svc, _, _ := newService()
	now := time.Date(2026, 10, 1, 10, 0, 0, 0, time.UTC)
	svc.Now = func() time.Time { now = now.Add(time.Minute); return now }
	a, b := publish(t, svc, student), publish(t, svc, student)

	if _, err := svc.Pending(ctx, ana, 1, 10); !errors.Is(err, model.ErrForbidden) {
		t.Errorf("un estudiante pide pendientes: err = %v, se esperaba ErrForbidden", err)
	}
	p, _ := svc.Pending(ctx, admin, 1, 10)
	if len(p.Items) != 2 || p.Items[0].ID != a.ID || p.Items[1].ID != b.ID {
		t.Errorf("orden = %v, se esperaba de la más vieja a la más nueva (RF-16)", p.Items)
	}
}

// ---------- Calificaciones (RN-28 a RN-31) ----------

func TestCalificaciones(t *testing.T) {
	svc, _, _ := newService()
	doc := publish(t, svc, student)

	if _, err := svc.Rate(ctx, beto, doc.ID, 4); !errors.Is(err, model.ErrNotFound) {
		t.Errorf("calificar uno pendiente ajeno: err = %v, se esperaba ErrNotFound (no lo ve)", err)
	}
	if _, err := svc.Rate(ctx, ana, doc.ID, 4); !errors.Is(err, model.ErrNotApproved) {
		t.Errorf("calificar uno pendiente propio: err = %v, se esperaba ErrNotApproved (RN-28)", err)
	}
	_ = svc.Approve(ctx, admin, doc.ID)

	if _, err := svc.Rate(ctx, ana, doc.ID, 5); !errors.Is(err, model.ErrOwnSummary) {
		t.Errorf("calificar el propio: err = %v, se esperaba ErrOwnSummary (RN-29, CA-23)", err)
	}
	var verr *model.ValidationError
	if _, err := svc.Rate(ctx, beto, doc.ID, 6); !errors.As(err, &verr) {
		t.Errorf("puntaje 6: err = %v, se esperaba ValidationError", err)
	}
	_, _ = svc.Rate(ctx, beto, doc.ID, 2)
	r, err := svc.Rate(ctx, beto, doc.ID, 5)
	if err != nil || r.Count != 1 || r.Average != 5 {
		t.Fatalf("calificar 2 y después 5: rating = %+v, err = %v; se esperaba promedio 5 con 1 calificación (RN-30, CA-22)", r, err)
	}
}

// ---------- Visibilidad (RN-24, RN-39) ----------

func TestVisibilidadPorAPIKey(t *testing.T) {
	svc, _, _ := newService()
	doc, _, err := svc.Register(ctx, "grupo4", "clave-0001", service.NewDocument{Title: "Guía", Category: "Patagonia", Author: model.Author{Name: "Lucía"}}, pdf)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := svc.Get(ctx, grupo4, doc.ID); err != nil {
		t.Errorf("la API key que lo registró no ve su pendiente: %v", err)
	}
	if _, err := svc.Get(ctx, grupo7, doc.ID); !errors.Is(err, model.ErrNotFound) {
		t.Errorf("otra API key ve un pendiente ajeno: err = %v (RN-39, CA-33)", err)
	}
	if _, err := svc.Get(ctx, beto, doc.ID); !errors.Is(err, model.ErrNotFound) {
		t.Errorf("un estudiante ve un pendiente ajeno: err = %v", err)
	}
	if _, err := svc.Get(ctx, admin, doc.ID); err != nil {
		t.Errorf("un administrador no ve un pendiente: %v", err)
	}
	if p, _ := svc.SearchApproved(ctx, model.SearchQuery{Page: 1, Size: 10}); p.Total != 0 {
		t.Error("un pendiente aparece en la búsqueda (CA-31)")
	}

	_ = svc.Approve(ctx, admin, doc.ID)
	if _, err := svc.Get(ctx, grupo7, doc.ID); err != nil {
		t.Errorf("otra API key no ve un aprobado: %v (RN-39)", err)
	}
	if p, _ := svc.SearchApproved(ctx, model.SearchQuery{Page: 1, Size: 10}); p.Total != 1 {
		t.Error("un aprobado no aparece en la búsqueda (CA-32)")
	}

	_ = svc.Delete(ctx, admin, doc.ID)
	if _, err := svc.Get(ctx, grupo4, doc.ID); !errors.Is(err, model.ErrNotFound) {
		t.Errorf("un eliminado se sigue viendo: err = %v", err)
	}
}

// ---------- Idempotencia (RN-40) ----------

func TestIdempotenciaDelRegistro(t *testing.T) {
	svc, store, _ := newService()
	in := service.NewDocument{Title: "Guía", Category: "Patagonia", Author: model.Author{Name: "Lucía"}}

	first, replayed, err := svc.Register(ctx, "grupo4", "clave-0001", in, pdf)
	if err != nil || replayed {
		t.Fatalf("primer registro: replayed = %v, err = %v", replayed, err)
	}
	again, replayed, err := svc.Register(ctx, "grupo4", "clave-0001", in, pdf)
	if err != nil || !replayed || again.ID != first.ID {
		t.Fatalf("reintento: id = %q, replayed = %v, err = %v; se esperaba el mismo documento (CA-34)", again.ID, replayed, err)
	}
	if p, _ := store.ListByConsumer(ctx, "grupo4", "", 1, 10); p.Total != 1 {
		t.Errorf("hay %d documentos, se esperaba 1 (RN-40)", p.Total)
	}

	other := in
	other.Title = "Otra guía"
	if _, _, err := svc.Register(ctx, "grupo4", "clave-0001", other, pdf); !errors.Is(err, model.ErrIdempotencyReused) {
		t.Errorf("misma clave, otro cuerpo: err = %v, se esperaba ErrIdempotencyReused", err)
	}
	if _, replayed, err := svc.Register(ctx, "grupo7", "clave-0001", in, pdf); err != nil || replayed {
		t.Errorf("la misma clave de otra API key es otro registro: replayed = %v, err = %v", replayed, err)
	}
}

func TestIdempotenciaConUnRegistroEnCurso(t *testing.T) {
	store := memory.NewStore()
	blocking := &blockingFiles{Files: memory.NewFiles(), started: make(chan struct{}), release: make(chan struct{})}
	svc := &service.Service{Repo: store, Search: store, Files: blocking}
	in := service.NewDocument{Title: "Guía", Category: "Patagonia", Author: model.Author{Name: "Lucía"}}

	// El primer pedido reserva la clave y queda subiendo el PDF.
	done := make(chan model.Summary)
	go func() {
		doc, _, err := svc.Register(ctx, "grupo4", "clave-0001", in, pdf)
		if err != nil {
			t.Error(err)
		}
		done <- doc
	}()
	<-blocking.started

	// Un reintento mientras tanto recibe "en curso" (409) y no registra otro.
	if _, _, err := svc.Register(ctx, "grupo4", "clave-0001", in, pdf); !errors.Is(err, model.ErrRequestInProgress) {
		t.Fatalf("en curso: err = %v, se esperaba ErrRequestInProgress", err)
	}

	close(blocking.release)
	first := <-done
	again, replayed, err := svc.Register(ctx, "grupo4", "clave-0001", in, pdf)
	if err != nil || !replayed || again.ID != first.ID {
		t.Fatalf("después de terminar: id = %q, replayed = %v, err = %v; se esperaba el mismo documento", again.ID, replayed, err)
	}
}

// blockingFiles frena la primera subida hasta que se cierra release.
type blockingFiles struct {
	*memory.Files
	once    sync.Once
	started chan struct{}
	release chan struct{}
}

func (b *blockingFiles) Put(ctx context.Context, key string, r io.Reader, size int64, ct string) error {
	b.once.Do(func() {
		close(b.started)
		<-b.release
	})
	return b.Files.Put(ctx, key, r, size, ct)
}

func TestElEventoLlevaNombreYMailDelAutor(t *testing.T) {
	svc, store, _ := newService()
	in := student
	in.Author = model.Author{UserID: "ana", Name: "Ana Pérez", Email: "ana@mail.com"}
	doc := publish(t, svc, in)
	_ = svc.Approve(ctx, admin, doc.ID)

	got, _ := store.Get(ctx, doc.ID)
	author, _ := got.Outbox[0].Data["author"].(map[string]any)
	if author["id"] != "ana" || author["name"] != "Ana Pérez" || author["email"] != "ana@mail.com" {
		t.Errorf("author del evento = %v, se esperaban id, nombre y mail (ADR-005, 3.2)", author)
	}

	ext, _, _ := svc.Register(ctx, "grupo4", "clave-0009", service.NewDocument{Title: "Guía", Category: "Patagonia", Author: model.Author{Name: "Lucía", Email: "no-deberia@mail.com"}}, pdf)
	_ = svc.Reject(ctx, admin, ext.ID, "no se lee")
	got, _ = store.Get(ctx, ext.ID)
	author, _ = got.Outbox[0].Data["author"].(map[string]any)
	if _, ok := author["email"]; ok {
		t.Errorf("el evento de un documento externo lleva mail: %v (RN-41)", author)
	}
}
