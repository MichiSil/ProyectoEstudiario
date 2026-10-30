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

	// RN-07: la cantidad de sesiones crece con la dificultad y con lo que hay
	// para estudiar (el material cargado o, si no hay, los temas del examen).
	topics := cleanTopics(in.Exam.Topics)
	units := len(topics)
	if len(in.Materials) > 0 {
		units = len(in.Materials)
	}
	count := baseSessions[in.Subject.Difficulty] + units/2
	if count > len(candidates) {
		count = len(candidates)
	}

	items := objectives(in.Exam.Modality, in.Materials, topics, count)
	what := fmt.Sprintf("el examen tiene %d temas", len(topics))
	if len(in.Materials) > 0 {
		what = fmt.Sprintf("hay %d materiales cargados para el examen", len(in.Materials))
	}
	reasoning := fmt.Sprintf("%s recibió %d sesiones porque está marcada como %s y %s; el estudio empieza %d días antes del examen.",
		in.Subject.Name, count, difficultyLabel(in.Subject.Difficulty), what, domain.AnticipationDays[in.Subject.Difficulty])

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

// unit es algo concreto para estudiar: un material cargado o un tema del examen.
type unit struct {
	topic string
	// first y again son lo que se hace en la primera pasada y en las siguientes.
	first, again string
}

// objectives arma el tema y el objetivo de cada sesión con lo que el
// estudiante realmente tiene: su material (teóricos y prácticos) y la
// modalidad del examen. Nunca inventa un teórico o un práctico que no está
// cargado: si no hay material, usa los temas del examen sin suponer si hay que
// leer teoría o hacer ejercicios, salvo que la modalidad lo diga. Las últimas
// sesiones son de repaso o simulacro (RN-07).
func objectives(modality domain.ExamModality, materials []domain.Material, topics []string, count int) []item {
	units, closing := studyUnits(modality, materials, topics)
	review := 0
	switch {
	case count >= 5:
		review = 2
	case count >= 3:
		review = 1
	}
	var items []item
	for i := 0; i < count-review; i++ {
		u := units[i%len(units)]
		if i < len(units) {
			items = append(items, item{u.topic, u.first})
		} else {
			items = append(items, item{u.topic, u.again})
		}
	}
	if review >= 1 {
		items = append(items, item{"Repaso general", closing})
	}
	if review >= 2 {
		items = append(items, item{"Repaso general", "Repaso liviano antes del examen"})
	}
	return items
}

// studyUnits ordena lo que hay para estudiar y elige el cierre del plan.
func studyUnits(modality domain.ExamModality, materials []domain.Material, topics []string) ([]unit, string) {
	if len(materials) > 0 {
		var theory, practice []unit
		for _, m := range materials {
			if m.Kind == domain.MaterialPractice {
				practice = append(practice, unit{m.Title, "Resolver " + m.Title, "Rehacer lo que costó de " + m.Title})
			} else {
				theory = append(theory, unit{m.Title, "Estudiar " + m.Title, "Repasar " + m.Title})
			}
		}
		switch {
		case modality == domain.ModalityTheory && len(theory) > 0:
			return theory, "Repasar todos los teóricos que entran"
		case modality == domain.ModalityPractice && len(practice) > 0:
			// La teoría que haya cargada va primero, como base para los prácticos.
			return append(theory, practice...), "Simulacro: resolver ejercicios como en el examen"
		case len(practice) > 0:
			return append(theory, practice...), "Simulacro: resolver ejercicios como en el examen"
		default:
			return theory, "Repasar todo el material que entra"
		}
	}

	if len(topics) == 0 {
		topics = []string{"el temario"}
	}
	var units []unit
	switch modality {
	case domain.ModalityTheory:
		for _, t := range topics {
			units = append(units, unit{t, "Estudiar " + t, "Repasar " + t})
		}
		return units, "Repasar todos los temas"
	case domain.ModalityPractice:
		for _, t := range topics {
			units = append(units, unit{t, "Practicar ejercicios de " + t, "Rehacer los ejercicios que costaron de " + t})
		}
		return units, "Simulacro: resolver ejercicios como en el examen"
	case domain.ModalityMixed:
		for _, t := range topics {
			units = append(units, unit{t, "Estudiar " + t, "Practicar ejercicios de " + t})
		}
		return units, "Simulacro: repasar teoría y resolver ejercicios"
	default:
		// Sin material ni modalidad no se sabe si hay teoría o ejercicios: no se supone.
		for _, t := range topics {
			units = append(units, unit{t, "Estudiar " + t, "Repasar " + t})
		}
		return units, "Repaso general del temario"
	}
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
