import { useMemo, type ReactNode } from 'react'
import { Link } from 'react-router-dom'
import { addDays, format, formatISO, startOfWeek } from 'date-fns'
import { es } from 'date-fns/locale'
import { AlertCircle, ArrowRight, CalendarPlus, CheckCircle2, Circle, Clock, LayoutList, NotebookPen, NotebookText, Timer } from 'lucide-react'
import { WeekActivityChart } from './WeekActivityChart'
import { Badge } from '@/components/ui/Badge'
import { Button } from '@/components/ui/Button'
import { ThemeToggle } from '@/components/ThemeToggle'
import { ApiErrorNotice } from '@/components/ApiErrorNotice'
import { usePlannerData } from '@/app/providers/PlannerDataProvider'
import { EXAM_TYPE_LABEL } from '@/api/types'
import { examTypeColorVar } from '@/lib/domain-ui'
import { formatDateLong, formatDaysUntil, formatMinutes, formatTimeRange, localDate, minutesBetween } from '@/lib/format'

function SectionLabel({ children }: { children: ReactNode }) {
  return <p className="mb-3 text-[11px] font-bold uppercase tracking-[0.1em] text-subtle">{children}</p>
}

export function HomeScreen() {
  const { subjects, sessions, exams, availability, subjectsById, examsById, loading, error } = usePlannerData()
  const isFreshStart = !loading && !error && subjects.length === 0
  const today = localDate(new Date())

  const todaySessions = useMemo(
    () => sessions.filter((s) => localDate(s.start) === today).sort((a, b) => a.start.localeCompare(b.start)),
    [sessions, today],
  )

  const upcomingExams = useMemo(
    () =>
      exams
        .filter((e) => e.status === 'programado' && new Date(e.startsAt) >= new Date())
        .sort((a, b) => a.startsAt.localeCompare(b.startsAt))
        .slice(0, 6),
    [exams],
  )

  const weekBreakdown = useMemo(() => {
    const weekStart = startOfWeek(new Date(), { weekStartsOn: 1 })
    return Array.from({ length: 7 }, (_, i) => {
      const date = formatISO(addDays(weekStart, i), { representation: 'date' })
      const daySessions = sessions.filter((s) => localDate(s.start) === date && s.status !== 'pospuesta')
      const planned = daySessions.reduce((sum, s) => sum + minutesBetween(s.start, s.end), 0)
      const completed = daySessions
        .filter((s) => s.status === 'completada')
        .reduce((sum, s) => sum + minutesBetween(s.start, s.end), 0)
      return { date, planned, completed }
    })
  }, [sessions])

  const weeklyPlanned = weekBreakdown.reduce((sum, d) => sum + d.planned, 0)
  const weeklyCompleted = weekBreakdown.reduce((sum, d) => sum + d.completed, 0)

  return (
    <div className="mx-auto max-w-3xl px-5 py-8 sm:px-8">
      <div className="flex items-start justify-between gap-3">
        <p className="text-xs font-bold uppercase tracking-[0.16em] text-accent">Estudiario</p>
        <ThemeToggle />
      </div>
      <div className="mt-1.5 flex flex-wrap items-baseline justify-between gap-x-3 gap-y-1">
        <h1 className="text-[30px] leading-tight sm:text-[32px]">Tu semana académica</h1>
        <span className="text-sm capitalize text-muted">{format(new Date(), "EEEE d 'de' MMMM", { locale: es })}</span>
      </div>

      {error && (
        <div className="mt-6">
          <ApiErrorNotice error={new Error(error)} />
        </div>
      )}

      {isFreshStart && (
        <div className="mt-6 rounded-xl bg-accent-soft p-5">
          <p className="text-lg font-bold text-accent-ink">Bienvenido/a a Estudiario</p>
          <p className="mt-1.5 text-sm text-accent-ink/80">
            Acá vas a organizar tus materias, parciales y entregas, y un agente de IA te va a armar un plan de estudio
            según la dificultad de cada materia y el tiempo que tengas disponible. Para arrancar, cargá tu primera
            materia.
          </p>
          <Link to="/materias" className="mt-3 inline-block">
            <Button size="sm">
              <NotebookText size={15} />
              Crear tu primera materia
            </Button>
          </Link>
        </div>
      )}

      <div className="mt-7 flex overflow-hidden rounded-xl border border-border">
        <QuickAction to="/materias?nueva=1" icon={<NotebookPen size={17} />} label="Materia" tone="accent" />
        <QuickAction to="/calendario?nuevo=1" icon={<CalendarPlus size={17} />} label="Examen" tone="delivery" />
        <QuickAction to="/plan" icon={<LayoutList size={17} />} label="Plan" tone="warning" />
        <QuickAction to="/pomodoro" icon={<Timer size={17} />} label="Pomodoro" tone="neutral" />
      </div>

      {!availability && !loading && !isFreshStart && (
        <Link
          to="/configuracion"
          className="mt-5 flex items-center gap-3 rounded-xl bg-warning/10 px-4 py-3 text-sm text-ink"
        >
          <AlertCircle size={17} className="shrink-0 text-warning" />
          Contanos cuándo podés estudiar para que el agente pueda armarte el plan.
        </Link>
      )}

      <section className="mt-9">
        <SectionLabel>Tu día de estudio</SectionLabel>
        {!todaySessions.length ? (
          <div className="flex items-center justify-between gap-3">
            <p className="text-sm text-muted">Nada planificado para hoy.</p>
            <Link to="/plan" className="flex items-center gap-1 text-sm font-medium text-accent">
              Armar un plan <ArrowRight size={14} />
            </Link>
          </div>
        ) : (
          <div className="flex flex-col gap-2">
            {todaySessions.map((session) => {
              const exam = examsById.get(session.examId)
              const subject = exam ? subjectsById.get(exam.subjectId) : undefined
              return (
                <Link
                  key={session.id}
                  to={subject ? `/materias/${subject.id}/plan` : '/plan'}
                  className="flex items-center gap-3 rounded-xl border border-border bg-surface p-4 shadow-[var(--shadow-sm)] hover:border-accent/40"
                >
                  <span className="shrink-0 text-subtle">
                    {session.status === 'completada' ? (
                      <CheckCircle2 size={22} className="text-success" />
                    ) : (
                      <Circle size={22} />
                    )}
                  </span>
                  <div className="min-w-0 flex-1">
                    <p className="truncate text-sm font-medium text-ink">
                      {subject ? `${subject.name} — ` : ''}
                      {session.topic}
                    </p>
                    <p className="text-xs text-muted">{session.objective || 'Sesión de estudio'}</p>
                  </div>
                  <span className="flex shrink-0 items-center gap-1 text-xs text-subtle">
                    <Clock size={12} />
                    {formatTimeRange(session.start, session.end)}
                  </span>
                </Link>
              )
            })}
          </div>
        )}
      </section>

      <section className="mt-9">
        <SectionLabel>Próximos parciales y entregas</SectionLabel>
        {!upcomingExams.length ? (
          <p className="text-sm text-muted">No hay fechas próximas cargadas.</p>
        ) : (
          <div className="flex flex-col gap-2">
            {upcomingExams.map((exam) => {
              const subject = subjectsById.get(exam.subjectId)
              const date = localDate(exam.startsAt)
              return (
                <Link
                  key={exam.id}
                  to={subject ? `/materias/${subject.id}` : '/calendario'}
                  className="flex items-center gap-3 rounded-xl border border-border bg-surface p-4 shadow-[var(--shadow-sm)] hover:border-accent/40"
                >
                  <span className="h-2.5 w-2.5 shrink-0 rounded-full" style={{ backgroundColor: examTypeColorVar(exam.type) }} />
                  <div className="min-w-0 flex-1">
                    <p className="truncate text-sm font-medium text-ink">
                      {subject ? `${subject.name} — ` : ''}
                      {EXAM_TYPE_LABEL[exam.type]}
                    </p>
                    <p className="text-xs text-muted">{formatDateLong(date)}</p>
                  </div>
                  <Badge tone="accent">{formatDaysUntil(date)}</Badge>
                </Link>
              )
            })}
          </div>
        )}
      </section>

      <section className="mt-9 mb-6">
        <SectionLabel>Progreso semanal</SectionLabel>
        <p className="mb-4 text-sm text-muted">
          {formatMinutes(weeklyCompleted)} completadas de {formatMinutes(weeklyPlanned)} reservadas
        </p>
        <WeekActivityChart days={weekBreakdown} />
      </section>
    </div>
  )
}

const TONE_CLASSES = {
  accent: 'text-accent',
  delivery: 'text-event-delivery',
  warning: 'text-warning',
  neutral: 'text-muted',
} as const

function QuickAction({ to, icon, label, tone }: { to: string; icon: ReactNode; label: string; tone: keyof typeof TONE_CLASSES }) {
  return (
    <Link
      to={to}
      className="flex flex-1 flex-col items-center gap-1.5 border-l border-border py-4 first:border-l-0 hover:bg-surface-raised"
    >
      <span className={TONE_CLASSES[tone]}>{icon}</span>
      <span className="text-[13px] font-medium text-ink">{label}</span>
    </Link>
  )
}
