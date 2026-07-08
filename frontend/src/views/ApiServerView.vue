<script setup lang="ts">
import { computed, onMounted, ref } from "vue";
import {
  GetAPIStatus,
  StartAPIServer,
  StopAPIServer,
  ShuffleAPIPort,
  GetSettings,
  SetAPIAutoStart,
  SetHTTPS,
  AddAllowlistEntry,
  RemoveAllowlistEntry,
  RotateAPIKey,
} from "../../wailsjs/go/main/App";
import { go, busyInc, busyDec } from "../store";

const running = ref(false);
const url = ref("");
const port = ref(0);
const autoStart = ref(false);
const useHttps = ref(false);
const tls = ref(false);
const fingerprint = ref("");
const allowlist = ref<string[]>([]);
const apiKey = ref("");

const newEntry = ref("");
const entryError = ref("");
const serverError = ref("");
const busy = ref(false);
const copied = ref("");

const maskedKey = computed(() =>
  apiKey.value
    ? `${apiKey.value.slice(0, 6)}${"•".repeat(12)}${apiKey.value.slice(-4)}`
    : "",
);

// The public-key pin isn't secret, but it's long — show it shortened.
const shortFingerprint = computed(() =>
  fingerprint.value
    ? `${fingerprint.value.slice(0, 14)}…${fingerprint.value.slice(-6)}`
    : "",
);

// A short, copy-paste snippet to hand an agent so it can get started. When TLS
// is on, it carries the pin so the client can verify the self-signed cert.
const instructions = computed(() => {
  const lines = [
    `Base URL: ${url.value}`,
    `Header:   X-API-Key: ${apiKey.value}`,
  ];
  if (tls.value && fingerprint.value) {
    lines.push(`TLS pin:  sha256//${fingerprint.value}  (self-signed — pin this)`);
  }
  lines.push(
    `GET  /v1/ax    -> how it works + where to click (start here)`,
    `POST /v1/stats -> {"text":"hello world"} (word/line/char counts)`,
    `POST /v1/ui/input -> {"testid":"editor","value":"..."} (type into the doc)`,
    `POST /v1/ui/press -> {"testid":"new-tab"} (operate the real UI)`,
  );
  return lines.join("\n");
});

// A ready-to-run curl, with the pin baked in when serving HTTPS.
const curlExample = computed(() =>
  tls.value && fingerprint.value
    ? `curl --pinnedpubkey "sha256//${fingerprint.value}" \\\n  -H "X-API-Key: ${apiKey.value}" \\\n  ${url.value}/v1/ax`
    : `curl -H "X-API-Key: ${apiKey.value}" ${url.value}/v1/ax`,
);

function applyStatus(st: { running: boolean; url: string; port: number; tls: boolean; fingerprint: string }) {
  running.value = st.running;
  url.value = st.url;
  port.value = st.port;
  tls.value = st.tls;
  fingerprint.value = st.fingerprint;
}

// refreshStatus re-reads the live server status; never throws (used in finally
// blocks, where an exception would skip the busy-counter cleanup).
async function refreshStatus() {
  try {
    applyStatus(await GetAPIStatus());
  } catch {
    /* keep the last known status */
  }
}

async function refresh() {
  applyStatus(await GetAPIStatus());
  const s = await GetSettings();
  autoStart.value = s.apiAutoStart;
  useHttps.value = s.apiHttps;
  allowlist.value = s.apiAllowlist ?? [];
  apiKey.value = s.apiKey;
}

onMounted(refresh);

async function toggleServer() {
  busy.value = true;
  serverError.value = "";
  busyInc();
  try {
    applyStatus(running.value ? await StopAPIServer() : await StartAPIServer());
  } catch (e) {
    serverError.value = typeof e === "string" ? e : "failed to toggle the server";
  } finally {
    busy.value = false;
    busyDec();
  }
}

async function shufflePort() {
  busy.value = true;
  serverError.value = "";
  busyInc();
  try {
    applyStatus(await ShuffleAPIPort());
  } catch (e) {
    serverError.value = typeof e === "string" ? e : "failed to change the port";
    await refreshStatus();
  } finally {
    busy.value = false;
    busyDec();
  }
}

async function onAutoStart(e: Event) {
  const v = (e.target as HTMLInputElement).checked;
  autoStart.value = v;
  busyInc();
  try {
    await SetAPIAutoStart(v);
  } finally {
    busyDec();
  }
}

async function onHttps(e: Event) {
  const v = (e.target as HTMLInputElement).checked;
  useHttps.value = v;
  serverError.value = "";
  busyInc();
  try {
    // returns the fresh status (scheme + fingerprint) since the server restarts
    applyStatus(await SetHTTPS(v));
  } catch (err) {
    serverError.value = typeof err === "string" ? err : "failed to switch the transport";
    await refreshStatus();
  } finally {
    busyDec();
  }
}

