export type Loader = {
  loadText: (name: string) => Promise<string | null>
  loadBlob: (name: string) => Promise<Blob | null>
  getSize: (name: string) => number
}
export type TocItem = { label: string; href: string; subitems?: TocItem[] | null }
export type Target = { index: number; anchor: (doc: Document) => unknown }
export type Book = {
  dir?: string
  rendition?: { layout?: string }
  sections: { linear?: string; size?: number; resolveHref: (href: string) => string }[]
  toc?: TocItem[] | null
  transformTarget?: EventTarget
  resolveHref(href: string): Target | null
  isExternal(href: string): boolean
}
export class EPUB {
  constructor(loader: Loader)
  init(): Promise<Book>
}
