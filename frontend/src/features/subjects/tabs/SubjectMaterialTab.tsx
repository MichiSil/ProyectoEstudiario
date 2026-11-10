import { useEffect, useState } from 'react'
import { BookOpen, Download, FileText, PenLine, Plus, Trash2 } from 'lucide-react'
import { useSubjectContext } from '../context'
import { plannerApi } from '@/api/planner'
import type { Material, MaterialKind } from '@/api/types'
import { Button } from '@/components/ui/Button'
import { Card } from '@/components/ui/Card'
import { Sheet } from '@/components/ui/Sheet'
import { ApiErrorNotice } from '@/components/ApiErrorNotice'
import { formatSize } from '@/features/forum/shared'
import { MaterialForm } from '../MaterialForm'

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
  const [downloading, setDownloading] = useState<string | null>(null)

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

  async function download(m: Material) {
    setDownloading(m.id)
    setError(null)
    try {
      await plannerApi.downloadMaterial(m)
    } catch (err) {
      setError(err)
    } finally {
      setDownloading(null)
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
                      {m.file && (
                        <button
                          onClick={() => void download(m)}
                          disabled={downloading === m.id}
                          className="mt-1.5 inline-flex max-w-full items-center gap-1.5 rounded-lg bg-accent-soft px-2 py-1 text-xs text-accent-ink hover:bg-accent/15 disabled:opacity-60"
                        >
                          {downloading === m.id ? <Download size={13} /> : <FileText size={13} />}
                          <span className="truncate">{downloading === m.id ? 'Descargando…' : m.file.fileName}</span>
                          <span className="shrink-0 text-accent-ink/70">· {formatSize(m.file.sizeBytes)}</span>
                        </button>
                      )}
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
  if (!kind) return null
  return (
    <Sheet open onClose={onClose} title={kind === 'teorico' ? 'Agregar teórico' : 'Agregar práctico'}>
      <MaterialForm
        key={kind}
        subjectId={subjectId}
        kind={kind}
        onCreated={(m) => {
          onCreated(m)
          onClose()
        }}
      />
    </Sheet>
  )
}