// Allowlist/key changes restart a running server; if that restart fails the
// server ends up stopped, so each handler re-applies the fresh status and
// surfaces the error instead of leaving a stale "Running" pill on screen.

async function addEntry() {
  entryError.value = "";
  const val = newEntry.value.trim();
  if (!val) return;
  busyInc();
  try {
    allowlist.value = await AddAllowlistEntry(val);
    newEntry.value = "";
  } catch (e) {
    entryError.value = typeof e === "string" ? e : "invalid IP/mask";
  } finally {
    await refreshStatus();
    busyDec();
  }
}

async function removeEntry(entry: string) {
  entryError.value = "";
  busyInc();
  try {
    allowlist.value = await RemoveAllowlistEntry(entry);
  } catch (e) {
    // e.g. "cannot remove the last allowlist entry"
    entryError.value = typeof e === "string" ? e : "failed to remove the entry";
  } finally {
    await refreshStatus();
    busyDec();
  }
}

async function rotateKey() {
  serverError.value = "";
  busyInc();
  try {
    apiKey.value = await RotateAPIKey();
  } catch (e) {
    serverError.value = typeof e === "string" ? e : "failed to rotate the key";
  } finally {
    await refreshStatus();
    busyDec();
  }
}

async function copy(text: string, field: string) {
  try {
    await navigator.clipboard.writeText(text);
    copied.value = field;
    window.setTimeout(() => {
      if (copied.value === field) copied.value = "";
    }, 1500);
  } catch {
    /* clipboard may be unavailable; ignore */
  }
}
</script>

