import { nextTick } from "vue";
import { EventsOn } from "../wailsjs/runtime/runtime";
import { UIAck } from "../wailsjs/go/main/App";
import { ui, notes } from "./store";

// The Go REST server sends a "ui:command"; we perform it against the REAL DOM
// (click the button, press the key, type), let Vue re-render, then report the
// resulting on-screen state back via UIAck. This is what lets an external agent
// actually operate the UI.

interface UICommand {
  id: string;
  type: "state" | "press" | "dblclick" | "key" | "input";
  testid?: string;
  key?: string;
  value?: string;
}

function el(testid: string): HTMLElement | null {
  return document.querySelector(`[data-testid="${testid}"]`);
}

function text(testid: string): string | undefined {
  const node = el(testid);
  return node ? (node.textContent ?? "").trim() : undefined;
}

// Turn a key string into a KeyboardEventInit. Plain keys pass through
// unchanged ("Enter", "Escape"), while combos carry modifier prefixes so an
// agent can trigger the app's shortcuts exactly as the ax tree advertises them
// ("Ctrl+N", "Ctrl+Shift+S"). Accepted prefixes: Ctrl/Control, Alt, Shift,
// Meta/Cmd/Command. The last segment is the actual key; everything before a
// separator (+ or -) is a modifier. The lookahead keeps a trailing "-"/"+"
// (e.g. "Ctrl+-" to shrink the font) intact instead of splitting on it.
function parseKey(combo: string): KeyboardEventInit {
  const init: KeyboardEventInit = { bubbles: true };
  const parts = combo.split(/[+-](?=.)/);
  const key = parts.pop() ?? combo;
  for (const raw of parts) {
    switch (raw.trim().toLowerCase()) {
      case "ctrl":
      case "control":
        init.ctrlKey = true;
        break;
      case "alt":
      case "option":
        init.altKey = true;
        break;
      case "shift":
        init.shiftKey = true;
        break;
      case "meta":
      case "cmd":
      case "command":
      case "super":
        init.metaKey = true;
        break;
    }
  }
  init.key = key;
  return init;
}

// A snapshot of what is currently on screen, read from the rendered DOM.
function collectState() {
  const controls = Array.from(document.querySelectorAll("[data-testid]"))
    .map((e) => e.getAttribute("data-testid"))
    .filter((v): v is string => !!v);

  const state: Record<string, unknown> = {
    view: ui.view,
    theme: ui.theme,
    opacity: ui.opacity,
    fontFamily: notes.fontFamily,
    fontSize: notes.fontSize,
    controls, // every testid currently clickable on screen
  };

  // The active document's text and the open tabs — the notepad's core state.
  const editor = el("editor") as HTMLTextAreaElement | null;
  if (editor) state.text = editor.value;
  state.tabs = notes.tabs.map((t) => ({
    name: t.name,
    path: t.path,
    dirty: t.dirty,
    active: t.id === notes.activeId,
  }));

  const status = text("status");
  if (status !== undefined) state.serverStatus = status;
  const apiUrl = text("api-url");
  if (apiUrl !== undefined) state.apiUrl = apiUrl;

  const allowlist = el("allowlist");
  if (allowlist) {
    state.allowlist = Array.from(
      allowlist.querySelectorAll("td.mono"),
    ).map((td) => (td.textContent ?? "").trim());
  }

  const inputs: Record<string, string> = {};
  document.querySelectorAll("input[data-testid]").forEach((n) => {
    const t = n.getAttribute("data-testid");
    if (t) inputs[t] = (n as HTMLInputElement).value;
  });
  if (Object.keys(inputs).length) state.inputs = inputs;

  state.winH = window.innerHeight;
  const pb = document.querySelector(".panel-body") as HTMLElement | null;
  if (pb) {
    state.panelOverflow = pb.scrollHeight - pb.clientHeight;
    state.neededHeight = Math.ceil(
      pb.getBoundingClientRect().top + pb.scrollHeight,
    );
  }

  return state;
}

// A structured, application-level failure the Go side maps to an HTTP status.
interface UIError {
  code: "unknown_testid" | "disabled_control";
  message: string;
}

function isDisabled(node: HTMLElement): boolean {
  return (
    node.hasAttribute("disabled") ||
    (node as HTMLButtonElement).disabled === true ||
    node.getAttribute("aria-disabled") === "true"
  );
}

