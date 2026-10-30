import { useState } from 'react'
import clsx from 'clsx'
import { Pause, Play, RotateCcw, Timer } from 'lucide-react'
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
  study: number
  rest: number
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
    return { mode, study, rest }
  })
  const studyMinutes = prefs.study
  const breakMinutes = prefs.rest

  function updatePrefs(patch: Partial<Preferences>) {
    const next = { ...prefs, ...patch }
    writeJson(STORAGE_KEY, next)
    setPrefs(next)
  }

  const { phase, secondsLeft, running, paused, completedCycles, start, pause, reset } = usePomodoro(studyMinutes, breakMinutes)

  const totalSeconds = (phase === 'estudio' ? studyMinutes : breakMinutes) * 60
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
            onClick={() => updatePrefs({ mode: 'propio' })}
            title="Armar el mío"
            detail="Elegís los minutos"
          />
        </div>

        {prefs.mode === 'propio' && (
          <div className="animate-fade-in mt-4 grid grid-cols-2 gap-3">
            <div className="min-w-0">
              <Label>Estudio (min)</Label>
              <Input
                type="number"
                min={5}
                step={5}
                value={studyMinutes}
                onChange={(e) => updatePrefs({ study: Math.max(1, Number(e.target.value)) })}
              />
            </div>
            <div className="min-w-0">
              <Label>Descanso (min)</Label>
              <Input
                type="number"
                min={1}
                step={5}
                value={breakMinutes}
                onChange={(e) => updatePrefs({ rest: Math.max(1, Number(e.target.value)) })}
              />
            </div>
          </div>
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
