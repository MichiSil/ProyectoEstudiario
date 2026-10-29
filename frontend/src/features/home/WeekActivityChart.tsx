import { format, isToday } from 'date-fns'
import { es } from 'date-fns/locale'
import { formatMinutes } from '@/lib/format'

interface DayPoint {
  date: string
  planned: number
  completed: number
}

const DAY_LETTER = ['L', 'M', 'X', 'J', 'V', 'S', 'D']

/**
 * Barra de actividad semanal: una cápsula por día, con el tramo completado
 * relleno sobre el total planificado. El día de hoy se marca aparte.
 */
export function WeekActivityChart({ days }: { days: DayPoint[] }) {
  const maxMinutes = Math.max(...days.map((d) => d.planned), 60)

  return (
    <div className="flex items-end gap-2.5" role="img" aria-label="Minutos de estudio reservados y completados esta semana">
      {days.map((day, i) => {
        const today = isToday(new Date(`${day.date}T00:00:00`))
        const trackHeight = 64
        const plannedHeight = day.planned > 0 ? Math.max((day.planned / maxMinutes) * trackHeight, 6) : 0
        const completedRatio = day.planned > 0 ? Math.min(day.completed / day.planned, 1) : 0

        return (
          <div key={day.date} className="flex flex-1 flex-col items-center gap-2">
            <div
              className="relative w-full overflow-hidden rounded-full bg-surface-raised"
              style={{ height: trackHeight }}
              title={
                day.planned > 0
                  ? `${formatMinutes(day.completed)} completadas de ${formatMinutes(day.planned)}`
                  : 'Sin sesiones reservadas'
              }
            >
              <div
                className="absolute inset-x-0 bottom-0 rounded-full bg-accent transition-all"
                style={{ height: plannedHeight, opacity: 0.25 }}
              />
              <div
                className="absolute inset-x-0 bottom-0 rounded-full bg-accent transition-all"
                style={{ height: plannedHeight * completedRatio }}
              />
            </div>
            <span className={today ? 'text-xs font-bold text-accent' : 'text-xs text-subtle'}>{DAY_LETTER[i]}</span>
            {today && (
              <span className="text-[10px] font-medium text-accent">
                {format(new Date(`${day.date}T00:00:00`), 'd MMM', { locale: es })}
              </span>
            )}
          </div>
        )
      })}
    </div>
  )
}
