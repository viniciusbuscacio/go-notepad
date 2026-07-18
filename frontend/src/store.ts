import { reactive, watch } from "vue";
import {
  GetSettings,
  GetVersion,
  SetTheme,
  SetOpacity,
  SetTabPosition,
  SetWordWrap,
  SetFontFamily,
  SetFontSize,
  SaveSession,
  LoadSession,
  OpenPath,
  ConsumePendingFile,
  GetUpdateInfo,
  CheckForUpdates,
  InstallUpdate,
  SkipUpdateVersion,
  RemindUpdateLater,
  SetUpdateAutoCheck,
} from "../wailsjs/go/main/App";
import { EventsOn } from "../wailsjs/runtime/runtime";

export type View = "editor" | "options" | "api" | "installer";
export type TabPosition = "top" | "left";

// Editor font families offered in Settings. Every non-"mono" option is a Google
// Font bundled inside the app as a .woff2 (@font-face in theme/fonts.css), so it
// renders identically on Windows / macOS / Linux without being installed. Keep
// this list in sync with scripts/fetch-fonts.sh and settings.go's fontFamilies.
export interface FontOption {
  key: string;
  label: string;
  group: string;
  stack: string;
}

const SANS = "ui-sans-serif, system-ui, sans-serif";
const SERIF = "ui-serif, Georgia, serif";
const MONO = "ui-monospace, monospace";

export const FONT_FAMILIES: FontOption[] = [
  { key: "mono", label: "System (mono)", group: "System", stack: `"Cascadia Code","Cascadia Mono","Consolas","SF Mono",${MONO}` },

  { key: "inter", label: "Inter", group: "Sans-serif", stack: `"Inter", ${SANS}` },
  { key: "roboto", label: "Roboto", group: "Sans-serif", stack: `"Roboto", ${SANS}` },
  { key: "open-sans", label: "Open Sans", group: "Sans-serif", stack: `"Open Sans", ${SANS}` },
  { key: "lato", label: "Lato", group: "Sans-serif", stack: `"Lato", ${SANS}` },
  { key: "montserrat", label: "Montserrat", group: "Sans-serif", stack: `"Montserrat", ${SANS}` },
  { key: "poppins", label: "Poppins", group: "Sans-serif", stack: `"Poppins", ${SANS}` },
  { key: "nunito", label: "Nunito", group: "Sans-serif", stack: `"Nunito", ${SANS}` },
  { key: "work-sans", label: "Work Sans", group: "Sans-serif", stack: `"Work Sans", ${SANS}` },
  { key: "dm-sans", label: "DM Sans", group: "Sans-serif", stack: `"DM Sans", ${SANS}` },
  { key: "manrope", label: "Manrope", group: "Sans-serif", stack: `"Manrope", ${SANS}` },
  { key: "rubik", label: "Rubik", group: "Sans-serif", stack: `"Rubik", ${SANS}` },
  { key: "libre-franklin", label: "Libre Franklin", group: "Sans-serif", stack: `"Libre Franklin", ${SANS}` },
  { key: "jost", label: "Jost", group: "Sans-serif", stack: `"Jost", ${SANS}` },
  { key: "comic-neue", label: "Comic Neue", group: "Sans-serif", stack: `"Comic Neue", ${SANS}` },

  { key: "merriweather", label: "Merriweather", group: "Serif", stack: `"Merriweather", ${SERIF}` },
  { key: "lora", label: "Lora", group: "Serif", stack: `"Lora", ${SERIF}` },
  { key: "playfair-display", label: "Playfair Display", group: "Serif", stack: `"Playfair Display", ${SERIF}` },
  { key: "libre-baskerville", label: "Libre Baskerville", group: "Serif", stack: `"Libre Baskerville", ${SERIF}` },
  { key: "tinos", label: "Tinos (Times-like)", group: "Serif", stack: `"Tinos", ${SERIF}` },
  { key: "gelasio", label: "Gelasio (Georgia-like)", group: "Serif", stack: `"Gelasio", ${SERIF}` },

  { key: "jetbrains-mono", label: "JetBrains Mono", group: "Monospace", stack: `"JetBrains Mono", ${MONO}` },
  { key: "fira-code", label: "Fira Code", group: "Monospace", stack: `"Fira Code", ${MONO}` },
  { key: "ibm-plex-mono", label: "IBM Plex Mono", group: "Monospace", stack: `"IBM Plex Mono", ${MONO}` },
  { key: "inconsolata", label: "Inconsolata", group: "Monospace", stack: `"Inconsolata", ${MONO}` },
  { key: "cousine", label: "Cousine (Courier-like)", group: "Monospace", stack: `"Cousine", ${MONO}` },
];

