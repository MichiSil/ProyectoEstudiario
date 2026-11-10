import { useState } from 'react'
import clsx from 'clsx'
import { CheckCircle2, Pause, Play, RotateCcw, Save, Timer } from 'lucide-react'
import { PageHeader } from '@/components/PageHeader'
import { Button } from '@/components/ui/Button'
import { Card } from '@/components/ui/Card'
import { Input, Label } from '@/components/ui/Field'
import { readJson, writeJson } from '@/lib/storage'
import { usePomodoro } from './usePomodoro'

// Pomodoros predefinidos. Con "Armar el mío" el estudiante elige sus propios minutos.
const PRESETS = [
  { id: '25-5', study: 25, rest: 5 },
  { id: '50-10', study: 50, rest: 10 },
]

type Mode = (typeof PRESETS)[number]['id'] | 'propio'

interface Preferences {
  mode: Mode
  // Los minutos que se usan ahora (los del predefinido o los propios).
  study: number
  rest: number
  // Los minutos propios, guardados aparte: elegir 25/5 y volver a "Armar el mío" no los pierde.
  customStudy: number
  customRest: number
}

// Límites de los minutos propios.
const STUDY_RANGE = { min: 5, max: 120 }
const REST_RANGE = { min: 1, max: 60 }

/** Valida un campo de minutos: entero dentro del rango, o un mensaje de error. */
function parseMinutes(text: string, range: { min: number; max: number }, label: string): number | string {
  const value = Number(text.trim())
  if (!text.trim() || !Number.isInteger(value) || value < range.min || value > range.max) {
    return `${label}: un número entero de ${range.min} a ${range.max}.`
  }
  return value
}

const RADIUS = 90
const CIRCUMFERENCE = 2 * Math.PI * RADIUS
// Preferencia del temporizador: es sólo de este dispositivo, no es un dato de estudio.
const STORAGE_KEY = 'estudiario-pomodoro'

