import Download from '@lucide/svelte/icons/download'
import FolderOpen from '@lucide/svelte/icons/folder-open'
import type { Action } from '../components/EntryList.svelte'
import { rawURL, zipURL, saveURL, selectURL, type Loc } from './api'
import { navigate } from './router.svelte'
import { t } from './i18n'

export const folderAction: Action = { id: 'folder', label: t.openFolder, icon: FolderOpen }
export const downloadAction: Action = { id: 'download', label: t.download, icon: Download }

export function actOn(id: string, f: Loc & { name: string; dir: boolean }) {
  if (id === 'folder') navigate(selectURL(f.vol, f.path))
  else if (id === 'download') saveURL(f.dir ? zipURL(f.vol, [f.path], f.name) : rawURL(f.vol, f.path, true))
}
