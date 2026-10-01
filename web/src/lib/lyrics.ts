export type Line = { at: number; text: string }

const stamp = /\[(\d+):(\d+(?:[.:]\d+)?)\]/g

export function parse(s: string): Line[] {
  let offset = 0
  const out: Line[] = []
  for (const row of s.split(/\r?\n/)) {
    const off = /^\[offset:\s*([+-]?\d+)\s*\]/i.exec(row)
    if (off) offset = Number(off[1]) / 1000
    const times = [...row.matchAll(stamp)]
    if (!times.length) continue
    const text = row.replace(/\[[^\]]*\]/g, '').trim()
    for (const m of times) out.push({ at: Number(m[1]) * 60 + Number(m[2].replace(':', '.')), text })
  }
  return out.map((l) => ({ ...l, at: Math.max(0, l.at - offset) })).sort((a, b) => a.at - b.at)
}

// Chinese lyrics files are often GBK; a strict UTF-8 decode tells them apart.
export function decode(b: ArrayBuffer) {
  try {
    return new TextDecoder('utf-8', { fatal: true }).decode(b)
  } catch {
    return new TextDecoder('gb18030').decode(b)
  }
}
