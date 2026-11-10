package service_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/MichiSil/ProyectoEstudiario/services/forum-service/internal/model"
	"github.com/MichiSil/ProyectoEstudiario/services/forum-service/internal/search"
	"github.com/MichiSil/ProyectoEstudiario/services/forum-service/internal/service"
	"github.com/MichiSil/ProyectoEstudiario/services/forum-service/internal/service/memory"
)

var (
	ctx     = context.Background()
	admin   = service.Viewer{UserID: "admin-1", Admin: true}
	ana     = service.Viewer{UserID: "ana"}
	beto    = service.Viewer{UserID: "beto"}
	agencia = service.Viewer{Consumer: "agencia_de_viajes"}
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
	ext, _, err := svc.Register(ctx, "agencia_de_viajes", "clave-0001", service.NewDocument{Title: "Guía", Category: "Patagonia", Author: model.Author{Name: "Lucía", ExternalID: "v-1"}}, pdf)
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
	if ev.EventType != model.EventRejected || ev.Data["notifyAuthor"] != false || ev.Data["source"] != "agencia_de_viajes" || ev.Data["reason"] != "no se lee" {
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
	doc, _, err := svc.Register(ctx, "agencia_de_viajes", "clave-0001", service.NewDocument{Title: "Guía", Category: "Patagonia", Author: model.Author{Name: "Lucía"}}, pdf)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := svc.Get(ctx, agencia, doc.ID); err != nil {
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
	if _, err := svc.Get(ctx, agencia, doc.ID); !errors.Is(err, model.ErrNotFound) {
		t.Errorf("un eliminado se sigue viendo: err = %v", err)
	}
}

// ---------- Idempotencia (RN-40) ----------

func TestIdempotenciaDelRegistro(t *testing.T) {
	svc, store, _ := newService()
	in := service.NewDocument{Title: "Guía", Category: "Patagonia", Author: model.Author{Name: "Lucía"}}

	first, replayed, err := svc.Register(ctx, "agencia_de_viajes", "clave-0001", in, pdf)
	if err != nil || replayed {
		t.Fatalf("primer registro: replayed = %v, err = %v", replayed, err)
	}
	again, replayed, err := svc.Register(ctx, "agencia_de_viajes", "clave-0001", in, pdf)
	if err != nil || !replayed || again.ID != first.ID {
		t.Fatalf("reintento: id = %q, replayed = %v, err = %v; se esperaba el mismo documento (CA-34)", again.ID, replayed, err)
	}
	if p, _ := store.ListByConsumer(ctx, "agencia_de_viajes", "", 1, 10); p.Total != 1 {
		t.Errorf("hay %d documentos, se esperaba 1 (RN-40)", p.Total)
	}

	other := in
	other.Title = "Otra guía"
	if _, _, err := svc.Register(ctx, "agencia_de_viajes", "clave-0001", other, pdf); !errors.Is(err, model.ErrIdempotencyReused) {
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
		doc, _, err := svc.Register(ctx, "agencia_de_viajes", "clave-0001", in, pdf)
		if err != nil {
			t.Error(err)
		}
		done <- doc
	}()
	<-blocking.started

	// Un reintento mientras tanto recibe "en curso" (409) y no registra otro.
	if _, _, err := svc.Register(ctx, "agencia_de_viajes", "clave-0001", in, pdf); !errors.Is(err, model.ErrRequestInProgress) {
		t.Fatalf("en curso: err = %v, se esperaba ErrRequestInProgress", err)
	}

	close(blocking.release)
	first := <-done
	again, replayed, err := svc.Register(ctx, "agencia_de_viajes", "clave-0001", in, pdf)
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

	// Aunque se registre con authorEmail, el rechazo no le manda mail (RN-41).
	ext, _, _ := svc.Register(ctx, "agencia_de_viajes", "clave-0009", service.NewDocument{Title: "Guía", Category: "Patagonia", Author: model.Author{Name: "Lucía", Email: "lucia@mail.com"}}, pdf)
	_ = svc.Reject(ctx, admin, ext.ID, "no se lee")
	got, _ = store.Get(ctx, ext.ID)
	rejected := got.Outbox[len(got.Outbox)-1]
	if rejected.EventType != model.EventRejected {
		t.Fatalf("último evento = %s, se esperaba %s", rejected.EventType, model.EventRejected)
	}
	author, _ = rejected.Data["author"].(map[string]any)
	if _, ok := author["email"]; ok {
		t.Errorf("el evento de un documento externo lleva mail: %v (RN-41)", author)
	}
}

// ---------- Correcciones de la cátedra a la capacidad (ADR-014, 2026-11-09) ----------

func register(t *testing.T, svc *service.Service, consumer, key string, author model.Author) model.Summary {
	t.Helper()
	doc, _, err := svc.Register(ctx, consumer, key, service.NewDocument{Title: "Guía de glaciares", Category: "Patagonia", Author: author}, pdf)
	if err != nil {
		t.Fatal(err)
	}
	return doc
}

func newServiceWithPublic() (*service.Service, *memory.Store, *memory.Files, *memory.Files) {
	svc, store, files := newService()
	public := memory.NewFiles()
	svc.PublicFiles = public
	return svc, store, files, public
}

func TestElSourceEsElConsumidorDeLaAPIKey(t *testing.T) {
	svc, _, _ := newService()
	doc := register(t, svc, "agencia_de_viajes", "clave-0001", model.Author{Name: "Lucía"})
	if doc.Source != "agencia_de_viajes" {
		t.Errorf("source = %q, se esperaba el consumidor agencia_de_viajes", doc.Source)
	}
	if est := publish(t, svc, student); est.Source != model.SourceEstudiario {
		t.Errorf("source de un estudiante = %q", est.Source)
	}
}

func TestLosPDFExternosVanAlBucketPublico(t *testing.T) {
	svc, _, files, public := newServiceWithPublic()
	ext := register(t, svc, "agencia_de_viajes", "clave-0001", model.Author{Name: "Lucía"})
	est := publish(t, svc, student)
	if !ext.File.Public || !public.Has(ext.File.Key) || files.Has(ext.File.Key) {
		t.Errorf("el PDF externo no quedó sólo en el bucket público (public = %v)", ext.File.Public)
	}
	if est.File.Public || public.Has(est.File.Key) || !files.Has(est.File.Key) {
		t.Errorf("el PDF de un estudiante no quedó sólo en el bucket privado (public = %v)", est.File.Public)
	}
	// La descarga con API key también lo lee del bucket público.
	if _, r, err := svc.Download(ctx, service.Viewer{Consumer: "agencia_de_viajes"}, ext.ID); err != nil {
		t.Fatalf("descarga con API key: %v", err)
	} else {
		_ = r.Close()
	}
}

func TestDescargaPublica(t *testing.T) {
	svc, store, _, _ := newServiceWithPublic()
	open := func(id string) error {
		_, r, err := svc.PublicDownload(ctx, id)
		if err == nil {
			b, _ := io.ReadAll(r)
			_ = r.Close()
			if string(b) != string(pdf.Data) {
				t.Errorf("contenido = %q", b)
			}
		}
		return err
	}

	pendiente := register(t, svc, "agencia_de_viajes", "clave-0001", model.Author{Name: "Lucía"})
	if err := open(pendiente.ID); err != nil {
		t.Errorf("externo pendiente: %v, se esperaba el PDF (público desde que se sube)", err)
	}
	aprobado := register(t, svc, "agencia_de_viajes", "clave-0002", model.Author{Name: "Lucía"})
	_ = svc.Approve(ctx, admin, aprobado.ID)
	if err := open(aprobado.ID); err != nil {
		t.Errorf("externo aprobado: %v, se esperaba el PDF", err)
	}

	rechazado := register(t, svc, "agencia_de_viajes", "clave-0003", model.Author{Name: "Lucía"})
	_ = svc.Reject(ctx, admin, rechazado.ID, "no se lee")
	eliminado := register(t, svc, "agencia_de_viajes", "clave-0004", model.Author{Name: "Lucía"})
	_ = svc.Delete(ctx, admin, eliminado.ID)
	est := publish(t, svc, student)
	_ = svc.Approve(ctx, admin, est.ID)
	// Un documento viejo, con source "externo" y el PDF en el bucket privado.
	viejo := register(t, svc, "agencia_de_viajes", "clave-0005", model.Author{Name: "Lucía"})
	v, _ := store.Get(ctx, viejo.ID)
	v.Source, v.File.Public = model.SourceExternal, false
	_ = store.Create(ctx, v)

	for name, id := range map[string]string{
		"externo rechazado": rechazado.ID, "externo eliminado": eliminado.ID,
		"estudiario aprobado": est.ID, "externo viejo en el bucket privado": viejo.ID, "inexistente": "noexiste1234",
	} {
		if err := open(id); !errors.Is(err, model.ErrNotFound) {
			t.Errorf("%s: err = %v, se esperaba ErrNotFound", name, err)
		}
	}
}

func TestEventoParaCrearLaCuentaDelAutor(t *testing.T) {
	svc, store, _, _ := newServiceWithPublic()
	sinMail := register(t, svc, "agencia_de_viajes", "clave-0001", model.Author{Name: "Lucía"})
	if got, _ := store.Get(ctx, sinMail.ID); len(got.Outbox) != 0 {
		t.Errorf("sin authorEmail se guardaron eventos: %+v", got.Outbox)
	}

	in := service.NewDocument{Title: "Guía de glaciares", Category: "Patagonia", Author: model.Author{Name: "Lucía Fernández", Email: "lucia@mail.com", ExternalID: "viajero-1842"}}
	doc, _, err := svc.Register(ctx, "agencia_de_viajes", "clave-0002", in, pdf)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := store.Get(ctx, doc.ID)
	if len(got.Outbox) != 1 {
		t.Fatalf("eventos = %d, se esperaba 1", len(got.Outbox))
	}
	ev := got.Outbox[0]
	author, _ := ev.Data["author"].(map[string]any)
	if ev.EventType != model.EventExternalRegistered || !uuidV4.MatchString(ev.EventID) || ev.PublishedAt != nil ||
		ev.Data["documentId"] != doc.ID || ev.Data["title"] != "Guía de glaciares" || ev.Data["source"] != "agencia_de_viajes" ||
		author["name"] != "Lucía Fernández" || author["email"] != "lucia@mail.com" || author["externalId"] != "viajero-1842" {
		t.Errorf("evento = %+v", ev)
	}

	// El reintento con la misma Idempotency-Key no vuelve a publicarlo.
	if _, replayed, err := svc.Register(ctx, "agencia_de_viajes", "clave-0002", in, pdf); err != nil || !replayed {
		t.Fatalf("reintento: replayed = %v, err = %v", replayed, err)
	}
	if got, _ := store.Get(ctx, doc.ID); len(got.Outbox) != 1 {
		t.Errorf("después del reintento hay %d eventos", len(got.Outbox))
	}
	if p, _ := store.ListByConsumer(ctx, "agencia_de_viajes", "", 1, 10); p.Total != 2 {
		t.Errorf("documentos registrados = %d, se esperaban 2", p.Total)
	}

	// Un estudiante tiene mail (del token) y no genera este evento.
	in = student
	in.Author.Email = "ana@mail.com"
	if got, _ := store.Get(ctx, publish(t, svc, in).ID); len(got.Outbox) != 0 {
		t.Errorf("un resumen de estudiante generó eventos al publicarse: %+v", got.Outbox)
	}
}

func TestAuthorEmailInvalido(t *testing.T) {
	svc, _, _ := newService()
	for i, email := range []string{"no-es-mail", "Lucía <lucia@mail.com>", strings.Repeat("a", 250) + "@mail.com"} {
		_, _, err := svc.Register(ctx, "agencia_de_viajes", "clave-000"+string(rune('1'+i)), service.NewDocument{Title: "Guía", Category: "Patagonia", Author: model.Author{Name: "Lucía", Email: email}}, pdf)
		var verr *model.ValidationError
		if !errors.As(err, &verr) || verr.Fields[0].Field != "authorEmail" {
			t.Errorf("authorEmail %.30q: err = %v, se esperaba un error de validación en authorEmail", email, err)
		}
	}
}

// ---------- Listado sin Solr (POSTMORTEM, acción 2) ----------

// failingSearch es Solr caído.
type failingSearch struct{}

func (failingSearch) Search(context.Context, model.SearchQuery) (model.Page, error) {
	return model.Page{}, errors.Join(model.ErrSearchUnavailable, errors.New("solr: connection refused"))
}

func TestConSolrCaidoElListadoSaleIgual(t *testing.T) {
	svc, store, _ := newService()
	svc.Search = &search.ListFallback{Primary: failingSearch{}, Fallback: store, Logger: slog.New(slog.NewJSONHandler(io.Discard, nil))}
	approved := publish(t, svc, student)
	_ = svc.Approve(ctx, admin, approved.ID)
	_ = publish(t, svc, student) // pendiente: no aparece

	page, err := svc.SearchApproved(ctx, model.SearchQuery{Category: model.NormalizeKey("Análisis II"), Page: 1, Size: 20})
	if err != nil || page.Total != 1 || page.Items[0].ID != approved.ID {
		t.Fatalf("listado con Solr caído = %+v, %v; se esperaba el aprobado", page, err)
	}
	// La búsqueda por texto no tiene alternativa: 503.
	if _, err := svc.SearchApproved(ctx, model.SearchQuery{Text: "integrales", Page: 1, Size: 20}); !errors.Is(err, model.ErrSearchUnavailable) {
		t.Errorf("búsqueda con texto: err = %v, se esperaba ErrSearchUnavailable", err)
	}
}

// ---------- El autor elimina su resumen (RF-31) ----------

func TestElAutorEliminaSusResumenes(t *testing.T) {
	svc, store, _ := newService()
	pendiente := publish(t, svc, student)
	aprobado := publish(t, svc, student)
	if err := svc.Approve(ctx, admin, aprobado.ID); err != nil {
		t.Fatal(err)
	}

	for _, doc := range []model.Summary{pendiente, aprobado} {
		if err := svc.DeleteOwn(ctx, beto, doc.ID); !errors.Is(err, model.ErrNotAuthor) {
			t.Errorf("otro estudiante elimina: err = %v, se esperaba ErrNotAuthor", err)
		}
		if err := svc.DeleteOwn(ctx, ana, doc.ID); err != nil {
			t.Fatalf("la autora elimina uno %s: %v", doc.Status, err)
		}
		if _, err := svc.Get(ctx, ana, doc.ID); !errors.Is(err, model.ErrNotFound) {
			t.Errorf("después de eliminar: err = %v, se esperaba ErrNotFound (RN-24)", err)
		}
		if err := svc.DeleteOwn(ctx, ana, doc.ID); !errors.Is(err, model.ErrNotFound) {
			t.Errorf("eliminar dos veces: err = %v, se esperaba ErrNotFound", err)
		}
		// La misma eliminación que la del admin: summary.deleted en el outbox,
		// que lo saca del índice (RN-26). No hay evento de mail (RN-35).
		got, _ := store.Get(ctx, doc.ID)
		last := got.Outbox[len(got.Outbox)-1]
		if got.Status != model.StatusDeleted || last.EventType != model.EventDeleted || last.Data["deletedBy"] != "ana" {
			t.Errorf("estado = %s, último evento = %s %v", got.Status, last.EventType, last.Data)
		}
	}

	rechazado := publish(t, svc, student)
	if err := svc.Reject(ctx, admin, rechazado.ID, "el PDF está incompleto"); err != nil {
		t.Fatal(err)
	}
	if err := svc.DeleteOwn(ctx, ana, rechazado.ID); err != nil {
		t.Errorf("la autora elimina uno rechazado: %v", err)
	}
	if err := svc.DeleteOwn(ctx, ana, "no-existe"); !errors.Is(err, model.ErrNotFound) {
		t.Errorf("uno que no existe: err = %v, se esperaba ErrNotFound", err)
	}
}

func TestUnEstudianteNoEliminaDocumentosExternos(t *testing.T) {
	svc, _, _ := newService()
	doc := register(t, svc, "agencia_de_viajes", "", model.Author{Name: "Guía", ExternalID: "ana"})
	if err := svc.DeleteOwn(ctx, ana, doc.ID); !errors.Is(err, model.ErrNotAuthor) {
		t.Errorf("err = %v, se esperaba ErrNotAuthor", err)
	}
	if err := svc.Delete(ctx, admin, doc.ID); err != nil {
		t.Errorf("el admin sigue eliminando cualquiera (RF-18): %v", err)
	}
}
