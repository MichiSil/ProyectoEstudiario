package app

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/MichiSil/ProyectoEstudiario/services/planner-service/internal/domain"
)

// Service reúne los casos de uso de planner-service.
type Service struct {
	Repo      PlanRepository
	Generator PlanGenerator
	Clock     Clock
	IDs       IDGenerator
}

func (s *Service) CreateSubject(ctx context.Context, sub domain.Subject) (domain.Subject, error) {
	verr := &domain.ValidationError{}
	if strings.TrimSpace(sub.Name) == "" {
		verr.Violations = append(verr.Violations, domain.RuleViolation{Rule: "RF-04", Message: "la materia necesita un nombre", SessionIndex: -1})
	}
	if !sub.Difficulty.Valid() {
		verr.Violations = append(verr.Violations, domain.RuleViolation{Rule: "RF-04", Message: "la dificultad tiene que ser de 1 a 4", SessionIndex: -1})
	}
	if len(verr.Violations) > 0 {
		return domain.Subject{}, verr
	}
	if sub.Professors == nil {
		sub.Professors = []string{} // en la API se devuelve [] y no null
	}
	sub.ID = s.IDs.NewID()
	return sub, s.Repo.CreateSubject(ctx, sub)
}

func (s *Service) ListSubjects(ctx context.Context, studentID string) ([]domain.Subject, error) {
	return s.Repo.ListSubjects(ctx, studentID)
}

func (s *Service) CreateExam(ctx context.Context, e domain.Exam) (domain.Exam, error) {
	if _, err := s.Repo.GetSubject(ctx, e.StudentID, e.SubjectID); err != nil {
		return domain.Exam{}, err // RN-03: sólo sobre materias propias
	}
	verr := &domain.ValidationError{}
	if !e.Type.Valid() {
		verr.Violations = append(verr.Violations, domain.RuleViolation{Rule: "RF-05", Message: "tipo de examen inválido", SessionIndex: -1})
	}
	if err := s.checkExamMaterial(ctx, e, verr); err != nil {
		return domain.Exam{}, err
	}
	if e.StartsAt.Before(s.Clock.Now()) {
		verr.Violations = append(verr.Violations, domain.RuleViolation{Rule: "RN-04", Message: "la fecha del examen no puede estar en el pasado", SessionIndex: -1})
	}
	if len(verr.Violations) > 0 {
		return domain.Exam{}, verr
	}
	e.ID = s.IDs.NewID()
	e.Status = domain.ExamScheduled
	if e.MaterialIDs == nil {
		e.MaterialIDs = []string{}
	}
	return e, s.Repo.CreateExam(ctx, e)
}

// checkExamMaterial valida la modalidad y que el material elegido sea de la
// misma materia y del mismo estudiante (RN-03).
func (s *Service) checkExamMaterial(ctx context.Context, e domain.Exam, verr *domain.ValidationError) error {
	if !e.Modality.Valid() {
		verr.Violations = append(verr.Violations, domain.RuleViolation{Rule: "RF-05", Message: "la modalidad tiene que ser teórico, práctico o mixto", SessionIndex: -1})
	}
	if len(e.MaterialIDs) == 0 {
		return nil
	}
	materials, err := s.Repo.ListMaterials(ctx, e.StudentID, e.SubjectID)
	if err != nil {
		return err
	}
	own := make(map[string]bool, len(materials))
	for _, m := range materials {
		own[m.ID] = true
	}
	for _, id := range e.MaterialIDs {
		if !own[id] {
			verr.Violations = append(verr.Violations, domain.RuleViolation{Rule: "RF-05", Message: "el material elegido no es de esta materia", SessionIndex: -1})
			break
		}
	}
	return nil
}

