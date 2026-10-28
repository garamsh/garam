// Package goja implements collab.DocEngine by running the Yjs
// reference implementation inside goja, a JavaScript engine written in
// Go, so the binary stays pure Go and merges by the clients' own rules
// (ADR-0127). The script is engine.gen.js, which cowork/engine builds.
package goja

import (
	crand "crypto/rand"
	_ "embed"
	"encoding/json"
	"sync"

	"github.com/dop251/goja"

	"github.com/garamsh/garam/cowork/internal/collab"
	"github.com/garamsh/garam/lib/errorx"
)

//go:embed engine.gen.js
var bundle string

// Engine is one goja runtime holding the bundle. A runtime runs one
// call at a time, so every call holds mu.
type Engine struct {
	mu  sync.Mutex
	vm  *goja.Runtime
	fns map[string]goja.Callable
}

var _ collab.DocEngine = (*Engine)(nil)

// engineFunctions are the functions the bundle defines on
// globalThis.engine that this package calls.
var engineFunctions = []string{
	"merge", "stateVector", "diff", "blocks",
	"open", "close", "apply", "state", "clientStateVector",
	"appendBlock", "insertBlock", "replaceBlock", "deleteBlock", "insertText",
}

// New loads the bundle into a fresh runtime.
func New() (*Engine, error) {
	vm := goja.New()
	// lib0, which Yjs generates client IDs with, reads the Web Crypto
	// API, which goja does not provide.
	webCrypto := vm.NewObject()
	if err := webCrypto.Set("getRandomValues", func(call goja.FunctionCall) goja.Value {
		arg := call.Argument(0)
		_, _ = crand.Read(bytesOf(vm, arg))
		return arg
	}); err != nil {
		return nil, errorx.Wrap(err, "yjs engine", errorx.KindInternal)
	}
	if err := vm.Set("crypto", webCrypto); err != nil {
		return nil, errorx.Wrap(err, "yjs engine", errorx.KindInternal)
	}
	if _, err := vm.RunString(bundle); err != nil {
		return nil, errorx.Wrap(err, "yjs engine: load bundle", errorx.KindInternal)
	}
	obj := vm.Get("engine")
	if obj == nil || goja.IsUndefined(obj) {
		return nil, errorx.New(errorx.KindInternal, "yjs engine: the bundle defines no engine")
	}
	e := &Engine{vm: vm, fns: map[string]goja.Callable{}}
	for _, name := range engineFunctions {
		fn, ok := goja.AssertFunction(obj.ToObject(vm).Get(name))
		if !ok {
			return nil, errorx.New(errorx.KindInternal, "yjs engine: the bundle defines no "+name)
		}
		e.fns[name] = fn
	}
	return e, nil
}

// call runs one bundle function. A JavaScript exception is the input's
// fault: the bundle throws on bytes Yjs cannot decode, and on nothing
// else it is handed.
func (e *Engine) call(name string, args ...goja.Value) (goja.Value, error) {
	v, err := e.fns[name](goja.Undefined(), args...)
	if err != nil {
		return nil, errorx.Wrap(err, "yjs "+name, errorx.KindInvalidArgument)
	}
	return v, nil
}

// u8 copies b into a new Uint8Array, so the script never holds memory
// the caller goes on to change.
func (e *Engine) u8(b []byte) (goja.Value, error) {
	ctor := e.vm.Get("Uint8Array")
	v, err := e.vm.New(ctor, e.vm.ToValue(e.vm.NewArrayBuffer(append([]byte(nil), b...))))
	if err != nil {
		return nil, errorx.Wrap(err, "yjs engine: Uint8Array", errorx.KindInternal)
	}
	return v, nil
}

// bytesOf is the memory a Uint8Array views, shared with the script.
func bytesOf(vm *goja.Runtime, v goja.Value) []byte {
	o := v.ToObject(vm)
	buf, ok := o.Get("buffer").Export().(goja.ArrayBuffer)
	if !ok {
		return nil
	}
	off, n := o.Get("byteOffset").ToInteger(), o.Get("byteLength").ToInteger()
	return buf.Bytes()[off : off+n]
}

// copyOut copies a Uint8Array the script answered out of its memory.
func (e *Engine) copyOut(v goja.Value) []byte {
	return append([]byte(nil), bytesOf(e.vm, v)...)
}

// bytesCall runs a function whose arguments are all byte strings and
// whose answer is one.
func (e *Engine) bytesCall(name string, args ...[]byte) ([]byte, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	values := make([]goja.Value, len(args))
	for i, a := range args {
		v, err := e.u8(a)
		if err != nil {
			return nil, err
		}
		values[i] = v
	}
	out, err := e.call(name, values...)
	if err != nil {
		return nil, err
	}
	return e.copyOut(out), nil
}

