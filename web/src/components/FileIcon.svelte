<script lang="ts">
  import Folder from '@lucide/svelte/icons/folder'
  import File from '@lucide/svelte/icons/file'
  import FileImage from '@lucide/svelte/icons/file-image'
  import FileVideo from '@lucide/svelte/icons/file-video-camera'
  import FileMusic from '@lucide/svelte/icons/file-music'
  import FileText from '@lucide/svelte/icons/file-text'
  import { kind } from '../lib/format'

  let { name, dir, size = 24 }: { name: string; dir: boolean; size?: number } = $props()
  const icons = { image: FileImage, video: FileVideo, audio: FileMusic, pdf: FileText, text: FileText, '': File }
  const Icon = $derived(dir ? Folder : icons[kind(name)])
</script>

<Icon {size} class={dir ? 'ficon dir' : 'ficon'} aria-hidden="true" />
