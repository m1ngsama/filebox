export type Loader = {
  loadText: (name: string) => Promise<string | null>
  loadBlob: (name: string) => Promise<Blob | null>
  getSize: (name: string) => number
}
export type Book = {
  dir?: string
  rendition?: { layout?: string }
  sections: { linear?: string }[]
  transformTarget?: EventTarget
}
export class EPUB {
  constructor(loader: Loader)
  init(): Promise<Book>
}
