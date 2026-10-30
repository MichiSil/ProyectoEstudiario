package rules

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/MichiSil/ProyectoEstudiario/services/planner-service/internal/app"
	"github.com/MichiSil/ProyectoEstudiario/services/planner-service/internal/domain"
)

var cba, _ = time.LoadLocation("America/Argentina/Cordoba")

// Lunes 19/10/2026 a las 10:00.
var now = time.Date(2026, 10, 19, 10, 0, 0, 0, cba)

func everyDay(start, end int) []domain.Slot {
	var slots []domain.Slot
	for d := time.Sunday; d <= time.Saturday; d++ {
		slots = append(slots, domain.Slot{Weekday: d, StartMinute: start, EndMinute: end})
	}
	return slots
}

func input(difficulty domain.Difficulty, examIn time.Duration, topics ...string) app.ProposalInput {
	return app.ProposalInput{
		Subject: domain.Subject{ID: "mat-1", Name: "Análisis II", Difficulty: difficulty},
		Exam:    domain.Exam{ID: "ex-1", StartsAt: now.Add(examIn), Topics: topics, Status: domain.ExamScheduled},
		Availability: domain.Availability{
			Slots:                   everyDay(18*60, 21*60),
			MaxDailyMinutes:         120,
			PreferredSessionMinutes: 90,
			Location:                cba,
		},
		Now: now,
	}
}

func generate(t *testing.T, in app.ProposalInput) app.Proposal {
	t.Helper()
	p, err := Engine{}.Generate(context.Background(), in)
	if err != nil {
		t.Fatalf("no se esperaba error: %v", err)
	}
	if p.GeneratedBy != domain.GeneratedByRules {
		t.Fatalf("GeneratedBy = %q, se esperaba %q", p.GeneratedBy, domain.GeneratedByRules)
	}
	// Toda propuesta del motor tiene que poder confirmarse tal cual.
	if err := domain.ValidatePlan(p.Drafts, in.Exam, in.Availability, in.Booked, in.Now); err != nil {
		t.Fatalf("la propuesta no cumple las reglas de confirmación: %v", err)
	}
	return p
}

// CA-04: materia muy difícil con examen en 30 días: las sesiones empiezan como
// mucho 21 días antes y las últimas son de repaso o simulacro.
func TestGenerate_MuyDificilRespetaAnticipacionYRepaso(t *testing.T) {
	in := input(domain.DifficultyVeryHard, 30*24*time.Hour, "Integrales dobles", "Integrales triples", "Cambio de variables")
	p := generate(t, in)

	limit := in.Exam.StartsAt.AddDate(0, 0, -21).Add(-24 * time.Hour)
	for i, d := range p.Drafts {
		if d.Start.Before(limit) {
			t.Errorf("la sesión %d empieza el %s, antes de los 21 días de anticipación", i, d.Start)
		}
	}
	if len(p.Drafts) != 10 { // 9 por dificultad + 3 temas / 2
		t.Errorf("se esperaban 10 sesiones, llegaron %d", len(p.Drafts))
	}
	last := p.Drafts[len(p.Drafts)-2:]
	for _, d := range last {
		if d.Topic != "Repaso general" {
			t.Errorf("se esperaba que las últimas sesiones fueran de repaso, llegó %q", d.Topic)
		}
	}
	// Sin material ni modalidad, el plan no supone que hay teoría para leer.
	if p.Drafts[0].Objective != "Estudiar Integrales dobles" {
		t.Errorf("la primera sesión debería ser estudiar el primer tema, llegó %q", p.Drafts[0].Objective)
	}
	for _, d := range p.Drafts {
		if strings.Contains(strings.ToLower(d.Objective), "teoría") || strings.Contains(strings.ToLower(d.Objective), "teórico") {
			t.Errorf("sin material ni modalidad no tiene que inventar teoría: %q", d.Objective)
		}
	}
}

