// Package rules implementa el motor de reglas: arma una propuesta de plan sin
// IA, a partir de la dificultad de la materia, los temas del examen y la
// disponibilidad del estudiante (RN-06, RN-07). Es la propuesta que se usa
// cuando el agente de IA no está disponible (RN-09).
//
// La lógica parte del planificador del proyecto Estudiario original
// (src/services/planner/engine.ts), adaptada para asignar horarios concretos
// dentro de las franjas disponibles.
package rules

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/MichiSil/ProyectoEstudiario/services/planner-service/internal/app"
	"github.com/MichiSil/ProyectoEstudiario/services/planner-service/internal/domain"
)

// ErrNoAvailability indica que no hay ningún horario libre entre hoy y el examen.
var ErrNoAvailability = errors.New("no hay horarios disponibles antes del examen")

// baseSessions es la cantidad base de sesiones según la dificultad (RN-07).
var baseSessions = map[domain.Difficulty]int{
	domain.DifficultyEasy:     3,
	domain.DifficultyMedium:   5,
	domain.DifficultyHard:     7,
	domain.DifficultyVeryHard: 9,
}

// Engine es el generador de planes por reglas. Implementa app.PlanGenerator.
type Engine struct{}

var _ app.PlanGenerator = Engine{}

func (Engine) Generate(_ context.Context, in app.ProposalInput) (app.Proposal, error) {
	av := in.Availability
	loc := av.Location
	if loc == nil {
		loc = time.UTC
	}
	duration := av.PreferredSessionMinutes
	if av.MaxDailyMinutes > 0 && duration > av.MaxDailyMinutes {
		duration = av.MaxDailyMinutes
	}
	if duration <= 0 {
		return app.Proposal{}, fmt.Errorf("la duración preferida de las sesiones tiene que ser positiva")
	}

	// RN-06: ventana de estudio según la dificultad. Si faltan menos días,
	// se usa el tiempo que queda.
	now := in.Now.In(loc)
	examStart := in.Exam.StartsAt.In(loc)
	windowStart := startOfDay(examStart).AddDate(0, 0, -domain.AnticipationDays[in.Subject.Difficulty])
	if windowStart.Before(startOfDay(now)) {
		windowStart = startOfDay(now)
	}

	candidates := freeStarts(windowStart, now, examStart, duration, av, in.Booked, loc)
	if len(candidates) == 0 {
		return app.Proposal{}, ErrNoAvailability
	}

	// RN-07: la cantidad de sesiones crece con la dificultad y con los temas.
	topics := cleanTopics(in.Exam.Topics)
	count := baseSessions[in.Subject.Difficulty] + len(topics)/2
	if count > len(candidates) {
		count = len(candidates)
	}

	items := objectives(topics, count)
	reasoning := fmt.Sprintf("%s recibió %d sesiones porque está marcada como %s y el examen tiene %d temas; el estudio empieza %d días antes del examen.",
		in.Subject.Name, count, difficultyLabel(in.Subject.Difficulty), len(topics), domain.AnticipationDays[in.Subject.Difficulty])

	drafts := make([]domain.SessionDraft, count)
	for i := range drafts {
		// Reparte las sesiones a lo largo de la ventana; la última queda lo
		// más cerca posible del examen (repaso).
		idx := len(candidates) - 1
		if count > 1 {
			idx = i * (len(candidates) - 1) / (count - 1)
		}
		drafts[i] = domain.SessionDraft{
			Start:           candidates[idx],
			DurationMinutes: duration,
			Topic:           items[i].topic,
			Objective:       items[i].objective,
			Reasoning:       reasoning,
		}
	}
	return app.Proposal{Drafts: drafts, GeneratedBy: domain.GeneratedByRules}, nil
}

// freeStarts devuelve, como máximo, un horario de inicio libre por día entre
// windowStart y el examen: el primero de las franjas de ese día que no se
// superpone con sesiones reservadas ni supera la capacidad diaria.
func freeStarts(windowStart, now, examStart time.Time, duration int, av domain.Availability, booked []domain.Session, loc *time.Location) []time.Time {
	slots := append([]domain.Slot(nil), av.Slots...)
	sort.Slice(slots, func(a, b int) bool { return slots[a].StartMinute < slots[b].StartMinute })

	var result []time.Time
	for day := windowStart; day.Before(examStart); day = day.AddDate(0, 0, 1) {
		used := bookedMinutes(day, booked, loc)
		if av.MaxDailyMinutes > 0 && used+duration > av.MaxDailyMinutes {
			continue
		}
	slots:
		for _, s := range slots {
			if s.Weekday != day.Weekday() {
				continue
			}
			for m := s.StartMinute; m+duration <= s.EndMinute; m += 15 {
				start := day.Add(time.Duration(m) * time.Minute)
				end := start.Add(time.Duration(duration) * time.Minute)
				if start.Before(now) || end.After(examStart) {
					continue
				}
				if overlaps(start, end, booked) {
					continue
				}
				result = append(result, start)
				break slots
			}
		}
	}
	return result
}

func bookedMinutes(day time.Time, booked []domain.Session, loc *time.Location) int {
	total := 0
	key := day.Format("2006-01-02")
	for _, b := range booked {
		if b.OccupiesCalendar() && b.Start.In(loc).Format("2006-01-02") == key {
			total += int(b.End.Sub(b.Start).Minutes())
		}
	}
	return total
}

func overlaps(start, end time.Time, booked []domain.Session) bool {
	for _, b := range booked {
		if b.OccupiesCalendar() && start.Before(b.End) && b.Start.Before(end) {
			return true
		}
	}
	return false
}

type item struct{ topic, objective string }

// objectives arma el tema y el objetivo de cada sesión: primero teoría, después
// ejercicios y repaso de errores por tema, y al final simulacro y repaso
// liviano (RN-07: las últimas sesiones son de repaso o simulacro).
func objectives(topics []string, count int) []item {
	if len(topics) == 0 {
		topics = []string{"el temario"}
	}
	review := 0
	switch {
	case count >= 5:
		review = 2
	case count >= 3:
		review = 1
	}
	var items []item
	for i := 0; i < count-review; i++ {
		topic := topics[i%len(topics)]
		switch i / len(topics) {
		case 0:
			items = append(items, item{topic, "Leer teoría de " + topic})
		case 1:
			items = append(items, item{topic, "Resolver ejercicios de " + topic})
		default:
			items = append(items, item{topic, "Repasar errores de " + topic})
		}
	}
	if review >= 1 {
		items = append(items, item{"Repaso general", "Simulacro o parcial de práctica"})
	}
	if review >= 2 {
		items = append(items, item{"Repaso general", "Repaso liviano antes del examen"})
	}
	return items
}

func cleanTopics(raw []string) []string {
	var out []string
	for _, t := range raw {
		if t = strings.TrimSpace(t); t != "" {
			out = append(out, t)
		}
	}
	return out
}

func startOfDay(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, t.Location())
}

func difficultyLabel(d domain.Difficulty) string {
	switch d {
	case domain.DifficultyEasy:
		return "fácil"
	case domain.DifficultyMedium:
		return "media"
	case domain.DifficultyHard:
		return "difícil"
	default:
		return "muy difícil"
	}
}