// AddMaterial carga un teórico o un práctico en una materia propia.
func (s *Service) AddMaterial(ctx context.Context, m domain.Material) (domain.Material, error) {
	if _, err := s.Repo.GetSubject(ctx, m.StudentID, m.SubjectID); err != nil {
		return domain.Material{}, err // RN-03: sólo en materias propias
	}
	verr := &domain.ValidationError{}
	m.Title = strings.TrimSpace(m.Title)
	m.Content = strings.TrimSpace(m.Content)
	if !m.Kind.Valid() {
		verr.Violations = append(verr.Violations, domain.RuleViolation{Rule: "RF-04", Message: "el material tiene que ser teórico o práctico", SessionIndex: -1})
	}
	if m.Title == "" || len([]rune(m.Title)) > 200 {
		verr.Violations = append(verr.Violations, domain.RuleViolation{Rule: "RF-04", Message: "el material necesita un título de hasta 200 caracteres", SessionIndex: -1})
	}
	if len([]rune(m.Content)) > 5000 {
		verr.Violations = append(verr.Violations, domain.RuleViolation{Rule: "RF-04", Message: "el contenido tiene como máximo 5000 caracteres", SessionIndex: -1})
	}
	if len(verr.Violations) > 0 {
		return domain.Material{}, verr
	}
	m.ID = s.IDs.NewID()
	m.CreatedAt = s.Clock.Now()
	return m, s.Repo.CreateMaterial(ctx, m)
}

func (s *Service) ListMaterials(ctx context.Context, studentID, subjectID string) ([]domain.Material, error) {
	if _, err := s.Repo.GetSubject(ctx, studentID, subjectID); err != nil {
		return nil, err
	}
	return s.Repo.ListMaterials(ctx, studentID, subjectID)
}

func (s *Service) DeleteMaterial(ctx context.Context, studentID, materialID string) error {
	return s.Repo.DeleteMaterial(ctx, studentID, materialID)
}

// examMaterials es el material que entra en el examen: el elegido o, si no
// se eligió ninguno, todo el de la materia.
func (s *Service) examMaterials(ctx context.Context, exam domain.Exam) ([]domain.Material, error) {
	all, err := s.Repo.ListMaterials(ctx, exam.StudentID, exam.SubjectID)
	if err != nil || len(exam.MaterialIDs) == 0 {
		return all, err
	}
	chosen := make(map[string]bool, len(exam.MaterialIDs))
	for _, id := range exam.MaterialIDs {
		chosen[id] = true
	}
	var out []domain.Material
	for _, m := range all {
		if chosen[m.ID] {
			out = append(out, m)
		}
	}
	return out, nil
}

func (s *Service) ListExams(ctx context.Context, studentID string) ([]domain.Exam, error) {
	return s.Repo.ListExams(ctx, studentID)
}

// ExamChanges son los campos de un examen que el estudiante puede editar. Los
// nil no cambian. La materia no se cambia: se cancela y se carga otro examen.
type ExamChanges struct {
	Type        *domain.ExamType
	StartsAt    *time.Time
	Topics      *[]string
	Modality    *domain.ExamModality
	MaterialIDs *[]string
}

