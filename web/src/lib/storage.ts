export function load(key: string) {
  try {
    return localStorage.getItem(key)
  } catch {
    return null
  }
}

export function save(key: string, value: string) {
  try {
    localStorage.setItem(key, value)
  } catch {}
}

export type View = 'grid' | 'list'

function views(): Record<string, View> {
  try {
    const o = JSON.parse(load('views') ?? '')
    return o && typeof o === 'object' ? o : {}
  } catch {
    return {}
  }
}

export const viewOf = (key: string): View | undefined => views()[key]

export function keepView(key: string, v: View) {
  const m = views()
  delete m[key]
  m[key] = v
  save('views', JSON.stringify(Object.fromEntries(Object.entries(m).slice(-500))))
}
