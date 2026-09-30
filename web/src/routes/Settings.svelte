<script lang="ts">
  import { icon } from '../lib/icon'
  import KeyRound from '@lucide/svelte/icons/key-round'
  import Fingerprint from '@lucide/svelte/icons/fingerprint'
  import MonitorSmartphone from '@lucide/svelte/icons/monitor-smartphone'
  import Plus from '@lucide/svelte/icons/plus'
  import LogOut from '@lucide/svelte/icons/log-out'
  import type { Snippet } from 'svelte'
  import RowList from '../components/RowList.svelte'
  import Activity from '../components/Activity.svelte'
  import EmptyState from '../components/EmptyState.svelte'
  import CopyButton from '../components/CopyButton.svelte'
  import ConfirmDialog from '../components/ConfirmDialog.svelte'
  import NameDialog from '../components/NameDialog.svelte'
  import { api, session, type Passkey, type Session, type Token } from '../lib/api'
  import { addPasskey, deviceName, passkeyError, validPasskeyName } from '../lib/passkey'
  import { date, ago, device } from '../lib/format'
  import { t, langPref, setLang, type LangPref } from '../lib/i18n'
  import { toast } from '../lib/toast.svelte'
  import { theme, setTheme, type Theme } from '../lib/theme'

  let { vols }: { vols: string[] } = $props()

  let tokens = $state<Token[] | null>(null)
  let tokensError = $state('')
  let label = $state('')
  let readonly = $state(false)
  let created = $state('')
  let error = $state('')
  let busy = $state(false)
  let revoking = $state<Token | null>(null)
  let sessions = $state<Session[] | null>(null)
  let sessionsError = $state('')
  let signingOut = $state<Session | 'others' | null>(null)
  let passkeysOn = $state(false)
  let passkeys = $state<Passkey[] | null>(null)
  let passkeysError = $state('')
  let adding = $state(false)
  let renaming = $state<Passkey | null>(null)
  let removing = $state<Passkey | null>(null)
  let mode = $state(theme())
  const language = langPref()
  const dav = `${location.origin}/dav/`
  const vol = $derived(encodeURIComponent(vols[0] ?? ''))
  const sections = $derived([
    ['appearance', t.appearance],
    ['sessions', t.sessions],
    ...(passkeysOn ? [['passkeys', t.passkeys]] : []),
    ['tokens', t.appPasswords],
    ['activity', t.activity],
  ])

  function jump(e: MouseEvent, id: string) {
    e.preventDefault()
    document.getElementById(id)?.scrollIntoView({ behavior: matchMedia('(prefers-reduced-motion: reduce)').matches ? 'auto' : 'smooth' })
  }

  async function load() {
    try {
      tokens = (await api.tokens()).tokens
      tokensError = ''
    } catch (e) {
      tokensError = (e as Error).message
    }
  }
  load()

  async function loadSessions() {
    try {
      sessions = (await api.sessions()).sessions
      sessionsError = ''
    } catch (e) {
      sessionsError = (e as Error).message
    }
  }
  loadSessions()

  async function loadPasskeys() {
    try {
      passkeysOn = (await api.passkeysEnabled()).enabled
      if (passkeysOn) passkeys = (await api.passkeys()).passkeys
      passkeysError = ''
    } catch (e) {
      passkeysError = (e as Error).message
    }
  }
  loadPasskeys()

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