// Distinct group names in display order, for the picker's <optgroup>s.
export const FONT_GROUPS = ["System", "Sans-serif", "Serif", "Monospace"];

export const FONT_SIZE_MIN = 8;
export const FONT_SIZE_MAX = 48;
export const FONT_SIZE_DEFAULT = 14;

// Minimal shared UI state. A single reactive object is enough of a "router"
// for a small single-window app and reuses cleanly across the framework.
export const ui = reactive({
  view: "editor" as View,
  theme: "dark",
  opacity: 100,
  // Number of in-flight async operations triggered from the UI (e.g. a save
  // round-trip). The UI bridge waits until this reaches 0 before reading the
  // screen back, so it never samples a half-settled state.
  busy: 0,
  // App version, fetched once at startup. Loading it here (not on the About
  // panel's mount) keeps the version element present the moment the panel
  // renders, so a UI-state snapshot never races the fetch.
  version: "",
});

// ---- open documents (tabs) ------------------------------------------------
// The tab list lives here, NOT inside the editor view, so it survives when the
// user navigates to Settings and back (the view is unmounted; this store is not).

export interface Tab {
  id: number;
  name: string; // tab title (file base name, or "Untitled")
  path: string; // absolute path once saved; "" while unsaved
  content: string;
  dirty: boolean; // unsaved changes
}

let tabSeq = 0;

// Cursor position per tab, kept OUTSIDE the reactive `notes` (which is watched
// for session autosave) so moving the caret doesn't trigger disk writes. It
// survives view switches (the editor unmounts when you open Settings) so the
// cursor is restored where it was, not dumped at the end of the document.
export const caretPositions = new Map<number, { start: number; end: number }>();

export const notes = reactive({
  tabs: [] as Tab[],
  activeId: -1,
  tabPosition: "top" as TabPosition,
  wordWrap: true,
  fontFamily: "mono",
  fontSize: FONT_SIZE_DEFAULT,
});

export function activeTab(): Tab | undefined {
  return notes.tabs.find((t) => t.id === notes.activeId);
}

export function newTab(name = "Untitled", content = "", path = ""): Tab {
  const tab: Tab = { id: tabSeq++, name, path, content, dirty: false };
  notes.tabs.push(tab);
  notes.activeId = tab.id;
  return tab;
}

export function setActive(id: number) {
  if (notes.tabs.some((t) => t.id === id)) notes.activeId = id;
}

export function closeTab(id: number) {
  const idx = notes.tabs.findIndex((t) => t.id === id);
  if (idx === -1) return;
  notes.tabs.splice(idx, 1);
  caretPositions.delete(id);
  if (notes.tabs.length === 0) {
    newTab(); // always keep one document open, like Windows Notepad
    return;
  }
  if (notes.activeId === id) {
    // activate the neighbour that slid into this slot (or the new last tab)
    const next = notes.tabs[Math.min(idx, notes.tabs.length - 1)];
    notes.activeId = next.id;
  }
}

// ---- session persistence --------------------------------------------------
// The open tabs are saved to disk (app-data dir, via the Go SaveSession) so the
// next launch reopens with the same documents. Autosaved, debounced, whenever
// the tab set or their contents change.

interface SerializedSession {
  activeIndex: number;
  tabs: { name: string; path: string; content: string; dirty: boolean }[];
}

function serializeSession(): string {
  const activeIndex = notes.tabs.findIndex((t) => t.id === notes.activeId);
  const data: SerializedSession = {
    activeIndex: activeIndex < 0 ? 0 : activeIndex,
    tabs: notes.tabs.map((t) => ({
      name: t.name,
      path: t.path,
      content: t.content,
      dirty: t.dirty,
    })),
  };
  return JSON.stringify(data);
}

// restoreSession rebuilds the tabs from a previous run; returns false if there
// was nothing usable to restore.
async function restoreSession(raw: string): Promise<boolean> {
  if (!raw) return false;
  let data: SerializedSession;
  try {
    data = JSON.parse(raw);
  } catch {
    return false;
  }
  if (!Array.isArray(data.tabs) || data.tabs.length === 0) return false;
  for (const t of data.tabs) {
    // A clean (non-dirty) tab belongs to its file: reread it from disk, since
    // another program may have changed it after the session was written — the
    // stale session copy must not win over the newer file.
    if (!t.dirty && t.path) {
      try {
        const r = await OpenPath(t.path);
        newTab(r.name, r.content, r.path);
        continue;
      } catch {
        // The file is gone or unreadable: fall back to the session copy and
        // mark the tab dirty so it's clear it is no longer backed by the disk.
        const tab = newTab(t.name || "Untitled", t.content || "", t.path);
        tab.dirty = true;
        continue;
      }
    }
    const tab = newTab(t.name || "Untitled", t.content || "", t.path || "");
    tab.dirty = !!t.dirty;
  }
  const idx = Math.max(0, Math.min(data.activeIndex ?? 0, notes.tabs.length - 1));
  notes.activeId = notes.tabs[idx].id;
  return true;
}

