<script setup lang="ts">
import { computed, nextTick, onMounted, onUnmounted, reactive, ref, watch } from "vue";
import {
  OpenFile,
  SaveFile,
  SaveFileAs,
  Stats,
} from "../../wailsjs/go/main/App";
import {
  notes,
  activeTab,
  newTab,
  setActive,
  closeTab,
  busyInc,
  busyDec,
  bumpFontSize,
  resetFontSize,
  caretPositions,
  type Tab,
} from "../store";

const editorEl = ref<HTMLTextAreaElement | null>(null);

const active = computed(() => activeTab());

// What the status bar's left corner shows: the active document's full path,
// or just its name while it hasn't been saved anywhere yet.
const filePath = computed(
  () => active.value?.path || active.value?.name || "Untitled",
);
const caret = reactive({ line: 1, col: 1 });
const counts = reactive({ lines: 1, words: 0, chars: 0 });

const TABSTRIP_MIN = 160;
const TABSTRIP_MAX = 320;
const TABSTRIP_DEFAULT = 190;
const TABSTRIP_STORAGE_KEY = "go-notepad:tabstrip-width:v2";

function clampTabstripWidth(v: number) {
  return Math.max(TABSTRIP_MIN, Math.min(TABSTRIP_MAX, Math.round(v)));
}

function loadTabstripWidth() {
  const saved = Number(window.localStorage.getItem(TABSTRIP_STORAGE_KEY));
  return Number.isFinite(saved) ? clampTabstripWidth(saved) : TABSTRIP_DEFAULT;
}

const tabstripWidth = ref(loadTabstripWidth());
const tabstripStyle = computed(() =>
  notes.tabPosition === "left"
    ? { "--tabstrip-width": `${tabstripWidth.value}px` }
    : undefined,
);

let resizeStartX = 0;
let resizeStartWidth = 0;

function onTabstripResizePointerMove(e: PointerEvent) {
  tabstripWidth.value = clampTabstripWidth(resizeStartWidth + e.clientX - resizeStartX);
}

function stopTabstripResize() {
  window.removeEventListener("pointermove", onTabstripResizePointerMove);
  window.removeEventListener("pointerup", stopTabstripResize);
  document.body.classList.remove("is-resizing-tabstrip");
  window.localStorage.setItem(TABSTRIP_STORAGE_KEY, String(tabstripWidth.value));
}

function onTabstripResizePointerDown(e: PointerEvent) {
  if (notes.tabPosition !== "left") return;
  e.preventDefault();
  resizeStartX = e.clientX;
  resizeStartWidth = tabstripWidth.value;
  document.body.classList.add("is-resizing-tabstrip");
  window.addEventListener("pointermove", onTabstripResizePointerMove);
  window.addEventListener("pointerup", stopTabstripResize);
}

// --- status bar counts come from Go (the same engine behind /v1/stats), so the
// numbers on screen and over the API always agree. Debounced to stay smooth.
let statsTimer: number | undefined;
function scheduleCounts() {
  clearTimeout(statsTimer);
  statsTimer = window.setTimeout(async () => {
    const t = active.value;
    if (!t) return;
    try {
      const s = await Stats(t.content);
      counts.lines = s.lines;
      counts.words = s.words;
      counts.chars = s.chars;
    } catch {
      /* leave the last known counts */
    }
  }, 120);
}

// Recompute the Ln/Col display from the textarea's current caret.
function computeCaretDisplay() {
  const ta = editorEl.value;
  const t = active.value;
  if (!ta || !t) return;
  const pos = ta.selectionStart;
  const before = t.content.slice(0, pos);
  const lastNL = before.lastIndexOf("\n");
  caret.line = (before.match(/\n/g)?.length ?? 0) + 1;
  caret.col = pos - lastNL; // lastNL = -1 when on the first line → pos + 1
}

// The user moved the caret: remember it for this tab, then update the display.
function updateCaret() {
  const ta = editorEl.value;
  const t = active.value;
  if (ta && t) caretPositions.set(t.id, { start: ta.selectionStart, end: ta.selectionEnd });
  computeCaretDisplay();
}

// Put the caret back where it was for the active tab — after remounting (the
// editor unmounts when Settings opens) or switching tabs, so it never jumps to
// the end of the document.
function restoreCaret() {
  const ta = editorEl.value;
  const t = active.value;
  if (!ta || !t) return;
  const end = t.content.length;
  const saved = caretPositions.get(t.id);
  const s = Math.min(saved?.start ?? 0, end);
  const e = Math.min(saved?.end ?? s, end);
  ta.setSelectionRange(s, e);
  computeCaretDisplay();
}

function onInput(e: Event) {
  const t = active.value;
  if (!t) return;
  t.content = (e.target as HTMLTextAreaElement).value;
  t.dirty = true;
  updateCaret();
  scheduleCounts();
}

