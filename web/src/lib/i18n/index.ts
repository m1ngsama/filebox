import { load, save } from '../storage'
import zh, { type Table } from './zh'

export type Lang = 'zh' | 'en'
export type LangPref = 'auto' | Lang

export const langPref = (): LangPref => {
  const v = load('lang')
  return v === 'zh' || v === 'en' ? v : 'auto'
}

export function setLang(v: LangPref) {
  save('lang', v)
  location.reload()
}

const prefs = navigator.languages?.length ? navigator.languages : [navigator.language]
const pref = langPref()
export const lang: Lang = pref !== 'auto' ? pref : prefs.find((l) => /^(zh|en)\b/i.test(l))?.slice(0, 2).toLowerCase() === 'zh' ? 'zh' : 'en'
export const locale = prefs.find((l) => l.slice(0, 2).toLowerCase() === lang) ?? (lang === 'zh' ? 'zh-CN' : 'en')
export const t: Table = lang === 'en' ? (await import('./en')).default : zh
document.documentElement.lang = lang === 'zh' ? 'zh-CN' : 'en'
