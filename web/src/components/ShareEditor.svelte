<script lang="ts">
  import { untrack } from 'svelte'
  import Modal from './Modal.svelte'
  import { api, type Share, type ShareEdit } from '../lib/api'
  import { t } from '../lib/i18n'
  import { toast } from '../lib/toast.svelte'

  let { share, dir, onclose, onsaved }: { share: Share; dir: boolean; onclose: () => void; onsaved: () => void } = $props()

  const modes = ['read', 'upload', 'drop'] as const
  const init = untrack(() => share)
  let mode = $state(init.mode)
  let protect = $state(init.has_password)
  let password = $state('')
  let expires = $state(-1)
  let note = $state(init.note)
  let limit = $state(init.max_upload ? String(Math.round(init.max_upload / 1048576)) : '')
  let error = $state('')
  let busy = $state(false)

  async function save() {
    const e: ShareEdit = { mode, note: note.trim() }
    if (protect && password) e.password = password
    else if (protect && !share.has_password) return (error = t.passwordRequired)
    else if (!protect && share.has_password) e.password = ''
    if (expires >= 0) e.expires_in = expires
    e.max_upload = mode === 'read' ? share.max_upload : Math.max(0, Math.round(Number(limit) * 1048576) || 0)
    busy = true
    try {
      await api.editShare(share.id, e)
      toast(t.shareSaved)
      onsaved()
      onclose()
    } catch (err) {
      error = (err as Error).message
    }
    busy = false
  }
</script>

<Modal title={t.editShare} {onclose} onsubmit={save}>
  <fieldset class="modes">
    <legend>{t.permission}</legend>
    {#each modes as m (m)}
      <label class="radio"><input type="radio" name="edit-mode" value={m} bind:group={mode} disabled={m !== 'read' && !dir} />{t.modes[m]}</label>
    {/each}
  </fieldset>
  <label class="radio"><input type="checkbox" bind:checked={protect} />{t.requirePassword}</label>
  {#if protect}
    <label class="field">
      <span>{t.newPassword}</span>
      <input type="password" bind:value={password} autocomplete="new-password" placeholder={share.has_password ? t.keepPassword : ''} />
    </label>
  {/if}
  <label class="field">
    <span>{t.expiry}</span>
    <select bind:value={expires}>
      <option value={-1}>{t.keepExpiry}</option>
      {#each t.expiries as [sec, label] (sec)}<option value={sec}>{label}</option>{/each}
    </select>
  </label>
  <label class="field">
    <span>{t.note}</span>
    <textarea bind:value={note} rows="3" maxlength="300" placeholder={t.noteHint}></textarea>
  </label>
  {#if mode !== 'read'}
    <label class="field">
      <span>{t.uploadLimit}</span>
      <input type="number" min="0" step="1" inputmode="numeric" bind:value={limit} placeholder={t.uploadLimitHint} />
    </label>
  {/if}
  {#if init.mode === 'drop' && mode !== 'drop'}<p class="warn-note" role="alert">{t.dropExposed}</p>{/if}
  {#if error}<p class="error">{error}</p>{/if}
  {#snippet footer()}
    <button type="button" onclick={onclose}>{t.cancel}</button>
    <button class="primary" disabled={busy}>{t.save}</button>
  {/snippet}
</Modal>
