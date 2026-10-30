import { useEffect, useMemo, useState } from 'react'
import { useSearchParams } from 'react-router-dom'
import {
  addDays,
  addMonths,
  eachDayOfInterval,
  endOfMonth,
  endOfWeek,
  format,
  formatISO,
  isSameMonth,
  isToday,
  startOfMonth,
  startOfWeek,
} from 'date-fns'
import { es } from 'date-fns/locale'
import { ChevronLeft, ChevronRight, Plus } from 'lucide-react'
import clsx from 'clsx'
import { Button } from '@/components/ui/Button'
import { EmptyState } from '@/components/ui/EmptyState'
import { usePlannerData } from '@/app/providers/PlannerDataProvider'
import { EXAM_TYPE_LABEL, type Exam, type Session } from '@/api/types'
import { SESSION_COLOR, SESSION_STATUS_LABEL, examTypeColorVar } from '@/lib/domain-ui'
import { formatTime, formatTimeRange, localDate } from '@/lib/format'
import { ExamFormSheet } from './ExamFormSheet'
import { ExamDetailSheet } from './ExamDetailSheet'

type ViewMode = 'mes' | 'semana' | 'dia'

type CalendarItem =
  | { kind: 'exam'; id: string; date: string; sortKey: string; exam: Exam }
  | { kind: 'session'; id: string; date: string; sortKey: string; session: Session }

function iso(d: Date) {
  return formatISO(d, { representation: 'date' })
}

