package ai

import (
	"strings"
	"testing"
	"time"
)

// Las franjas de hoy y del día del examen se recortan a lo que todavía se
// puede usar, para que el modelo no proponga sesiones que RN-11 descarta.
func TestUserPrompt_RecortaFranjasDeHoyYDelExamen(t *testing.T) {
	cases := []struct {
		name     string
		now      time.Time
		exam     time.Time
		want     []string
		dontWant []string
	}{
		{
			// Lo que pasó en la nube el 11/11: a las 19:06 el prompt decía
			// "hoy de 18:00 a 21:00" y el modelo proponía las 18:00.
			name:     "hoy, con la franja empezada",
			now:      time.Date(2026, 10, 19, 19, 6, 0, 0, cba),
			exam:     time.Date(2026, 10, 30, 10, 0, 0, 0, cba),
			want:     []string{"2026-10-19 (lunes): 19:15 a 21:00", "2026-10-20 (martes): 18:00 a 21:00"},
			dontWant: []string{"2026-10-19 (lunes): 18:00"},
		},
		{
			name:     "hoy, sin lugar para una sesión de 90 minutos",
			now:      time.Date(2026, 10, 19, 19, 40, 0, 0, cba),
			exam:     time.Date(2026, 10, 30, 10, 0, 0, 0, cba),
			want:     []string{"2026-10-20 (martes): 18:00 a 21:00"},
			dontWant: []string{"2026-10-19 (lunes)"},
		},
		{
			name: "hoy, antes de la franja",
			now:  time.Date(2026, 10, 19, 10, 0, 0, 0, cba),
			exam: time.Date(2026, 10, 30, 10, 0, 0, 0, cba),
			want: []string{"2026-10-19 (lunes): 18:00 a 21:00"},
		},
		{
			name:     "el día del examen, antes de la franja",
			now:      time.Date(2026, 10, 19, 10, 0, 0, 0, cba),
			exam:     time.Date(2026, 10, 30, 10, 0, 0, 0, cba),
			want:     []string{"2026-10-29 (jueves): 18:00 a 21:00"},
			dontWant: []string{"2026-10-30 (viernes)"},
		},
		{
			name: "el día del examen, en el medio de la franja",
			now:  time.Date(2026, 10, 19, 10, 0, 0, 0, cba),
			exam: time.Date(2026, 10, 30, 19, 30, 0, 0, cba),
			want: []string{"2026-10-30 (viernes): 18:00 a 19:30"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := input()
			in.Now = tc.now
			in.Exam.StartsAt = tc.exam
			prompt := userPrompt(in)
			for _, w := range tc.want {
				if !strings.Contains(prompt, w) {
					t.Errorf("el prompt no tiene %q:\n%s", w, prompt)
				}
			}
			for _, w := range tc.dontWant {
				if strings.Contains(prompt, w) {
					t.Errorf("el prompt no debería tener %q:\n%s", w, prompt)
				}
			}
		})
	}
}

func TestCeilQuarter(t *testing.T) {
	for in, want := range map[string]string{"19:00": "19:00", "19:06": "19:15", "19:15": "19:15", "19:46": "20:00"} {
		h, _ := time.ParseInLocation("2006-01-02 15:04", "2026-10-19 "+in, cba)
		if got := ceilQuarter(h).Format("15:04"); got != want {
			t.Errorf("ceilQuarter(%s) = %s, se esperaba %s", in, got, want)
		}
	}
}
