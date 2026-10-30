import { differenceInCalendarDays, format, formatISO } from 'date-fns'
import { es } from 'date-fns/locale'

export function formatMinutes(totalMinutes: number): string {
  if (totalMinutes <= 0) return '0 min'
  const hours = Math.floor(totalMinutes / 60)
  const minutes = totalMinutes % 60
  if (hours === 0) return `${minutes} min`
  if (minutes === 0) return `${hours} h`
  return `${hours} h ${minutes} min`
}

/** Fecha local (YYYY-MM-DD) de un instante RFC 3339. */
export function localDate(instant: string | Date): string {
  return formatISO(new Date(instant), { representation: 'date' })
}

export function formatDateLong(isoDate: string): string {
  return format(new Date(`${isoDate}T00:00:00`), "d 'de' MMMM", { locale: es })
}

export function formatDateShort(isoDate: string): string {
  return format(new Date(`${isoDate}T00:00:00`), 'd MMM', { locale: es })
}

export function formatTime(instant: string): string {
  return format(new Date(instant), 'HH:mm')
}

export function formatTimeRange(start: string, end: string): string {
  return `${formatTime(start)}–${formatTime(end)}`
}

export function minutesBetween(start: string, end: string): number {
  return Math.round((new Date(end).getTime() - new Date(start).getTime()) / 60000)
}

export function daysUntil(isoDate: string): number {
  return differenceInCalendarDays(new Date(`${isoDate}T00:00:00`), new Date(new Date().toDateString()))
}

export function formatDaysUntil(isoDate: string): string {
  const days = daysUntil(isoDate)
  if (days < 0) return 'Ya pasó'
  if (days === 0) return 'Hoy'
  if (days === 1) return 'Mañana'
  return `Faltan ${days} días`
}

/**
 * Arma un instante RFC 3339 con el desfasaje de la zona del navegador, como
 * pide POST /exams (por ejemplo 2026-11-10T09:00:00-03:00). Sin hora, se toma
 * el comienzo del día, igual que el dominio de planner-service.
 */
export function toRfc3339(date: string, time: string | null): string {
  const local = new Date(`${date}T${time || '00:00'}:00`)
  const offset = -local.getTimezoneOffset()
  const sign = offset >= 0 ? '+' : '-'
  const hh = String(Math.floor(Math.abs(offset) / 60)).padStart(2, '0')
  const mm = String(Math.abs(offset) % 60).padStart(2, '0')
  return `${date}T${time || '00:00'}:00${sign}${hh}:${mm}`
}

export function browserTimezone(): string {
  try {
    return Intl.DateTimeFormat().resolvedOptions().timeZone || 'America/Argentina/Cordoba'
  } catch {
    return 'America/Argentina/Cordoba'
  }
}
