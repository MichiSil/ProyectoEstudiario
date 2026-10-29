import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
// Se importan como módulos JS (no @import en CSS) para que Vite resuelva y
// copie los archivos de fuente correctamente en el build de producción.
import '@fontsource/manrope/400.css'
import '@fontsource/manrope/500.css'
import '@fontsource/manrope/600.css'
import '@fontsource/manrope/700.css'
import '@fontsource/manrope/800.css'
import './index.css'
import App from './App.tsx'

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <App />
  </StrictMode>,
)
