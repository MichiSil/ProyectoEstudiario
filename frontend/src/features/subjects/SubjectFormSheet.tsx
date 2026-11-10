import { useEffect, useState } from 'react'
import { ChevronDown, ChevronUp } from 'lucide-react'
import { Sheet } from '@/components/ui/Sheet'
import { Button } from '@/components/ui/Button'
import { FieldGroup, Input, Label, Select } from '@/components/ui/Field'
import { TagInput } from '@/components/ui/TagInput'
import { ApiErrorNotice } from '@/components/ApiErrorNotice'
import { plannerApi } from '@/api/planner'
import { ANTICIPATION_DAYS, DIFFICULTY_LABEL, type Difficulty, type Subject } from '@/api/types'
import { usePlannerData } from '@/app/providers/PlannerDataProvider'

/**
 * Alta y edición de una materia (RF-04): nombre, profesores y dificultad. Con
 * `subject` edita esa materia (PATCH, sólo los campos que cambiaron); sin
 * él, crea una nueva (POST).
 */
export function SubjectFormSheet({
  open,
  onClose,
  onCreated,
  subject,
  onSaved,
}: {
  open: boolean
  onClose: () => void
  onCreated?: (subject: Subject) => void
  /** Materia a editar. */
  subject?: Subject
  /** Al guardar una edición, con la materia como estaba antes. */
  onSaved?: (saved: Subject, before: Subject) => void
}) {
  const { addSubject, rememberSubject } = usePlannerData()
  const editing = !!subject
  const [name, setName] = useState('')
  const [difficulty, setDifficulty] = useState<Difficulty>(2)
  const [professors, setProfessors] = useState<string[]>([])
  const [showDetails, setShowDetails] = useState(false)
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState<unknown>(null)

  useEffect(() => {
    if (!open) return
    setName(subject?.name ?? '')
    setDifficulty(subject?.difficulty ?? 2)
    setProfessors(subject?.professors ?? [])
    // Al editar se muestran los profesores: suele ser lo que hay que corregir.
    setShowDetails(!!subject)
    setError(null)
  }, [open, subject])

  async function handleSubmit() {
    if (!name.trim()) return
    setSubmitting(true)
    setError(null)
    try {
      if (subject) {
        const changes: { name?: string; professors?: string[]; difficulty?: Difficulty } = {}
        if (name.trim() !== subject.name) changes.name = name.trim()
        if (professors.join('\n') !== subject.professors.join('\n')) changes.professors = professors
        if (difficulty !== subject.difficulty) changes.difficulty = difficulty
        const saved = Object.keys(changes).length ? await plannerApi.updateSubject(subject.id, changes) : subject
        rememberSubject(saved)
        onSaved?.(saved, subject)
      } else {
        const created = await plannerApi.createSubject({ name: name.trim(), professors, difficulty })
        addSubject(created)
        onCreated?.(created)
      }
      onClose()
    } catch (err) {
      setError(err)
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <Sheet open={open} onClose={onClose} title={editing ? 'Editar materia' : 'Nueva materia'}>
      <FieldGroup>
        <Label>Nombre</Label>
        <Input autoFocus value={name} onChange={(e) => setName(e.target.value)} placeholder="Física I" />
      </FieldGroup>

      <FieldGroup>
        <Label>Dificultad</Label>
        <Select value={difficulty} onChange={(e) => setDifficulty(Number(e.target.value) as Difficulty)}>
          {([1, 2, 3, 4] as Difficulty[]).map((d) => (
            <option key={d} value={d}>
              {DIFFICULTY_LABEL[d]}
            </option>
          ))}
        </Select>
        <p className="mt-1 text-xs text-subtle">
          Esto es lo que más pesa a la hora de armarte el plan: con dificultad {DIFFICULTY_LABEL[difficulty].toLowerCase()}{' '}
          se empieza a estudiar {ANTICIPATION_DAYS[difficulty]} días antes.
        </p>
      </FieldGroup>

      {!editing && (
        <button
          onClick={() => setShowDetails((v) => !v)}
          className="mb-4 flex w-full items-center justify-between rounded-xl border border-border px-3 py-2.5 text-sm font-medium text-muted hover:bg-surface-raised"
        >
          Más detalles (opcional)
          {showDetails ? <ChevronUp size={16} /> : <ChevronDown size={16} />}
        </button>
      )}

      {showDetails && (
        <FieldGroup>
          <Label>Profesores/as</Label>
          <TagInput values={professors} onChange={setProfessors} placeholder="Escribí un nombre y Enter" />
        </FieldGroup>
      )}

      {!!error && (
        <div className="mb-4">
          <ApiErrorNotice error={error} />
        </div>
      )}

      <Button className="w-full" disabled={!name.trim() || submitting} onClick={() => void handleSubmit()}>
        {submitting ? 'Guardando…' : editing ? 'Guardar cambios' : 'Crear materia'}
      </Button>
    </Sheet>
  )
}
