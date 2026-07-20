<script lang="ts">
  import { onMount } from 'svelte'
  import Settings from './Settings.svelte'
  import History from './History.svelte'
  import { GetState, ToggleRecording } from '../wailsjs/go/main/App.js'
  import { EventsOn } from '../wailsjs/runtime/runtime.js'

  let tab: 'settings' | 'history' = $state('settings')
  let appState: string = $state('waiting')
  let lastError: string = $state('')

  const stateColors: Record<string, string> = {
    waiting: 'var(--gray)',
    recording: 'var(--red)',
    transcribing: 'var(--amber)',
    pasted: 'var(--green)',
  }

  onMount(() => {
    GetState().then((s) => (appState = s))
    EventsOn('state:changed', (s: string) => {
      appState = s
      if (s === 'recording') lastError = ''
    })
    EventsOn('pipeline:error', (msg: string) => (lastError = msg))
  })
</script>

<header>
  <nav>
    <button class:active={tab === 'settings'} onclick={() => (tab = 'settings')}>Settings</button>
    <button class:active={tab === 'history'} onclick={() => (tab = 'history')}>History</button>
  </nav>
  <div class="status">
    <span class="dot" style="background: {stateColors[appState] ?? 'var(--gray)'}"></span>
    <span class="state-label">{appState}</span>
    <button class="record-btn" onclick={() => ToggleRecording()}>
      {appState === 'recording' ? 'Stop' : 'Record'}
    </button>
  </div>
</header>

{#if lastError}
  <div class="error-bar" title={lastError}>
    {lastError}
    <button class="dismiss" onclick={() => (lastError = '')}>✕</button>
  </div>
{/if}

<main>
  {#if tab === 'settings'}
    <Settings />
  {:else}
    <History />
  {/if}
</main>

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

  .error-bar {
    display: flex;
    justify-content: space-between;
    align-items: center;
    gap: 12px;
    background: #4a1f1d;
    color: #ffb4ae;
    padding: 8px 16px;
    font-size: 13px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .error-bar .dismiss {
    background: transparent;
    border: none;
    color: #ffb4ae;
    flex-shrink: 0;
  }

  main {
    flex: 1;
    overflow-y: auto;
    padding: 20px;
  }
</style>