<template>
  <div class="view view--panel">
    <div class="subheader">
      <button
        class="icon-btn"
        title="Back"
        data-testid="back"
        @click="go('options')"
      >
        <svg viewBox="0 0 24 24" fill="currentColor" aria-hidden="true">
          <path d="M20 11H7.83l5.59-5.59L12 4l-8 8 8 8 1.41-1.41L7.83 13H20v-2Z" />
        </svg>
      </button>
      <h1 class="subheader-title">REST API Server</h1>
    </div>

    <div class="panel-body">
      <!-- Start / stop -->
      <div class="api-status">
        <button
          class="play-btn"
          :class="{ running }"
          :disabled="busy"
          data-testid="toggle-server"
          @click="toggleServer"
        >
          <svg v-if="running" viewBox="0 0 24 24" fill="currentColor">
            <path d="M6 6h12v12H6z" />
          </svg>
          <svg v-else viewBox="0 0 24 24" fill="currentColor">
            <path d="M8 5v14l11-7L8 5Z" />
          </svg>
          <span>{{ running ? "Stop" : "Start" }}</span>
        </button>
        <span class="status-pill" :class="{ on: running }" data-testid="status">
          {{ running ? "Running" : "Stopped" }}
        </span>
      </div>
      <p v-if="serverError" class="err" data-testid="server-error">
        {{ serverError }}
      </p>

      <!-- Port -->
      <div class="row">
        <span class="row-text">
          <span class="row-label">Port <span class="mono">{{ port }}</span></span>
          <span class="row-desc">Pick a free port (8800–8899)</span>
        </span>
        <button
          class="btn"
          :disabled="busy"
          data-testid="shuffle-port"
          @click="shufflePort"
        >
          Shuffle
        </button>
      </div>

      <!-- Auto-start -->
      <label class="row switch-row">
        <span class="row-text">
          <span class="row-label">Start automatically</span>
          <span class="row-desc">Launch the server when the app starts</span>
        </span>
        <input
          type="checkbox"
          class="switch"
          role="switch"
          :checked="autoStart"
          data-testid="autostart"
          @change="onAutoStart"
        />
      </label>

      <!-- Transport: HTTP or HTTPS -->
      <label class="row switch-row">
        <span class="row-text">
          <span class="row-label">Use HTTPS</span>
          <span class="row-desc">Encrypt the connection (off = plain HTTP)</span>
        </span>
        <input
          type="checkbox"
          class="switch"
          role="switch"
          :checked="useHttps"
          data-testid="use-https"
          @change="onHttps"
        />
      </label>

      <!-- Allowlist -->
      <section class="field">
        <label class="field-label">Allowed IPs (with mask)</label>
        <table class="ip-table" data-testid="allowlist">
          <tbody>
            <tr v-for="cidr in allowlist" :key="cidr">
              <td class="mono">{{ cidr }}</td>
              <td class="ip-actions">
                <button
                  class="icon-btn small danger"
                  title="Remove"
                  :data-testid="`remove-${cidr}`"
                  @click="removeEntry(cidr)"
                >
                  <svg viewBox="0 0 24 24" fill="currentColor" aria-hidden="true">
                    <path d="M19 6.41 17.59 5 12 10.59 6.41 5 5 6.41 10.59 12 5 17.59 6.41 19 12 13.41 17.59 19 19 17.59 13.41 12 19 6.41Z" />
                  </svg>
                </button>
              </td>
            </tr>
            <tr v-if="allowlist.length === 0">
              <td class="muted" colspan="2">No allowed IPs</td>
            </tr>
          </tbody>
        </table>
        <div class="add-row">
          <input
            v-model="newEntry"
            class="text-input mono"
            placeholder="e.g. 192.168.0.0/24"
            data-testid="new-ip"
            @keyup.enter="addEntry"
          />
          <button
            class="btn"
            data-testid="add-ip"
            :disabled="!newEntry.trim()"
            @click="addEntry"
          >
            Add
          </button>
        </div>
        <p v-if="entryError" class="err" data-testid="ip-error">{{ entryError }}</p>
      </section>

      <!-- Agent instructions (URL + key + starting endpoints) -->
      <section class="field">
        <div class="field-head">
          <label class="field-label">Agent instructions</label>
          <button
            class="icon-btn small"
            title="Copy"
            data-testid="copy-instructions"
            @click="copy(instructions, 'instructions')"
          >
            <svg viewBox="0 0 24 24" fill="currentColor" aria-hidden="true">
              <path d="M16 1H4a2 2 0 0 0-2 2v12h2V3h12V1Zm3 4H8a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h11a2 2 0 0 0 2-2V7a2 2 0 0 0-2-2Zm0 16H8V7h11v14Z" />
            </svg>
          </button>
        </div>
        <pre
          class="instructions-box mono"
          data-testid="agent-instructions"
        >{{ instructions }}</pre>
        <span v-if="copied === 'instructions'" class="copied">Copied!</span>
      </section>

      <!-- Key -->
      <section class="field">
        <label class="field-label">Access key</label>
        <div class="copy-field">
          <span class="value-box mono" data-testid="api-key">{{ maskedKey }}</span>
          <button
            class="icon-btn"
            title="Copy key"
            data-testid="copy-key"
            @click="copy(apiKey, 'key')"
          >
            <svg viewBox="0 0 24 24" fill="currentColor" aria-hidden="true">
              <path d="M16 1H4a2 2 0 0 0-2 2v12h2V3h12V1Zm3 4H8a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h11a2 2 0 0 0 2-2V7a2 2 0 0 0-2-2Zm0 16H8V7h11v14Z" />
            </svg>
          </button>
          <button
            class="icon-btn"
            title="Generate new key"
            data-testid="rotate-key"
            @click="rotateKey"
          >
            <svg viewBox="0 0 24 24" fill="currentColor" aria-hidden="true">
              <path d="M17.65 6.35A7.958 7.958 0 0 0 12 4a8 8 0 1 0 7.73 10h-2.08A6 6 0 1 1 12 6c1.66 0 3.14.69 4.22 1.78L13 11h7V4l-2.35 2.35Z" />
            </svg>
          </button>
        </div>
        <span v-if="copied === 'key'" class="copied">Copied!</span>
      </section>

      <!-- Certificate pin (only when serving HTTPS) -->
      <section v-if="tls && fingerprint" class="field" data-testid="tls-section">
        <label class="field-label">Certificate pin (HTTPS)</label>
        <div class="copy-field">
          <span class="value-box mono" data-testid="fingerprint">{{ shortFingerprint }}</span>
          <button
            class="icon-btn"
            title="Copy pin"
            data-testid="copy-fingerprint"
            @click="copy('sha256//' + fingerprint, 'fp')"
          >
            <svg viewBox="0 0 24 24" fill="currentColor" aria-hidden="true">
              <path d="M16 1H4a2 2 0 0 0-2 2v12h2V3h12V1Zm3 4H8a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h11a2 2 0 0 0 2-2V7a2 2 0 0 0-2-2Zm0 16H8V7h11v14Z" />
            </svg>
          </button>
        </div>
        <span v-if="copied === 'fp'" class="copied">Copied!</span>
        <p class="row-desc pin-hint">
          Self-signed — the client pins this key (no CA needed).
        </p>
        <div class="field-head">
          <label class="field-label">Test command</label>
          <button
            class="icon-btn small"
            title="Copy"
            data-testid="copy-curl"
            @click="copy(curlExample, 'curl')"
          >
            <svg viewBox="0 0 24 24" fill="currentColor" aria-hidden="true">
              <path d="M16 1H4a2 2 0 0 0-2 2v12h2V3h12V1Zm3 4H8a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h11a2 2 0 0 0 2-2V7a2 2 0 0 0-2-2Zm0 16H8V7h11v14Z" />
            </svg>
          </button>
        </div>
        <pre class="instructions-box mono" data-testid="curl-example">{{ curlExample }}</pre>
        <span v-if="copied === 'curl'" class="copied">Copied!</span>
      </section>
    </div>
  </div>
</template>
