import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import './index.css'
import App from './App'
import { applyBrand, loadBrand } from './brand/brand'

// Branding is fetched before the first render (bounded by a short timeout) so the page never
// flashes the default name before switching to a deployment's own.
loadBrand().then((brand) => {
  applyBrand(brand)
  createRoot(document.getElementById('root')!).render(
    <StrictMode>
      <App brand={brand} />
    </StrictMode>,
  )
})
