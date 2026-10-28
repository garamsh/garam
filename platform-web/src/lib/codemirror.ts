import {
  defaultKeymap,
  history,
  historyKeymap,
  indentWithTab,
} from "@codemirror/commands";
import { json, jsonParseLinter } from "@codemirror/lang-json";
import {
  HighlightStyle,
  bracketMatching,
  ensureSyntaxTree,
  indentOnInput,
  syntaxHighlighting,
} from "@codemirror/language";
import {
  linter,
  lintGutter,
  setDiagnostics,
  type Diagnostic,
} from "@codemirror/lint";
import { Compartment, EditorState, type Extension } from "@codemirror/state";
import {
  EditorView,
  highlightActiveLine,
  highlightActiveLineGutter,
  keymap,
  lineNumbers,
  placeholder as placeholderText,
} from "@codemirror/view";
import { tags } from "@lezer/highlight";
import type { SyntaxNode } from "@lezer/common";

import { pointerTokens } from "@/lib/json-pointer";

/**
 * The console's JSON editor, built on CodeMirror 6 and loaded only in
 * the browser. Colours come from the design tokens, so the editor
 * follows the theme without an extension of its own: the accent marks
 * keys and everything else stays neutral, since status colours carry
 * meaning only (ADR-0103).
 */

const theme = EditorView.theme({
  "&": {
    color: "var(--foreground)",
    backgroundColor: "transparent",
    fontSize: "var(--text-table)",
  },
  "&.cm-focused": { outline: "none" },
  ".cm-scroller": {
    fontFamily: "var(--font-mono)",
    lineHeight: "1.6",
  },
  ".cm-content": { caretColor: "var(--foreground)", padding: "8px 0" },
  ".cm-cursor, .cm-dropCursor": { borderLeftColor: "var(--foreground)" },
  "&.cm-focused .cm-selectionBackground, .cm-selectionBackground, ::selection":
    { backgroundColor: "color-mix(in oklch, var(--primary) 22%, transparent)" },
  ".cm-gutters": {
    backgroundColor: "transparent",
    color: "var(--muted-foreground)",
    border: "none",
  },
  ".cm-activeLine": {
    backgroundColor: "color-mix(in oklch, var(--muted) 70%, transparent)",
  },
  ".cm-activeLineGutter": {
    backgroundColor: "transparent",
    color: "var(--foreground)",
  },
  ".cm-matchingBracket": {
    backgroundColor: "color-mix(in oklch, var(--primary) 16%, transparent)",
    outline: "none",
  },
  ".cm-lintRange-error": {
    backgroundImage: "none",
    textDecoration: "underline wavy var(--danger)",
    textUnderlineOffset: "3px",
  },
  ".cm-lint-marker-error": { content: "none" },
  ".cm-gutter-lint": { width: "0.75rem" },
  ".cm-tooltip": {
    backgroundColor: "var(--popover)",
    color: "var(--popover-foreground)",
    border: "1px solid var(--border)",
    borderRadius: "var(--radius-lg)",
  },
  ".cm-diagnostic-error": { borderLeftColor: "var(--danger)" },
  ".cm-placeholder": { color: "var(--muted-foreground)" },
});

const highlight = HighlightStyle.define([
  { tag: tags.propertyName, color: "var(--primary)" },
  { tag: [tags.string], color: "var(--foreground)" },
  {
    tag: [tags.number, tags.bool, tags.null],
    color: "var(--foreground)",
    fontWeight: "500",
  },
  {
    tag: [tags.separator, tags.squareBracket, tags.brace, tags.punctuation],
    color: "var(--muted-foreground)",
  },
]);

/** What the editor's mount is told about the field it stands in. */
export type JsonEditorOptions = {
  parent: HTMLElement;
  doc: string;
  /** The accessible attributes of the field's control. */
  attributes: Record<string, string>;
  readOnly: boolean;
  placeholder?: string;
  onChange: (doc: string) => void;
};

/** A mounted editor, and what the field changes on it later. */
export type JsonEditorHandle = {
  view: EditorView;
  /** Replaces the text where it differs, as an edit made elsewhere. */
  setDoc(doc: string): void;
  setAttributes(attributes: Record<string, string>): void;
  /** Marks where `path` points, or clears the mark with `null`. */
  setProblem(problem: { message: string; path?: string } | null): void;
  destroy(): void;
};

