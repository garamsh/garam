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

function element(block) {
  const el = new Y.XmlElement(block.type);
  for (const [k, v] of Object.entries(block.attrs || {})) el.setAttribute(k, v);
  if (block.text) {
    const t = new Y.XmlText();
    t.insert(0, block.text);
    el.insert(0, [t]);
  }
  return el;
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

  // blocks answers a JSON array of the page's top-level blocks, each
  // its type, attributes and text.
  blocks(state) {
    const fragment = load(state).getXmlFragment(FIELD);
    return JSON.stringify(
      fragment.toArray().map((node) => ({
        type: node instanceof Y.XmlElement ? node.nodeName : "text",
        attrs: node instanceof Y.XmlElement ? node.getAttributes() : {},
        text: textOf(node),
      })),
    );
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
  appendBlock(handle, blockJSON) {
    const doc = client(handle);
    const before = Y.encodeStateVector(doc);
    doc.getXmlFragment(FIELD).push([element(JSON.parse(blockJSON))]);
    return Y.encodeStateAsUpdate(doc, before);
  },
  insertText(handle, blockIndex, offset, text) {
    const doc = client(handle);
    const before = Y.encodeStateVector(doc);
    const block = doc.getXmlFragment(FIELD).get(blockIndex);
    const t = block.toArray().find((n) => n instanceof Y.XmlText);
    t.insert(offset, text);
    return Y.encodeStateAsUpdate(doc, before);
  },
};
