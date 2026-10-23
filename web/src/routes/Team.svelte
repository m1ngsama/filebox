<script lang="ts">
  import { icon } from '../lib/icon'
  import UserPlus from '@lucide/svelte/icons/user-plus'
  import UserRound from '@lucide/svelte/icons/user-round'
  import KeyRound from '@lucide/svelte/icons/key-round'
  import Ticket from '@lucide/svelte/icons/ticket'
  import Trash from '@lucide/svelte/icons/trash'
  import CloudOff from '@lucide/svelte/icons/cloud-off'
  import Modal from '../components/Modal.svelte'
  import ConfirmDialog from '../components/ConfirmDialog.svelte'
  import CopyButton from '../components/CopyButton.svelte'
  import EmptyState from '../components/EmptyState.svelte'
  import { api, HttpError, type Invite, type Member } from '../lib/api'
  import { date, ago } from '../lib/format'
  import { t } from '../lib/i18n'
  import { toast, fail } from '../lib/toast.svelte'

  let users = $state<Member[] | null>(null)
  let invites = $state<Invite[]>([])
  let vols = $state<string[]>([])
  let error = $state('')
  let inviting = $state(false)
  let removing = $state<Member | null>(null)
  let resetting = $state<Member | null>(null)
  let shown = $state<{ title: string; token: string; expires: number } | null>(null)

  let role = $state<'admin' | 'member'>('member')
  let grants = $state<Record<string, string>>({})
  let hours = $state(168)
  let creating = $state(false)
  let createError = $state('')

  async function load() {
    try {
      const r = await api.team()
      users = r.users
      invites = r.invites
      vols = r.vols
      error = ''
    } catch (e) {
      error = (e as Error).message
    }
  }
  $effect(() => void load())

  const link = (token: string) => `${location.origin}/invite/${token}`

  function failure(e: unknown) {
    if (e instanceof HttpError && e.status === 409) toast(t.lastAdmin, { kind: 'error' })
    else fail(e)
  }

  async function change(m: Member, c: { role?: string; access?: Record<string, string> }) {
    try {
      await api.editMember(m.id, c)
      toast(t.memberSaved)
    } catch (e) {
      failure(e)
    }
    await load()
  }

  function startInvite() {
    role = 'member'
    grants = {}
    hours = 168
    createError = ''
    inviting = true
  }

  async function create() {
    creating = true
    createError = ''
    try {
      const access = role === 'admin' ? {} : Object.fromEntries(Object.entries(grants).filter(([, l]) => l))
      const r = await api.newInvite(role, access, hours)
      inviting = false
      shown = { title: t.inviteLink, token: r.token, expires: r.expires }
      await load()
    } catch (e) {
      createError = (e as Error).message
    }
    creating = false
  }

  async function reset(m: Member) {
    const r = await api.resetMember(m.id)
    shown = { title: t.inviteReset(m.name), token: r.token, expires: r.expires }
    await load()
  }

  const roleName = (r: string) => (r === 'admin' ? t.roleAdmin : t.roleMember)
</script>

