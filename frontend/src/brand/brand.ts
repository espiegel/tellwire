// Per-deployment branding. A deployment can serve its own /brand.json next to index.html
// (mount a file over /srv/brand.json in the frontend container) to rename the client for
// its MUD without rebuilding. Anything missing or invalid falls back to the defaults.

export interface Brand {
  /** Header wordmark. Rendered uppercase. */
  name: string
  /** Index in `name` where the accent colour starts (e.g. "DUNEMUD" with 4 -> DUNE + MUD). */
  accentStart: number
  /** Small text next to the wordmark (hidden on narrow screens). */
  tagline: string
  /** Browser tab title. */
  title: string
  /** <meta name="description">. */
  description: string
}

export const DEFAULT_BRAND: Brand = {
  name: 'Tellwire',
  accentStart: 4,
  tagline: 'play in your browser',
  title: 'Tellwire — MUD in your browser',
  description: 'Tellwire — a browser MUD client. Play your favourite MUD with no install.',
}

const LIMITS: Record<'name' | 'tagline' | 'title' | 'description', number> = { name: 24, tagline: 60, title: 80, description: 200 }

function text(v: unknown, max: number, fallback: string): string {
  if (typeof v !== 'string') return fallback
  const s = v.trim()
  return s.length > 0 && s.length <= max ? s : fallback
}

/** Validate untrusted JSON. Never throws; unknown fields are ignored. Values are rendered as text, never HTML. */
export function parseBrand(input: unknown): Brand {
  if (typeof input !== 'object' || input === null || Array.isArray(input)) return DEFAULT_BRAND
  const o = input as Record<string, unknown>
  const name = text(o.name, LIMITS.name, DEFAULT_BRAND.name)
  const nameChanged = name !== DEFAULT_BRAND.name
  const rawAccent = o.accentStart
  const accentStart =
    typeof rawAccent === 'number' && Number.isInteger(rawAccent) && rawAccent >= 0 && rawAccent <= name.length
      ? rawAccent
      : nameChanged ? name.length : DEFAULT_BRAND.accentStart // renamed without accent info: single colour
  return {
    name,
    accentStart,
    tagline: text(o.tagline, LIMITS.tagline, DEFAULT_BRAND.tagline),
    title: text(o.title, LIMITS.title, nameChanged ? name : DEFAULT_BRAND.title),
    description: text(o.description, LIMITS.description, DEFAULT_BRAND.description),
  }
}

/** Fetch /brand.json. Any failure (network, 404 served as HTML, bad JSON, timeout) yields the defaults. */
export async function loadBrand(fetchImpl: typeof fetch = fetch, timeoutMs = 1500): Promise<Brand> {
  try {
    const ctl = new AbortController()
    const timer = setTimeout(() => ctl.abort(), timeoutMs)
    try {
      const res = await fetchImpl('/brand.json', { cache: 'no-cache', signal: ctl.signal })
      if (!res.ok) return DEFAULT_BRAND
      return parseBrand(await res.json())
    } finally {
      clearTimeout(timer)
    }
  } catch {
    return DEFAULT_BRAND
  }
}

export function applyBrand(brand: Brand, doc: Document = document): void {
  doc.title = brand.title
  doc.querySelector('meta[name="description"]')?.setAttribute('content', brand.description)
}
