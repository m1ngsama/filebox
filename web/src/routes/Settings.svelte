<script lang="ts">
  import KeyRound from '@lucide/svelte/icons/key-round'
  import CopyButton from '../components/CopyButton.svelte'
  import ConfirmDialog from '../components/ConfirmDialog.svelte'
  import { api, type Token } from '../lib/api'
  import { date, ago } from '../lib/format'
  import { t } from '../lib/i18n'

  let { vols }: { vols: string[] } = $props()

  let tokens = $state<Token[] | null>(null)
  let label = $state('')
  let readonly = $state(false)
  let created = $state('')
  let error = $state('')
  let busy = $state(false)
  let revoking = $state<Token | null>(null)
  const dav = `${location.origin}/dav/`
  const vol = $derived(encodeURIComponent(vols[0] ?? ''))

  async function load() {
    try {
      tokens = (await api.tokens()).tokens
    } catch (e) {
      error = (e as Error).message
    }
  }
  load()

  async function create(e: SubmitEvent) {
    e.preventDefault()
    busy = true
    error = ''
    try {
      created = (await api.newToken(label.trim(), readonly)).token
      label = ''
      readonly = false
    } catch (err) {
      error = (err as Error).message
    }
    busy = false
    await load()
  }
</script>

<section class="settings">
  <h2>{t.appPasswords}</h2>
  <p class="hint">{t.appPasswordsHint}</p>

  <form class="token-form" onsubmit={create}>
    <label class="field grow">
      <span>{t.label}</span>
      <input bind:value={label} placeholder={t.labelPlaceholder} required maxlength="64" />
    </label>
    <label class="check"><input type="checkbox" bind:checked={readonly} />{t.readonly}</label>
    <button class="primary" disabled={busy || !label.trim()}>{t.newToken}</button>
  </form>
  {#if error}<p class="error">{error}</p>{/if}

  {#if created}
    <div class="token-new" role="status">
      <p>{t.tokenOnce}</p>
      <div class="code-line"><code>{created}</code><CopyButton text={created} label={t.copy} /></div>
      <dl>
        <dt>{t.webdavURL}</dt>
        <dd class="code-line"><code>{dav}</code><CopyButton text={dav} label={t.copy} /></dd>
        <dd class="hint">{t.webdavLogin}</dd>
        <dt>{t.curlUpload}</dt>
        <dd><code>curl -T file.txt -u :{created} {dav}{vol}/</code></dd>
        <dt>{t.curlDownload}</dt>
        <dd><code>curl -C - -O -u :{created} {dav}{vol}/file.txt</code></dd>
      </dl>
    </div>
  {/if}

  {#if tokens}
    <ul class="rows">
      {#each tokens as k (k.id)}
        <li>
          <KeyRound size={18} class="row-icon" />
          <div class="row-main">
            <span class="row-title" title={k.label}>{k.label}</span>
            <span class="tags">
              <span class="tag">{k.readonly ? t.readonly : t.readWrite}</span>
              {#if k.last_used}
                <span class="hint" title={date(k.last_used * 1000)}>{t.lastUsed(ago(k.last_used * 1000))}</span>
              {:else}
                <span class="hint">{t.neverUsed}</span>
              {/if}
            </span>
          </div>
          <button class="danger" onclick={() => (revoking = k)}>{t.revoke}</button>
        </li>
      {:else}
        <li class="hint">{t.noTokens}</li>
      {/each}
    </ul>
  {/if}
</section>

{#if revoking}
  {@const k = revoking}
  <ConfirmDialog
    title={t.revokeTitle}
    message={t.revokeMessage(k.label)}
    action={t.revoke}
    onconfirm={async () => {
      await api.delToken(k.id)
      await load()
    }}
    onclose={() => (revoking = null)}
  />
{/if}
