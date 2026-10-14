# Workshop: write your own light app

A workshop app is a folder with a manifest and a program in any
language. The program describes its window as a tree of inua's building
blocks; inua draws it in the shell's look (themes included) and tells the
program what the user does. The two speak JSON-RPC 2.0 over the
program's stdin and stdout. Python and JavaScript (Node or Bun) apps get
a small helper that comes with inua, so there is nothing to install.

Installed apps are in the launcher and in the dock's workshop drawer
(the puzzle button beside the apps' one, there once an app is
installed; `inua call drawer workshop` toggles it), by category, ending
with the Workshop app: the apps installed, to open or remove, and
installing one from a folder.

## Five minutes

```sh
inua workshop new ~/src/hello "Hello"   # an app from the template (--js for JavaScript)
inua workshop dev ~/src/hello           # run it; it starts again on every save
inua workshop install ~/src/hello       # put it in the launcher and the dock's workshop drawer
```

`main.py` of the template:

```python
from inua import app, ui

@app.view
def view(state):
    name = state.get("name") or "world"
    return ui.page(
        ui.section("Hello",
                   ui.row("Hello, %s" % name, sub="Type a name below"),
                   ui.entry("name", placeholder="Your name")),
        ui.bar(ui.button("add", "Click me", style="primary")),
    )

@app.on("add")
def add(ev):
    app.call("notify", title="Hello", body="Clicked")

app.run()
```

`state` holds every input's value by its id. After each event the view is
drawn again; inua keeps the widgets that are still there, so an entry
keeps its focus and its text while you type.

The examples in [`workshop/examples`](../workshop/examples) show more:
Todo (a list kept in the store, confirm dialogs, a notification), SSH
Hosts (two columns, a filter, the terminal) and Pomodoro (JavaScript:
timers, a drop-down, a switch).

## JavaScript

The same, with Node or Bun (`run = ["node", "main.js"]` or `["bun",
"main.js"]`); the helper takes options as an object where Python takes
keyword arguments, and calls return promises:

```js
const { app, ui, InuaError } = require("inua");

app.init(async () => { app.state.count = await app.store.get("count", 0); });

app.view(state => ui.page(
  ui.section("Hello",
    ui.row(`Hello, ${state.name || "world"}`, { sub: "Type a name below" }),
    ui.entry("name", { placeholder: "Your name" })),
  ui.bar(ui.button("add", "Click me", { style: "primary" })),
));

app.on("add", async ev => {
  app.state.count += 1;
  await app.store.set("count", app.state.count);
});

app.run();
```

`require("inua")` finds the helper on `NODE_PATH`, which inua sets; an ES
module imports it by its path:
`const { app, ui } = await import(process.env.INUA_WORKSHOP_LIB + "/node/inua.mjs")`.
Containers take their options first: `ui.page({ scroll: false }, …)`,
`ui.section("Title", { meta: "3 left" }, …)`; arrays of children are
flattened, so `rows.map(…)` goes in as it is. A desktop session's PATH
seldom has nvm's node or `~/.bun/bin`: inua looks there (and in Volta,
fnm, mise, asdf and Deno's places) when the command is not on PATH.

## The manifest: app.toml

```toml
id = "dev.example.todo"          # letters, digits, . - _ ; the folder's name once installed
name = "Todo"
description = "Things to do"
version = "0.1.0"
icon = "icon.svg"                # a file in the folder
category = "tools"               # its group in the workshop drawer, below
run = ["python3", "main.py"]     # run in the app's folder
uses = ["clipboard", "notify"]   # see Functions
width = 520                      # the window's size
height = 600
```

`category` is one of `everyday`, `tools`, `network`, `development`,
`media`, `games`, `system`; without one, or with another, the app is
under OTHER.

## Building blocks

A node is `{"type": …, "id": …, …}`; containers have `children`. In
Python, `ui.<type>(…)` makes one.

| Block | Python | Notes |
|---|---|---|
| page | `ui.page(*children, scroll=True)` | The window's content; a `bar` among the children sits at the bottom |
| columns | `ui.columns(*children)` | Side by side; a child's `width` fixes it, the others share the rest. Use with `page(scroll=False)` and `scroll` children |
| scroll | `ui.scroll(*children, width=None)` | A scrolling area |
| box | `ui.box(*children, horizontal=False, spacing=8)` | |
| section | `ui.section(title, *children, meta=None)` | Under a `[TITLE]` head, as in Settings |
| row | `ui.row(title, sub=None, id=None, icon=None, selected=False, end=[])` | With an id, a click on it is an event; `end` are widgets at its end |
| text | `ui.text(text, style=None)` | style: `heading`, `title`, `sub`, `mono` |
| facts | `ui.facts([(name, value), …])` | Names and values in two columns |
| button | `ui.button(id, label=None, icon=None, style=None, tooltip=None)` | style: `primary`, `danger`, `plain` |
| entry | `ui.entry(id, placeholder=None, value=None, password=False)` | `change` as you type, `submit` on Enter |
| search | `ui.search(id, placeholder=None, value=None)` | |
| textarea | `ui.textarea(id, value=None)` | |
| switch | `ui.switch(id, value=False)` | |
| dropdown | `ui.dropdown(id, options, value=None)` | Its value is the option chosen |
| bar | `ui.bar(*children)` | Actions at the bottom, to the right |
| empty | `ui.empty(title, text=None, icon=None)` | A page with nothing to show |