// Merge implements collab.DocEngine.
func (e *Engine) Merge(updates [][]byte) ([]byte, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	values := make([]any, len(updates))
	for i, u := range updates {
		v, err := e.u8(u)
		if err != nil {
			return nil, err
		}
		values[i] = v
	}
	out, err := e.call("merge", e.vm.NewArray(values...))
	if err != nil {
		return nil, err
	}
	return e.copyOut(out), nil
}

// StateVector implements collab.DocEngine.
func (e *Engine) StateVector(state []byte) ([]byte, error) {
	return e.bytesCall("stateVector", state)
}

// Diff implements collab.DocEngine.
func (e *Engine) Diff(state, stateVector []byte) ([]byte, error) {
	return e.bytesCall("diff", state, stateVector)
}

// Blocks implements collab.DocEngine.
func (e *Engine) Blocks(state []byte) ([]collab.Block, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	v, err := e.u8(state)
	if err != nil {
		return nil, err
	}
	out, err := e.call("blocks", v)
	if err != nil {
		return nil, err
	}
	var blocks []collab.Block
	if err := json.Unmarshal([]byte(out.String()), &blocks); err != nil {
		return nil, errorx.Wrap(err, "yjs blocks", errorx.KindInternal)
	}
	return blocks, nil
}

// Client is a document held in the engine and edited as an editor
// edits one: each edit answers the update it made, to be sent on. The
// server edits through one for an AI's changes, and tests drive one
// as a browser would.
type Client struct {
	e      *Engine
	handle goja.Value
}

// NewClient opens a document holding state, which may be empty.
func (e *Engine) NewClient(state []byte) (*Client, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	v, err := e.u8(state)
	if err != nil {
		return nil, err
	}
	h, err := e.call("open", v)
	if err != nil {
		return nil, err
	}
	return &Client{e: e, handle: h}, nil
}

// Open implements collab.DocEngine.
func (e *Engine) Open(state []byte) (collab.Editor, error) { return e.NewClient(state) }

var _ collab.Editor = (*Client)(nil)

// Close releases the document.
func (c *Client) Close() {
	c.e.mu.Lock()
	defer c.e.mu.Unlock()
	_, _ = c.e.call("close", c.handle)
}

// Apply merges an update into the document.
func (c *Client) Apply(update []byte) error {
	c.e.mu.Lock()
	defer c.e.mu.Unlock()
	v, err := c.e.u8(update)
	if err != nil {
		return err
	}
	_, err = c.e.call("apply", c.handle, v)
	return err
}

// State answers the whole document as one update.
func (c *Client) State() ([]byte, error) {
	return c.handleCall("state")
}

// StateVector answers what the document holds.
func (c *Client) StateVector() ([]byte, error) {
	return c.handleCall("clientStateVector")
}

func (c *Client) handleCall(name string) ([]byte, error) {
	c.e.mu.Lock()
	defer c.e.mu.Unlock()
	out, err := c.e.call(name, c.handle)
	if err != nil {
		return nil, err
	}
	return c.e.copyOut(out), nil
}

// blockCall runs a block edit whose last argument is b as JSON.
func (c *Client) blockCall(name string, b collab.Block, args ...any) ([]byte, error) {
	raw, err := json.Marshal(b)
	if err != nil {
		return nil, errorx.Wrap(err, "yjs "+name, errorx.KindInvalidArgument)
	}
	return c.editCall(name, append(args, string(raw))...)
}

// editCall runs an edit on the client's document and answers the
// update it made.
func (c *Client) editCall(name string, args ...any) ([]byte, error) {
	c.e.mu.Lock()
	defer c.e.mu.Unlock()
	values := []goja.Value{c.handle}
	for _, a := range args {
		values = append(values, c.e.vm.ToValue(a))
	}
	out, err := c.e.call(name, values...)
	if err != nil {
		return nil, err
	}
	return c.e.copyOut(out), nil
}

// AppendBlock adds b after the last block and answers the update.
func (c *Client) AppendBlock(b collab.Block) ([]byte, error) { return c.blockCall("appendBlock", b) }

// InsertBlock puts b at index, moving the blocks from there on down.
func (c *Client) InsertBlock(index int, b collab.Block) ([]byte, error) {
	return c.blockCall("insertBlock", b, index)
}

// ReplaceBlock puts b in place of the block at index.
func (c *Client) ReplaceBlock(index int, b collab.Block) ([]byte, error) {
	return c.blockCall("replaceBlock", b, index)
}

// DeleteBlock removes the block at index.
func (c *Client) DeleteBlock(index int) ([]byte, error) { return c.editCall("deleteBlock", index) }

// InsertText inserts text into the block at index, offset characters
// in, and answers the update.
func (c *Client) InsertText(index, offset int, text string) ([]byte, error) {
	return c.editCall("insertText", index, offset, text)
}