{#snippet head(id: string, title: string, hint: string, action?: Snippet)}
  <header>
    <div>
      <h2 id={`${id}-title`}>{title}</h2>
      <p class="hint">{hint}</p>
    </div>
    {@render action?.()}
  </header>
{/snippet}

{#snippet addKey()}<button onclick={() => (adding = true)}><Plus size={icon.sm} />{t.addPasskey}</button>{/snippet}

<div class="settings-layout">
  <nav class="settings-nav" aria-label={t.settings}>
    {#each sections as [id, label] (id)}
      <a href={`#${id}`} onclick={(e) => jump(e, id)}>{label}</a>
    {/each}
  </nav>
  <div class="settings">
    <section class="card-section" id="appearance" aria-labelledby="appearance-title">
      {@render head('appearance', t.appearance, t.appearanceHint)}
      <div class="chips" role="group" aria-label={t.appearance}>
        {#each Object.entries(t.themes) as [k, label] (k)}
          <button type="button" class="chip" aria-pressed={mode === k} onclick={() => setTheme((mode = k as Theme))}>{label}</button>
        {/each}
      </div>
      <div class="pref" role="group" aria-labelledby="language-title">
        <h3 id="language-title">{t.language}</h3>
        <div class="chips">
          {#each Object.entries(t.languages) as [k, label] (k)}
            <button type="button" class="chip" lang={k === 'auto' ? undefined : k} aria-pressed={language === k} onclick={() => language !== k && setLang(k as LangPref)}>{label}</button>
          {/each}
        </div>
      </div>
    </section>

    <section class="card-section" id="sessions" aria-labelledby="sessions-title">
      {@render head('sessions', t.sessions, t.sessionsHint)}
      <RowList items={sessions} error={sessionsError} onretry={loadSessions} key={(s) => s.id} label={t.sessions}>
        {#snippet row(s)}
          <MonitorSmartphone size={icon.md} class="row-icon" />
          <div class="row-main">
            <span class="row-title" title={s.user_agent}>{device(s.user_agent) || t.unknownDevice}</span>
            <span class="tags">
              {#if s.current}<span class="tag">{t.thisDevice}</span>{/if}
              {#if s.ip}<span class="hint">{s.ip}</span>{/if}
              <span class="hint" title={date(s.last_used * 1000)}>{t.lastUsed(ago(s.last_used * 1000))}</span>
            </span>
          </div>
          <button class="quiet" onclick={() => (signingOut = s)}>{t.signOut}</button>
        {/snippet}
        {#snippet empty()}{/snippet}
      </RowList>
      {#if sessions && sessions.length > 1}
        <div><button onclick={() => (signingOut = 'others')}><LogOut size={icon.sm} />{t.signOutOthers}</button></div>
      {/if}
    </section>

    {#if passkeysOn}
      <section class="card-section" id="passkeys" aria-labelledby="passkeys-title">
        {@render head('passkeys', t.passkeys, t.passkeysHint, addKey)}
        <RowList items={passkeys} error={passkeysError} onretry={loadPasskeys} key={(k) => k.id}>
          {#snippet row(k)}
            <Fingerprint size={icon.md} class="row-icon" />
            <div class="row-main">
              <span class="row-title" title={k.name}>{k.name}</span>
              <span class="tags">
                <span class="hint">{t.addedAt(date(k.created * 1000))}</span>
                {#if k.last_used}
                  <span class="hint" title={date(k.last_used * 1000)}>{t.lastUsed(ago(k.last_used * 1000))}</span>
                {:else}
                  <span class="hint">{t.neverUsed}</span>
                {/if}
              </span>
            </div>
            <button class="ghost" onclick={() => (renaming = k)}>{t.rename}</button>
            <button class="quiet" onclick={() => (removing = k)}>{t.remove}</button>
          {/snippet}
          {#snippet empty()}<EmptyState compact icon={Fingerprint} title={t.noPasskeys} />{/snippet}
        </RowList>
      </section>
    {/if}

    <section class="card-section" id="tokens" aria-labelledby="tokens-title">
      {@render head('tokens', t.appPasswords, t.appPasswordsHint)}
      <form class="token-form" onsubmit={create}>
        <label class="field grow">
          <span>{t.label}</span>
          <input bind:value={label} placeholder={t.labelPlaceholder} required maxlength="64" />
        </label>
        <label class="check"><input type="checkbox" bind:checked={readonly} />{t.readonly}</label>
        <button class="primary" disabled={busy || !label.trim()}>{t.newToken}</button>
      </form>
      {#if error}<p class="error" role="alert">{error}</p>{/if}

      {#if created}
        <div class="token-new" role="status">
          <p>{t.tokenOnce}</p>
          <div class="code-line"><code>{created}</code><CopyButton text={created} label={t.copy} done={t.copied} /></div>
          <dl>
            <dt>{t.webdavURL}</dt>
            <dd class="code-line"><code>{dav}</code><CopyButton text={dav} label={t.copy} done={t.copied} /></dd>
            <dd class="hint">{t.webdavLogin}</dd>
            <dt>{t.curlUpload}</dt>
            <dd><code>curl -T file.txt -u :{created} {dav}{vol}/</code></dd>
            <dt>{t.curlDownload}</dt>
            <dd><code>curl -C - -O -u :{created} {dav}{vol}/file.txt</code></dd>
          </dl>
        </div>
      {/if}

      <RowList items={tokens} error={tokensError} onretry={load} key={(k) => k.id}>
        {#snippet row(k)}
          <KeyRound size={icon.md} class="row-icon" />
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
          <button class="quiet" onclick={() => (revoking = k)}>{t.revoke}</button>
        {/snippet}
        {#snippet empty()}<EmptyState compact icon={KeyRound} title={t.noTokens} />{/snippet}
      </RowList>
    </section>

    <section class="card-section" id="activity" aria-labelledby="activity-title">
      {@render head('activity', t.activity, t.activityHint)}
      <Activity />
    </section>
  </div>
</div>

{#if revoking}
  {@const k = revoking}
  <ConfirmDialog
    title={t.revokeTitle}
    message={t.revokeMessage(k.label)}
    action={t.revoke}
    onconfirm={async () => {
      await api.delToken(k.id)
      toast(t.revoked(k.label))
      await load()
    }}
    onclose={() => (revoking = null)}
  />
{/if}

{#if signingOut}
  {@const s = signingOut}
  <ConfirmDialog
    title={s === 'others' ? t.signOutOthers : t.signOutTitle}
    message={s === 'others' ? t.signOutOthersMessage : s.current ? t.signOutSelfMessage : t.signOutMessage(device(s.user_agent) || t.unknownDevice)}
    action={t.signOut}
    onconfirm={async () => {
      if (s === 'others') await api.revokeOtherSessions()
      else await api.delSession(s.id)
      if (s !== 'others' && s.current) session.lost()
      else {
        toast(t.signedOut)
        await loadSessions()
      }
    }}
    onclose={() => (signingOut = null)}
  />
{/if}

{#if adding}
  <NameDialog
    title={t.addPasskey}
    label={t.name}
    action={t.add}
    value={deviceName()}
    fresh
    valid={validPasskeyName}
    onsave={async (name) => {
      try {
        await addPasskey(name)
      } catch (e) {
        throw new Error(passkeyError(e))
      }
      toast(t.passkeyAdded)
      await loadPasskeys()
    }}
    onclose={() => (adding = false)}
  />
{/if}

{#if renaming}
  {@const k = renaming}
  <NameDialog
    title={t.renamePasskey}
    label={t.name}
    action={t.rename}
    value={k.name}
    valid={validPasskeyName}
    onsave={async (name) => {
      await api.renamePasskey(k.id, name)
      toast(t.saved)
      await loadPasskeys()
    }}
    onclose={() => (renaming = null)}
  />
{/if}

{#if removing}
  {@const k = removing}
  <ConfirmDialog
    title={t.deletePasskeyTitle}
    message={t.deletePasskeyMessage(k.name)}
    action={t.remove}
    onconfirm={async () => {
      await api.delPasskey(k.id)
      toast(t.passkeyDeleted)
      await loadPasskeys()
    }}
    onclose={() => (removing = null)}
  />
{/if}
