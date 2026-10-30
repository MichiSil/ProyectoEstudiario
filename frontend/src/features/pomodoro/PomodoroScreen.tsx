import { useState } from 'react'
import clsx from 'clsx'
import { Pause, Play, RotateCcw, Timer } from 'lucide-react'
import { PageHeader } from '@/components/PageHeader'
import { Button } from '@/components/ui/Button'
import { Card } from '@/components/ui/Card'
import { Input, Label } from '@/components/ui/Field'
import { readJson, writeJson } from '@/lib/storage'
import { usePomodoro } from './usePomodoro'

const PRESETS = [
  { label: '25 / 5', study: 25, rest: 5 },
  { label: '50 / 10', study: 50, rest: 10 },
  { label: '90 / 15', study: 90, rest: 15 },
]

const RADIUS = 90
const CIRCUMFERENCE = 2 * Math.PI * RADIUS
// Preferencia del temporizador: es sólo de este dispositivo, no es un dato de estudio.
const STORAGE_KEY = 'estudiario-pomodoro'

export function PomodoroScreen() {
  const [durations, setDurations] = useState(() => readJson(STORAGE_KEY, { study: 25, rest: 5 }))
  const studyMinutes = durations.study
  const breakMinutes = durations.rest

  function updateDurations(patch: Partial<typeof durations>) {
    const next = { ...durations, ...patch }
    writeJson(STORAGE_KEY, next)
    setDurations(next)
  }

  const { phase, secondsLeft, running, completedCycles, start, pause, reset } = usePomodoro(studyMinutes, breakMinutes)

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
          <Button size="icon" variant="secondary" onClick={reset} aria-label="Reiniciar">
            <RotateCcw size={18} />
          </Button>
          <Button size="lg" onClick={running ? pause : start} className="w-36">
            {running ? <Pause size={18} /> : <Play size={18} />}
            {running ? 'Pausar' : 'Empezar'}
          </Button>
        </div>

        {completedCycles > 0 && (
          <p className="mt-4 text-sm text-muted">
            {completedCycles} sesión{completedCycles === 1 ? '' : 'es'} de estudio completada{completedCycles === 1 ? '' : 's'} hoy.
          </p>
        )}
      </div>

      <Card className="mt-8">
        <p className="text-sm font-medium text-ink">Duración</p>
        <p className="mt-1 text-xs text-muted">Se aplica a partir del próximo cambio de fase.</p>

        <div className="mt-3 flex gap-2">
          {PRESETS.map((p) => (
            <button
              key={p.label}
              onClick={() => updateDurations({ study: p.study, rest: p.rest })}
              className={clsx(
                'flex-1 rounded-xl border px-3 py-2 text-sm font-medium',
                studyMinutes === p.study && breakMinutes === p.rest
                  ? 'border-accent bg-accent-soft text-accent-ink'
                  : 'border-border text-muted hover:bg-surface-raised',
              )}
            >
              {p.label}
            </button>
          ))}
        </div>

        <div className="mt-4 grid grid-cols-2 gap-3">
          <div>
            <Label>Estudio (min)</Label>
            <Input type="number" min={5} step={5} value={studyMinutes} onChange={(e) => updateDurations({ study: Number(e.target.value) })} />
          </div>
          <div>
            <Label>Descanso (min)</Label>
            <Input type="number" min={0} step={5} value={breakMinutes} onChange={(e) => updateDurations({ rest: Number(e.target.value) })} />
          </div>
        </div>
      </Card>
    </div>
  )
}
