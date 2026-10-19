package domain

import (
	"errors"
	"testing"
	"time"
)

var cba, _ = time.LoadLocation("America/Argentina/Cordoba")

// Martes 20/10/2026 a las 9:00: momento en que se confirma el plan.
var now = time.Date(2026, 10, 20, 9, 0, 0, 0, cba)

func at(day, hour, minute int) time.Time { return time.Date(2026, 10, day, hour, minute, 0, 0, cba) }

func availability() Availability {
	return Availability{
		StudentID: "est-1",
		Slots: []Slot{
			{Weekday: time.Tuesday, StartMinute: 18 * 60, EndMinute: 21 * 60},
			{Weekday: time.Thursday, StartMinute: 18 * 60, EndMinute: 21 * 60},
		},
		MaxDailyMinutes:         120,
		PreferredSessionMinutes: 60,
		Location:                cba,
	}
}

func exam() Exam {
	return Exam{ID: "ex-1", StudentID: "est-1", StartsAt: at(30, 9, 0), Status: ExamScheduled}
}

func draft(start time.Time, minutes int) SessionDraft {
	return SessionDraft{Start: start, DurationMinutes: minutes, Topic: "Integrales", Objective: "Leer teoría"}
}

func violatedRules(t *testing.T, err error) map[string]bool {
	t.Helper()
	rules := map[string]bool{}
	if err == nil {
		return rules
	}
	var verr *ValidationError
	if !errors.As(err, &verr) {
		t.Fatalf("se esperaba *ValidationError, llegó %T", err)
	}
	for _, v := range verr.Violations {
		rules[v.Rule] = true
	}
	return rules
}

// CA-06: un plan válido se puede confirmar.
func TestValidatePlan_PlanValido(t *testing.T) {
	drafts := []SessionDraft{draft(at(20, 18, 0), 60), draft(at(22, 19, 0), 90)}
	if err := ValidatePlan(drafts, exam(), availability(), nil, now); err != nil {
		t.Fatalf("no se esperaba error: %v", err)
	}
}

// CA-07: una sesión fuera de las franjas disponibles rechaza todo el plan.
func TestValidatePlan_SesionFueraDeFranja(t *testing.T) {
	drafts := []SessionDraft{
		draft(at(20, 18, 0), 60),
		draft(at(21, 18, 0), 60),  // miércoles: no hay franja
		draft(at(22, 20, 30), 60), // jueves, pero termina 21:30
	}
	err := ValidatePlan(drafts, exam(), availability(), nil, now)
	var verr *ValidationError
	if !errors.As(err, &verr) {
		t.Fatalf("se esperaba ValidationError, llegó %v", err)
	}
	if len(verr.Violations) != 2 || verr.Violations[0].SessionIndex != 1 || verr.Violations[1].SessionIndex != 2 {
		t.Fatalf("se esperaban violaciones RN-10 en las sesiones 1 y 2, llegó %+v", verr.Violations)
	}
	if !violatedRules(t, err)["RN-10"] {
		t.Fatal("se esperaba RN-10")
	}
}

// CA-08: sesión reservada el martes 18:00-19:00 y otra propuesta 18:30-19:30.
func TestValidatePlan_SuperposicionConSesionReservada(t *testing.T) {
	booked := []Session{{Start: at(20, 18, 0), End: at(20, 19, 0), Status: SessionPending}}
	drafts := []SessionDraft{draft(at(20, 18, 30), 60)}
	if !violatedRules(t, ValidatePlan(drafts, exam(), availability(), booked, now))["RN-12"] {
		t.Fatal("se esperaba RN-12 por superposición")
	}
}

func TestValidatePlan_SuperposicionDentroDelMismoPlan(t *testing.T) {
	drafts := []SessionDraft{draft(at(20, 18, 0), 60), draft(at(20, 18, 45), 30)}
	if !violatedRules(t, ValidatePlan(drafts, exam(), availability(), nil, now))["RN-12"] {
		t.Fatal("se esperaba RN-12 por superposición entre sesiones del plan")
	}
}

// Una sesión pospuesta libera su horario (sección 6.3 del SPEC).
func TestValidatePlan_SesionPospuestaLiberaHorario(t *testing.T) {
	booked := []Session{{Start: at(20, 18, 0), End: at(20, 19, 0), Status: SessionPostponed}}
	drafts := []SessionDraft{draft(at(20, 18, 0), 60)}
	if err := ValidatePlan(drafts, exam(), availability(), booked, now); err != nil {
		t.Fatalf("no se esperaba error: %v", err)
	}
}

// CA-09: capacidad de 120 minutos, 90 ya reservados el jueves y se agregan 60.
func TestValidatePlan_SuperaCapacidadDiaria(t *testing.T) {
	booked := []Session{{Start: at(22, 18, 0), End: at(22, 19, 30), Status: SessionPending}}
	drafts := []SessionDraft{draft(at(22, 19, 30), 60)}
	if !violatedRules(t, ValidatePlan(drafts, exam(), availability(), booked, now))["RN-13"] {
		t.Fatal("se esperaba RN-13 por superar la capacidad diaria")
	}
}

func TestValidatePlan_SesionDespuesDelExamen(t *testing.T) {
	ex := exam()
	ex.StartsAt = at(22, 18, 30)
	drafts := []SessionDraft{draft(at(22, 18, 0), 60)}
	if !violatedRules(t, ValidatePlan(drafts, ex, availability(), nil, now))["RN-11"] {
		t.Fatal("se esperaba RN-11: la sesión termina después del examen")
	}
}

func TestValidatePlan_SesionEnElPasado(t *testing.T) {
	drafts := []SessionDraft{draft(at(13, 18, 0), 60)}
	if !violatedRules(t, ValidatePlan(drafts, exam(), availability(), nil, now))["RN-11"] {
		t.Fatal("se esperaba RN-11: la sesión empieza en el pasado")
	}
}

func TestValidatePlan_SinSesiones(t *testing.T) {
	if !violatedRules(t, ValidatePlan(nil, exam(), availability(), nil, now))["RN-10"] {
		t.Fatal("se esperaba error por plan vacío")
	}
}

func TestValidateSlots(t *testing.T) {
	ok := []Slot{{Weekday: time.Monday, StartMinute: 600, EndMinute: 720}, {Weekday: time.Monday, StartMinute: 720, EndMinute: 800}}
	if err := ValidateSlots(ok); err != nil {
		t.Fatalf("no se esperaba error: %v", err)
	}
	invertida := []Slot{{Weekday: time.Monday, StartMinute: 720, EndMinute: 600}}
	if !violatedRules(t, ValidateSlots(invertida))["RN-05"] {
		t.Fatal("se esperaba RN-05 por franja invertida")
	}
	superpuestas := []Slot{{Weekday: time.Friday, StartMinute: 600, EndMinute: 720}, {Weekday: time.Friday, StartMinute: 700, EndMinute: 800}}
	if !violatedRules(t, ValidateSlots(superpuestas))["RN-05"] {
		t.Fatal("se esperaba RN-05 por franjas superpuestas")
	}
}
