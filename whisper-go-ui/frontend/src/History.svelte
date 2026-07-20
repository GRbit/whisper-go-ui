<script lang="ts">
  import { onMount } from 'svelte'
  import { GetHistory, ClearHistory } from '../wailsjs/go/main/App.js'
  import { EventsOn } from '../wailsjs/runtime/runtime.js'
  import { main } from '../wailsjs/go/models'

  let entries: main.HistoryEntry[] = $state([])
  let copiedIdx: number = $state(-1)

  onMount(() => {
    load()
    EventsOn('history:added', () => load())
  })

  async function load() {
    entries = (await GetHistory()) ?? []
  }

  async function clearAll() {
    await ClearHistory()
    entries = []
  }

  async function copy(text: string, idx: number) {
    await navigator.clipboard.writeText(text)
    copiedIdx = idx
    setTimeout(() => (copiedIdx = -1), 1500)
  }

  function fmtTime(t: any): string {
    return new Date(t).toLocaleString()
  }
</script>

<div class="toolbar">
  <span class="count">{entries.length} transcript{entries.length === 1 ? '' : 's'}</span>
  {#if entries.length > 0}
    <button class="clear" onclick={clearAll}>Clear history</button>
  {/if}
</div>

{#if entries.length === 0}
  <p class="empty">No transcripts yet. Press the hotkey to record.</p>
{:else}
  <ul>
    {#each entries as e, i}
      <li>
        <div class="meta">
          <span>{fmtTime(e.time)}</span>
          <span>{e.durationSec.toFixed(1)}s audio</span>
          <button class="copy" onclick={() => copy(e.text, i)}>
            {copiedIdx === i ? 'Copied ✓' : 'Copy'}
          </button>
        </div>
        <p class="text">{e.text}</p>
      </li>
    {/each}
  </ul>
{/if}

<style>
  .toolbar {
    display: flex;
    justify-content: space-between;
    align-items: center;
    margin-bottom: 14px;
  }

  .count {
    color: var(--text-dim);
  }

  .clear {
    background: transparent;
    color: var(--text-dim);
    border: 1px solid var(--border);
    border-radius: 6px;
    padding: 6px 14px;
  }

  .clear:hover {
    color: #ffb4ae;
    border-color: var(--red);
  }

  .empty {
    color: var(--text-dim);
    text-align: center;
    margin-top: 60px;
  }

  ul {
    list-style: none;
    display: flex;
    flex-direction: column;
    gap: 12px;
    text-align: left;
  }

  li {
    background: var(--bg-panel);
    border: 1px solid var(--border);
    border-radius: 8px;
    padding: 12px 14px;
  }

  .meta {
    display: flex;
    gap: 16px;
    align-items: center;
    font-size: 12px;
    color: var(--text-dim);
    margin-bottom: 8px;
  }

  .meta .copy {
    margin-left: auto;
    background: var(--bg-input);
    color: var(--text);
    border: 1px solid var(--border);
    border-radius: 6px;
    padding: 4px 12px;
    font-size: 12px;
  }

  .text {
    white-space: pre-wrap;
    word-break: break-word;
    line-height: 1.5;
  }
</style>