// Con material cargado, cada sesión trabaja sobre un material real, nombrado por su título.
func TestGenerate_UsaElMaterialCargado(t *testing.T) {
	in := input(domain.DifficultyMedium, 30*24*time.Hour, "Ondas")
	in.Exam.Modality = domain.ModalityMixed
	in.Materials = []domain.Material{
		{ID: "m1", Kind: domain.MaterialTheory, Title: "Unidad 1 — Ondas"},
		{ID: "m2", Kind: domain.MaterialPractice, Title: "TP 3"},
	}
	p := generate(t, in)
	if p.Drafts[0].Objective != "Estudiar Unidad 1 — Ondas" || p.Drafts[1].Objective != "Resolver TP 3" {
		t.Fatalf("se esperaba estudiar el teórico y después resolver el práctico, llegó %q y %q", p.Drafts[0].Objective, p.Drafts[1].Objective)
	}
	known := map[string]bool{"Unidad 1 — Ondas": true, "TP 3": true, "Repaso general": true}
	for _, d := range p.Drafts {
		if !known[d.Topic] {
			t.Errorf("la sesión habla de algo que no está cargado: %q", d.Topic)
		}
	}
}

// Un examen teórico se arma con los teóricos, aunque haya prácticos cargados.
func TestGenerate_ExamenTeoricoUsaLosTeoricos(t *testing.T) {
	in := input(domain.DifficultyEasy, 30*24*time.Hour)
	in.Exam.Modality = domain.ModalityTheory
	in.Materials = []domain.Material{
		{ID: "m1", Kind: domain.MaterialTheory, Title: "Capítulo 2"},
		{ID: "m2", Kind: domain.MaterialPractice, Title: "Guía de ejercicios"},
	}
	for _, d := range generate(t, in).Drafts {
		if d.Topic == "Guía de ejercicios" {
			t.Fatalf("un examen teórico no debería planificar el práctico: %q", d.Objective)
		}
	}
}

// Un examen práctico sin material propone ejercicios de los temas, no lectura de teoría.
func TestGenerate_ExamenPracticoSinMaterial(t *testing.T) {
	in := input(domain.DifficultyEasy, 30*24*time.Hour, "Derivadas")
	in.Exam.Modality = domain.ModalityPractice
	p := generate(t, in)
	if p.Drafts[0].Objective != "Practicar ejercicios de Derivadas" {
		t.Fatalf("se esperaba practicar ejercicios, llegó %q", p.Drafts[0].Objective)
	}
}

func TestGenerate_MasDificilMasSesiones(t *testing.T) {
	facil := generate(t, input(domain.DifficultyEasy, 30*24*time.Hour, "Tema"))
	dificil := generate(t, input(domain.DifficultyHard, 30*24*time.Hour, "Tema"))
	if len(dificil.Drafts) <= len(facil.Drafts) {
		t.Errorf("una materia difícil debería tener más sesiones: fácil %d, difícil %d", len(facil.Drafts), len(dificil.Drafts))
	}
}

// Si faltan menos días que la anticipación, se usa el tiempo que queda.
func TestGenerate_ExamenCercano(t *testing.T) {
	p := generate(t, input(domain.DifficultyVeryHard, 3*24*time.Hour, "Tema"))
	if len(p.Drafts) == 0 || len(p.Drafts) > 3 {
		t.Errorf("con 3 días hasta el examen se esperaban entre 1 y 3 sesiones, llegaron %d", len(p.Drafts))
	}
}

func TestGenerate_EvitaSesionesReservadasYCapacidad(t *testing.T) {
	in := input(domain.DifficultyMedium, 10*24*time.Hour, "Tema")
	// Mañana ya tiene 60 minutos reservados de 18:00 a 19:00: con 120 de
	// capacidad no entra otra sesión de 90 minutos ese día.
	tomorrow := time.Date(2026, 10, 20, 18, 0, 0, 0, cba)
	in.Booked = []domain.Session{{Start: tomorrow, End: tomorrow.Add(time.Hour), Status: domain.SessionPending}}
	p := generate(t, in)
	for _, d := range p.Drafts {
		if d.Start.Format("2006-01-02") == "2026-10-20" {
			t.Errorf("no debería proponer sesiones el 20/10: superaría la capacidad diaria")
		}
	}
}

func TestGenerate_SinDisponibilidad(t *testing.T) {
	in := input(domain.DifficultyMedium, 10*24*time.Hour, "Tema")
	in.Availability.Slots = nil
	_, err := Engine{}.Generate(context.Background(), in)
	if !errors.Is(err, ErrNoAvailability) {
		t.Fatalf("se esperaba ErrNoAvailability, llegó %v", err)
	}
}