/** Mounts a JSON editor into `parent`. */
export function mountJsonEditor(options: JsonEditorOptions): JsonEditorHandle {
  const attributes = new Compartment();
  const extensions: Extension[] = [
    lineNumbers(),
    highlightActiveLineGutter(),
    highlightActiveLine(),
    history(),
    bracketMatching(),
    indentOnInput(),
    json(),
    linter(jsonParseLinter(), { delay: 300 }),
    lintGutter(),
    syntaxHighlighting(highlight),
    theme,
    keymap.of([indentWithTab, ...defaultKeymap, ...historyKeymap]),
    EditorState.tabSize.of(2),
    EditorState.readOnly.of(options.readOnly),
    EditorView.editable.of(!options.readOnly),
    EditorView.lineWrapping,
    attributes.of(EditorView.contentAttributes.of(options.attributes)),
    EditorView.updateListener.of((update) => {
      if (update.docChanged) options.onChange(update.state.doc.toString());
    }),
  ];
  if (options.placeholder) {
    extensions.push(placeholderText(options.placeholder));
  }
  const view = new EditorView({
    parent: options.parent,
    state: EditorState.create({ doc: options.doc, extensions }),
  });

  return {
    view,
    setDoc(doc) {
      const current = view.state.doc.toString();
      if (current === doc) return;
      view.dispatch({ changes: { from: 0, to: current.length, insert: doc } });
    },
    setAttributes(next) {
      view.dispatch({
        effects: attributes.reconfigure(EditorView.contentAttributes.of(next)),
      });
    },
    setProblem(problem) {
      const diagnostics: Diagnostic[] = [];
      if (problem !== null) {
        const range = pointerRange(view.state, problem.path ?? "");
        diagnostics.push({
          ...range,
          severity: "error",
          message: problem.message,
        });
      }
      view.dispatch(setDiagnostics(view.state, diagnostics));
    },
    destroy() {
      view.destroy();
    },
  };
}

const VALUE_NODES = new Set([
  "Object",
  "Array",
  "String",
  "Number",
  "True",
  "False",
  "Null",
]);

/** The value nodes directly inside an array or holding a property. */
function childValues(node: SyntaxNode): SyntaxNode[] {
  const values: SyntaxNode[] = [];
  for (let child = node.firstChild; child; child = child.nextSibling) {
    if (VALUE_NODES.has(child.name)) values.push(child);
  }
  return values;
}

/** A property's value, or null while the text after the colon is missing. */
function propertyValue(property: SyntaxNode): SyntaxNode | null {
  const last = property.lastChild;
  return last !== null && VALUE_NODES.has(last.name) ? last : null;
}

/**
 * The text range a JSON Pointer leads to in the editor's document. A
 * pointer to a key the document lacks — a missing Action — leads to
 * the object that should hold it, marked at its key where it has one,
 * so the mark lands as near the place as the text allows. A pointer
 * the text cannot place marks the whole document.
 */
export function pointerRange(
  state: EditorState,
  pointer: string,
): { from: number; to: number } {
  const whole = { from: 0, to: state.doc.length };
  const tokens = pointerTokens(pointer);
  const tree = ensureSyntaxTree(state, state.doc.length, 200);
  if (tokens === null || tree === null) return whole;
  let node = childValues(tree.topNode)[0];
  if (node === undefined) return whole;
  // Where the current node is marked: its property's key once there is
  // one, since a key is shorter and easier to see than an object.
  let mark: { from: number; to: number } = { from: node.from, to: node.to };

  for (const token of tokens) {
    let next: SyntaxNode | null = null;
    let nextMark: { from: number; to: number } | null = null;
    if (node.name === "Object") {
      const property = propertyNamed(state, node, token);
      if (property !== null) {
        next = propertyValue(property.node);
        nextMark = property.key;
      }
    } else if (node.name === "Array" && /^\d+$/.test(token)) {
      next = childValues(node)[Number(token)] ?? null;
      if (next !== null) nextMark = { from: next.from, to: next.to };
    }
    if (next === null) {
      // The token is not in the text: keep the nearest place it would
      // be.
      return nextMark ?? markOpening(state, node, mark);
    }
    node = next;
    mark = nextMark ?? mark;
  }
  // A scalar is marked whole; a container at its key, or its first
  // line.
  return VALUE_NODES.has(node.name) &&
    node.name !== "Object" &&
    node.name !== "Array"
    ? { from: node.from, to: node.to }
    : markOpening(state, node, mark);
}

/**
 * The property of an object a pointer token names. garam points at a
 * known key by the model's spelling however the document cased it,
 * since the engine matches keys without regard to case, so an exact
 * spelling wins and a case-folded one is taken after it.
 */
function propertyNamed(
  state: EditorState,
  object: SyntaxNode,
  token: string,
): { node: SyntaxNode; key: { from: number; to: number } } | null {
  let folded: { node: SyntaxNode; key: { from: number; to: number } } | null =
    null;
  for (let child = object.firstChild; child; child = child.nextSibling) {
    if (child.name !== "Property") continue;
    const key = child.getChild("PropertyName");
    if (key === null) continue;
    let name: unknown;
    try {
      name = JSON.parse(state.sliceDoc(key.from, key.to));
    } catch {
      continue;
    }
    if (typeof name !== "string") continue;
    const found = { node: child, key: { from: key.from, to: key.to } };
    if (name === token) return found;
    if (folded === null && name.toLowerCase() === token.toLowerCase()) {
      folded = found;
    }
  }
  return folded;
}

/** A container's key where it has one, or else its opening line. */
function markOpening(
  state: EditorState,
  node: SyntaxNode,
  mark: { from: number; to: number },
): { from: number; to: number } {
  if (mark.from !== node.from || mark.to !== node.to) return mark;
  const line = state.doc.lineAt(node.from);
  return { from: node.from, to: Math.max(node.from + 1, line.to) };
}
