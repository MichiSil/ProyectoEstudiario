import { useEffect, useRef, useState } from 'react'
import { FileUp } from 'lucide-react'
import { Sheet } from '@/components/ui/Sheet'
import { Button } from '@/components/ui/Button'
import { FieldGroup, Input, Label, Textarea } from '@/components/ui/Field'
import { TagInput } from '@/components/ui/TagInput'
import { ApiErrorNotice } from '@/components/ApiErrorNotice'
import { forumApi } from '@/api/forum'
import type { Summary } from '@/api/types'
import { usePlannerData } from '@/app/providers/PlannerDataProvider'
import { formatSize } from './shared'

const MAX_BYTES = 10 * 1024 * 1024 // RN-20

/** Subir un resumen al foro (RF-14). Queda en revisión hasta que lo apruebe un admin (RN-19). */
export function UploadSummarySheet({ open, onClose, onUploaded }: { open: boolean; onClose: () => void; onUploaded: (s: Summary) => void }) {
  const { subjects } = usePlannerData()
  const fileInput = useRef<HTMLInputElement>(null)
  const [title, setTitle] = useState('')
  const [category, setCategory] = useState('')
  const [description, setDescription] = useState('')
  const [tags, setTags] = useState<string[]>([])
  const [file, setFile] = useState<File | null>(null)
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState<unknown>(null)

  useEffect(() => {
    if (!open) return
    setTitle('')
    setCategory('')
    setDescription('')
    setTags([])
    setFile(null)
    setError(null)
  }, [open])

  const fileProblem = !file
    ? null
    : file.type !== 'application/pdf' && !file.name.toLowerCase().endsWith('.pdf')
      ? 'El archivo tiene que ser un PDF.'
      : file.size > MAX_BYTES
        ? 'El archivo pesa más de 10 MB.'
        : null
  const canSubmit = !submitting && !!title.trim() && !!category.trim() && !!file && !fileProblem

  async function handleSubmit() {
    if (!canSubmit || !file) return
    setSubmitting(true)
    setError(null)
    try {
      const created = await forumApi.publish({ title: title.trim(), category: category.trim(), description: description.trim(), tags, file })
      onUploaded(created)
      onClose()
    } catch (err) {
      setError(err)
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <Sheet open={open} onClose={onClose} title="Subir resumen">
      <FieldGroup>
        <Label>Título</Label>
        <Input value={title} onChange={(e) => setTitle(e.target.value)} placeholder="Resumen del primer parcial" maxLength={200} />
      </FieldGroup>

      <FieldGroup>
        <Label>Materia</Label>
        <Input value={category} onChange={(e) => setCategory(e.target.value)} placeholder="Análisis Matemático II" list="materias-foro" maxLength={120} />
        <datalist id="materias-foro">
          {subjects.map((s) => (
            <option key={s.id} value={s.name} />
          ))}
        </datalist>
        <p className="mt-1 text-xs text-subtle">Escribila como la conoce todo el mundo, así otros la encuentran.</p>
      </FieldGroup>

      <FieldGroup>
        <Label>Descripción (opcional)</Label>
        <Textarea value={description} onChange={(e) => setDescription(e.target.value)} placeholder="Qué temas cubre, de qué cátedra es…" maxLength={2000} />
      </FieldGroup>

      <FieldGroup>
        <Label>Etiquetas (opcional)</Label>
        <TagInput values={tags} onChange={setTags} placeholder="integrales, series… (Enter para agregar)" />
      </FieldGroup>

      <FieldGroup>
        <Label>Archivo PDF</Label>
        <input
          ref={fileInput}
          type="file"
          accept="application/pdf,.pdf"
          className="hidden"
          onChange={(e) => setFile(e.target.files?.[0] ?? null)}
        />
        <button
          type="button"
          onClick={() => fileInput.current?.click()}
          className="flex w-full items-center gap-3 rounded-xl border border-dashed border-border bg-paper px-4 py-3 text-left hover:border-accent"
        >
          <FileUp size={20} className="shrink-0 text-accent" />
          <span className="min-w-0 flex-1">
            <span className="block truncate text-sm font-medium text-ink">{file ? file.name : 'Elegir PDF'}</span>
            <span className="block text-xs text-subtle">{file ? formatSize(file.size) : 'Hasta 10 MB'}</span>
          </span>
        </button>
        {fileProblem && <p className="mt-1.5 text-xs text-danger">{fileProblem}</p>}
      </FieldGroup>

      {!!error && (
        <div className="mb-4">
          <ApiErrorNotice error={error} />
        </div>
      )}

      <Button className="w-full" disabled={!canSubmit} onClick={() => void handleSubmit()}>
        {submitting ? 'Subiendo…' : 'Subir resumen'}
      </Button>
      <p className="mt-3 text-center text-xs text-subtle">Un administrador lo revisa antes de publicarlo en el foro.</p>
    </Sheet>
  )
}
