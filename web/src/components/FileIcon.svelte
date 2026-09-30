<script lang="ts">
  import { icon } from '../lib/icon'
  import Folder from '@lucide/svelte/icons/folder'
  import File from '@lucide/svelte/icons/file'
  import FileImage from '@lucide/svelte/icons/file-image'
  import FileVideo from '@lucide/svelte/icons/file-video-camera'
  import FileMusic from '@lucide/svelte/icons/file-music'
  import FileText from '@lucide/svelte/icons/file-text'
  import FileCode from '@lucide/svelte/icons/file-code'
  import FileArchive from '@lucide/svelte/icons/file-archive'
  import FileSpreadsheet from '@lucide/svelte/icons/file-spreadsheet'
  import { look } from '../lib/format'

  let { name, dir, size = icon.lg }: { name: string; dir: boolean; size?: number } = $props()
  const icons = { image: FileImage, video: FileVideo, audio: FileMusic, pdf: FileText, text: FileText, code: FileCode, archive: FileArchive, sheet: FileSpreadsheet, '': File }
  const k = $derived(dir ? 'dir' : look(name))
  const Icon = $derived(k === 'dir' ? Folder : icons[k])
</script>

<Icon {size} class={`ficon ${k}`} aria-hidden="true" />
