import { mount } from 'svelte'
import './app.css'
import Reader from './components/Reader.svelte'
import { fileURL, shareFileURL, rawURL, shareRawURL, validShareToken, type Src } from './lib/api'

const p = new URLSearchParams(location.hash.slice(1))
const path = p.get('p') ?? ''
const vol = p.get('vol') ?? ''
const share = p.get('share') ?? ''
const url: Src = share ? (_, as) => shareFileURL(share, path, as) : (_, as) => fileURL(vol, path, as)
const post = (kind: string) => parent !== window && parent.postMessage({ fb: kind }, location.origin)

if (share ? !validShareToken(share) : !vol) post('fail')
else
  mount(Reader, {
    target: document.getElementById('app')!,
    props: {
      list: url({} as never, 'zip-entries'),
      entry: url({} as never, 'zip-entry'),
      spot: 'pos:' + (share ? shareRawURL(share, path) : rawURL(vol, path)),
      onready: () => post('ready'),
      onfail: () => post('fail'),
      onclose: () => post('close'),
    },
  })
