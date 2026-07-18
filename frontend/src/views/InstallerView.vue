<script setup lang="ts">
import { onMounted, reactive } from "vue";
import { Quit, BrowserOpenURL } from "../../wailsjs/runtime/runtime";
import {
  InstallerState,
  InstallerChooseDir,
  InstallerInstall,
  InstallerFinish,
  InstallerUninstall,
} from "../../wailsjs/go/main/App";
import { ui, setTheme } from "../store";

// The wizard: welcome → license → destination → installing → done. The
// Reinstall/Uninstall maintenance screen (setup run with the app already
// installed) and the uninstall confirmation are their own single screens
// (state.mode). Mechanics live in Go (install_windows.go + the
// go-installer library); this view only walks the steps.
type Step = "welcome" | "license" | "destination" | "installing" | "done";

const state = reactive({
  mode: "",
  dir: "",
  version: "",
  installedVersion: "",
  url: "",
  license: "",
  step: "welcome" as Step,
  error: "",
  busy: false,
  // Uninstall reached from the maintenance screen: Cancel goes back to it
  // instead of quitting the setup.
  fromMaintenance: false,
  // Final-screen choices (Q8: the user picks where shortcuts go, at the end).
  startMenu: true,
  desktop: false,
  launch: true,
});

onMounted(async () => {
  const s = await InstallerState();
  state.mode = s.mode;
  state.dir = s.dir;
  state.version = s.version;
  state.installedVersion = s.installedVersion;
  state.url = s.url;
  state.license = s.license;
});

async function chooseDir() {
  const dir = await InstallerChooseDir();
  if (dir) state.dir = dir;
}

// The copy itself is near-instant; the install screen exists to show that
// something happened. Run it, then hold the bar a moment before "done".
async function install() {
  state.step = "installing";
  state.error = "";
  state.busy = true;
  const started = Date.now();
  const err = await InstallerInstall();
  const remaining = Math.max(0, 900 - (Date.now() - started));
  setTimeout(() => {
    state.busy = false;
    if (err) {
      state.error = err;
      state.step = "destination";
    } else {
      state.step = "done";
    }
  }, remaining);
}

async function finish() {
  state.busy = true;
  const err = await InstallerFinish(state.startMenu, state.desktop, state.launch);
  // On success the app quits; an error string means we're still here.
  state.busy = false;
  if (err) state.error = err;
}

// Reinstall (maintenance screen): straight to the copy, into the same
// folder the app is already installed in — no destination step.
function reinstall() {
  state.mode = "";
  install();
}

function askUninstall() {
  state.fromMaintenance = true;
  state.mode = "uninstall";
}

function cancelUninstall() {
  if (state.fromMaintenance) {
    state.mode = "maintenance";
    return;
  }
  Quit();
}

async function uninstall() {
  state.busy = true;
  const err = await InstallerUninstall();
  state.busy = false;
  if (err) state.error = err;
}

function toggleTheme() {
  setTheme(ui.theme === "dark" ? "light" : "dark");
}
</script>

