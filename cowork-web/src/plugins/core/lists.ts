import {
  BulletList,
  ListItem,
  OrderedList,
  TaskItem,
  TaskList,
} from "@tiptap/extension-list";

import type { Plugin } from "../registry";

/** Bulleted and numbered lists, and the to-do list of checkable items. */
export const listsPlugin: Plugin = {
  name: "core/lists",
  extensions: [
    BulletList,
    OrderedList,
    ListItem,
    TaskList,
    TaskItem.configure({ nested: true }),
  ],
  blocks: [
    {
      id: "bullet-list",
      node: "bulletList",
      label: "Bulleted list",
      insert: (editor) => editor.chain().focus().toggleBulletList().run(),
    },
    {
      id: "ordered-list",
      node: "orderedList",
      label: "Numbered list",
      insert: (editor) => editor.chain().focus().toggleOrderedList().run(),
    },
    {
      id: "to-do",
      node: "taskList",
      label: "To-do list",
      insert: (editor) => editor.chain().focus().toggleTaskList().run(),
    },
  ],
};