/** Calendario del estudiante (RF-10): exámenes y sesiones reservadas. */
export function CalendarScreen() {
  const { exams, sessions, subjectsById, examsById } = usePlannerData()
  const [searchParams, setSearchParams] = useSearchParams()
  const [viewMode, setViewMode] = useState<ViewMode>('mes')
  const [cursor, setCursor] = useState(new Date())
  const [selectedDate, setSelectedDate] = useState(iso(new Date()))
  const [formOpen, setFormOpen] = useState(false)
  const [detailExam, setDetailExam] = useState<Exam | null>(null)

  useEffect(() => {
    if (searchParams.get('nuevo')) {
      setFormOpen(true)
      setSearchParams({}, { replace: true })
    }
  }, [searchParams, setSearchParams])

  const gridStart = startOfWeek(startOfMonth(cursor), { weekStartsOn: 1 })
  const gridEnd = endOfWeek(endOfMonth(cursor), { weekStartsOn: 1 })
  const rangeStart = viewMode === 'mes' ? gridStart : startOfWeek(cursor, { weekStartsOn: 1 })
  const rangeEnd = viewMode === 'mes' ? gridEnd : endOfWeek(cursor, { weekStartsOn: 1 })

  const itemsByDay = useMemo(() => {
    const items: CalendarItem[] = [
      ...exams.map((exam) => ({ kind: 'exam' as const, id: exam.id, date: localDate(exam.startsAt), sortKey: exam.startsAt, exam })),
      ...sessions
        .filter((s) => s.status !== 'pospuesta')
        .map((session) => ({ kind: 'session' as const, id: session.id, date: localDate(session.start), sortKey: session.start, session })),
    ]
    const map = new Map<string, CalendarItem[]>()
    for (const item of items.sort((a, b) => a.sortKey.localeCompare(b.sortKey))) {
      if (!map.has(item.date)) map.set(item.date, [])
      map.get(item.date)!.push(item)
    }
    return map
  }, [exams, sessions])

  const days = eachDayOfInterval({ start: rangeStart, end: rangeEnd })
  const selectedDayItems = itemsByDay.get(selectedDate) ?? []

  function goToday() {
    setCursor(new Date())
    setSelectedDate(iso(new Date()))
  }

  function shift(amount: number) {
    let next: Date
    if (viewMode === 'mes') next = addMonths(cursor, amount)
    else if (viewMode === 'semana') next = addDays(cursor, amount * 7)
    else next = addDays(cursor, amount)
    setCursor(next)
    if (viewMode === 'dia') setSelectedDate(iso(next))
  }

  function itemColor(item: CalendarItem) {
    return item.kind === 'exam' ? examTypeColorVar(item.exam.type) : SESSION_COLOR
  }

  function itemTitle(item: CalendarItem) {
    if (item.kind === 'exam') {
      const subject = subjectsById.get(item.exam.subjectId)
      return `${subject ? `${subject.name} — ` : ''}${EXAM_TYPE_LABEL[item.exam.type]}`
    }
    const exam = examsById.get(item.session.examId)
    const subject = exam ? subjectsById.get(exam.subjectId) : undefined
    return `${subject ? `${subject.name} — ` : ''}${item.session.topic}`
  }

  return (
    <div className="mx-auto max-w-3xl px-4 py-6">
      <div className="flex items-center justify-between">
        <h1 className="text-[28px] leading-tight">Calendario</h1>
        <Button size="icon" onClick={() => setFormOpen(true)} aria-label="Nuevo examen">
          <Plus size={18} />
        </Button>
      </div>

      <div className="mt-4 flex items-center gap-1 rounded-xl bg-surface-raised p-1">
        {(['mes', 'semana', 'dia'] as ViewMode[]).map((mode) => (
          <button
            key={mode}
            onClick={() => {
              setViewMode(mode)
              if (mode === 'dia') setCursor(new Date(`${selectedDate}T00:00:00`))
            }}
            className={clsx(
              'flex-1 rounded-lg py-1.5 text-sm font-medium capitalize transition-colors',
              viewMode === mode ? 'bg-accent text-white' : 'text-muted',
            )}
          >
            {mode === 'dia' ? 'día' : mode}
          </button>
        ))}
      </div>

      <div className="mt-4 flex items-center justify-between">
        <button onClick={() => shift(-1)} aria-label="Anterior" className="rounded-full p-2 text-muted hover:bg-accent-soft">
          <ChevronLeft size={18} />
        </button>
        <button onClick={goToday} className="text-sm font-medium capitalize text-ink">
          {format(cursor, viewMode === 'dia' ? "EEEE d 'de' MMMM" : 'MMMM yyyy', { locale: es })}
        </button>
        <button onClick={() => shift(1)} aria-label="Siguiente" className="rounded-full p-2 text-muted hover:bg-accent-soft">
          <ChevronRight size={18} />
        </button>
      </div>

      {viewMode === 'mes' && (
        <>
          <div className="mt-3 grid grid-cols-7 gap-1 text-center text-xs text-subtle">
            {['L', 'M', 'X', 'J', 'V', 'S', 'D'].map((d) => (
              <div key={d}>{d}</div>
            ))}
          </div>
          <div className="mt-1 grid grid-cols-7 gap-1">
            {days.map((day) => {
              const dISO = iso(day)
              const dayItems = itemsByDay.get(dISO) ?? []
              return (
                <button
                  key={dISO}
                  onClick={() => setSelectedDate(dISO)}
                  className={clsx(
                    'flex h-14 flex-col items-center gap-1 rounded-xl border py-1.5 text-sm',
                    isSameMonth(day, cursor) ? 'border-border' : 'border-transparent text-subtle',
                    selectedDate === dISO && 'border-accent bg-accent-soft',
                    isToday(day) && selectedDate !== dISO && 'font-semibold text-accent',
                  )}
                >
                  {format(day, 'd')}
                  <span className="flex gap-0.5">
                    {dayItems.slice(0, 3).map((item) => (
                      <span key={item.id} className="h-1.5 w-1.5 rounded-full" style={{ backgroundColor: itemColor(item) }} />
                    ))}
                  </span>
                </button>
              )
            })}
          </div>
        </>
      )}

      {viewMode === 'semana' && (
        <div className="mt-3 flex flex-col gap-2">
          {days.map((day) => {
            const dISO = iso(day)
            const dayItems = itemsByDay.get(dISO) ?? []
            return (
              <button
                key={dISO}
                onClick={() => setSelectedDate(dISO)}
                className={clsx(
                  'rounded-xl border px-3 py-2 text-left',
                  selectedDate === dISO ? 'border-accent bg-accent-soft' : 'border-border',
                )}
              >
                <p className="text-xs capitalize text-subtle">{format(day, 'EEEE d', { locale: es })}</p>
                {dayItems.length ? (
                  <p className="truncate text-sm text-ink">{dayItems.map(itemTitle).join(', ')}</p>
                ) : (
                  <p className="text-sm text-subtle">Sin eventos</p>
                )}
              </button>
            )
          })}
        </div>
      )}

      <section className="mt-6">
        <h2 className="mb-3 text-base font-medium text-ink">
          {viewMode === 'dia' ? 'Eventos del día' : format(new Date(`${selectedDate}T00:00:00`), "d 'de' MMMM", { locale: es })}
        </h2>
        {!selectedDayItems.length ? (
          <EmptyState title="No hay eventos este día" />
        ) : (
          <div className="flex flex-col gap-2">
            {selectedDayItems.map((item) => (
              <button
                key={item.id}
                onClick={() => {
                  if (item.kind === 'exam') setDetailExam(item.exam)
                  else {
                    const exam = examsById.get(item.session.examId)
                    if (exam) setDetailExam(exam)
                  }
                }}
                className="animate-fade-in flex w-full items-center gap-3 rounded-2xl border border-border bg-surface p-4 text-left hover:border-accent/40"
              >
                <span className="h-2.5 w-2.5 shrink-0 rounded-full" style={{ backgroundColor: itemColor(item) }} />
                <div className="min-w-0 flex-1">
                  <p className="truncate text-sm font-medium text-ink">{itemTitle(item)}</p>
                  <p className="text-xs text-muted">
                    {item.kind === 'exam'
                      ? `${EXAM_TYPE_LABEL[item.exam.type]}${formatTime(item.exam.startsAt) !== '00:00' ? ` · ${formatTime(item.exam.startsAt)}` : ''}`
                      : `Sesión de estudio · ${formatTimeRange(item.session.start, item.session.end)} · ${SESSION_STATUS_LABEL[item.session.status]}`}
                  </p>
                </div>
              </button>
            ))}
          </div>
        )}
      </section>

      <ExamFormSheet open={formOpen} onClose={() => setFormOpen(false)} defaultDate={selectedDate} />
      <ExamDetailSheet exam={detailExam} onClose={() => setDetailExam(null)} />
    </div>
  )
}
