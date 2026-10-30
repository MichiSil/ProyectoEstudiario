import { useEffect, useMemo, useState } from 'react'
import { CheckCircle2, Plus, Trash2 } from 'lucide-react'
import { Card } from '@/components/ui/Card'
import { Button } from '@/components/ui/Button'
import { Input, Label, Select } from '@/components/ui/Field'
import { ApiErrorNotice } from '@/components/ApiErrorNotice'
import { plannerApi } from '@/api/planner'
import type { Availability, WeeklySlot } from '@/api/types'
import { usePlannerData } from '@/app/providers/PlannerDataProvider'
import { WEEKDAY_LABEL } from '@/lib/domain-ui'
import { browserTimezone, formatMinutes } from '@/lib/format'

// Lunes primero, como el resto de la app.
const WEEKDAY_ORDER = [1, 2, 3, 4, 5, 6, 0]

function toMinutes(hhmm: string): number {
  const [h, m] = hhmm.split(':').map(Number)
  return h * 60 + m
}

/** Validación local de RN-05 para avisar antes de enviar. planner-service la vuelve a aplicar. */
function slotProblems(slots: WeeklySlot[]): Map<number, string> {
  const problems = new Map<number, string>()
  slots.forEach((slot, i) => {
    if (!slot.startTime || !slot.endTime || toMinutes(slot.startTime) >= toMinutes(slot.endTime)) {
      problems.set(i, 'La hora de inicio tiene que ser anterior a la de fin.')
      return
    }
    slots.forEach((other, j) => {
      if (j >= i || other.weekday !== slot.weekday) return
      if (toMinutes(slot.startTime) < toMinutes(other.endTime) && toMinutes(other.startTime) < toMinutes(slot.endTime)) {
        problems.set(i, `Se superpone con otra franja del ${WEEKDAY_LABEL[slot.weekday].toLowerCase()}.`)
      }
    })
  })
  return problems
}

const DEFAULT_AVAILABILITY: Availability = {
  weeklySlots: [
    { weekday: 1, startTime: '18:00', endTime: '21:00' },
    { weekday: 3, startTime: '18:00', endTime: '21:00' },
  ],
  maxDailyMinutes: 120,
  preferredSessionMinutes: 60,
  timezone: browserTimezone(),
}

