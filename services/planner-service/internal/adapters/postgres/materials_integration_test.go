package postgres_test

import (
	"context"
	"testing"

	"github.com/MichiSil/ProyectoEstudiario/services/planner-service/internal/domain"
)

// El material guarda la referencia a su PDF (ADR-003) y la devuelve al
// listarlo y al leerlo; un material sólo con título no tiene archivo.
func TestIntegration_MaterialConArchivo(t *testing.T) {
	svc, _, ids := newService(t)
	student, examID := split(ids)
	ctx := context.Background()
	exam, err := svc.Repo.GetExam(ctx, student, examID)
	if err != nil {
		t.Fatal(err)
	}
	conPDF := domain.Material{ID: "mat-pdf-" + examID, StudentID: student, SubjectID: exam.SubjectID, Kind: domain.MaterialPractice,
		Title: "TP4", File: &domain.MaterialFile{Key: "mat-pdf-" + examID + ".pdf", FileName: "tp4.pdf", ContentType: "application/pdf", SizeBytes: 1234}}
	if err := svc.Repo.CreateMaterial(ctx, conPDF); err != nil {
		t.Fatal(err)
	}
	soloTitulo, err := svc.AddMaterial(ctx, domain.Material{StudentID: student, SubjectID: exam.SubjectID, Title: "Unidad 1"})
	if err != nil {
		t.Fatal(err)
	}

	got, err := svc.Repo.GetMaterial(ctx, student, conPDF.ID)
	if err != nil || got.File == nil || *got.File != *conPDF.File {
		t.Fatalf("GetMaterial = %+v (archivo %+v), err = %v", got, got.File, err)
	}
	if _, err := svc.Repo.GetMaterial(ctx, "otro-"+student, conPDF.ID); err != domain.ErrNotFound {
		t.Errorf("otro estudiante: err = %v, se esperaba ErrNotFound (RN-03)", err)
	}
	list, err := svc.ListMaterials(ctx, student, exam.SubjectID)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range list {
		switch m.ID {
		case conPDF.ID:
			if m.File == nil || m.File.SizeBytes != 1234 {
				t.Errorf("listado: el material con PDF vino sin archivo: %+v", m)
			}
		case soloTitulo.ID:
			if m.File != nil || m.Kind != domain.MaterialTheory {
				t.Errorf("listado: el material sólo con título = %+v", m)
			}
		}
	}
}
