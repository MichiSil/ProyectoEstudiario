package domain

import (
	"sort"
	"time"
)

// ValidatePlan verifica las reglas que tiene que cumplir un plan para poder
// confirmarse (sección 6.2 del SPEC, "Confirmación del plan"). Devuelve un
// *ValidationError con todas las reglas incumplidas, o nil.
//
// Las reglas de idempotencia y de un único plan vigente (RN-14, RN-15) no se
// validan acá: dependen del estado guardado y las garantiza la persistencia.
func ValidatePlan(drafts []SessionDraft, exam Exam, av Availability, booked []Session, now time.Time) error {
	verr := &ValidationError{}
	loc := av.Location
	if loc == nil {
		loc = time.UTC
	}

	if len(drafts) == 0 {
		verr.add("RN-10", -1, "el plan no tiene sesiones")
		return verr
	}

	for i, d := range drafts {
		if d.DurationMinutes <= 0 {
			verr.add("RN-10", i, "la duración de la sesión tiene que ser positiva")
			continue
		}
		if d.Start.Before(now) {
			verr.add("RN-11", i, "la sesión empieza en el pasado")
		}
		// RN-10: la sesión cae completa dentro de una franja disponible.
		if !fitsInSlot(d, av.Slots, loc) {
			verr.add("RN-10", i, "la sesión del %s no cae completa dentro de una franja disponible",
				d.Start.In(loc).Format("2006-01-02 15:04"))
		}
		// RN-11: la sesión termina antes del inicio del examen.
		if d.End().After(exam.StartsAt) {
			verr.add("RN-11", i, "la sesión termina después del inicio del examen")
		}
	}

	// RN-12: ninguna sesión se superpone con otra (de este plan o ya reservada).
	type interval struct {
		start, end time.Time
		index      int // -1 = sesión ya reservada
	}
	var all []interval
	for _, s := range booked {
		if s.OccupiesCalendar() {
			all = append(all, interval{s.Start, s.End, -1})
		}
	}
	for i, d := range drafts {
		if d.DurationMinutes > 0 {
			all = append(all, interval{d.Start, d.End(), i})
		}
	}
	sort.Slice(all, func(a, b int) bool { return all[a].start.Before(all[b].start) })
	for i := 1; i < len(all); i++ {
		prev, cur := all[i-1], all[i]
		if cur.start.Before(prev.end) && (prev.index >= 0 || cur.index >= 0) {
			idx := cur.index
			if idx < 0 {
				idx = prev.index
			}
			verr.add("RN-12", idx, "la sesión se superpone con otra sesión del %s",
				prev.start.In(loc).Format("2006-01-02 15:04"))
		}
	}

	// RN-13: la suma de minutos de cada día no supera la capacidad diaria.
	if av.MaxDailyMinutes > 0 {
		minutesByDay := map[string]int{}
		touched := map[string]bool{}
		for _, s := range booked {
			if s.OccupiesCalendar() {
				minutesByDay[dayKey(s.Start, loc)] += int(s.End.Sub(s.Start).Minutes())
			}
		}
		for _, d := range drafts {
			k := dayKey(d.Start, loc)
			minutesByDay[k] += d.DurationMinutes
			touched[k] = true
		}
		days := make([]string, 0, len(touched))
		for k := range touched {
			days = append(days, k)
		}
		sort.Strings(days)
		for _, k := range days {
			if minutesByDay[k] > av.MaxDailyMinutes {
				verr.add("RN-13", -1, "el %s suma %d minutos y la capacidad diaria es de %d",
					k, minutesByDay[k], av.MaxDailyMinutes)
			}
		}
	}

	return verr.orNil()
}

func fitsInSlot(d SessionDraft, slots []Slot, loc *time.Location) bool {
	start := d.Start.In(loc)
	startMin := start.Hour()*60 + start.Minute()
	endMin := startMin + d.DurationMinutes
	for _, s := range slots {
		if s.Weekday == start.Weekday() && startMin >= s.StartMinute && endMin <= s.EndMinute {
			return true
		}
	}
	return false
}

func dayKey(t time.Time, loc *time.Location) string { return t.In(loc).Format("2006-01-02") }

// ValidateSlots verifica que las franjas de disponibilidad sean coherentes (RN-05).
func ValidateSlots(slots []Slot) error {
	verr := &ValidationError{}
	byDay := map[time.Weekday][]Slot{}
	for i, s := range slots {
		if s.StartMinute < 0 || s.EndMinute > 24*60 || s.StartMinute >= s.EndMinute {
			verr.add("RN-05", i, "la franja tiene que empezar antes de terminar, dentro del mismo día")
			continue
		}
		byDay[s.Weekday] = append(byDay[s.Weekday], s)
	}
	for day := time.Sunday; day <= time.Saturday; day++ {
		list := byDay[day]
		sort.Slice(list, func(a, b int) bool { return list[a].StartMinute < list[b].StartMinute })
		for i := 1; i < len(list); i++ {
			if list[i].StartMinute < list[i-1].EndMinute {
				verr.add("RN-05", -1, "las franjas del día %d de la semana se superponen", int(day))
			}
		}
	}
	return verr.orNil()
}