<div class="team">
  <div class="team-head">
    <p class="hint">{t.teamHint}</p>
    <button class="primary" onclick={startInvite}><UserPlus size={icon.sm} />{t.inviteMember}</button>
  </div>

  {#if error}
    <EmptyState icon={CloudOff} compact title={t.loadFailedTitle} hint={error}>
      <button onclick={load}>{t.retry}</button>
    </EmptyState>
  {:else if users}
    <section aria-labelledby="members-title">
      <h2 id="members-title">{t.members}</h2>
      <ul class="members">
        {#each users as m (m.id)}
          <li>
            <UserRound size={icon.md} class="row-icon" />
            <div class="member-main">
              <span class="row-title">{m.name}{#if m.self}<span class="tag">{t.you}</span>{/if}</span>
              <span class="hint" title={date(m.created * 1000)}>{ago(m.created * 1000)}</span>
            </div>
            <label class="member-role">
              <span class="sr-only">{t.inviteRole}</span>
              <select value={m.role} onchange={(e) => change(m, { role: e.currentTarget.value })}>
                <option value="admin">{t.roleAdmin}</option>
                <option value="member">{t.roleMember}</option>
              </select>
            </label>
            <div class="member-access">
              {#if m.role === 'admin'}
                <span class="hint">{t.allVolumes}</span>
              {:else if !vols.length}
                <span class="hint">{t.noVolumes2}</span>
              {:else}
                {#each vols as v (v)}
                  <label class="member-vol">
                    <span>{v}</span>
                    <select value={m.access[v] ?? ''} aria-label={`${m.name}: ${v}`} onchange={(e) => change(m, { access: { [v]: e.currentTarget.value } })}>
                      {#each Object.entries(t.accessLevels) as [k, label] (k)}<option value={k}>{label}</option>{/each}
                    </select>
                  </label>
                {/each}
              {/if}
            </div>
            <div class="member-actions">
              <button onclick={() => (resetting = m)}><KeyRound size={icon.sm} />{t.resetPassword}</button>
              {#if !m.self}<button class="quiet" onclick={() => (removing = m)}><Trash size={icon.sm} />{t.removeMember}</button>{/if}
            </div>
          </li>
        {/each}
      </ul>
    </section>

    {#if invites.length}
      <section aria-labelledby="invites-title">
        <h2 id="invites-title">{t.pendingInvites}</h2>
        <ul class="members invites">
          {#each invites as i (i.id)}
            <li>
              <Ticket size={icon.md} class="row-icon" />
              <div class="member-main">
                <span class="row-title">{i.kind === 'reset' ? t.inviteReset(i.user) : t.inviteJoin(roleName(i.role))}</span>
                <span class="hint" title={date(i.expires * 1000)}>{t.inviteExpires(date(i.expires * 1000))}</span>
              </div>
              {#if i.kind === 'join' && i.role !== 'admin'}
                <div class="member-access"><span class="hint">{Object.entries(i.access).map(([v, l]) => `${v}: ${t.accessLevels[l]}`).join(', ') || t.accessLevels['']}</span></div>
              {/if}
              <div class="member-actions">
                <button class="quiet" onclick={async () => (await api.delInvite(i.id).then(() => toast(t.inviteRevoked), fail), load())}>{t.revokeInvite}</button>
              </div>
            </li>
          {/each}
        </ul>
      </section>
    {/if}
  {/if}
</div>

{#if inviting}
  <Modal title={t.inviteMember} onclose={() => (inviting = false)} onsubmit={create}>
    <label class="field">
      <span>{t.inviteRole}</span>
      <select bind:value={role}>
        <option value="member">{t.roleMember}</option>
        <option value="admin">{t.roleAdmin}</option>
      </select>
    </label>
    {#if role === 'member'}
      <fieldset class="invite-access">
        <legend>{t.inviteAccess}</legend>
        {#each vols as v (v)}
          <label class="member-vol">
            <span>{v}</span>
            <select value={grants[v] ?? ''} onchange={(e) => (grants = { ...grants, [v]: e.currentTarget.value })}>
              {#each Object.entries(t.accessLevels) as [k, label] (k)}<option value={k}>{label}</option>{/each}
            </select>
          </label>
        {:else}
          <span class="hint">{t.noVolumes2}</span>
        {/each}
      </fieldset>
    {/if}
    <label class="field">
      <span>{t.inviteExpiry}</span>
      <select bind:value={hours}>
        {#each t.inviteHours as [h, label] (h)}<option value={h}>{label}</option>{/each}
      </select>
    </label>
    {#if createError}<p class="error" role="alert">{createError}</p>{/if}
    {#snippet footer()}
      <button type="button" onclick={() => (inviting = false)}>{t.cancel}</button>
      <button class="primary" disabled={creating}>{t.createInvite}</button>
    {/snippet}
  </Modal>
{/if}

{#if shown}
  {@const s = shown}
  <Modal title={s.title} onclose={() => (shown = null)} onsubmit={() => (shown = null)}>
    <p>{t.inviteOnce}</p>
    <div class="code-line"><code>{link(s.token)}</code><CopyButton text={link(s.token)} label={t.copy} done={t.copied} /></div>
    <p class="hint">{t.inviteExpires(date(s.expires * 1000))}</p>
    {#snippet footer()}<button class="primary">{t.close}</button>{/snippet}
  </Modal>
{/if}

{#if resetting}
  {@const m = resetting}
  <ConfirmDialog title={t.resetTitle(m.name)} message={t.resetMessage} action={t.resetPassword} onconfirm={() => reset(m)} onclose={() => (resetting = null)} />
{/if}

{#if removing}
  {@const m = removing}
  <ConfirmDialog
    title={t.removeMemberTitle(m.name)}
    message={t.removeMemberMessage}
    action={t.removeMember}
    onconfirm={async () => {
      try {
        await api.removeMember(m.id)
      } catch (e) {
        if (e instanceof HttpError && e.status === 409) throw new Error(t.lastAdmin)
        throw e
      }
      toast(t.memberRemoved(m.name))
      await load()
    }}
    onclose={() => (removing = null)}
  />
{/if}
