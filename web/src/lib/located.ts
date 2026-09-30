import Download from '@lucide/svelte/icons/download'
import FolderOpen from '@lucide/svelte/icons/folder-open'
import type { Action } from '../components/EntryList.svelte'
import { rawURL, zipURL, saveURL, selectURL, type Loc } from './api'
import { toast } from './toast.svelte'
import { navigate } from './router.svelte'
import { t } from './i18n'

export const packing = (name: string) => toast(t.zipping(name), { kind: 'info' })
export function saveZip(url: string, name: string) {
  packing(name)
  saveURL(url)
}

export const folderAction: Action = { id: 'folder', label: t.openFolder, icon: FolderOpen }
export const downloadAction: Action = { id: 'download', label: t.download, icon: Download }

export function actOn(id: string, f: Loc & { name: string; dir: boolean }) {
  if (id === 'folder') navigate(selectURL(f.vol, f.path))
  else if (id === 'download' && f.dir) saveZip(zipURL(f.vol, [f.path], f.name), f.name)
  else if (id === 'download') saveURL(rawURL(f.vol, f.path, true))
}
