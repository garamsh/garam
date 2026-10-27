// The functions cowork's server calls on a Yjs document (ADR-0127).
// Every argument and result that is a document or an update is a
// Uint8Array in Yjs's update encoding; the Go adapter converts at the
// boundary. A page's content is the XmlFragment TipTap's collaboration
// extension writes, named "default", whose top-level elements are the
// page's blocks.
import * as Y from "yjs";

const FIELD = "default";

function load(state) {
  const doc = new Y.Doc();
  if (state && state.length > 0) Y.applyUpdate(doc, state);
  return doc;
}

// textOf is a node's text with its formatting dropped. XmlText's
// toString renders formatting as markup, which text holding a "<"
// cannot be told apart from, so the delta is read instead.
function textOf(node) {
  if (node instanceof Y.XmlText) {
    return node
      .toDelta()
      .map((d) => (typeof d.insert === "string" ? d.insert : ""))
      .join("");
  }
  return node.toArray().map(textOf).join("");
}

function text(value) {
  const t = new Y.XmlText();
  if (value) t.insert(0, value);
  return t;
}

function wrap(name, children, attrs = {}) {
  const el = new Y.XmlElement(name);
  for (const [k, v] of Object.entries(attrs)) el.setAttribute(k, v);
  if (children.length > 0) el.insert(0, children);
  return el;
}

const paragraph = (value) => wrap("paragraph", [text(value)]);

// LISTS name the list types and what each of their items is.
const LISTS = { bulletList: "listItem", orderedList: "listItem", taskList: "taskItem" };

// element builds a block as the editor's schema nests it: a list holds
// items that hold paragraphs, and a quote holds paragraphs, so a block
// written here opens in the editor as one typed there.
function element(block) {
  const attrs = block.attrs || {};
  if (block.type in LISTS) {
    const items = (block.items || []).map((item) =>
      wrap(LISTS[block.type], [paragraph(item.text)], block.type === "taskList" ? { checked: !!item.checked } : {}),
    );
    return wrap(block.type, items, attrs);
  }
  switch (block.type) {
    case "blockquote":
      return wrap("blockquote", [paragraph(block.text)], attrs);
    case "horizontalRule":
    case "image":
      return wrap(block.type, [], attrs);
    default:
      return wrap(block.type, [text(block.text)], attrs);
  }
}

// read is a block as a reader takes it: its type, attributes and text,
// and for a list, its items.
function read(node) {
  if (!(node instanceof Y.XmlElement)) return { type: "text", attrs: {}, text: textOf(node) };
  const out = { type: node.nodeName, attrs: node.getAttributes(), text: "" };
  if (node.nodeName in LISTS) {
    out.items = node.toArray().map((item) => {
      const entry = { text: textOf(item) };
      if (node.nodeName === "taskList") entry.checked = item.getAttribute("checked") === true;
      return entry;
    });
    out.text = out.items.map((i) => i.text).join("\n");
  } else if (node.nodeName === "blockquote") {
    out.text = node.toArray().map(textOf).join("\n");
  } else {
    out.text = textOf(node);
  }
  return out;
}

// Clients the server edits through, and tests drive as editors would.
// Each is a document held in this engine until closed.
const clients = new Map();
let nextClient = 1;

function client(handle) {
  const doc = clients.get(handle);
  if (!doc) throw new Error("no client " + handle);
  return doc;
}

globalThis.engine = {
  // The update operations: none decodes a document.
  merge: (updates) => Y.mergeUpdates(updates),
  stateVector: (state) => Y.encodeStateVectorFromUpdate(state),
  diff: (state, stateVector) => Y.diffUpdate(state, stateVector),

  // blocks answers a JSON array of the page's top-level blocks.
  blocks(state) {
    return JSON.stringify(load(state).getXmlFragment(FIELD).toArray().map(read));
  },

  open(state) {
    const handle = nextClient++;
    clients.set(handle, load(state));
    return handle;
  },
  close(handle) {
    clients.delete(handle);
  },
  apply(handle, update) {
    Y.applyUpdate(client(handle), update);
  },
  state: (handle) => Y.encodeStateAsUpdate(client(handle)),
  clientStateVector: (handle) => Y.encodeStateVector(client(handle)),
  // The edits answer the update each made, for the caller to send.
  // An index past the last block is refused, as Yjs refuses one.
  appendBlock(handle, blockJSON) {
    return edit(handle, (f) => f.push([element(JSON.parse(blockJSON))]));
  },
  insertBlock(handle, index, blockJSON) {
    return edit(handle, (f) => f.insert(index, [element(JSON.parse(blockJSON))]));
  },
  replaceBlock(handle, index, blockJSON) {
    return edit(handle, (f) => {
      if (index >= f.length) throw new Error("no block " + index);
      f.delete(index, 1);
      f.insert(index, [element(JSON.parse(blockJSON))]);
    });
  },
  deleteBlock(handle, index) {
    return edit(handle, (f) => {
      if (index >= f.length) throw new Error("no block " + index);
      f.delete(index, 1);
    });
  },
  insertText(handle, blockIndex, offset, value) {
    return edit(handle, (f) => {
      const t = f
        .get(blockIndex)
        .toArray()
        .find((n) => n instanceof Y.XmlText);
      t.insert(offset, value);
    });
  },
};

// edit runs change on a client's page in one transaction and answers
// the update it made.
function edit(handle, change) {
  const doc = client(handle);
  const before = Y.encodeStateVector(doc);
  doc.transact(() => change(doc.getXmlFragment(FIELD)));
  return Y.encodeStateAsUpdate(doc, before);
}