let sessionTimer: number | undefined;
let sessionMaxTimer: number | undefined;
let sessionReady = false;

async function persistSession() {
  try {
    await SaveSession(serializeSession());
  } catch {
    /* best effort; the tabs still live in memory */
  }
}

// persistSessionNow cancels both autosave timers and writes immediately.
async function persistSessionNow() {
  clearTimeout(sessionTimer);
  sessionTimer = undefined;
  clearTimeout(sessionMaxTimer);
  sessionMaxTimer = undefined;
  await persistSession();
}

// flushSession is the pre-quit hook: whatever autosave is pending goes to disk
// right now, so the last keystrokes survive closing the app.
export async function flushSession() {
  if (!sessionReady) return;
  await persistSessionNow();
}

// Autosave: any change to the tab set or contents schedules a debounced write.
// Enabled only after the initial restore so we don't clobber the saved file
// while it is still being loaded. The 500ms debounce is reset by every
// keystroke, so on its own it would never fire under continuous typing; the
// max-wait timer is NOT reset and guarantees a write at most ~2s after the
// first unsaved change.
watch(
  notes,
  () => {
    if (!sessionReady) return;
    clearTimeout(sessionTimer);
    sessionTimer = window.setTimeout(persistSessionNow, 500);
    if (sessionMaxTimer === undefined) {
      sessionMaxTimer = window.setTimeout(persistSessionNow, 2000);
    }
  },
  { deep: true },
);

export function go(view: View) {
  ui.view = view;
}

// ---- in-app updater --------------------------------------------------------
// Mirror of the Go-side UpdateInfo (update.go). Go owns every rule (what is
// newer, when to notify); this store only renders it. Kept in sync two ways:
// explicit awaited calls below, plus the "update:state" event Go broadcasts on
// every change (auto-check results, install progress).

export interface UpdateState {
  checking: boolean;
  installing: boolean;
  progress: string; // downloading | verifying | applying
  available: boolean;
  version: string;
  notes: string;
  current: string;
  checkedAt: string;
  error: string;
  notify: boolean;
}

export const update = reactive<UpdateState & { autoCheck: boolean; seen: boolean }>({
  checking: false,
  installing: false,
  progress: "",
  available: false,
  version: "",
  notes: "",
  current: "",
  checkedAt: "",
  error: "",
  notify: false,
  // Local additions: the auto-check preference, and whether the user has
  // checked manually this session (a manual check shows the result card even
  // for a version that was skipped/snoozed before).
  autoCheck: false,
  seen: false,
});

function applyUpdateState(s: UpdateState) {
  Object.assign(update, s);
}

export function initUpdateEvents() {
  EventsOn("update:state", (s: UpdateState) => applyUpdateState(s));
}

export async function checkForUpdates() {
  update.seen = true;
  busyInc();
  try {
    applyUpdateState(await CheckForUpdates());
  } catch {
    /* the Go side reported what it could via the event */
  } finally {
    busyDec();
  }
}

// installUpdate flushes the session first (same as quitting), then hands over
// to Go: on success the app restarts itself and this call never resolves.
export async function installUpdate() {
  busyInc();
  try {
    await flushSession();
    await InstallUpdate();
  } catch {
    /* failure state (update.error) arrives via the update:state event */
  } finally {
    busyDec();
  }
}

export async function skipUpdate() {
  update.seen = false;
  busyInc();
  try {
    await SkipUpdateVersion();
  } catch {
    /* best effort */
  } finally {
    busyDec();
  }
}

export async function remindUpdateLater() {
  update.seen = false;
  busyInc();
  try {
    await RemindUpdateLater();
  } catch {
    /* best effort */
  } finally {
    busyDec();
  }
}

export async function setUpdateAutoCheck(on: boolean) {
  update.autoCheck = on;
  busyInc();
  try {
    await SetUpdateAutoCheck(on);
  } catch {
    /* best effort */
  } finally {
    busyDec();
  }
}

// busyInc/busyDec bracket any async handler that changes visible state, so the
// UI bridge knows when the screen has finished updating. Call busyInc()
// synchronously before the first await, and busyDec() in a finally block.
export function busyInc() {
  ui.busy++;
}

export function busyDec() {
  ui.busy = Math.max(0, ui.busy - 1);
}

export function applyTheme(theme: string) {
  const t = theme === "light" ? "light" : "dark";
  ui.theme = t;
  document.documentElement.dataset.theme = t;
}

