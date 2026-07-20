<script lang="ts">
  import { onMount } from 'svelte'
  import {
    GetConfig,
    SaveConfig,
    ValidateHotkey,
    ListInputDevices,
  } from '../wailsjs/go/main/App.js'
  import { main } from '../wailsjs/go/models'

  let cfg: main.Config | null = $state(null)
  let devices: main.AudioDevice[] = $state([])
  let hotkeyError: string = $state('')
  let saveMsg: string = $state('')
  let saveError: string = $state('')

  onMount(async () => {
    cfg = await GetConfig()
    devices = (await ListInputDevices()) ?? []
  })

  async function onHotkeyInput() {
    if (!cfg) return
    hotkeyError = await ValidateHotkey(cfg.hotkey)
  }

  async function save() {
    if (!cfg) return
    saveMsg = ''
    saveError = ''
    try {
      await SaveConfig(cfg)
      saveMsg = 'Saved ✓'
      setTimeout(() => (saveMsg = ''), 2500)
    } catch (e: any) {
      saveError = String(e)
    }
  }

  // Instant preview; persisted only on Save (reverts on restart otherwise).
  function applyTheme() {
    if (cfg) document.documentElement.dataset.theme = cfg.theme
  }

  function deviceLabel(d: main.AudioDevice): string {
    let tags = []
    if (d.isPulse) tags.push('PulseAudio/PipeWire')
    if (d.isDefault) tags.push('system default')
    const suffix = tags.length ? `  —  ${tags.join(', ')}` : ''
    return `[${d.id}] ${d.name}${suffix}`
  }
</script>

{#if cfg}
  <form onsubmit={(e) => { e.preventDefault(); save() }}>
    <section>
      <h2>ASR server</h2>
      <label>
        <span>API URL</span>
        <input type="text" bind:value={cfg.asrUrl} placeholder="http://localhost:9000" />
      </label>
      <div class="row">
        <label>
          <span>Auth header name</span>
          <input type="text" bind:value={cfg.authHeaderName} placeholder="e.g. X-Api-Key (optional)" />
        </label>
        <label>
          <span>Auth header value</span>
          <input type="password" bind:value={cfg.authHeaderValue} placeholder="secret" />
        </label>
      </div>
      <div class="row">
        <label>
          <span>Language</span>
          <input type="text" bind:value={cfg.language} placeholder="auto" />
        </label>
        <label>
          <span>Engine</span>
          <select bind:value={cfg.asrEngine}>
            <option value="faster_whisper">faster_whisper</option>
            <option value="openai_whisper">openai_whisper</option>
            <option value="whisperx">whisperx</option>
          </select>
        </label>
        <label>
          <span>Timeout (s)</span>
          <input type="number" min="1" max="600" bind:value={cfg.asrTimeout} />
        </label>
        <label>
          <span>Retries</span>
          <input type="number" min="1" max="10" bind:value={cfg.asrRetries} />
        </label>
      </div>
    </section>

    <section>
      <h2>Hotkey</h2>
      <label>
        <span>Combo (toggle: press to start, press again to stop)</span>
        <input
          type="text"
          bind:value={cfg.hotkey}
          oninput={onHotkeyInput}
          placeholder="ctrl+shift+r"
          class:invalid={hotkeyError !== ''}
        />
      </label>
      {#if hotkeyError}
        <p class="field-error">{hotkeyError}</p>
      {/if}
    </section>

    <section>
      <h2>Audio input</h2>
      <label>
        <span>Device</span>
        <select bind:value={cfg.deviceId}>
          <option value={-1}>Auto (PulseAudio / system default)</option>
          {#each devices as d}
            <option value={d.id}>{deviceLabel(d)}</option>
          {/each}
        </select>
      </label>
    </section>

    <section>
      <h2>History</h2>
      <div class="radio-group">
        <label class="inline">
          <input type="radio" bind:group={cfg.historyMode} value="ram" />
          <span>RAM only (lost on quit)</span>
        </label>
        <label class="inline">
          <input type="radio" bind:group={cfg.historyMode} value="disk" />
          <span>Disk (~/.local/share/whisper-go-ui/history.jsonl)</span>
        </label>
      </div>
    </section>

    <section>
      <h2>Appearance</h2>
      <div class="radio-group">
        <label class="inline">
          <input type="radio" bind:group={cfg.theme} value="dark" onchange={applyTheme} />
          <span>Dark</span>
        </label>
        <label class="inline">
          <input type="radio" bind:group={cfg.theme} value="light" onchange={applyTheme} />
          <span>Light (Solarized)</span>
        </label>
      </div>
    </section>

    <section>
      <label class="inline">
        <input type="checkbox" bind:checked={cfg.debug} />
        <span>Debug logging</span>
      </label>
    </section>

    <div class="actions">
      <button type="submit" class="save">Save</button>
      {#if saveMsg}<span class="ok">{saveMsg}</span>{/if}
      {#if saveError}<span class="field-error">{saveError}</span>{/if}
    </div>
  </form>
{:else}
  <p>Loading…</p>
{/if}

<style>
  form {
    max-width: 640px;
    display: flex;
    flex-direction: column;
    gap: 22px;
    text-align: left;
  }

  section {
    display: flex;
    flex-direction: column;
    gap: 10px;
  }

  h2 {
    font-size: 13px;
    text-transform: uppercase;
    letter-spacing: 0.08em;
    color: var(--text-dim);
    border-bottom: 1px solid var(--border);
    padding-bottom: 6px;
  }

  label {
    display: flex;
    flex-direction: column;
    gap: 4px;
    flex: 1;
  }

  label span {
    font-size: 12px;
    color: var(--text-dim);
  }

  label.inline {
    flex-direction: row;
    align-items: center;
    gap: 8px;
  }

  label.inline span {
    font-size: 14px;
    color: var(--text);
  }

  .row {
    display: flex;
    gap: 12px;
  }

  .radio-group {
    display: flex;
    flex-direction: column;
    gap: 8px;
  }

  input.invalid {
    border-color: var(--red);
  }

  .field-error {
    color: var(--error-fg);
    font-size: 12px;
    white-space: pre-wrap;
  }

  .ok {
    color: var(--green);
  }

  .actions {
    display: flex;
    align-items: center;
    gap: 12px;
  }

  .save {
    background: var(--accent);
    color: white;
    border: none;
    border-radius: 6px;
    padding: 9px 28px;
    font-size: 14px;
  }
</style>
