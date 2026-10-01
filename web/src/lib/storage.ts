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

export function viewOf(key: string): View | undefined {
  const v = views()[key]
  if (v) keepView(key, v)
  return v
}

export function keepView(key: string, v: View) {
  const m = views()
  delete m[key]
  m[key] = v
  let all = Object.entries(m)
  let json = JSON.stringify(Object.fromEntries(all))
  while (json.length > 64 << 10 && all.length > 1) json = JSON.stringify(Object.fromEntries((all = all.slice(Math.ceil(all.length / 8)))))
  save('views', json)
}

try {
  localStorage.removeItem('grid')
  localStorage.removeItem('shareGrid')
} catch {}
