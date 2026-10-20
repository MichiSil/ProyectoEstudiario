package ai

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/MichiSil/ProyectoEstudiario/services/planner-service/internal/app"
	"github.com/MichiSil/ProyectoEstudiario/services/planner-service/internal/domain"
)

const systemPrompt = `Sos un asistente que arma planes de estudio para estudiantes universitarios.
Respondés SIEMPRE con un único objeto JSON, sin texto antes ni después, con esta forma:
{
  "sessions": [
    {
      "date": "YYYY-MM-DD",
      "startTime": "HH:mm",
      "durationMinutes": 90,
      "topic": "tema del examen que se estudia",
      "objective": "qué se hace en la sesión, por ejemplo Leer teoría de Integrales",
      "reasoning": "por qué la sesión va en ese día y con ese objetivo"
    }
  ]
}
Reglas que el plan tiene que cumplir sí o sí:
- Cada sesión empieza y termina dentro de una de las franjas disponibles.
- Todas las sesiones duran exactamente la duración indicada.
- Ninguna sesión empieza antes del inicio de la ventana de estudio ni antes del momento actual.
- Todas las sesiones terminan antes del inicio del examen.
- Ninguna sesión se superpone con otra ni con las sesiones ya reservadas.
- La suma de minutos de un día (contando las reservadas) no supera la capacidad diaria.
- Las sesiones van en orden cronológico: primero teoría, después ejercicios y, si hay 3 sesiones o más, las últimas son de simulacro o repaso.
Las fechas y horas son locales a la zona horaria indicada. Escribí los textos en español.`

var weekdayNames = [...]string{"domingo", "lunes", "martes", "miercoles", "jueves", "viernes", "sabado"}

var difficultyNames = map[domain.Difficulty]string{
	domain.DifficultyEasy:     "fácil",
	domain.DifficultyMedium:   "media",
	domain.DifficultyHard:     "difícil",
	domain.DifficultyVeryHard: "muy difícil",
}

// suggestedSessions es la cantidad orientativa de sesiones (RN-07), la misma
// que usa el motor de reglas.
var suggestedSessions = map[domain.Difficulty]int{
	domain.DifficultyEasy:     3,
	domain.DifficultyMedium:   5,
	domain.DifficultyHard:     7,
	domain.DifficultyVeryHard: 9,
}

// userPrompt describe el examen, la dificultad, los temas y la disponibilidad
// del estudiante.
func userPrompt(in app.ProposalInput) string {
	loc := location(in)
	topics := cleanTopics(in.Exam.Topics)
	var b strings.Builder

	fmt.Fprintf(&b, "Zona horaria: %s\n", loc.String())
	fmt.Fprintf(&b, "Momento actual: %s\n", in.Now.In(loc).Format("2006-01-02 15:04"))
	fmt.Fprintf(&b, "Materia: %s (dificultad %s)\n", in.Subject.Name, difficultyNames[in.Subject.Difficulty])
	fmt.Fprintf(&b, "Examen: %s, empieza el %s\n", examType(in.Exam.Type), in.Exam.StartsAt.In(loc).Format("2006-01-02 15:04"))
	if len(topics) > 0 {
		fmt.Fprintf(&b, "Temas, en el orden en que conviene estudiarlos: %s\n", strings.Join(topics, "; "))
	} else {
		b.WriteString("Temas: no se indicaron; usá \"el temario\" como tema.\n")
	}
	fmt.Fprintf(&b, "Ventana de estudio: desde el %s (RN-06: %d días de anticipación según la dificultad)\n",
		windowStart(in).Format("2006-01-02"), domain.AnticipationDays[in.Subject.Difficulty])
	fmt.Fprintf(&b, "Duración de cada sesión: %d minutos\n", sessionMinutes(in.Availability))
	fmt.Fprintf(&b, "Capacidad diaria: %d minutos\n", in.Availability.MaxDailyMinutes)
	fmt.Fprintf(&b, "Cantidad sugerida de sesiones: %d (puede ser menor si no entran)\n",
		suggestedSessions[in.Subject.Difficulty]+len(topics)/2)

	b.WriteString("Franjas disponibles cada semana:\n")
	slots := append([]domain.Slot(nil), in.Availability.Slots...)
	sort.Slice(slots, func(a, c int) bool {
		if slots[a].Weekday != slots[c].Weekday {
			return slots[a].Weekday < slots[c].Weekday
		}
		return slots[a].StartMinute < slots[c].StartMinute
	})
	for _, s := range slots {
		fmt.Fprintf(&b, "- %s de %s a %s\n", weekdayNames[s.Weekday], clock(s.StartMinute), clock(s.EndMinute))
	}

	var booked []domain.Session
	for _, s := range in.Booked {
		if s.OccupiesCalendar() && s.End.After(in.Now) && s.Start.Before(in.Exam.StartsAt) {
			booked = append(booked, s)
		}
	}
	if len(booked) == 0 {
		b.WriteString("Sesiones ya reservadas: ninguna\n")
	} else {
		b.WriteString("Sesiones ya reservadas (no se pueden pisar):\n")
		for _, s := range booked {
			fmt.Fprintf(&b, "- %s de %s a %s\n", s.Start.In(loc).Format("2006-01-02"),
				s.Start.In(loc).Format("15:04"), s.End.In(loc).Format("15:04"))
		}
	}
	return b.String()
}

func location(in app.ProposalInput) *time.Location {
	if in.Availability.Location != nil {
		return in.Availability.Location
	}
	return time.UTC
}

// windowStart es el primer día en que se puede estudiar (RN-06): la fecha del
// examen menos la anticipación, o hoy si falta menos.
func windowStart(in app.ProposalInput) time.Time {
	loc := location(in)
	start := startOfDay(in.Exam.StartsAt.In(loc)).AddDate(0, 0, -domain.AnticipationDays[in.Subject.Difficulty])
	if today := startOfDay(in.Now.In(loc)); start.Before(today) {
		return today
	}
	return start
}

// sessionMinutes es la duración fija de las sesiones: la preferida, o la
// capacidad diaria si es menor (igual que el motor de reglas).
func sessionMinutes(av domain.Availability) int {
	if av.MaxDailyMinutes > 0 && av.PreferredSessionMinutes > av.MaxDailyMinutes {
		return av.MaxDailyMinutes
	}
	return av.PreferredSessionMinutes
}

func examType(t domain.ExamType) string {
	if t == "" {
		return string(domain.ExamParcial)
	}
	return string(t)
}

func clock(minute int) string { return fmt.Sprintf("%02d:%02d", minute/60, minute%60) }

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