// settle waits for the DOM to finish updating: one microtask flush (nextTick)
// for synchronous changes, then — if a handler started async work — until the
// shared busy counter drains, bounded so a stuck promise can't hang the bridge.
async function settle() {
  await nextTick();
  const deadline = performance.now() + 2500;
  while (ui.busy > 0 && performance.now() < deadline) {
    await new Promise((r) => window.setTimeout(r, 10));
  }
  await nextTick();
}

// perform runs the command and returns a structured error when the target
// control is missing or disabled, so the caller learns exactly what went wrong.
async function perform(cmd: UICommand): Promise<UIError | undefined> {
  let error: UIError | undefined;

  if (cmd.type === "press") {
    const node = cmd.testid ? el(cmd.testid) : null;
    if (!node) {
      error = { code: "unknown_testid", message: `unknown testid: ${cmd.testid}` };
    } else if (isDisabled(node)) {
      error = { code: "disabled_control", message: `control is disabled: ${cmd.testid}` };
    } else {
      node.click();
    }
  } else if (cmd.type === "dblclick") {
    const node = cmd.testid ? el(cmd.testid) : null;
    if (!node) {
      error = { code: "unknown_testid", message: `unknown testid: ${cmd.testid}` };
    } else if (isDisabled(node)) {
      error = { code: "disabled_control", message: `control is disabled: ${cmd.testid}` };
    } else {
      // Reproduce a real double-click end to end: mousedown/up + click for each
      // of the two presses, then a dblclick. This way handlers of every style
      // (mousedown-timing, click-timing, detail, native dblclick) react exactly
      // as they would to a physical double-click.
      for (const detail of [1, 2]) {
        node.dispatchEvent(new MouseEvent("mousedown", { bubbles: true, detail }));
        node.dispatchEvent(new MouseEvent("mouseup", { bubbles: true, detail }));
        node.dispatchEvent(new MouseEvent("click", { bubbles: true, detail }));
      }
      node.dispatchEvent(new MouseEvent("dblclick", { bubbles: true, detail: 2 }));
    }
  } else if (cmd.type === "key" && cmd.key) {
    window.dispatchEvent(new KeyboardEvent("keydown", parseKey(cmd.key)));
  } else if (cmd.type === "input") {
    const node = cmd.testid
      ? (el(cmd.testid) as HTMLInputElement | HTMLTextAreaElement | HTMLSelectElement | null)
      : null;
    if (!node) {
      error = { code: "unknown_testid", message: `unknown testid: ${cmd.testid}` };
    } else if (isDisabled(node)) {
      error = { code: "disabled_control", message: `control is disabled: ${cmd.testid}` };
    } else if (node instanceof HTMLSelectElement) {
      // A <select> takes its value directly; change is the event it listens to.
      node.value = cmd.value ?? "";
      node.dispatchEvent(new Event("change", { bubbles: true }));
      node.dispatchEvent(new Event("input", { bubbles: true }));
    } else {
      // Use the native value setter for the element's own prototype (input vs
      // textarea) so the framework's reactive tracking sees the change, then
      // fire the events the handler listens to.
      const proto =
        node instanceof HTMLTextAreaElement
          ? HTMLTextAreaElement.prototype
          : HTMLInputElement.prototype;
      const setter = Object.getOwnPropertyDescriptor(proto, "value")?.set;
      setter ? setter.call(node, cmd.value ?? "") : (node.value = cmd.value ?? "");
      node.dispatchEvent(new Event("input", { bubbles: true }));
      node.dispatchEvent(new Event("change", { bubbles: true }));
    }
  }

  await settle();
  return error;
}

// Commands are serialized: a busy bridge queues the next one so two DOM
// mutations never overlap and corrupt each other's read-back.
let queue: Promise<void> = Promise.resolve();

async function run(cmd: UICommand) {
  let error: UIError | undefined;
  try {
    error = await perform(cmd);
  } catch {
    /* still report whatever is on screen */
  }
  const state = collectState();
  if (error) state.error = error;
  try {
    await UIAck(cmd.id, JSON.stringify(state));
  } catch {
    /* nothing we can do; the Go side will time out */
  }
}

function handle(cmd: UICommand) {
  queue = queue.then(() => run(cmd));
}

export function initUIBridge() {
  EventsOn("ui:command", (cmd: UICommand) => {
    handle(cmd);
  });
}
