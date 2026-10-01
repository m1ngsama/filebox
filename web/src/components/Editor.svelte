<script lang="ts">
  import { onMount } from 'svelte'
  import { EditorState, Compartment, type Extension } from '@codemirror/state'
  import { EditorView, keymap, lineNumbers, highlightActiveLine, highlightActiveLineGutter, drawSelection, highlightSpecialChars } from '@codemirror/view'
  import { defaultKeymap, history, historyKeymap, indentWithTab } from '@codemirror/commands'
  import { bracketMatching, indentOnInput, syntaxHighlighting, StreamLanguage, type StreamParser } from '@codemirror/language'
  import { search, searchKeymap, highlightSelectionMatches } from '@codemirror/search'
  import { classHighlighter } from '@lezer/highlight'
  import Check from '@lucide/svelte/icons/check'
  import TextWrap from '@lucide/svelte/icons/text-wrap'
  import { t } from '../lib/i18n'
  import { toast, fail } from '../lib/toast.svelte'

  let {
    name,
    src,
    save,
    wrap = $bindable(false),
    ondone,
  }: { name: string; src: string; save: string; wrap?: boolean; ondone: (saved: boolean) => void } = $props()

  let host = $state<HTMLDivElement>()
  let view: EditorView | undefined
  let tag = ''
  let dirty = $state(false)
  let saving = $state(false)
  let saved = $state(false)
  let clash = $state<string | null>(null)
  let leaving = $state(false)
  let loading = $state(true)
  let wrote = false
  const wrapping = new Compartment()

  const ext = $derived(name.slice(name.lastIndexOf('.') + 1).toLowerCase())
  const legacy = (load: () => Promise<StreamParser<unknown>>) => load().then((p) => StreamLanguage.define(p))

  async function language(): Promise<Extension> {
    switch (ext) {
      case 'md':
      case 'markdown':
        return (await import('@codemirror/lang-markdown')).markdown()
      case 'js':
      case 'mjs':
      case 'cjs':
      case 'jsx':
        return (await import('@codemirror/lang-javascript')).javascript({ jsx: true })
      case 'ts':
      case 'tsx':
        return (await import('@codemirror/lang-javascript')).javascript({ jsx: ext === 'tsx', typescript: true })
      case 'json':
        return (await import('@codemirror/lang-json')).json()
      case 'py':
        return (await import('@codemirror/lang-python')).python()
      case 'go':
        return (await import('@codemirror/lang-go')).go()
      case 'yaml':
      case 'yml':
        return (await import('@codemirror/lang-yaml')).yaml()
      case 'css':
        return (await import('@codemirror/lang-css')).css()
      case 'html':
      case 'htm':
      case 'svelte':
      case 'vue':
        return (await import('@codemirror/lang-html')).html()
      case 'xml':
      case 'svg':
        return (await import('@codemirror/lang-xml')).xml()
      case 'sql':
        return (await import('@codemirror/lang-sql')).sql()
      case 'sh':
      case 'bash':
      case 'zsh':
      case 'fish':
        return legacy(async () => (await import('@codemirror/legacy-modes/mode/shell')).shell)
      case 'toml':
        return legacy(async () => (await import('@codemirror/legacy-modes/mode/toml')).toml)
      case 'ini':
      case 'conf':
      case 'cfg':
        return legacy(async () => (await import('@codemirror/legacy-modes/mode/properties')).properties)
      case 'rs':
        return legacy(async () => (await import('@codemirror/legacy-modes/mode/rust')).rust)
      case 'c':
      case 'h':
        return legacy(async () => (await import('@codemirror/legacy-modes/mode/clike')).c)
      case 'cpp':
      case 'hpp':
      case 'cc':
        return legacy(async () => (await import('@codemirror/legacy-modes/mode/clike')).cpp)
      case 'java':
        return legacy(async () => (await import('@codemirror/legacy-modes/mode/clike')).java)
      case 'kt':
        return legacy(async () => (await import('@codemirror/legacy-modes/mode/clike')).kotlin)
      case 'lua':
        return legacy(async () => (await import('@codemirror/legacy-modes/mode/lua')).lua)
      case 'rb':
        return legacy(async () => (await import('@codemirror/legacy-modes/mode/ruby')).ruby)
    }
    return []
  }

  async function write(force = false) {
    if (!view || saving) return
    saving = true
    try {
      const r = await fetch(save, { method: 'PUT', headers: { 'If-Match': force && clash ? clash : tag }, body: view.state.doc.toString() })
      if (r.status === 412) {
        clash = r.headers.get('ETag')
        return
      }
      if (!r.ok) throw new Error(r.status === 413 ? t.tooLargeToEdit : String(r.status))
      tag = r.headers.get('ETag') ?? ''
      clash = null
      dirty = false
      wrote = saved = true
      setTimeout(() => (saved = false), 1500)
    } catch (e) {
      fail(e)
    } finally {
      saving = false
    }
  }

  async function reload() {
    const r = await fetch(src, { cache: 'no-store' })
    if (!r.ok || !view) return
    tag = r.headers.get('ETag') ?? ''
    view.dispatch({ changes: { from: 0, to: view.state.doc.length, insert: await r.text() } })
    clash = null
    dirty = false
  }

  function done() {
    if (dirty && !leaving) {
      leaving = true
      return
    }
    ondone(wrote)
  }

  function toggleWrap() {
    wrap = !wrap
    view?.dispatch({ effects: wrapping.reconfigure(wrap ? EditorView.lineWrapping : []) })
  }

  onMount(() => {
    let gone = false
    ;(async () => {
      const [r, lang] = await Promise.all([fetch(src, { cache: 'no-store' }), language()])
      if (!r.ok) throw new Error(String(r.status))
      tag = r.headers.get('ETag') ?? ''
      const text = await r.text()
      if (gone || !host) return
      view = new EditorView({
        parent: host,
        state: EditorState.create({
          doc: text,
          extensions: [
            lineNumbers(),
            highlightActiveLineGutter(),
            highlightSpecialChars(),
            history(),
            drawSelection(),
            indentOnInput(),
            bracketMatching(),
            highlightActiveLine(),
            highlightSelectionMatches(),
            search({ top: true }),
            syntaxHighlighting(classHighlighter),
            EditorView.cspNonce.of(document.querySelector<HTMLMetaElement>('meta[name=csp-nonce]')?.content ?? ''),
            EditorState.phrases.of(t.editorPhrases),
            wrapping.of(wrap ? EditorView.lineWrapping : []),
            keymap.of([{ key: 'Mod-s', preventDefault: true, run: () => (write(), true) }, indentWithTab, ...searchKeymap, ...historyKeymap, ...defaultKeymap]),
            EditorView.updateListener.of((u) => {
              if (u.docChanged) {
                dirty = true
                leaving = false
              }
            }),
            lang,
          ],
        }),
      })
      loading = false
      view.focus()
    })().catch((e) => {
      fail(e)
      ondone(false)
    })
    const guard = (e: BeforeUnloadEvent) => dirty && e.preventDefault()
    addEventListener('beforeunload', guard)
    return () => {
      gone = true
      removeEventListener('beforeunload', guard)
      view?.destroy()
    }
  })

  function key(e: KeyboardEvent) {
    if (e.key === 'Escape' && !e.defaultPrevented && !host?.querySelector('.cm-panels')?.contains(e.target as Node)) {
      e.preventDefault()
      e.stopPropagation()
      done()
    }
  }