// UpdateExam edita un examen (RF-06). Si cambia la fecha, los temas, la
// modalidad o el material y el examen tenía un plan confirmado, el plan pasa a
// desactualizado y sus sesiones pendientes quedan marcadas hasta que el
// estudiante replanifique (RN-16).
func (s *Service) UpdateExam(ctx context.Context, studentID, examID string, ch ExamChanges) (domain.Exam, error) {
	now := s.Clock.Now()
	return s.Repo.UpdateExam(ctx, studentID, examID, func(e domain.Exam) (domain.Exam, bool, error) {
		if e.Status == domain.ExamCancelled {
			return e, false, domain.ErrExamCancelled
		}
		before := e
		verr := &domain.ValidationError{}
		if ch.Type != nil {
			if !ch.Type.Valid() {
				verr.Violations = append(verr.Violations, domain.RuleViolation{Rule: "RF-05", Message: "tipo de examen inválido", SessionIndex: -1})
			}
			e.Type = *ch.Type
		}
		dateChanged := false
		if ch.StartsAt != nil && !ch.StartsAt.Equal(e.StartsAt) {
			if ch.StartsAt.Before(now) {
				verr.Violations = append(verr.Violations, domain.RuleViolation{Rule: "RN-04", Message: "la fecha del examen no puede estar en el pasado", SessionIndex: -1})
			}
			e.StartsAt, dateChanged = *ch.StartsAt, true
		}
		if ch.Topics != nil {
			e.Topics = *ch.Topics
			if e.Topics == nil {
				e.Topics = []string{}
			}
		}
		if ch.Modality != nil {
			e.Modality = *ch.Modality
		}
		if ch.MaterialIDs != nil {
			e.MaterialIDs = *ch.MaterialIDs
			if e.MaterialIDs == nil {
				e.MaterialIDs = []string{}
			}
		}
		if err := s.checkExamMaterial(ctx, e, verr); err != nil {
			return e, false, err
		}
		if len(verr.Violations) > 0 {
			return e, false, verr
		}
		// RN-16: el plan confirmado queda desactualizado si cambia algo con lo
		// que se armó: la fecha, los temas, la modalidad o el material.
		outdated := dateChanged || !slices.Equal(before.Topics, e.Topics) ||
			before.Modality != e.Modality || !slices.Equal(before.MaterialIDs, e.MaterialIDs)
		return e, outdated, nil
	})
}

// Replan deja desactualizado el plan confirmado de un examen a pedido del
// estudiante, aunque el examen no haya cambiado (por ejemplo, porque cambió
// su disponibilidad). Sigue el mismo camino que RN-16: las sesiones siguen en
// el calendario hasta que confirme un plan nuevo, que las reemplaza. Si el
// examen no tiene plan confirmado ni desactualizado, devuelve ErrNoConfirmedPlan.
func (s *Service) Replan(ctx context.Context, studentID, examID string) (domain.Exam, error) {
	e, err := s.Repo.UpdateExam(ctx, studentID, examID, func(e domain.Exam) (domain.Exam, bool, error) {
		if e.Status == domain.ExamCancelled {
			return e, false, domain.ErrExamCancelled
		}
		return e, true, nil
	})
	if err != nil {
		return e, err
	}
	if e.PlanStatus != domain.PlanOutdated {
		return e, domain.ErrNoConfirmedPlan
	}
	return e, nil
}

// DeleteSubject borra una materia (RF-04): sus exámenes se cancelan y sus
// sesiones pendientes se liberan, como al cancelar un examen (RN-17).
func (s *Service) DeleteSubject(ctx context.Context, studentID, subjectID string) error {
	return s.Repo.DeleteSubject(ctx, studentID, subjectID)
}

// CancelExam cancela un examen (RF-06): sus sesiones pendientes se liberan y
// las completadas se conservan como historial (RN-17).
func (s *Service) CancelExam(ctx context.Context, studentID, examID string) (domain.Exam, error) {
	return s.Repo.CancelExam(ctx, studentID, examID)
}

func (s *Service) SetAvailability(ctx context.Context, av domain.Availability) error {
	if err := domain.ValidateSlots(av.Slots); err != nil {
		return err
	}
	if av.MaxDailyMinutes <= 0 || av.PreferredSessionMinutes <= 0 {
		return &domain.ValidationError{Violations: []domain.RuleViolation{{Rule: "RF-07", Message: "la capacidad diaria y la duración de sesión tienen que ser positivas", SessionIndex: -1}}}
	}
	return s.Repo.SaveAvailability(ctx, av)
}

// Availability devuelve la disponibilidad guardada del estudiante (RF-07), o
// ErrAvailabilityNotSet si todavía no la cargó.
func (s *Service) Availability(ctx context.Context, studentID string) (domain.Availability, error) {
	av, err := s.Repo.GetAvailability(ctx, studentID)
	if errors.Is(err, domain.ErrNotFound) {
		return av, domain.ErrAvailabilityNotSet
	}
	return av, err
}