<template>
  <div class="view wizard" data-testid="installer">
    <!-- ===== Uninstall confirmation ===== -->
    <template v-if="state.mode === 'uninstall'">
      <div class="wizard-body">
        <h1 class="wizard-title">Uninstall go-Notepad</h1>
        <p class="wizard-text">
          This removes go-Notepad from this computer — the application,
          its shortcuts <strong>and your settings and data</strong>.
        </p>
        <p v-if="state.error" class="wizard-error" data-testid="installer-error">
          {{ state.error }}
        </p>
      </div>
      <footer class="wizard-footer">
        <button class="btn btn-ghost" data-testid="uninstall-cancel" @click="cancelUninstall">
          Cancel
        </button>
        <span class="wizard-spacer" />
        <button
          class="btn btn-danger"
          data-testid="uninstall-confirm"
          :disabled="state.busy"
          @click="uninstall"
        >
          Uninstall
        </button>
      </footer>
    </template>

    <!-- ===== Maintenance: setup run with the app already installed ===== -->
    <template v-else-if="state.mode === 'maintenance'">
      <div class="wizard-body">
        <h1 class="wizard-title">go-Notepad is already installed</h1>
        <p class="wizard-text">
          Version {{ state.installedVersion || "?" }} is installed in:
        </p>
        <div class="wizard-path-row">
          <code class="wizard-path" data-testid="installer-dir">{{ state.dir }}</code>
        </div>
        <p class="wizard-text wizard-muted" style="margin-top: 10px">
          Reinstall version {{ state.version }} over it, or uninstall.
        </p>
        <p v-if="state.error" class="wizard-error" data-testid="installer-error">
          {{ state.error }}
        </p>
      </div>
      <footer class="wizard-footer">
        <button class="btn btn-danger" data-testid="maintenance-uninstall" @click="askUninstall">
          Uninstall
        </button>
        <span class="wizard-spacer" />
        <button class="btn btn-ghost" @click="Quit">Cancel</button>
        <button class="btn" data-testid="maintenance-reinstall" @click="reinstall">
          Reinstall
        </button>
      </footer>
    </template>

    <!-- ===== Install wizard ===== -->
    <template v-else>
      <div class="wizard-body">
        <template v-if="state.step === 'welcome'">
          <h1 class="wizard-title">Welcome to go-Notepad</h1>
          <p class="wizard-text">
            This will install go-Notepad {{ state.version }} on your computer —
            no administrator rights needed.
          </p>
          <p class="wizard-text wizard-muted">
            Click Next to continue.
          </p>
        </template>

        <template v-else-if="state.step === 'license'">
          <h1 class="wizard-title">License</h1>
          <pre class="wizard-license" data-testid="installer-license">{{ state.license }}</pre>
          <button
            class="wizard-link"
            data-testid="installer-github"
            @click="BrowserOpenURL(state.url)"
          >
            github.com/viniciusbuscacio/go-notepad
          </button>
        </template>

        <template v-else-if="state.step === 'destination'">
          <h1 class="wizard-title">Destination</h1>
          <p class="wizard-text">go-Notepad will be installed in:</p>
          <div class="wizard-path-row">
            <code class="wizard-path" data-testid="installer-dir">{{ state.dir }}</code>
            <button class="btn btn-ghost" data-testid="installer-change-dir" @click="chooseDir">
              Change…
            </button>
          </div>
          <p v-if="state.error" class="wizard-error" data-testid="installer-error">
            {{ state.error }}
          </p>
        </template>

        <template v-else-if="state.step === 'installing'">
          <h1 class="wizard-title">Installing…</h1>
          <div class="wizard-progress"><div class="wizard-progress-bar" /></div>
        </template>

        <template v-else>
          <h1 class="wizard-title">All set</h1>
          <p class="wizard-text">go-Notepad has been installed.</p>
          <label class="row switch-row">
            <span class="row-text"><span class="row-label">Start Menu shortcut</span></span>
            <input
              type="checkbox"
              class="switch"
              role="switch"
              v-model="state.startMenu"
              data-testid="installer-startmenu"
            />
          </label>
          <label class="row switch-row">
            <span class="row-text"><span class="row-label">Desktop shortcut</span></span>
            <input
              type="checkbox"
              class="switch"
              role="switch"
              v-model="state.desktop"
              data-testid="installer-desktop"
            />
          </label>
          <label class="row switch-row">
            <span class="row-text"><span class="row-label">Open go-Notepad now</span></span>
            <input
              type="checkbox"
              class="switch"
              role="switch"
              v-model="state.launch"
              data-testid="installer-launch"
            />
          </label>
          <p v-if="state.error" class="wizard-error" data-testid="installer-error">
            {{ state.error }}
          </p>
        </template>
      </div>

      <footer class="wizard-footer">
        <button
          class="icon-btn"
          :title="ui.theme === 'dark' ? 'Light theme' : 'Dark theme'"
          data-testid="installer-theme"
          @click="toggleTheme"
        >
          <!-- Lucide sun-moon, ISC licence -->
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"
            stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
            <path d="M12 8a2.83 2.83 0 0 0 4 4 4 4 0 1 1-4-4" />
            <path d="M12 2v2" /><path d="M12 20v2" />
            <path d="m4.9 4.9 1.4 1.4" /><path d="m17.7 17.7 1.4 1.4" />
            <path d="M2 12h2" /><path d="M20 12h2" />
            <path d="m6.3 17.7-1.4 1.4" /><path d="m19.1 4.9-1.4 1.4" />
          </svg>
        </button>
        <span class="wizard-spacer" />
        <template v-if="state.step === 'welcome'">
          <button class="btn" data-testid="installer-next" @click="state.step = 'license'">
            Next
          </button>
        </template>
        <template v-else-if="state.step === 'license'">
          <button class="btn btn-ghost" @click="state.step = 'welcome'">Back</button>
          <button class="btn" data-testid="installer-agree" @click="state.step = 'destination'">
            I agree
          </button>
        </template>
        <template v-else-if="state.step === 'destination'">
          <button class="btn btn-ghost" @click="state.step = 'license'">Back</button>
          <button class="btn" data-testid="installer-install" @click="install">
            Install
          </button>
        </template>
        <template v-else-if="state.step === 'done'">
          <button
            class="btn"
            data-testid="installer-finish"
            :disabled="state.busy"
            @click="finish"
          >
            Finish
          </button>
        </template>
      </footer>
    </template>
  </div>
