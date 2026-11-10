package app_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"

	"github.com/MichiSil/ProyectoEstudiario/services/planner-service/internal/app"
	"github.com/MichiSil/ProyectoEstudiario/services/planner-service/internal/domain"
)

// memFiles es el bucket materials en memoria.
type memFiles struct {
	mu   sync.Mutex
	data map[string][]byte
}

func newMemFiles() *memFiles { return &memFiles{data: map[string][]byte{}} }

func (f *memFiles) Put(_ context.Context, key string, r io.Reader, _ int64, _ string) error {
	b, err := io.ReadAll(r)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.data[key] = b
	return err
}

func (f *memFiles) Get(_ context.Context, key string) (io.ReadCloser, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	b, ok := f.data[key]
	if !ok {
		return nil, domain.ErrNotFound
	}
	return io.NopCloser(bytes.NewReader(b)), nil
}

func (f *memFiles) Delete(_ context.Context, key string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.data, key)
	return nil
}

var testPDF = []byte("%PDF-1.4\nmaterial de prueba\n%%EOF")

// RF-04, ADR-003: el material puede tener un PDF; sólo el dueño lo descarga
// (RN-03) y borrar el material borra el archivo.
func TestMaterialConPDF(t *testing.T) {
	svc, _, exam := setup(t)
	files := newMemFiles()
	svc.Files = files
	ctx := context.Background()

	m, err := svc.AddMaterialWithFile(ctx, domain.Material{StudentID: "est-1", SubjectID: exam.SubjectID, Title: "TP4"},
		&app.UploadedFile{Name: "tp4.pdf", ContentType: "application/pdf", Data: testPDF})
	if err != nil {
		t.Fatal(err)
	}
	if m.Kind != domain.MaterialTheory || m.File == nil || m.File.SizeBytes != int64(len(testPDF)) || m.File.FileName != "tp4.pdf" {
		t.Fatalf("material = %+v (archivo %+v); sólo con el título tiene que quedar como teórico, con su PDF", m, m.File)
	}

	_, body, err := svc.MaterialFile(ctx, "est-1", m.ID)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := io.ReadAll(body)
	_ = body.Close()
	if !bytes.Equal(got, testPDF) {
		t.Errorf("descarga = %q", got)
	}
	if _, _, err := svc.MaterialFile(ctx, "est-2", m.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("otro estudiante lo descarga: err = %v, se esperaba ErrNotFound (RN-03)", err)
	}

	if err := svc.DeleteMaterial(ctx, "est-1", m.ID); err != nil {
		t.Fatal(err)
	}
	if len(files.data) != 0 {
		t.Errorf("borrar el material no borró el PDF: %v", files.data)
	}
}

func TestMaterialSoloTituloYArchivosInvalidos(t *testing.T) {
	svc, _, exam := setup(t)
	svc.Files = newMemFiles()
	ctx := context.Background()

	// Sólo el título, sin archivo ni tipo.
	m, err := svc.AddMaterial(ctx, domain.Material{StudentID: "est-1", SubjectID: exam.SubjectID, Title: "TP4"})
	if err != nil || m.File != nil {
		t.Fatalf("sólo con el título: %+v, %v", m, err)
	}
	if _, _, err := svc.MaterialFile(ctx, "est-1", m.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("un material sin PDF no tiene descarga: err = %v", err)
	}

	var verr *domain.ValidationError
	for name, f := range map[string]*app.UploadedFile{
		"no es PDF":    {Name: "foto.png", ContentType: "image/png", Data: []byte("\x89PNG")},
		"disfrazado":   {Name: "falso.pdf", ContentType: "application/pdf", Data: []byte("hola")},
		"más de 10 MB": {Name: "grande.pdf", ContentType: "application/pdf", Data: append([]byte("%PDF-"), make([]byte, app.MaxMaterialFileSize)...)},
	} {
		if _, err := svc.AddMaterialWithFile(ctx, domain.Material{StudentID: "est-1", SubjectID: exam.SubjectID, Title: "X"}, f); !errors.As(err, &verr) {
			t.Errorf("%s: err = %v, se esperaba error de validación", name, err)
		}
	}
	if _, err := svc.AddMaterial(ctx, domain.Material{StudentID: "est-1", SubjectID: exam.SubjectID, Title: strings.Repeat(" ", 3)}); !errors.As(err, &verr) {
		t.Errorf("sin título: err = %v, se esperaba error de validación", err)
	}
}