// ProposePlan pide una propuesta de plan para un examen (RF-08). La propuesta
// se guarda en estado "propuesto" y no reserva nada (RN-08).
func (s *Service) ProposePlan(ctx context.Context, studentID, examID string) (domain.Plan, error) {
	exam, err := s.Repo.GetExam(ctx, studentID, examID)
	if err != nil {
		return domain.Plan{}, err
	}
	if exam.Status == domain.ExamCancelled {
		return domain.Plan{}, domain.ErrExamCancelled
	}
	subject, err := s.Repo.GetSubject(ctx, studentID, exam.SubjectID)
	if err != nil {
		return domain.Plan{}, err
	}
	av, err := s.Repo.GetAvailability(ctx, studentID)
	if err != nil {
		return domain.Plan{}, err
	}
	now := s.Clock.Now()
	all, err := s.Repo.ListBookedSessions(ctx, studentID, now)
	if err != nil {
		return domain.Plan{}, err
	}
	// Las sesiones desactualizadas de este mismo examen se van a reemplazar
	// al confirmar (RN-16): no le quitan lugar a la propuesta nueva.
	booked := make([]domain.Session, 0, len(all))
	for _, b := range all {
		if !(b.Outdated && b.ExamID == examID) {
			booked = append(booked, b)
		}
	}

	materials, err := s.examMaterials(ctx, exam)
	if err != nil {
		return domain.Plan{}, err
	}

	proposal, err := s.Generator.Generate(ctx, ProposalInput{Subject: subject, Exam: exam, Availability: av, Now: now, Materials: materials, Booked: booked})
	if err != nil {
		return domain.Plan{}, fmt.Errorf("generar propuesta: %w", err)
	}

	plan := domain.Plan{
		ID:          s.IDs.NewID(),
		StudentID:   studentID,
		ExamID:      examID,
		Status:      domain.PlanProposed,
		GeneratedBy: proposal.GeneratedBy,
		Warnings:    proposal.Warnings,
		Drafts:      proposal.Drafts,
		CreatedAt:   now,
	}
	return plan, s.Repo.SaveProposal(ctx, plan)
}

// ConfirmPlan confirma una propuesta y reserva sus sesiones (RF-09, acción
// principal). Las reglas RN-10 a RN-13 se validan dentro de la transacción
// del repositorio, contra las sesiones reservadas en ese momento; RN-14 y
// RN-15 las garantiza el repositorio con la clave de idempotencia y la
// restricción de un plan confirmado por examen.
func (s *Service) ConfirmPlan(ctx context.Context, studentID, planID, idempotencyKey string) (ConfirmResult, error) {
	if strings.TrimSpace(idempotencyKey) == "" {
		return ConfirmResult{}, &domain.ValidationError{Violations: []domain.RuleViolation{{Rule: "RN-14", Message: "falta el encabezado Idempotency-Key", SessionIndex: -1}}}
	}
	plan, err := s.Repo.GetPlan(ctx, studentID, planID)
	if err != nil {
		return ConfirmResult{}, err
	}
	exam, err := s.Repo.GetExam(ctx, studentID, plan.ExamID)
	if err != nil {
		return ConfirmResult{}, err
	}
	if exam.Status == domain.ExamCancelled {
		return ConfirmResult{}, domain.ErrExamCancelled
	}
	av, err := s.Repo.GetAvailability(ctx, studentID)
	if err != nil {
		return ConfirmResult{}, err
	}
	now := s.Clock.Now()

	return s.Repo.Confirm(ctx,
		ConfirmRequest{StudentID: studentID, PlanID: planID, IdempotencyKey: idempotencyKey},
		func(booked []domain.Session) error {
			return domain.ValidatePlan(plan.Drafts, exam, av, booked, now)
		})
}

func (s *Service) ListSessions(ctx context.Context, studentID string) ([]domain.Session, error) {
	return s.Repo.ListBookedSessions(ctx, studentID, s.Clock.Now().AddDate(0, 0, -30))
}
