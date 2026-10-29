/** localStorage tolerante: en modo privado o con el almacenamiento bloqueado, no rompe la app. */
export function readJson<T>(key: string, fallback: T): T {
  try {
    const raw = localStorage.getItem(key)
    return raw ? (JSON.parse(raw) as T) : fallback
  } catch {
    return fallback
  }
}

export function writeJson(key: string, value: unknown) {
  try {
    localStorage.setItem(key, JSON.stringify(value))
  } catch {
    // sin almacenamiento local la app sigue funcionando, sólo no recuerda la caché
  }
}

export function removeKey(key: string) {
  try {
    localStorage.removeItem(key)
  } catch {
    // idem
  }
}