export function applyOpacity(percent: number) {
  const clamped = Math.min(100, Math.max(20, percent));
  ui.opacity = clamped;
  document.documentElement.style.setProperty("--bg-alpha", String(clamped / 100));
}

function fontStack(key: string): string {
  return (FONT_FAMILIES.find((f) => f.key === key) ?? FONT_FAMILIES[0]).stack;
}

// applyFont writes the editor font to CSS variables the editor reads. This
// changes ONLY the editor text — never the app chrome — which is what the
// Ctrl +/- (and Ctrl+wheel) zoom targets.
export function applyFont() {
  const root = document.documentElement.style;
  root.setProperty("--editor-font-family", fontStack(notes.fontFamily));
  root.setProperty("--editor-font-size", `${notes.fontSize}px`);
}

export function setFontFamily(key: string) {
  notes.fontFamily = FONT_FAMILIES.some((f) => f.key === key) ? key : "mono";
  applyFont();
  busyInc();
  SetFontFamily(notes.fontFamily).catch(() => {}).finally(busyDec);
}

// setFontSize clamps, applies immediately, and persists (debounced, since the
// zoom can fire many times per second).
let fontSizeTimer: number | undefined;
export function setFontSize(px: number) {
  notes.fontSize = Math.min(FONT_SIZE_MAX, Math.max(FONT_SIZE_MIN, Math.round(px)));
  applyFont();
  clearTimeout(fontSizeTimer);
  fontSizeTimer = window.setTimeout(() => {
    SetFontSize(notes.fontSize).catch(() => {});
  }, 300);
}

export function bumpFontSize(delta: number) {
  setFontSize(notes.fontSize + delta);
}

export function resetFontSize() {
  setFontSize(FONT_SIZE_DEFAULT);
}

export async function loadSettings() {
  try {
    const s = await GetSettings();
    applyTheme(s.theme || "dark");
    applyOpacity(s.opacity || 100);
    notes.tabPosition = s.tabPosition === "left" ? "left" : "top";
    notes.wordWrap = s.wordWrap !== false;
    notes.fontFamily = s.fontFamily || "mono";
    notes.fontSize = s.fontSize || FONT_SIZE_DEFAULT;
    update.autoCheck = s.updateAutoCheck === true;
    applyFont();
  } catch {
    applyTheme("dark");
    applyOpacity(100);
    applyFont();
  }
  try {
    ui.version = await GetVersion();
  } catch {
    /* leave version empty if the backend isn't reachable */
  }
  try {
    // Snapshot from Go (a startup auto-check may already have run) + live
    // updates from here on.
    initUpdateEvents();
    applyUpdateState(await GetUpdateInfo());
  } catch {
    /* updater state stays at its defaults */
  }
  // Reopen the previous session's tabs; fall back to a single blank document,
  // like a fresh Notepad window. Enable autosave only after this is done.
  try {
    await restoreSession(await LoadSession());
  } catch {
    /* no session or unreadable; fall through to a blank tab */
  }
  // A file passed on the command line (go-notepad file.txt) opens on top of
  // the restored session, in a focused tab of its own.
  try {
    const pending = await ConsumePendingFile();
    if (pending) await openPendingFile(pending);
  } catch {
    /* no pending file, or the backend isn't reachable */
  }
  if (notes.tabs.length === 0) newTab();
  sessionReady = true;
}

// openPendingFile opens the command-line file: reuse the tab that already has
// this path (the session may have restored it), otherwise read it from disk; a
// path that doesn't exist yet becomes a new document that the first save
// creates.
async function openPendingFile(path: string) {
  const existing = notes.tabs.find((t) => t.path === path);
  if (existing) {
    notes.activeId = existing.id;
    return;
  }
  try {
    const r = await OpenPath(path);
    newTab(r.name, r.content, r.path);
  } catch {
    const name = path.split(/[\\/]/).pop() || "Untitled";
    newTab(name, "", path);
  }
}

export async function setTheme(theme: string) {
  applyTheme(theme);
  busyInc();
  try {
    await SetTheme(theme);
  } catch {
    /* best effort; UI already reflects the change */
  } finally {
    busyDec();
  }
}

export async function setOpacity(percent: number) {
  applyOpacity(percent);
  busyInc();
  try {
    await SetOpacity(Math.round(percent));
  } catch {
    /* best effort */
  } finally {
    busyDec();
  }
}

export async function setTabPosition(pos: TabPosition) {
  notes.tabPosition = pos;
  busyInc();
  try {
    await SetTabPosition(pos);
  } catch {
    /* best effort */
  } finally {
    busyDec();
  }
}

export async function setWordWrap(on: boolean) {
  notes.wordWrap = on;
  busyInc();
  try {
    await SetWordWrap(on);
  } catch {
    /* best effort */
  } finally {
    busyDec();
  }
}