export function PomodoroScreen() {
  const [prefs, setPrefs] = useState<Preferences>(() => {
    const stored = readJson<Partial<Preferences>>(STORAGE_KEY, {})
    const study = stored.study ?? 25
    const rest = stored.rest ?? 5
    // Preferencias guardadas antes de que existieran los modos: si coinciden con
    // un predefinido se muestran así; si no, como propias.
    const mode = stored.mode ?? PRESETS.find((p) => p.study === study && p.rest === rest)?.id ?? 'propio'
    // Antes de guardarse aparte, los propios eran los minutos de uso con el modo "propio".
    const customStudy = stored.customStudy ?? (mode === 'propio' ? study : 25)
    const customRest = stored.customRest ?? (mode === 'propio' ? rest : 5)
    return { mode, study, rest, customStudy, customRest }
  })
  // Los campos son texto mientras se escriben: se puede borrar el número y
  // escribir otro. Se validan recién al guardar.
  const [studyText, setStudyText] = useState(String(prefs.customStudy))
  const [restText, setRestText] = useState(String(prefs.customRest))
  const [formError, setFormError] = useState<string | null>(null)
  const [saved, setSaved] = useState<string | null>(null)
  const studyMinutes = prefs.study
  const breakMinutes = prefs.rest

  function updatePrefs(patch: Partial<Preferences>) {
    const next = { ...prefs, ...patch }
    writeJson(STORAGE_KEY, next)
    setPrefs(next)
  }

  const { phase, secondsLeft, phaseSeconds, running, paused, completedCycles, start, pause, reset } = usePomodoro(studyMinutes, breakMinutes)

  function chooseCustom() {
    setStudyText(String(prefs.customStudy))
    setRestText(String(prefs.customRest))
    setFormError(null)
    setSaved(null)
    updatePrefs({ mode: 'propio', study: prefs.customStudy, rest: prefs.customRest })
  }

  // Guarda los minutos propios y los usa. Si el temporizador está andando no lo
  // corta: valen desde la próxima fase (usePomodoro).
  function saveCustom() {
    const study = parseMinutes(studyText, STUDY_RANGE, 'Estudio')
    const rest = parseMinutes(restText, REST_RANGE, 'Descanso')
    const problems = [study, rest].filter((v): v is string => typeof v === 'string')
    if (problems.length) {
      setFormError(problems.join(' '))
      setSaved(null)
      return
    }
    const s = study as number
    const r = rest as number
    setFormError(null)
    setStudyText(String(s))
    setRestText(String(r))
    updatePrefs({ mode: 'propio', study: s, rest: r, customStudy: s, customRest: r })
    setSaved(running || paused ? 'Guardado. Los minutos nuevos valen desde la próxima fase.' : 'Guardado.')
  }

  const totalSeconds = phaseSeconds
  const progress = totalSeconds > 0 ? 1 - secondsLeft / totalSeconds : 0
  const minutes = Math.floor(secondsLeft / 60)
  const seconds = secondsLeft % 60

  return (
    <div className="mx-auto max-w-md px-4 py-6">
      <PageHeader icon={<Timer size={20} />} title="Pomodoro" subtitle="Estudiá con descansos cortos y regulares." />

      <div className="mt-8 flex flex-col items-center">
        <div className="relative flex h-56 w-56 items-center justify-center">
          <svg viewBox="0 0 200 200" className="h-full w-full -rotate-90">
            <circle cx="100" cy="100" r={RADIUS} fill="none" stroke="var(--border)" strokeWidth="10" />
            <circle
              cx="100"
              cy="100"
              r={RADIUS}
              fill="none"
              stroke={phase === 'estudio' ? 'var(--accent)' : 'var(--secondary)'}
              strokeWidth="10"
              strokeLinecap="round"
              strokeDasharray={CIRCUMFERENCE}
              strokeDashoffset={CIRCUMFERENCE * (1 - progress)}
              style={{ transition: 'stroke-dashoffset 1s linear' }}
            />
          </svg>
          <div className="absolute flex flex-col items-center">
            <span className="font-mono text-5xl font-bold tabular-nums text-ink">
              {String(minutes).padStart(2, '0')}:{String(seconds).padStart(2, '0')}
            </span>
            <span className={clsx('mt-1 text-sm font-medium capitalize', phase === 'estudio' ? 'text-accent' : 'text-secondary')}>
              {phase}
            </span>
          </div>
        </div>

        <div className="mt-6 flex items-center gap-3">
          <Button size="icon" variant="secondary" onClick={reset} aria-label="Reiniciar" title="Reiniciar: vuelve al comienzo">
            <RotateCcw size={18} />
          </Button>
          <Button size="lg" onClick={running ? pause : start} className="w-36">
            {running ? <Pause size={18} /> : <Play size={18} />}
            {running ? 'Pausar' : paused ? 'Reanudar' : 'Empezar'}
          </Button>
        </div>
        {paused && <p className="mt-3 text-sm text-muted">En pausa. Reanudá para seguir donde quedaste.</p>}

        {completedCycles > 0 && (
          <p className="mt-4 text-sm text-muted">
            {completedCycles} sesión{completedCycles === 1 ? '' : 'es'} de estudio completada{completedCycles === 1 ? '' : 's'} hoy.
          </p>
        )}
      </div>

      <Card className="mt-8">
        <p className="text-sm font-medium text-ink">Elegí tu pomodoro</p>
        <p className="mt-1 text-xs text-muted">
          Usá uno de los predefinidos o armá el tuyo. Si ya empezaste, el cambio se aplica en la próxima fase.
        </p>

        <div className="mt-3 grid grid-cols-3 gap-2" role="radiogroup" aria-label="Duración del pomodoro">
          {PRESETS.map((p) => (
            <ModeOption
              key={p.id}
              active={prefs.mode === p.id}
              onClick={() => updatePrefs({ mode: p.id, study: p.study, rest: p.rest })}
              title={`${p.study} / ${p.rest}`}
              detail={`${p.study} min estudio · ${p.rest} descanso`}
            />
          ))}
          <ModeOption
            active={prefs.mode === 'propio'}
            onClick={chooseCustom}
            title="Armar el mío"
            detail="Elegís los minutos"
          />
        </div>

        {prefs.mode === 'propio' && (
          <form
            noValidate
            className="animate-fade-in mt-4"
            onSubmit={(e) => {
              e.preventDefault()
              saveCustom()
            }}
          >
            <div className="grid grid-cols-2 gap-3">
              <div className="min-w-0">
                <Label>Estudio (min)</Label>
                <Input
                  type="number"
                  inputMode="numeric"
                  min={STUDY_RANGE.min}
                  max={STUDY_RANGE.max}
                  value={studyText}
                  onChange={(e) => {
                    setStudyText(e.target.value)
                    setSaved(null)
                  }}
                />
                <p className="mt-1 text-[11px] text-subtle">
                  De {STUDY_RANGE.min} a {STUDY_RANGE.max}
                </p>
              </div>
              <div className="min-w-0">
                <Label>Descanso (min)</Label>
                <Input
                  type="number"
                  inputMode="numeric"
                  min={REST_RANGE.min}
                  max={REST_RANGE.max}
                  value={restText}
                  onChange={(e) => {
                    setRestText(e.target.value)
                    setSaved(null)
                  }}
                />
                <p className="mt-1 text-[11px] text-subtle">
                  De {REST_RANGE.min} a {REST_RANGE.max}
                </p>
              </div>
            </div>
            {formError && <p className="mt-2 text-xs text-danger">{formError}</p>}
            <div className="mt-3 flex flex-wrap items-center gap-3">
              <Button type="submit" size="sm">
                <Save size={14} />
                Guardar y usar
              </Button>
              {saved && (
                <span className="animate-fade-in flex items-center gap-1 text-xs text-success">
                  <CheckCircle2 size={14} />
                  {saved}
                </span>
              )}
            </div>
          </form>
        )}
      </Card>
    </div>
  )
}

function ModeOption({ active, onClick, title, detail }: { active: boolean; onClick: () => void; title: string; detail: string }) {
  return (
    <button
      type="button"
      role="radio"
      aria-checked={active}
      onClick={onClick}
      className={clsx(
        'flex min-w-0 flex-col items-center rounded-xl border px-2 py-2.5 text-center',
        active ? 'border-accent bg-accent-soft text-accent-ink' : 'border-border text-muted hover:bg-surface-raised',
      )}
    >
      <span className="text-sm font-semibold">{title}</span>
      <span className="mt-0.5 text-[11px] leading-tight opacity-80">{detail}</span>
    </button>
  )
}