Icons are freedesktop names from inua's icon theme: `edit-copy-symbolic`,
`user-trash-symbolic`, `object-select-symbolic`, `list-add-symbolic`…
(the list is in `scripts/phosphor-icons.sh`).

An input's `value` is applied when it first appears and whenever the app
sends a different one; in between, what the user types stands. To set
an input anyway (to clear an entry after Enter): `app.set("new", "")`.

## Events

`@app.on(id)` handles the events of one id; `@app.on(id, "submit")` one
type of them; an id ending in `*` matches every id it begins
(`@app.on("delete:*")` for rows `delete:0`, `delete:1`…). The handler
gets an event with `type` (`click`, `change`, `submit`, `timer`), `id`,
`value` and `state`. `@app.init` runs once, before the first view.

## Functions

`app.call(method, **params)` asks inua and waits for the answer; an
error raises `InuaError` (with `code`). Some need the app to name them in
`uses`.

| Method | Params → result | Needs |
|---|---|---|
| `notify` | `title, body` | notify |
| `open` | `target`: a URL or a file | open |
| `clipboard.copy` | `text` | clipboard |
| `clipboard.copySecret` | `text`: kept out of clipboard histories, taken off after 45 s | clipboard |
| `terminal.open` | `argv`: in the terminal of Settings → Launcher | terminal |
| `system.run` | `argv, stdin="", timeout=30` → `{code, stdout, stderr}` | run |
| `dialog.confirm` | `title, text="", ok="OK", danger=False` → bool | |
| `store.get` / `store.set` | `key` / `key, value` | |
| `timer.set` | `id, seconds`: a `timer` event that often; 0 stops | |
| `window.setTitle` | `title` | |

Helpers: `app.store["key"]` reads and writes the store (a JSON file in
`~/.local/state/inua/workshop/<id>/`), `app.timer(id, seconds)`.

`uses` guards inua's functions. The program itself runs as you, like any
program you start: install apps you trust.

## The protocol

For other languages, or to see what goes on (`inua workshop dev` prints
every line). JSON-RPC 2.0, one message a line; stderr is the app's log.

- inua → app, request `initialize` `{protocol: 1, language, theme, app:
  {id, name}, dir}`; answer `{protocol: 1}`.
- app → inua, notification `render` `{root: <node>}`: the whole tree,
  each time.
- inua → app, notification `event` `{type, id, value?, state}`.
- app → inua, notification `state.set` `{values: {id: value}}`.
- app → inua, requests: the functions above. Errors: -32601 no such
  method, -32602 bad params, -32001 not in `uses`, -32000 failed.
- inua → app, request `shutdown` when the window closes; the app has 3
  seconds.

A shell app, to show the shape:

```sh
#!/bin/sh
while read -r line; do
  case $line in
  *'"initialize"'*)
    printf '%s\n' '{"jsonrpc":"2.0","id":1,"result":{"protocol":1}}'
    printf '%s\n' '{"jsonrpc":"2.0","method":"render","params":{"root":{"type":"page","children":[{"type":"text","text":"Hello from sh"}]}}}' ;;
  *'"shutdown"'*) exit 0 ;;
  esac
done
```

## Commands

| Command | |
|---|---|
| `inua workshop new [--js] DIR [NAME]` | An app from the template, in Python or JavaScript |
| `inua workshop dev DIR` | Run it, again on every save, printing the protocol and its stderr |
| `inua workshop run ID\|DIR` | Run it |
| `inua workshop install [-y] URL\|DIR` | Clone or copy it into `~/.local/share/inua/workshop/apps/<id>`, after showing what it uses; adds it to the launcher |
| `inua workshop remove ID` | Its folder and launcher entry to the trash (or, where there is none, to `~/.local/share/inua/workshop/removed/`); its store stays |
| `inua workshop list` | The installed apps |

`inua-apps workshop ID|DIR` is the window itself; `inua-apps workshop`
alone is the Workshop app.

## Not yet

Translating apps, charts and drawing, widgets for the bar or the control
center, a catalogue to browse, and running apps in a sandbox.
