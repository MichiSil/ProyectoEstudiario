package app_test

import (
	"context"
	"errors"
	"testing"

	"github.com/MichiSil/ProyectoEstudiario/services/planner-service/internal/domain"
)

// DeleteSubject en memoria: saca la materia y cancela sus exámenes como CancelExam.
func (r *memRepo) DeleteSubject(ctx context.Context, st, id string) error {
	r.mu.Lock()
	s, ok := r.subjects[id]
	if !ok || s.StudentID != st {
		r.mu.Unlock()
		return domain.ErrNotFound
	}
	delete(r.subjects, id)
	var exams []string
	for eid, e := range r.exams {
		if e.SubjectID == id {
			exams = append(exams, eid)
		}
	}
	r.mu.Unlock()
	for _, eid := range exams {
		if _, err := r.CancelExam(ctx, st, eid); err != nil {
			return err
		}
	}
	return nil
}

func TestBorrarMateriaCancelaSusExamenes(t *testing.T) {
	svc, repo, exam := setup(t)
	ctx := context.Background()
	if err := svc.DeleteSubject(ctx, "otro-estudiante", exam.SubjectID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("otro estudiante no puede borrarla: se esperaba ErrNotFound, llegó %v", err)
	}
	if err := svc.DeleteSubject(ctx, "est-1", exam.SubjectID); err != nil {
		t.Fatal(err)
	}
	if repo.exams[exam.ID].Status != domain.ExamCancelled {
		t.Errorf("el examen quedó %q, se esperaba cancelado", repo.exams[exam.ID].Status)
	}
	if _, ok := repo.subjects[exam.SubjectID]; ok {
		t.Error("la materia sigue estando")
	}
}