// El caso que encontramos probando: hoy viernes, el sábado libre de 8 a 20 y
// el parcial el lunes a las 9. Antes el motor proponía una sola sesión de un
// solo tema, porque buscaba como máximo un horario por día.
func weekendInput(maxDaily int) app.ProposalInput {
	friday := time.Date(2026, 10, 23, 10, 0, 0, 0, cba)
	return app.ProposalInput{
		Subject: domain.Subject{ID: "mat-1", Name: "Análisis", Difficulty: domain.DifficultyHard},
		Exam: domain.Exam{ID: "ex-1", StartsAt: time.Date(2026, 10, 26, 9, 0, 0, 0, cba),
			Topics: []string{"Límites", "Derivadas", "Integrales"}, Status: domain.ExamScheduled},
		Availability: domain.Availability{
			Slots:                   []domain.Slot{{Weekday: time.Saturday, StartMinute: 8 * 60, EndMinute: 20 * 60}},
			MaxDailyMinutes:         maxDaily,
			PreferredSessionMinutes: 60,
			Location:                cba,
		},
		Now: friday,
	}
}

func coveredTopics(p app.Proposal) string {
	var all []string
	for _, d := range p.Drafts {
		all = append(all, d.Topic)
	}
	return strings.Join(all, " | ")
}

func TestGenerate_VariasSesionesElMismoDia(t *testing.T) {
	p := generate(t, weekendInput(600))
	if len(p.Drafts) != 8 { // 7 por dificultad + 3 temas / 2
		t.Fatalf("se esperaban 8 sesiones el sábado, llegaron %d: %s", len(p.Drafts), coveredTopics(p))
	}
	for i := 1; i < len(p.Drafts); i++ {
		if gap := p.Drafts[i].Start.Sub(p.Drafts[i-1].End()); gap < breakMinutes*time.Minute {
			t.Errorf("entre la sesión %d y la %d hay %s de descanso, se esperaban al menos %d min", i-1, i, gap, breakMinutes)
		}
	}
	for _, topic := range []string{"Límites", "Derivadas", "Integrales", "Repaso general"} {
		if !strings.Contains(coveredTopics(p), topic) {
			t.Errorf("falta %q en el plan: %s", topic, coveredTopics(p))
		}
	}
	if len(p.Warnings) != 0 {
		t.Errorf("con tiempo suficiente no tiene que haber avisos: %v", p.Warnings)
	}
}

// Si la capacidad diaria no alcanza, el plan cubre igual todos los temas
// (juntándolos) y avisa que falta tiempo.
func TestGenerate_PocoTiempoCubreTodosLosTemasYAvisa(t *testing.T) {
	p := generate(t, weekendInput(120))
	if len(p.Drafts) != 2 {
		t.Fatalf("con 120 minutos de capacidad entran 2 sesiones de 60, llegaron %d", len(p.Drafts))
	}
	for _, topic := range []string{"Límites", "Derivadas", "Integrales"} {
		if !strings.Contains(coveredTopics(p), topic) {
			t.Errorf("falta %q en el plan: %s", topic, coveredTopics(p))
		}
	}
	if len(p.Warnings) != 1 || p.Warnings[0] != app.WarningInsufficientTime {
		t.Errorf("se esperaba el aviso %s, llegó %v", app.WarningInsufficientTime, p.Warnings)
	}
}

// Con varios días disponibles, primero se reparte una sesión por día y recién
// después se agrega una segunda en el mismo día.
func TestGenerate_PrimeroUnaSesionPorDia(t *testing.T) {
	in := input(domain.DifficultyEasy, 10*24*time.Hour, "Tema")
	in.Availability.MaxDailyMinutes = 300
	in.Availability.PreferredSessionMinutes = 60
	p := generate(t, in)
	seen := map[string]bool{}
	for _, d := range p.Drafts {
		day := d.Start.Format("2006-01-02")
		if seen[day] {
			t.Errorf("hay dos sesiones el %s habiendo días libres", day)
		}
		seen[day] = true
	}
}
