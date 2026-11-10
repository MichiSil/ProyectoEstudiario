import { useRef, useState } from 'react'
import { FileUp, X } from 'lucide-react'
import { plannerApi } from '@/api/planner'
import type { Material, MaterialKind } from '@/api/types'
import { Button } from '@/components/ui/Button'
import { FieldGroup, Input, Label, Select, Textarea } from '@/components/ui/Field'
import { ApiErrorNotice } from '@/components/ApiErrorNotice'
import { formatSize } from '@/features/forum/shared'

/** Tamaño máximo del PDF de un material (lo controla también planner-service). */
const MAX_FILE_SIZE = 10 * 1024 * 1024

/**
 * Formulario para cargar un material en una materia: título (lo único
 * obligatorio), qué tiene y un PDF opcional de hasta 10 MB. Lo usan la
 * pestaña Material y el formulario del examen ("Subir material"). Con kind
 * fijo no muestra el selector de tipo.
 */
export function MaterialForm({
  subjectId,
  kind,
  onCreated,
  onCancel,
  submitLabel = 'Guardar',
}: {
  subjectId: string
  kind?: MaterialKind
  onCreated: (m: Material) => void
  onCancel?: () => void
  submitLabel?: string
}) {
  const [chosenKind, setChosenKind] = useState<MaterialKind>(kind ?? 'teorico')
  const [title, setTitle] = useState('')
  const [content, setContent] = useState('')
  const [file, setFile] = useState<File | null>(null)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<unknown>(null)
  const fileInput = useRef<HTMLInputElement>(null)
  const current = kind ?? chosenKind

  const fileProblem = !file
    ? null
    : file.size > MAX_FILE_SIZE
      ? 'El archivo pesa más de 10 MB.'
      : file.type && file.type !== 'application/pdf'
        ? 'Tiene que ser un PDF.'
        : null

  async function save() {
    if (!title.trim() || fileProblem) return
    setSaving(true)
    setError(null)
    try {
      onCreated(await plannerApi.createMaterial(subjectId, { kind: current, title: title.trim(), content: content.trim() }, file))
      setTitle('')
      setContent('')
      setFile(null)
    } catch (err) {
      setError(err)
    } finally {
      setSaving(false)
    }
  }

  return (
    <div>
      {!kind && (
        <FieldGroup>
          <Label>Tipo</Label>
          <Select value={chosenKind} onChange={(e) => setChosenKind(e.target.value as MaterialKind)}>
            <option value="teorico">Teórico</option>
            <option value="practico">Práctico</option>
          </Select>
        </FieldGroup>
      )}
      <FieldGroup>
        <Label>Título</Label>
        <Input
          value={title}
          onChange={(e) => setTitle(e.target.value)}
          placeholder={current === 'teorico' ? 'Unidad 2 — Ondas' : 'TP 4'}
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
        <p className="mt-1 text-xs text-subtle">Cuanto más detalle, mejor arma las sesiones el agente: él lee este texto, no el PDF.</p>
      </FieldGroup>
      <FieldGroup>
        <Label>Archivo PDF (opcional)</Label>
        <input
          ref={fileInput}
          type="file"
          accept="application/pdf,.pdf"
          className="hidden"
          onChange={(e) => setFile(e.target.files?.[0] ?? null)}
        />
        <div className="flex items-center gap-2">
          <button
            type="button"
            onClick={() => fileInput.current?.click()}
            className="flex min-w-0 flex-1 items-center gap-3 rounded-xl border border-dashed border-border bg-paper px-4 py-3 text-left hover:border-accent"
          >
            <FileUp size={20} className="shrink-0 text-accent" />
            <span className="min-w-0 flex-1">
              <span className="block truncate text-sm font-medium text-ink">{file ? file.name : 'Adjuntar PDF'}</span>
              <span className="block text-xs text-subtle">{file ? formatSize(file.size) : 'Hasta 10 MB'}</span>
            </span>
          </button>
          {file && (
            <button
              type="button"
              onClick={() => {
                setFile(null)
                if (fileInput.current) fileInput.current.value = ''
              }}
              aria-label="Quitar el archivo"
              className="flex h-9 w-9 shrink-0 items-center justify-center rounded-lg text-muted hover:bg-danger/10 hover:text-danger"
            >
              <X size={16} />
            </button>
          )}
        </div>
        {fileProblem && <p className="mt-1.5 text-xs text-danger">{fileProblem}</p>}
      </FieldGroup>
      {!!error && (
        <div className="mb-4">
          <ApiErrorNotice error={error} />
        </div>
      )}
      <div className="flex gap-2">
        {onCancel && (
          <Button type="button" variant="ghost" onClick={onCancel} disabled={saving}>
            Cancelar
          </Button>
        )}
        <Button type="button" className="flex-1" disabled={saving || !title.trim() || !!fileProblem} onClick={() => void save()}>
          {saving ? 'Guardando…' : submitLabel}
        </Button>
      </div>
    </div>
  )
}
