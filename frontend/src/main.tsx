import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { BrowserRouter } from 'react-router-dom'
import { AuthProvider } from './auth/AuthProvider'
import App from './App'
import './index.css'

const accentColor = import.meta.env.VITE_ACCENT_COLOR
if (accentColor) {
  const root = document.documentElement
  root.style.setProperty('--color-accent', accentColor)
  root.style.setProperty('--color-accent-light', `color-mix(in srgb, ${accentColor} 70%, white)`)
  root.style.setProperty('--color-accent-dark', `color-mix(in srgb, ${accentColor} 85%, black)`)
}

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <BrowserRouter>
      <AuthProvider>
        <App />
      </AuthProvider>
    </BrowserRouter>
  </StrictMode>,
)
