<script lang="ts">
  import { BrowserOpenURL } from '../wailsjs/runtime/runtime.js'

  let { mode, onclose }: { mode: 'help' | 'credits' | null; onclose: () => void } = $props()

  const REPO_URL = 'https://github.com/GRbit/whisper-go-ui'

  function openRepo() {
    // External links must go through the OS browser, not the webview.
    BrowserOpenURL(REPO_URL)
  }
</script>

<svelte:window onkeydown={(e) => e.key === 'Escape' && mode && onclose()} />

{#if mode}
  <div class="overlay">
    <div
      class="backdrop"
      role="button"
      tabindex="0"
      aria-label="Close"
      onclick={onclose}
      onkeydown={(e) => (e.key === 'Enter' || e.key === ' ') && onclose()}
    ></div>

    <div class="modal" role="dialog" aria-modal="true">
      <header>
        <h2>{mode === 'help' ? 'How to run' : 'Credits'}</h2>
        <button class="x" onclick={onclose} aria-label="Close">✕</button>
      </header>

      {#if mode === 'help'}
        <section>
          <h3>In the app</h3>
          <p>
            Press the global hotkey (or the Record button) to start recording;
            press it again to stop. The transcript is copied to the clipboard
            and/or pasted into the focused window, depending on the Paste
            behaviour settings. The app lives in the system tray: left click
            hides or shows the window, right click opens a menu. Closing the
            window only hides it; quit via the Help or tray menu, or Ctrl+Q.
          </p>
        </section>

        <section>
          <h3>Command line</h3>
          <table>
            <tbody>
              <tr>
                <td><code>whisper-go-ui</code></td>
                <td>
                  Start the app. If it is already running, the existing window
                  is brought to the front instead of starting a second copy.
                </td>
              </tr>
              <tr>
                <td><code>whisper-go-ui --toggle-recording</code></td>
                <td>
                  Toggle recording exactly like the hotkey: first call starts,
                  the next one stops and copies/pastes the transcript. Starts
                  the app recording if it is not running yet.
                </td>
              </tr>
              <tr>
                <td><code>whisper-go-ui --help</code></td>
                <td>Print this help in the terminal.</td>
              </tr>
            </tbody>
          </table>
        </section>

        <section>
          <h3>Hotkey via your desktop environment</h3>
          <p>
            Instead of the app's built-in hotkey you can tick "Off" next to
            the hotkey in Settings and, in your desktop environment's keyboard
            shortcut settings, bind a shortcut to:
          </p>
          <p><code>whisper-go-ui --toggle-recording</code></p>
        </section>
      {:else}
        <section>
          <p>
            <strong>Whisper Transcriber</strong>: voice transcription with
            hotkey paste, built with Wails and Svelte.
          </p>
          <p>
            Source code:
            <button class="link" onclick={openRepo}>{REPO_URL}</button>
          </p>
          <p class="dim">Contributions are welcome!</p>
        </section>
      {/if}

      <footer>
        <button class="primary" onclick={onclose}>Close</button>
      </footer>
    </div>
  </div>
{/if}

<style>
  .overlay {
    position: fixed;
    inset: 0;
    display: flex;
    align-items: center;
    justify-content: center;
    z-index: 10;
  }

  .backdrop {
    position: absolute;
    inset: 0;
    background: rgba(0, 0, 0, 0.5);
    cursor: default;
    z-index: 1;
  }

  .modal {
    position: relative;
    z-index: 2;
    width: 580px;
    max-width: 92vw;
    max-height: 88vh;
    overflow: auto;
    background: var(--bg-panel);
    color: var(--text);
    border: 1px solid var(--border);
    border-radius: 10px;
    padding: 16px 20px;
  }

  header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    margin-bottom: 8px;
  }

  h2 {
    margin: 0;
    font-size: 17px;
  }

  h3 {
    margin: 16px 0 6px;
    font-size: 12px;
    color: var(--text-dim);
    text-transform: uppercase;
    letter-spacing: 0.04em;
  }

  .x {
    background: transparent;
    border: none;
    font-size: 16px;
    color: var(--text-dim);
    cursor: pointer;
  }

  p {
    margin: 6px 0;
    font-size: 13px;
    line-height: 1.5;
  }

  table {
    border-collapse: collapse;
    font-size: 13px;
  }

  td {
    padding: 4px 12px 4px 0;
    vertical-align: top;
    line-height: 1.5;
  }

  code {
    background: var(--bg-input);
    border: 1px solid var(--border);
    border-radius: 4px;
    padding: 1px 5px;
    font-size: 12px;
    white-space: nowrap;
  }

  .link {
    background: transparent;
    border: none;
    padding: 0;
    color: var(--accent);
    text-decoration: underline;
    cursor: pointer;
    font-size: 13px;
  }

  .dim {
    color: var(--text-dim);
    font-size: 12px;
  }

  footer {
    display: flex;
    justify-content: flex-end;
    margin-top: 16px;
  }

  .primary {
    background: var(--accent);
    color: white;
    border: none;
    border-radius: 6px;
    padding: 7px 20px;
    cursor: pointer;
  }
</style>
