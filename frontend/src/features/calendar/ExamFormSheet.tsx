import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { AlertTriangle } from 'lucide-react'
import { Sheet } from '@/components/ui/Sheet'
import { Button } from '@/components/ui/Button'
import { FieldGroup, Input, Label, Select } from '@/components/ui/Field'
import { TagInput } from '@/components/ui/TagInput'
import { ApiErrorNotice } from '@/components/ApiErrorNotice'
import { plannerApi } from '@/api/planner'
import { EXAM_TYPE_LABEL, MODALITY_LABEL, type Exam, type ExamModality, type ExamType, type Material } from '@/api/types'
import { usePlannerData } from '@/app/providers/PlannerDataProvider'
import { formatTime, localDate, toRfc3339 } from '@/lib/format'

const EXAM_TYPES = Object.keys(EXAM_TYPE_LABEL) as ExamType[]

/**
 * Carga de un examen en el calendario (RF-05) con POST /exams o, si recibe
 * `exam`, edición (RF-06) con PATCH /exams/{id}. La materia no se cambia al
 * editar: para eso se cancela el examen y se carga otro.
 */
export function ExamFormSheet({
  open,
  onClose,
  exam,
  defaultDate,
  defaultSubjectId,
  onCreated,
}: {
  open: boolean
  onClose: () => void
  /** Examen a editar; sin él, el formulario carga uno nuevo. */
  exam?: Exam | null
  defaultDate?: string
  defaultSubjectId?: string
  onCreated?: (exam: Exam) => void
}) {
  const { subjects, rememberExam, refreshSessions } = usePlannerData()
  const editing = !!exam
  const [subjectId, setSubjectId] = useState('')
  const [type, setType] = useState<ExamType>('parcial')
  const [date, setDate] = useState('')
  const [time, setTime] = useState('')
  const [topics, setTopics] = useState<string[]>([])
  const [modality, setModality] = useState<ExamModality>('')
  const [materialIds, setMaterialIds] = useState<string[]>([])
  const [materials, setMaterials] = useState<Material[]>([])
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState<unknown>(null)

  useEffect(() => {
    if (!open) return
    if (exam) {
      const examTime = formatTime(exam.startsAt)
      setSubjectId(exam.subjectId)
      setType(exam.type)
      setDate(localDate(exam.startsAt))
      setTime(examTime === '00:00' ? '' : examTime)
      setTopics(exam.topics)
      setModality(exam.modality ?? '')
      setMaterialIds(exam.materialIds ?? [])
    } else {
      setSubjectId(defaultSubjectId ?? subjects[0]?.id ?? '')
      setType('parcial')
      setDate(defaultDate ?? '')
      setTime('')
      setTopics([])
      setModality('')
      setMaterialIds([])
    }
    setError(null)
  }, [open, exam, defaultDate, defaultSubjectId, subjects])

  useEffect(() => {
    if (!open || !subjectId) return
    let cancelled = false
    plannerApi
      .listMaterials(subjectId)
      .then((list) => !cancelled && setMaterials(list))
      .catch(() => !cancelled && setMaterials([]))
    return () => {
      cancelled = true
    }
  }, [open, subjectId])

  function toggleMaterial(id: string) {
    setMaterialIds((prev) => (prev.includes(id) ? prev.filter((x) => x !== id) : [...prev, id]))
  }

  // RN-04: la fecha no puede estar en el pasado. El backend también lo valida.
  const startsAt = date ? toRfc3339(date, time || null) : null
  const dateChanged = !!exam && !!startsAt && new Date(startsAt).getTime() !== new Date(exam.startsAt).getTime()
  const inPast = !!startsAt && new Date(startsAt) < new Date() && (!editing || dateChanged)
  const outdatesPlan = dateChanged && exam?.planStatus === 'confirmado'

  async function handleSubmit() {
    if (!startsAt || !subjectId || inPast) return
    setSubmitting(true)
    setError(null)
    try {
      if (exam) {
        const changes: { type?: ExamType; startsAt?: string; topics?: string[]; modality?: ExamModality; materialIds?: string[] } = {}
        if (type !== exam.type) changes.type = type
        if (dateChanged) changes.startsAt = startsAt
        if (topics.join('\n') !== exam.topics.join('\n')) changes.topics = topics
        if (modality !== (exam.modality ?? '')) changes.modality = modality
        if ([...materialIds].sort().join() !== [...(exam.materialIds ?? [])].sort().join()) changes.materialIds = materialIds
        const updated = await plannerApi.updateExam(exam.id, changes)
        rememberExam(updated)
        // RN-16: si cambió la fecha, las sesiones vuelven marcadas como desactualizadas.
        if (changes.startsAt) await refreshSessions()
      } else {
        const created = await plannerApi.createExam({ subjectId, type, startsAt, topics, modality, materialIds })
        rememberExam(created)
        onCreated?.(created)
      }
      onClose()
    } catch (err) {
      setError(err)
    } finally {
      setSubmitting(false)
    }
  }

  if (open && !subjects.length) {
    return (
      <Sheet open={open} onClose={onClose} title="Nuevo examen">
        <p className="text-sm text-muted">Primero necesitás una materia para asociarle el examen.</p>
        <Link to="/materias?nueva=1" onClick={onClose}>
          <Button className="mt-4 w-full">Crear una materia</Button>
        </Link>
      </Sheet>
    )
  }

  return (
    <Sheet open={open} onClose={onClose} title={editing ? 'Editar examen' : 'Nuevo examen'}>
      <FieldGroup>
        <Label>Tipo</Label>
        <Select value={type} onChange={(e) => setType(e.target.value as ExamType)}>
          {EXAM_TYPES.map((t) => (
            <option key={t} value={t}>
              {EXAM_TYPE_LABEL[t]}
            </option>
          ))}
        </Select>
      </FieldGroup>

      <FieldGroup>
        <Label>Materia</Label>
        <Select value={subjectId} onChange={(e) => setSubjectId(e.target.value)} disabled={editing}>
          {subjects.map((s) => (
            <option key={s.id} value={s.id}>
              {s.name}
            </option>
          ))}
        </Select>
        {editing && (
          <p className="mt-1 text-xs text-subtle">La materia no se cambia: cancelá el examen y cargá otro.</p>
        )}
      </FieldGroup>

      <div className="grid grid-cols-2 gap-3">
        <FieldGroup>
          <Label>Fecha</Label>
          <Input type="date" value={date} onChange={(e) => setDate(e.target.value)} required />
        </FieldGroup>
        <FieldGroup>
          <Label>Hora (opcional)</Label>
          <Input type="time" value={time} onChange={(e) => setTime(e.target.value)} />
        </FieldGroup>
      </div>
      {inPast && <p className="-mt-2 mb-4 text-xs text-danger">La fecha del examen no puede estar en el pasado.</p>}
      {outdatesPlan && (
        <p className="-mt-2 mb-4 flex gap-2 rounded-xl bg-warning/10 px-3 py-2 text-xs text-ink">
          <AlertTriangle size={14} className="mt-0.5 shrink-0 text-warning" />
          Este examen ya tiene un plan confirmado. Si cambiás la fecha, el plan queda desactualizado: sus sesiones
          siguen en el calendario hasta que pidas uno nuevo.
        </p>
      )}

      <FieldGroup>
        <Label>¿Cómo se evalúa?</Label>
        <Select value={modality} onChange={(e) => setModality(e.target.value as ExamModality)}>
          {(Object.keys(MODALITY_LABEL) as ExamModality[]).map((m) => (
            <option key={m} value={m}>
              {MODALITY_LABEL[m]}
            </option>
          ))}
        </Select>
        <p className="mt-1 text-xs text-subtle">Así el plan sabe si conviene estudiar teoría, resolver ejercicios o las dos cosas.</p>
      </FieldGroup>

      <FieldGroup>
        <Label>Material que entra</Label>
        {!materials.length ? (
          <p className="rounded-xl bg-paper px-3 py-2.5 text-xs text-muted">
            Esta materia no tiene material cargado. Podés agregar teóricos y prácticos en Materias → la materia → Material; mientras
            tanto, el plan se arma con los temas.
          </p>
        ) : (
          <>
            <div className="flex flex-col gap-1.5">
              {materials.map((m) => (
                <label key={m.id} className="flex cursor-pointer items-start gap-2.5 rounded-xl bg-paper px-3 py-2 text-sm">
                  <input
                    type="checkbox"
                    checked={materialIds.includes(m.id)}
                    onChange={() => toggleMaterial(m.id)}
                    className="mt-0.5 h-4 w-4 accent-[var(--accent)]"
                  />
                  <span className="min-w-0">
                    <span className="block text-ink">{m.title}</span>
                    <span className="text-xs text-subtle">{m.kind === 'teorico' ? 'Teórico' : 'Práctico'}</span>
                  </span>
                </label>
              ))}
            </div>
            <p className="mt-1 text-xs text-subtle">Si no marcás ninguno, entra todo el material de la materia.</p>
          </>
        )}
      </FieldGroup>

      <FieldGroup>
        <Label>Temas incluidos</Label>
        <TagInput values={topics} onChange={setTopics} placeholder="Integrales dobles, Series… (Enter para agregar)" />
        <p className="mt-1 text-xs text-subtle">Más temas suman más sesiones al plan.</p>
      </FieldGroup>

      {!!error && (
        <div className="mb-4">
          <ApiErrorNotice error={error} />
        </div>
      )}

      <Button className="w-full" disabled={!date || !subjectId || inPast || submitting} onClick={() => void handleSubmit()}>
        {submitting ? 'Guardando…' : editing ? 'Guardar cambios' : 'Cargar examen'}
      </Button>
    </Sheet>
  )
}
