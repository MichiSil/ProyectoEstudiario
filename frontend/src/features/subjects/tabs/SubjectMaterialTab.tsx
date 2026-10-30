import { useEffect, useState } from 'react'
import { BookOpen, PenLine, Plus, Trash2 } from 'lucide-react'
import { useSubjectContext } from '../context'
import { plannerApi } from '@/api/planner'
import type { Material, MaterialKind } from '@/api/types'
import { Button } from '@/components/ui/Button'
import { Card } from '@/components/ui/Card'
import { FieldGroup, Input, Label, Textarea } from '@/components/ui/Field'
import { Sheet } from '@/components/ui/Sheet'
import { ApiErrorNotice } from '@/components/ApiErrorNotice'

const KIND_LABEL: Record<MaterialKind, string> = { teorico: 'Teóricos', practico: 'Prácticos' }
const KIND_HINT: Record<MaterialKind, string> = {
  teorico: 'Apuntes, unidades, capítulos del libro.',
  practico: 'Trabajos prácticos, guías de ejercicios, parciales viejos.',
}

/**
 * Material de la materia: los teóricos y prácticos que el estudiante tiene
 * para estudiar. El plan se arma con este material real; si no hay nada
 * cargado, no inventa teóricos ni prácticos.
 */
export function SubjectMaterialTab() {
  const { subject } = useSubjectContext()
  const [items, setItems] = useState<Material[] | null>(null)
  const [error, setError] = useState<unknown>(null)
  const [adding, setAdding] = useState<MaterialKind | null>(null)
  const [deleting, setDeleting] = useState<string | null>(null)

  useEffect(() => {
    plannerApi
      .listMaterials(subject.id)
      .then(setItems)
      .catch((err) => {
        setError(err)
        setItems([])
      })
  }, [subject.id])

  async function remove(id: string) {
    setDeleting(id)
    setError(null)
    try {
      await plannerApi.deleteMaterial(id)
      setItems((prev) => prev?.filter((m) => m.id !== id) ?? null)
    } catch (err) {
      setError(err)
    } finally {
      setDeleting(null)
    }
  }

  if (items === null) return <p className="py-8 text-center text-sm text-muted">Cargando material…</p>

  return (
    <div className="flex flex-col gap-4">
      <p className="text-sm text-muted">
        Cargá lo que tenés para estudiar. El plan se arma con este material: si no cargás nada, se basa sólo en los temas
        del examen.
      </p>
      {!!error && <ApiErrorNotice error={error} />}

      {(['teorico', 'practico'] as const).map((kind) => {
        const list = items.filter((m) => m.kind === kind)
        const Icon = kind === 'teorico' ? BookOpen : PenLine
        return (
          <Card key={kind}>
            <div className="flex items-start justify-between gap-3">
              <div className="min-w-0">
                <h3 className="flex items-center gap-2 font-medium text-ink">
                  <Icon size={17} className="text-accent" />
                  {KIND_LABEL[kind]}
                </h3>
                <p className="mt-0.5 text-xs text-muted">{KIND_HINT[kind]}</p>
              </div>
              <Button size="sm" variant="secondary" onClick={() => setAdding(kind)}>
                <Plus size={14} />
                Agregar
              </Button>
            </div>
            {!list.length ? (
              <p className="mt-3 text-sm text-subtle">Todavía no cargaste {kind === 'teorico' ? 'teóricos' : 'prácticos'}.</p>
            ) : (
              <ul className="mt-3 flex flex-col gap-2">
                {list.map((m) => (
                  <li key={m.id} className="flex items-start gap-3 rounded-xl bg-paper px-3 py-2.5">
                    <div className="min-w-0 flex-1">
                      <p className="text-sm font-medium text-ink">{m.title}</p>
                      {m.content && <p className="mt-0.5 line-clamp-2 whitespace-pre-line text-xs text-muted">{m.content}</p>}
                    </div>
                    <button
                      onClick={() => void remove(m.id)}
                      disabled={deleting === m.id}
                      aria-label={`Quitar ${m.title}`}
                      className="flex h-8 w-8 shrink-0 items-center justify-center rounded-lg text-muted hover:bg-danger/10 hover:text-danger disabled:opacity-50"
                    >
                      <Trash2 size={15} />
                    </button>
                  </li>
                ))}
              </ul>
            )}
          </Card>
        )
      })}

      <MaterialFormSheet
        kind={adding}
        subjectId={subject.id}
        onClose={() => setAdding(null)}
        onCreated={(m) => setItems((prev) => [...(prev ?? []), m])}
      />
    </div>
  )
}

function MaterialFormSheet({
  kind,
  subjectId,
  onClose,
  onCreated,
}: {
  kind: MaterialKind | null
  subjectId: string
  onClose: () => void
  onCreated: (m: Material) => void
}) {
  const [title, setTitle] = useState('')
  const [content, setContent] = useState('')
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<unknown>(null)

  useEffect(() => {
    setTitle('')
    setContent('')
    setError(null)
  }, [kind])

  if (!kind) return null
  const current = kind

  async function save() {
    if (!title.trim()) return
    setSaving(true)
    setError(null)
    try {
      onCreated(await plannerApi.createMaterial(subjectId, { kind: current, title: title.trim(), content: content.trim() }))
      onClose()
    } catch (err) {
      setError(err)
    } finally {
      setSaving(false)
    }
  }

  return (
    <Sheet open onClose={onClose} title={current === 'teorico' ? 'Agregar teórico' : 'Agregar práctico'}>
      <FieldGroup>
        <Label>Título</Label>
        <Input
          value={title}
          onChange={(e) => setTitle(e.target.value)}
          placeholder={current === 'teorico' ? 'Unidad 2 — Ondas' : 'TP 3 — Movimiento ondulatorio'}
          maxLength={200}
          autoFocus
        />
      </FieldGroup>
      <FieldGroup>
        <Label>¿Qué tiene? (opcional)</Label>
        <Textarea
          value={content}
          onChange={(e) => setContent(e.target.value)}
          placeholder={
            current === 'teorico'
              ? 'Temas que cubre: ondas transversales, ecuación de onda, interferencia…'
              : 'Ejercicios 1 a 12: velocidad de propagación, ondas estacionarias…'
          }
          maxLength={5000}
        />
        <p className="mt-1 text-xs text-subtle">Cuanto más detalle, mejor arma las sesiones el agente.</p>
      </FieldGroup>
      {!!error && (
        <div className="mb-4">
          <ApiErrorNotice error={error} />
        </div>
      )}
      <Button className="w-full" disabled={saving || !title.trim()} onClick={() => void save()}>
        {saving ? 'Guardando…' : 'Guardar'}
      </Button>
    </Sheet>
  )
}