// Tab key inserts a real tab character instead of leaving the textarea.
function onEditorKeydown(e: KeyboardEvent) {
  if (e.key !== "Tab" || e.ctrlKey || e.metaKey || e.altKey) return;
  e.preventDefault();
  const ta = editorEl.value;
  const t = active.value;
  if (!ta || !t) return;
  const start = ta.selectionStart;
  const end = ta.selectionEnd;
  t.content = t.content.slice(0, start) + "\t" + t.content.slice(end);
  t.dirty = true;
  nextTick(() => {
    ta.selectionStart = ta.selectionEnd = start + 1;
    updateCaret();
  });
  scheduleCounts();
}

// --- file actions ---------------------------------------------------------

// The last open/save failure, shown in the status bar (and cleared by the next
// attempt) so a failed disk operation is never silent.
const fileError = ref("");

function reportFileError(operation: string, file: string, e: unknown) {
  const detail = typeof e === "string" ? e : e instanceof Error ? e.message : "";
  fileError.value = `Could not ${operation} "${file}"${detail ? `: ${detail}` : ""}`;
}

async function openFile() {
  fileError.value = "";
  busyInc();
  try {
    const r = await OpenFile();
    if (!r.canceled) newTab(r.name, r.content, r.path);
  } catch (e) {
    // The Go error string carries the path of the file that failed to load.
    reportFileError("open", "the selected file", e);
  } finally {
    busyDec();
  }
}

// saveTab writes a specific tab to disk. Returns true if it was saved, false if
// the user cancelled the Save-As dialog or the write failed (so callers like the
// close-confirm flow know whether it's safe to proceed).
async function saveTab(t: Tab): Promise<boolean> {
  fileError.value = "";
  busyInc();
  try {
    if (t.path) {
      await SaveFile(t.path, t.content);
      t.dirty = false;
      return true;
    }
    return await saveTabAs(t);
  } catch (e) {
    reportFileError("save", t.path || t.name, e);
    return false;
  } finally {
    busyDec();
  }
}

// saveTabAs always shows the Save-As dialog, even for a tab that already has a
// path (Ctrl+Shift+S), and rebinds the tab to whatever path the user picked.
async function saveTabAs(t: Tab): Promise<boolean> {
  fileError.value = "";
  busyInc();
  try {
    const suggested = t.name.endsWith(".txt") ? t.name : `${t.name}.txt`;
    const r = await SaveFileAs(suggested, t.content);
    if (r.canceled) return false;
    t.path = r.path;
    t.name = r.name;
    t.dirty = false;
    return true;
  } catch (e) {
    reportFileError("save", t.name, e);
    return false;
  } finally {
    busyDec();
  }
}

async function save() {
  const t = active.value;
  if (t) await saveTab(t);
}

async function saveAs() {
  const t = active.value;
  if (t) await saveTabAs(t);
}

// --- closing a tab with unsaved changes -----------------------------------
// Closing a dirty tab opens an in-app confirm (save / discard / cancel) instead
// of silently losing the edits. Kept in the DOM so an agent can drive it too.
const pendingCloseId = ref<number | null>(null);
const pendingTab = computed(() =>
  notes.tabs.find((t) => t.id === pendingCloseId.value),
);

function requestClose(id: number) {
  const t = notes.tabs.find((x) => x.id === id);
  if (!t) return;
  if (t.dirty) pendingCloseId.value = id;
  else closeTab(id);
}

async function confirmSave() {
  const t = pendingTab.value;
  pendingCloseId.value = null;
  if (t && (await saveTab(t))) closeTab(t.id);
}

function confirmDiscard() {
  if (pendingCloseId.value !== null) closeTab(pendingCloseId.value);
  pendingCloseId.value = null;
}

function cancelClose() {
  pendingCloseId.value = null;
}

function addTab() {
  newTab();
  nextTick(() => editorEl.value?.focus());
}


// --- window-level shortcuts ----------------------------------------------
function onKey(e: KeyboardEvent) {
  if (!(e.ctrlKey || e.metaKey)) return;
  const k = e.key.toLowerCase();
  // Zoom the editor TEXT only (never the whole app). We intercept Ctrl/Cmd
  // +/-/0 and preventDefault so the webview never zooms the page itself.
  if (k === "=" || k === "+") {
    e.preventDefault();
    bumpFontSize(+1);
  } else if (k === "-" || k === "_") {
    e.preventDefault();
    bumpFontSize(-1);
  } else if (k === "0") {
    e.preventDefault();
    resetFontSize();
  } else if (k === "n") {
    e.preventDefault();
    addTab();
  } else if (k === "o") {
    e.preventDefault();
    openFile();
  } else if (k === "s") {
    e.preventDefault();
    // Ctrl+Shift+S is Save As: always ask for a path, even for a saved file.
    if (e.shiftKey) saveAs();
    else save();
  } else if (k === "w") {
    e.preventDefault();
    const t = active.value;
    if (t) requestClose(t.id);
  }
}