</script>

<svelte:window onkeydowncapture={key} />

<div class="editor" class:loading>
  <div class="host" bind:this={host}></div>
</div>

<div class="edit-bar">
  {#if clash}
    <div class="pill warn" role="alert">
      <span class="say">{t.changedElsewhere}</span>
      <button type="button" onclick={() => write(true)}>{t.keepMine}</button>
      <button type="button" onclick={reload}>{t.loadTheirs}</button>
    </div>
  {:else if leaving}
    <div class="pill warn" role="alert">
      <span class="say">{t.unsavedChanges}</span>
      <button type="button" onclick={() => write().then(() => !dirty && ondone(true))}>{t.save}</button>
      <button type="button" onclick={() => ondone(wrote)}>{t.discard}</button>
    </div>
  {:else}
    <div class="pill">
      <span class="say" aria-live="polite">{saved ? t.saved : dirty ? t.unsaved : t.editing}</span>
      <button type="button" class="icon-btn" aria-pressed={wrap} aria-label={t.wrapLines} title={t.wrapLines} onclick={toggleWrap}><TextWrap size={18} /></button>
      <button type="button" class="ghost" onclick={done}>{t.done}</button>
      <button type="button" class="primary" disabled={!dirty || saving} onclick={() => write()}>
        {#if saved}<Check size={16} />{/if}{t.save}
      </button>
    </div>
  {/if}
</div>

<style>
  .editor {
    position: absolute;
    inset: 0;
    display: flex;
    padding: 0 var(--space-4) calc(var(--hit) + var(--space-8));
  }
  .host {
    flex: 1;
    min-width: 0;
    border-radius: var(--radius-lg);
    overflow: hidden;
    background: var(--panel);
    color: var(--fg);
    transition: opacity var(--dur-2) var(--ease-out);
  }
  .loading .host {
    opacity: 0;
  }
  .host :global(.cm-editor) {
    height: 100%;
    font-size: var(--text-sm);
  }
  .host :global(.cm-editor.cm-focused) {
    outline: none;
  }
  .host :global(.cm-scroller) {
    font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
    line-height: 1.6;
  }
  .host :global(.cm-content) {
    padding: var(--space-3) 0;
    caret-color: var(--accent);
  }
  .host :global(.cm-gutters) {
    background: var(--code-bg);
    color: var(--hl-comment);
    border-right: 1px solid var(--line);
  }
  .host :global(.cm-activeLine) {
    background: color-mix(in srgb, var(--accent) 6%, transparent);
  }
  .host :global(.cm-activeLineGutter) {
    background: color-mix(in srgb, var(--accent) 10%, transparent);
    color: var(--fg);
  }
  .host :global(.cm-cursor) {
    border-left: 2px solid var(--accent);
  }
  .host :global(.cm-selectionBackground),
  .host :global(.cm-focused .cm-selectionBackground) {
    background: color-mix(in srgb, var(--accent) 24%, transparent) !important;
  }
  .host :global(.cm-selectionMatch) {
    background: color-mix(in srgb, var(--accent) 14%, transparent);
  }
  .host :global(.cm-matchingBracket) {
    background: color-mix(in srgb, var(--accent) 18%, transparent);
    outline: 1px solid color-mix(in srgb, var(--accent) 40%, transparent);
  }
  .host :global(.cm-searchMatch) {
    background: color-mix(in srgb, #f5c400 35%, transparent);
  }
  .host :global(.cm-searchMatch-selected) {
    background: color-mix(in srgb, #f5c400 70%, transparent);
  }
  .host :global(.cm-panels) {
    background: var(--nav);
    color: var(--fg);
    border-bottom: 1px solid var(--line);
  }
  .host :global(.cm-panel.cm-search) {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--space-1) var(--space-2);
    padding: var(--space-2) var(--space-3);
    font-family: inherit;
  }
  .host :global(.cm-panel.cm-search .cm-textfield) {
    height: 32px;
    margin: 0;
    padding: 0 var(--space-2);
    border: 1px solid var(--line);
    border-radius: var(--radius-sm);
    background: var(--panel);
    color: var(--fg);
    font-size: var(--text-sm);
  }
  .host :global(.cm-panel.cm-search .cm-button) {
    height: 32px;
    margin: 0;
    padding: 0 var(--space-2-5, 10px);
    border: 1px solid var(--line);
    border-radius: var(--radius-sm);
    background: var(--panel);
    background-image: none;
    color: var(--fg);
    font-size: var(--text-sm);
  }
  .host :global(.cm-panel.cm-search label) {
    display: inline-flex;
    align-items: center;
    gap: var(--space-1);
    margin: 0;
    font-size: var(--text-sm);
  }
  .host :global(.cm-panel.cm-search br) {
    display: none;
  }
  .host :global(.cm-panel.cm-search [name='close']) {
    position: static;
    margin-left: auto;
    width: 32px;
    height: 32px;
    border-radius: var(--radius-sm);
    font-size: 18px;
    color: var(--muted);
  }
  .host :global(.tok-keyword),
  .host :global(.tok-operatorKeyword),
  .host :global(.tok-modifier) {
    color: var(--hl-keyword);
  }
  .host :global(.tok-string),
  .host :global(.tok-string2),
  .host :global(.tok-url) {
    color: var(--hl-string);
  }
  .host :global(.tok-comment),
  .host :global(.tok-meta) {
    color: var(--hl-comment);
    font-style: italic;
  }
  .host :global(.tok-number),
  .host :global(.tok-bool),
  .host :global(.tok-atom),
  .host :global(.tok-literal) {
    color: var(--hl-number);
  }
  .host :global(.tok-variableName.tok-definition),
  .host :global(.tok-propertyName.tok-definition),
  .host :global(.tok-function) {
    color: var(--hl-func);
  }
  .host :global(.tok-typeName),
  .host :global(.tok-className),
  .host :global(.tok-namespace) {
    color: var(--hl-type);
  }
  .host :global(.tok-tagName),
  .host :global(.tok-heading) {
    color: var(--hl-tag);
    font-weight: 600;
  }
  .host :global(.tok-attributeName),
  .host :global(.tok-propertyName) {
    color: var(--hl-number);
  }
  .host :global(.tok-link) {
    color: var(--accent);
    text-decoration: underline;
  }
  .host :global(.tok-emphasis) {
    font-style: italic;
  }
  .host :global(.tok-strong) {
    font-weight: 700;
  }
  .host :global(.tok-strikethrough) {
    text-decoration: line-through;
  }
  .host :global(.tok-invalid) {
    color: var(--danger);
  }
  .edit-bar {
    position: absolute;
    left: 0;
    right: 0;
    bottom: calc(env(safe-area-inset-bottom) + var(--space-3));
    display: flex;
    justify-content: center;
    padding: 0 var(--space-3);
    pointer-events: none;
  }
  .pill {
    pointer-events: auto;
    display: flex;
    align-items: center;
    gap: var(--space-1);
    max-width: 100%;
    min-height: var(--hit);
    padding: var(--space-1) var(--space-1) var(--space-1) var(--space-4);
    border-radius: var(--radius-full);
    background: color-mix(in srgb, var(--viewer-bg) 82%, transparent);
    color: var(--viewer-fg);
    backdrop-filter: blur(12px);
    -webkit-backdrop-filter: blur(12px);
    box-shadow: var(--shadow-sm);
    font-size: var(--text-sm);
    animation: appear var(--dur-2) var(--ease-out);
  }
  .pill.warn {
    background: color-mix(in srgb, #8a5a00 88%, transparent);
  }
  .say {
    padding-inline-end: var(--space-2);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
    color: var(--viewer-icon);
  }
  .warn .say {
    color: #fff;
  }
  .pill button:not(.icon-btn) {
    height: 34px;
    padding: 0 var(--space-3);
    border-radius: var(--radius-full);
    white-space: nowrap;
  }
  .pill .ghost,
  .warn button {
    border: 0;
    background: rgb(255 255 255 / 0.12);
    color: var(--viewer-fg);
  }
  .pill :global(.icon-btn) {
    color: var(--viewer-fg);
  }
  .pill :global(.icon-btn[aria-pressed='true']) {
    background: var(--viewer-hover);
  }
  .pill .primary {
    display: inline-flex;
    align-items: center;
    gap: var(--space-1);
  }
  .pill .primary:disabled {
    opacity: 0.5;
  }
</style>