/** Disponibilidad del estudiante (RF-07): franjas semanales, capacidad diaria y duración preferida. */
export function AvailabilitySection() {
  const { availability, rememberAvailability } = usePlannerData()
  const [draft, setDraft] = useState<Availability>(availability ?? DEFAULT_AVAILABILITY)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<unknown>(null)
  const [saved, setSaved] = useState(false)

  useEffect(() => {
    if (availability) setDraft(availability)
  }, [availability])

  const problems = useMemo(() => slotProblems(draft.weeklySlots), [draft.weeklySlots])
  const weeklyMinutes = draft.weeklySlots.reduce(
    (sum, s, i) => sum + (problems.has(i) ? 0 : toMinutes(s.endTime) - toMinutes(s.startTime)),
    0,
  )
  const invalid = problems.size > 0 || draft.maxDailyMinutes <= 0 || draft.preferredSessionMinutes <= 0

  function update(patch: Partial<Availability>) {
    setSaved(false)
    setDraft((d) => ({ ...d, ...patch }))
  }

  function updateSlot(index: number, patch: Partial<WeeklySlot>) {
    update({ weeklySlots: draft.weeklySlots.map((s, i) => (i === index ? { ...s, ...patch } : s)) })
  }

  async function handleSave() {
    setSaving(true)
    setError(null)
    try {
      await plannerApi.setAvailability(draft)
      rememberAvailability(draft)
      setSaved(true)
    } catch (err) {
      setError(err)
    } finally {
      setSaving(false)
    }
  }

  const orderedIndexes = draft.weeklySlots
    .map((_, i) => i)
    .sort((a, b) => {
      const sa = draft.weeklySlots[a]
      const sb = draft.weeklySlots[b]
      return WEEKDAY_ORDER.indexOf(sa.weekday) - WEEKDAY_ORDER.indexOf(sb.weekday) || sa.startTime.localeCompare(sb.startTime)
    })

  return (
    <Card>
      <h3 className="font-medium text-ink">Disponibilidad para estudiar</h3>
      <p className="mt-1 text-sm text-muted">
        Las franjas de la semana en las que podés estudiar. El agente sólo propone sesiones dentro de ellas y sin pasarse
        del máximo diario.
      </p>

      <div className="mt-4 flex flex-col gap-3 sm:gap-2">
        {orderedIndexes.map((i) => {
          const slot = draft.weeklySlots[i]
          return (
            <div key={i}>
              <div className="grid grid-cols-[minmax(0,1fr)_auto] items-center gap-2 sm:grid-cols-[minmax(0,1fr)_7.5rem_7.5rem_auto]">
                <Select value={slot.weekday} onChange={(e) => updateSlot(i, { weekday: Number(e.target.value) })} aria-label="Día">
                  {WEEKDAY_ORDER.map((d) => (
                    <option key={d} value={d}>
                      {WEEKDAY_LABEL[d]}
                    </option>
                  ))}
                </Select>
                <button
                  onClick={() => update({ weeklySlots: draft.weeklySlots.filter((_, j) => j !== i) })}
                  aria-label="Quitar franja"
                  className="flex h-10 w-10 items-center justify-center rounded-xl text-muted hover:bg-danger/10 hover:text-danger sm:order-last"
                >
                  <Trash2 size={16} />
                </button>
                <div className="col-span-2 grid grid-cols-2 gap-2 sm:contents">
                  <Input
                    type="time"
                    value={slot.startTime}
                    onChange={(e) => updateSlot(i, { startTime: e.target.value })}
                    aria-label="Desde"
                  />
                  <Input
                    type="time"
                    value={slot.endTime}
                    onChange={(e) => updateSlot(i, { endTime: e.target.value })}
                    aria-label="Hasta"
                  />
                </div>
              </div>
              {problems.has(i) && <p className="mt-1 text-xs text-danger">{problems.get(i)}</p>}
            </div>
          )
        })}
        {!draft.weeklySlots.length && <p className="text-sm text-subtle">Todavía no agregaste franjas.</p>}
      </div>

      <Button
        variant="ghost"
        size="sm"
        className="mt-2"
        onClick={() => update({ weeklySlots: [...draft.weeklySlots, { weekday: 1, startTime: '18:00', endTime: '20:00' }] })}
      >
        <Plus size={14} />
        Agregar franja
      </Button>

      <div className="mt-4 grid grid-cols-2 items-end gap-3">
        <div className="min-w-0">
          <Label>Máximo por día (min)</Label>
          <Input
            type="number"
            min={15}
            step={15}
            value={draft.maxDailyMinutes}
            onChange={(e) => update({ maxDailyMinutes: Number(e.target.value) })}
          />
        </div>
        <div className="min-w-0">
          <Label>Cada sesión (min)</Label>
          <Input
            type="number"
            min={15}
            step={15}
            value={draft.preferredSessionMinutes}
            onChange={(e) => update({ preferredSessionMinutes: Number(e.target.value) })}
          />
        </div>
      </div>

      <p className="mt-3 text-xs text-subtle">
        {formatMinutes(weeklyMinutes)} disponibles por semana · zona horaria {draft.timezone}
      </p>

      {!!error && (
        <div className="mt-4">
          <ApiErrorNotice error={error} />
        </div>
      )}

      <div className="mt-4 flex items-center gap-3">
        <Button onClick={() => void handleSave()} disabled={saving || invalid}>
          {saving ? 'Guardando…' : 'Guardar disponibilidad'}
        </Button>
        {saved && (
          <span className="animate-fade-in flex items-center gap-1.5 text-sm text-success">
            <CheckCircle2 size={15} />
            Guardada
          </span>
        )}
      </div>
    </Card>
  )
}
