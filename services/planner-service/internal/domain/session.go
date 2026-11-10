package domain

import "time"

// ChangeStatus aplica RF-11: el estudiante marca una sesión reservada como
// completada o pospuesta, o deshace una que marcó como completada por error.
// Devuelve la sesión con el estado nuevo y si el plan queda desactualizado.
//
//   - completada: sólo una sesión pendiente que ya empezó (RN-48);
//   - pendiente: sólo para deshacer una completada;
//   - pospuesta: sólo una pendiente. Libera su horario, y el plan del examen
//     queda desactualizado para que el tema vuelva a considerarse al rehacerlo
//     (sección 6.3, RN-49). No se puede deshacer: el horario ya quedó libre.
//
// Pedir el estado que ya tiene no cambia nada (es idempotente).
func (s Session) ChangeStatus(to SessionStatus, now time.Time) (next Session, outdatePlan bool, err error) {
	if to == s.Status {
		return s, false, nil
	}
	fail := func(rule, msg string) (Session, bool, error) {
		return s, false, &ValidationError{Violations: []RuleViolation{{Rule: rule, Message: msg, SessionIndex: -1}}}
	}
	switch to {
	case SessionCompleted:
		if s.Status != SessionPending {
			return fail("RF-11", "sólo se puede completar una sesión pendiente")
		}
		if now.Before(s.Start) {
			return fail("RN-48", "la sesión todavía no empezó: se puede marcar como completada desde su hora de inicio")
		}
	case SessionPending:
		if s.Status != SessionCompleted {
			return fail("RN-49", "una sesión pospuesta no vuelve a pendiente: su tema entra en la próxima replanificación")
		}
	case SessionPostponed:
		if s.Status != SessionPending {
			return fail("RF-11", "sólo se puede posponer una sesión pendiente")
		}
		outdatePlan = true
	default:
		return fail("RF-11", "el estado tiene que ser pendiente, completada o pospuesta")
	}
	s.Status = to
	return s, outdatePlan, nil
}