</template>

<style scoped>
.wizard {
  display: flex;
  flex-direction: column;
  min-height: 0;
  flex: 1;
}

.wizard-body {
  flex: 1;
  min-height: 0;
  display: flex;
  flex-direction: column;
  padding: 20px 24px 8px;
  overflow-y: auto;
}

.wizard-title {
  font-size: 20px;
  font-weight: 600;
  margin: 0 0 12px;
}

.wizard-text {
  margin: 0 0 10px;
  line-height: 1.5;
}

.wizard-muted {
  color: var(--muted);
}

.wizard-license {
  flex: 1;
  min-height: 0;
  overflow: auto;
  background: var(--input-bg);
  border: 1px solid var(--border);
  border-radius: 6px;
  padding: 12px;
  font-size: 12px;
  line-height: 1.5;
  white-space: pre-wrap;
  margin: 0 0 10px;
}

.wizard-link {
  align-self: flex-start;
  background: none;
  border: none;
  padding: 0;
  color: var(--accent);
  cursor: pointer;
  font-size: 13px;
}
.wizard-link:hover {
  text-decoration: underline;
}

.wizard-path-row {
  display: flex;
  align-items: center;
  gap: 10px;
}

.wizard-path {
  flex: 1;
  min-width: 0;
  overflow-wrap: anywhere;
  background: var(--input-bg);
  border: 1px solid var(--border);
  border-radius: 6px;
  padding: 10px 12px;
  font-size: 12px;
}

.wizard-error {
  color: var(--danger);
  margin: 10px 0 0;
}

.wizard-progress {
  height: 6px;
  border-radius: 3px;
  background: var(--panel-bg);
  overflow: hidden;
  margin-top: 18px;
}

.wizard-progress-bar {
  height: 100%;
  width: 40%;
  border-radius: 3px;
  background: var(--accent);
  animation: wizard-slide 1.1s ease-in-out infinite;
}

@keyframes wizard-slide {
  0% {
    margin-left: -40%;
  }
  100% {
    margin-left: 100%;
  }
}

.wizard-footer {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 14px 24px 18px;
  border-top: 1px solid var(--border);
}

.wizard-spacer {
  flex: 1;
}

.btn-danger {
  background: var(--danger);
  color: var(--bg);
  border-color: transparent;
}
</style>
