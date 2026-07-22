<script lang="ts">
  import { onMount, tick } from 'svelte'
  import Settings from './Settings.svelte'
  import History from './History.svelte'
  import InfoModal from './InfoModal.svelte'
  import { AbortRecording, GetConfig, GetState, ToggleRecording } from '../wailsjs/go/main/App.js'
  import { EventsOn, EventsEmit, Quit } from '../wailsjs/runtime/runtime.js'

  let tab: 'settings' | 'history' = $state('settings')
  let appState: string = $state('waiting')
  let lastError: string = $state('')
  let infoModal: 'help' | 'credits' | null = $state(null)

  // Both tabs render inside the same scrollable <main>, so its scrollTop
  // survives tab switches: a scrolled-down Settings leaves History's short
  // content stranded above the viewport with no scrollbar to recover.
  // Remember the position per tab and restore it after the switch renders.
  let mainEl: HTMLElement | undefined = $state()
  const scrollPos: Record<string, number> = { settings: 0, history: 0 }

  async function switchTab(t: 'settings' | 'history') {
    if (t === tab) return
    if (mainEl) scrollPos[tab] = mainEl.scrollTop
    tab = t
    await tick()
    if (mainEl) mainEl.scrollTop = scrollPos[t]
  }

  const stateColors: Record<string, string> = {
    waiting: 'var(--gray)',
    recording: 'var(--red)',
    transcribing: 'var(--amber)',
    pasted: 'var(--green)',
  }

  onMount(() => {
    GetConfig().then((c) => (document.documentElement.dataset.theme = c.theme))
    GetState().then((s) => (appState = s))
    EventsOn('state:changed', (s: string) => {
      appState = s
      if (s === 'recording') lastError = ''
    })
    EventsOn('pipeline:error', (msg: string) => (lastError = msg))
    // Window-menu items (Help > How to run / Credits) open modals via events.
    EventsOn('menu:help', () => (infoModal = 'help'))
    EventsOn('menu:credits', () => (infoModal = 'credits'))
    // Keep the backend's window-visibility flag (used by the tray left-click
    // toggle) in sync: fires when the window is hidden via the close button,
    // minimize, or WindowHide.
    document.addEventListener('visibilitychange', () =>
      EventsEmit('window:visibility', document.visibilityState === 'visible'),
    )
    // Ctrl+Q in the window (not global) quits the app for real, unlike the
    // close button, which only hides to tray.
    window.addEventListener('keydown', (e) => {
      if (e.ctrlKey && !e.shiftKey && !e.altKey && !e.metaKey && e.key.toLowerCase() === 'q') {
        e.preventDefault()
        Quit()
      }
    })
  })
</script>

<header>
  <nav>
    <button class:active={tab === 'settings'} onclick={() => switchTab('settings')}>Settings</button>
    <button class:active={tab === 'history'} onclick={() => switchTab('history')}>History</button>
  </nav>
  <div class="status">
    <span class="dot" style="background: {stateColors[appState] ?? 'var(--gray)'}"></span>
    <span class="state-label">{appState}</span>
    <button class="record-btn" onclick={() => ToggleRecording()}>
      {appState === 'recording' ? 'Stop' : 'Record'}
    </button>
    {#if appState === 'recording'}
      <button class="abort-btn" onclick={() => AbortRecording()} title="Discard the recording without transcribing">
        Abort
      </button>
    {/if}
  </div>
</header>

{#if lastError}
  <div class="error-bar" title={lastError}>
    {lastError}
    <button class="dismiss" onclick={() => (lastError = '')}>✕</button>
  </div>
{/if}

<!-- Both tabs stay mounted; hidden toggles visibility. An {#if} would
     destroy the Settings component on switch, discarding unsaved edits. -->
<main bind:this={mainEl}>
  <div hidden={tab !== 'settings'}>
    <Settings />
  </div>
  <div hidden={tab !== 'history'}>
    <History />
  </div>
</main>

<InfoModal mode={infoModal} onclose={() => (infoModal = null)} />

<style>
  header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 10px 16px;
    background: var(--bg-panel);
    border-bottom: 1px solid var(--border);
  }

  nav {
    display: flex;
    gap: 6px;
  }

  nav button {
    background: transparent;
    color: var(--text-dim);
    border: 1px solid transparent;
    border-radius: 6px;
    padding: 7px 16px;
    font-size: 14px;
  }

  nav button.active {
    color: var(--text);
    background: var(--bg-input);
    border-color: var(--border);
  }

  .status {
    display: flex;
    align-items: center;
    gap: 8px;
  }

  .dot {
    width: 12px;
    height: 12px;
    border-radius: 50%;
    display: inline-block;
  }

  .state-label {
    color: var(--text-dim);
    min-width: 84px;
    text-align: left;
  }

  .record-btn {
    background: var(--accent);
    color: white;
    border: none;
    border-radius: 6px;
    padding: 7px 16px;
  }

  .abort-btn {
    background: transparent;
    color: var(--text-dim);
    border: 1px solid var(--border);
    border-radius: 6px;
    padding: 6px 14px;
  }

  .abort-btn:hover {
    color: var(--red);
    border-color: var(--red);
  }

  .error-bar {
    display: flex;
    justify-content: space-between;
    align-items: center;
    gap: 12px;
    background: var(--error-bg);
    color: var(--error-fg);
    padding: 8px 16px;
    font-size: 13px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .error-bar .dismiss {
    background: transparent;
    border: none;
    color: var(--error-fg);
    flex-shrink: 0;
  }

  /* No bottom padding: the Settings save bar sticks to the scroller's
     bottom edge, and any scroller padding below it would let content
     peek through under the bar. Tabs add their own bottom spacing. */
  main {
    flex: 1;
    overflow-y: auto;
    padding: 20px 20px 0;
  }
</style>
