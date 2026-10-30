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

	days := freeStarts(windowStart, now, examStart, duration, av, in.Booked, loc)
	available := 0
	for _, d := range days {
		available += len(d)
	}
	if available == 0 {
		return app.Proposal{}, ErrNoAvailability
	}

	// RN-07: la cantidad de sesiones crece con la dificultad y con lo que hay
	// para estudiar (el material cargado o, si no hay, los temas del examen).
	topics := cleanTopics(in.Exam.Topics)
	units := len(topics)
	if len(in.Materials) > 0 {
		units = len(in.Materials)
	}
	wanted := baseSessions[in.Subject.Difficulty] + units/2
	count := min(wanted, available)
	var warnings []string
	if count < wanted {
		warnings = append(warnings, app.WarningInsufficientTime)
	}

	items := objectives(in.Exam.Modality, in.Materials, topics, count)
	what := fmt.Sprintf("el examen tiene %d temas", len(topics))
	if len(in.Materials) > 0 {
		what = fmt.Sprintf("hay %d materiales cargados para el examen", len(in.Materials))
	}
	reasoning := fmt.Sprintf("%s recibió %d sesiones porque está marcada como %s y %s; el estudio empieza %d días antes del examen.",
		in.Subject.Name, count, difficultyLabel(in.Subject.Difficulty), what, domain.AnticipationDays[in.Subject.Difficulty])
	if count < wanted {
		reasoning = fmt.Sprintf("%s Harían falta %d sesiones, pero antes del examen sólo hay lugar para %d: se juntaron temas.",
			reasoning, wanted, count)
	}

	starts := spread(days, count)
	drafts := make([]domain.SessionDraft, count)
	for i := range drafts {
		drafts[i] = domain.SessionDraft{
			Start:           starts[i],
			DurationMinutes: duration,
			Topic:           items[i].topic,
			Objective:       items[i].objective,
			Reasoning:       reasoning,
		}
	}
	return app.Proposal{Drafts: drafts, GeneratedBy: domain.GeneratedByRules, Warnings: warnings}, nil
}

// breakMinutes es el descanso entre dos sesiones del mismo día.
const breakMinutes = 15

// freeStarts devuelve, día por día entre windowStart y el examen, los horarios
// de inicio libres: tantas sesiones como entren en las franjas de ese día, con
// un descanso entre una y otra, sin superponerse con sesiones reservadas y sin
// superar la capacidad diaria (RN-10 a RN-13).
func freeStarts(windowStart, now, examStart time.Time, duration int, av domain.Availability, booked []domain.Session, loc *time.Location) [][]time.Time {
	slots := append([]domain.Slot(nil), av.Slots...)
	sort.Slice(slots, func(a, b int) bool { return slots[a].StartMinute < slots[b].StartMinute })

	var result [][]time.Time
	for day := windowStart; day.Before(examStart); day = day.AddDate(0, 0, 1) {
		left := -1 // sin límite
		if av.MaxDailyMinutes > 0 {
			left = av.MaxDailyMinutes - bookedMinutes(day, booked, loc)
		}
		var starts []time.Time
		for _, s := range slots {
			if s.Weekday != day.Weekday() {
				continue
			}
			for m := s.StartMinute; m+duration <= s.EndMinute && (left < 0 || left >= duration); {
				start := day.Add(time.Duration(m) * time.Minute)
				end := start.Add(time.Duration(duration) * time.Minute)
				if start.Before(now) || end.After(examStart) || overlaps(start, end, booked) {
					m += 15
					continue
				}
				starts = append(starts, start)
				if left >= 0 {
					left -= duration
				}
				m += duration + breakMinutes
			}
		}
		if len(starts) > 0 {
			result = append(result, starts)
		}
	}
	return result
}

// spread elige count horarios repartidos a lo largo de la ventana: primero uno
// por día (repartidos entre el primero y el último, así la última sesión queda
// cerca del examen), después un segundo en cada día, y así. Devuelve los
// horarios en orden.
func spread(days [][]time.Time, count int) []time.Time {
	var chosen []time.Time
	for layer := 0; len(chosen) < count; layer++ {
		var row []time.Time
		for _, d := range days {
			if layer < len(d) {
				row = append(row, d[layer])
			}
		}
		need := count - len(chosen)
		if need >= len(row) {
			chosen = append(chosen, row...)
			continue
		}
		for i := 0; i < need; i++ {
			idx := len(row) - 1
			if need > 1 {
				idx = i * (len(row) - 1) / (need - 1)
			}
			chosen = append(chosen, row[idx])
		}
	}
	sort.Slice(chosen, func(a, b int) bool { return chosen[a].Before(chosen[b]) })
	return chosen
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
	// Primero se cubren todos los temas: si no alcanzan las sesiones, se
	// resigna el repaso y, si aun así faltan, se juntan temas en una sesión.
	review = max(0, min(review, count-len(units)))
	study := count - review

	var items []item
	if study < len(units) {
		for i := 0; i < study; i++ {
			group := units[i*len(units)/study : (i+1)*len(units)/study]
			var names, goals []string
			for _, u := range group {
				names = append(names, u.topic)
				goals = append(goals, u.first)
			}
			items = append(items, item{joinList(names), strings.Join(goals, "; ")})
		}
	}
	for i := 0; len(items) < study; i++ {
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

// joinList une nombres como en castellano: "A", "A y B", "A, B y C".
func joinList(names []string) string {
	if len(names) <= 1 {
		return strings.Join(names, "")
	}
	return strings.Join(names[:len(names)-1], ", ") + " y " + names[len(names)-1]
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
