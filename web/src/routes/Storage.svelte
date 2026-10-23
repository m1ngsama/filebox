<script lang="ts">
  import { icon } from '../lib/icon'
  import CloudOff from '@lucide/svelte/icons/cloud-off'
  import ChartPie from '@lucide/svelte/icons/chart-pie'
  import Copy from '@lucide/svelte/icons/copy'
  import FolderIcon from '@lucide/svelte/icons/folder'
  import EmptyState from '../components/EmptyState.svelte'
  import FileIcon from '../components/FileIcon.svelte'
  import { api, filesURL, selectURL, type UsageReport } from '../lib/api'
  import { link } from '../lib/router.svelte'
  import { size } from '../lib/format'
  import { t } from '../lib/i18n'

  let report = $state.raw<UsageReport>()
  let error = $state('')
  let tries = $state(0)

  $effect(() => {
    void tries
    const stop = new AbortController()
    api.usage(stop.signal).then(
      (r) => ((report = r), (error = '')),
      (e: Error) => !stop.signal.aborted && (error = e.message),
    )
    return () => stop.abort()
  })

  const total = $derived(report?.kinds.reduce((n, k) => n + k.size, 0) ?? 0)
  const files = $derived(report?.kinds.reduce((n, k) => n + k.files, 0) ?? 0)
  const label = (k: string) => (k === 'other' ? t.storageOther : (t.kinds[k] ?? k))
  const share = (n: number, of: number) => (of ? `${Math.max(0.5, (100 * n) / of)}%` : '0%')
  const biggest = $derived(report?.folders[0]?.size ?? 0)
  const heaviest = $derived(report?.largest[0]?.size ?? 0)
  const where = (p: string) => p.slice(0, Math.max(0, p.lastIndexOf('/')))
</script>

{#if error}
  <EmptyState icon={CloudOff} as="h2" title={t.loadFailedTitle} hint={error}>
    <button class="primary" onclick={() => tries++}>{t.retry}</button>
  </EmptyState>
{:else if report && !total}
  <EmptyState icon={ChartPie} title={t.storageEmpty} hint={report.scanning ? t.indexing : ''} />
{:else if report}
  <div class="storage">
    {#if report.scanning}<p class="hint banner">{t.indexing}</p>{/if}
    <a class="button" href="/duplicates" onclick={link}><Copy size={icon.sm} />{t.dupFind}</a>
    <section aria-labelledby="st-kind">
      <h2 id="st-kind">{t.storageByKind}</h2>
      <p class="st-total">{size(total)} · {t.storageOf(files)}</p>
      <div class="st-bar" role="img" aria-label={report.kinds.map((k) => `${label(k.kind)} ${size(k.size)}`).join(', ')}>
        {#each report.kinds as k (k.kind)}<span class={`k-${k.kind}`} style:flex-grow={k.size}></span>{/each}
      </div>
      <ul class="st-legend">
        {#each report.kinds as k (k.kind)}
          <li><i class={`k-${k.kind}`}></i><span>{label(k.kind)}</span><b>{size(k.size)}</b><span class="hint">{t.storageOf(k.files)}</span></li>
        {/each}
      </ul>
    </section>

    {#if report.folders.length}
      <section aria-labelledby="st-folder">
        <h2 id="st-folder">{t.storageByFolder}</h2>
        <ul class="st-rows">
          {#each report.folders as f (`${f.vol}/${f.path}`)}
            <li>
              <a href={filesURL(f.vol, f.path)} onclick={link}>
                <FolderIcon size={icon.md} />
                <span class="st-name">{f.path}<span class="hint"> · {f.vol} · {t.storageOf(f.files)}</span></span>
                <b>{size(f.size)}</b>
                <span class="st-meter" style:--p={share(f.size, biggest)}></span>
              </a>
            </li>
          {/each}
        </ul>
      </section>
    {/if}

    <section aria-labelledby="st-big">
      <h2 id="st-big">{t.storageLargest}</h2>
      <ul class="st-rows">
        {#each report.largest as f (`${f.vol}/${f.path}`)}
          <li>
            <a href={selectURL(f.vol, f.path)} onclick={link}>
              <FileIcon name={f.name} dir={false} size={icon.md} />
              <span class="st-name">{f.name}<span class="hint"> · {f.vol}{where(f.path) ? `/${where(f.path)}` : ''}</span></span>
              <b>{size(f.size)}</b>
              <span class="st-meter" style:--p={share(f.size, heaviest)}></span>
            </a>
          </li>
        {/each}
      </ul>
    </section>
  </div>
{/if}