// Ctrl/Cmd + mouse wheel zooms the editor text (up = bigger). preventDefault
// stops the webview's own page zoom, so only the text scales.
function onWheel(e: WheelEvent) {
  if (!(e.ctrlKey || e.metaKey)) return;
  e.preventDefault();
  bumpFontSize(e.deltaY < 0 ? +1 : -1);
}

// When the active tab changes, refresh caret + counts against its content.
watch(
  () => notes.activeId,
  () => {
    nextTick(() => {
      restoreCaret();
      scheduleCounts();
    });
  },
);

onMounted(() => {
  window.addEventListener("keydown", onKey);
  // passive:false so preventDefault can stop the webview's page zoom.
  window.addEventListener("wheel", onWheel, { passive: false });
  scheduleCounts();
  nextTick(() => {
    editorEl.value?.focus();
    restoreCaret();
  });
});
onUnmounted(() => {
  window.removeEventListener("keydown", onKey);
  window.removeEventListener("wheel", onWheel);
  window.removeEventListener("pointermove", onTabstripResizePointerMove);
  window.removeEventListener("pointerup", stopTabstripResize);
  document.body.classList.remove("is-resizing-tabstrip");
});
</script>

<template>
  <div class="view view--editor" :class="notes.tabPosition === 'left' ? 'pos-left' : 'pos-top'">
    <div class="editor-main">
      <div class="tabstrip" data-testid="tablist" :style="tabstripStyle">
        <div class="tabs">
          <button
            v-for="(t, i) in notes.tabs"
            :key="t.id"
            class="tab"
            :class="{ active: t.id === notes.activeId }"
            :data-testid="`tab-${i}`"
            :title="t.path || t.name"
            @click="setActive(t.id)"
          >
            <span class="tab-name">{{ t.name }}</span>
            <span v-if="t.dirty" class="tab-dot" aria-label="unsaved">•</span>
            <span
              class="tab-close"
              role="button"
              :data-testid="`close-${i}`"
              title="Close tab"
              @click.stop="requestClose(t.id)"
              >✕</span
            >
          </button>
          <button
            class="tab-new"
            data-testid="new-tab"
            title="New tab (Ctrl+N)"
            @click="addTab"
          >
            <svg viewBox="0 0 24 24" fill="currentColor" aria-hidden="true">
              <path d="M12 3.25C12.4142 3.25 12.75 3.58579 12.75 4V11.25H20C20.4142 11.25 20.75 11.5858 20.75 12C20.75 12.4142 20.4142 12.75 20 12.75H12.75V20C12.75 20.4142 12.4142 20.75 12 20.75C11.5858 20.75 11.25 20.4142 11.25 20V12.75H4C3.58579 12.75 3.25 12.4142 3.25 12C3.25 11.5858 3.58579 11.25 4 11.25H11.25V4C11.25 3.58579 11.5858 3.25 12 3.25Z" />
            </svg>
          </button>
        </div>

        <div class="strip-actions">
          <button
            class="strip-btn"
            data-testid="open-file"
            title="Open (Ctrl+O)"
            @click="openFile"
          >
            <svg viewBox="0 0 24 24" fill="currentColor" aria-hidden="true">
              <path d="M3.50014 6.25C3.50014 5.2835 4.28364 4.5 5.25014 4.5H8.12882C8.32773 4.5 8.5185 4.57902 8.65915 4.71967L10.7198 6.78033C10.8605 6.92098 11.0512 7 11.2501 7H16.7501C17.7166 7 18.5001 7.7835 18.5001 8.75C18.5001 8.83602 18.5146 8.91866 18.5413 8.99561L8.71867 8.99561C7.37893 8.99561 6.14095 9.71035 5.47108 10.8706L3.50014 14.2844V6.25ZM2.00036 17.7883C2.02089 19.5656 3.468 21 5.25014 21H11.0001C11.0292 21 11.058 20.9983 11.0862 20.9951H16.2812C17.6209 20.9951 18.8589 20.2804 19.5288 19.1201L22.5596 13.8706C23.7748 11.7657 22.3337 9.14964 19.9567 9.00214C19.9848 8.92334 20.0001 8.83846 20.0001 8.75C20.0001 6.95507 18.5451 5.5 16.7501 5.5H11.5608L9.71981 3.65901C9.29786 3.23705 8.72556 3 8.12882 3H5.25014C3.45522 3 2.00014 4.45507 2.00014 6.25V17.71C1.9999 17.7362 1.99997 17.7623 2.00036 17.7883ZM8.71867 10.4956L19.745 10.4956C21.0921 10.4956 21.9341 11.9539 21.2605 13.1206L18.2297 18.3701C17.8278 19.0663 17.085 19.4951 16.2812 19.4951H5.25485C3.9077 19.4951 3.06573 18.0368 3.7393 16.8701L6.77011 11.6206C7.17204 10.9245 7.91482 10.4956 8.71867 10.4956Z" />
            </svg>
          </button>
          <button
            class="strip-btn"
            data-testid="save-file"
            title="Save (Ctrl+S)"
            @click="save"
          >
            <svg viewBox="0 0 24 24" fill="currentColor" aria-hidden="true">
              <path d="M3 5.75C3 4.23122 4.23122 3 5.75 3H15.7145C16.5764 3 17.4031 3.34241 18.0126 3.9519L20.0481 5.98744C20.6576 6.59693 21 7.42358 21 8.28553V18.25C21 19.7688 19.7688 21 18.25 21H5.75C4.23122 21 3 19.7688 3 18.25V5.75ZM5.75 4.5C5.05964 4.5 4.5 5.05964 4.5 5.75V18.25C4.5 18.9404 5.05964 19.5 5.75 19.5H6V14.25C6 13.0074 7.00736 12 8.25 12H15.75C16.9926 12 18 13.0074 18 14.25V19.5H18.25C18.9404 19.5 19.5 18.9404 19.5 18.25V8.28553C19.5 7.8214 19.3156 7.37629 18.9874 7.0481L16.9519 5.01256C16.6918 4.75246 16.3582 4.58269 16 4.52344V7.25C16 8.49264 14.9926 9.5 13.75 9.5H9.25C8.00736 9.5 7 8.49264 7 7.25V4.5H5.75ZM16.5 19.5V14.25C16.5 13.8358 16.1642 13.5 15.75 13.5H8.25C7.83579 13.5 7.5 13.8358 7.5 14.25V19.5H16.5ZM8.5 4.5V7.25C8.5 7.66421 8.83579 8 9.25 8H13.75C14.1642 8 14.5 7.66421 14.5 7.25V4.5H8.5Z" />
            </svg>
          </button>
        </div>

        <div
          v-if="notes.tabPosition === 'left'"
          class="tabstrip-resizer"
          role="separator"
          aria-orientation="vertical"
          :aria-valuemin="TABSTRIP_MIN"
          :aria-valuemax="TABSTRIP_MAX"
          :aria-valuenow="tabstripWidth"
          title="Resize open files pane"
          @pointerdown="onTabstripResizePointerDown"
        ></div>
      </div>

      <div class="editor-pane">
        <textarea
          ref="editorEl"
          class="editor"
          :class="{ nowrap: !notes.wordWrap }"
          data-testid="editor"
          spellcheck="false"
          :wrap="notes.wordWrap ? 'soft' : 'off'"
          :value="active?.content ?? ''"
          @input="onInput"
          @keydown="onEditorKeydown"
          @keyup="updateCaret"
          @click="updateCaret"
        ></textarea>
      </div>
    </div>

    <Teleport to="body">
      <div
        v-if="pendingTab"
        class="modal-overlay"
        data-testid="close-confirm"
        @click.self="cancelClose"
      >
        <div class="modal">
          <p class="modal-title">Save changes?</p>
          <p class="modal-msg">
            “{{ pendingTab.name }}” has unsaved changes. Do you want to save them
            before closing?
          </p>
          <div class="modal-actions">
            <button class="btn btn-ghost" data-testid="confirm-cancel" @click="cancelClose">
              Cancel
            </button>
            <button class="btn btn-ghost" data-testid="confirm-discard" @click="confirmDiscard">
              Don't save
            </button>
            <button class="btn" data-testid="confirm-save" @click="confirmSave">
              Save
            </button>
          </div>
        </div>
      </div>
    </Teleport>

    <footer class="statusbar" data-testid="statusbar">
      <span
        class="status-cell status-file"
        data-testid="file-path"
        :title="filePath"
        >File {{ filePath }}</span
      >
      <span
        v-if="fileError"
        class="status-cell status-error"
        data-testid="file-error"
        :title="fileError"
        >{{ fileError }}</span
      >
      <span class="status-cell status-right"
        >Ln {{ caret.line }}, Col {{ caret.col }}</span
      >
      <span class="status-cell">{{ counts.words }} words</span>
      <span class="status-cell">{{ counts.chars }} chars</span>
      <span class="status-cell" data-testid="font-size-status"
        >Font size {{ notes.fontSize }}px</span
      >
    </footer>
  </div>
</template>
