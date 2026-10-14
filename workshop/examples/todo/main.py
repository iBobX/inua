# Todo: a workshop example. Things to do, kept in the app's store; Enter
# adds one, a click marks it done, the bin asks before deleting.
from inua import app, ui

SHOW = ["All", "Open", "Done"]


def save():
    app.store["todos"] = app.state["todos"]


@app.init
def start():
    app.state["todos"] = app.store.get("todos", [])


@app.view
def view(state):
    todos = state["todos"]
    show = state.get("show") or "All"
    left = sum(1 for t in todos if not t["done"])
    rows = []
    for i, t in enumerate(todos):
        if (show == "Open" and t["done"]) or (show == "Done" and not t["done"]):
            continue
        rows.append(ui.row(
            t["text"], sub="Done" if t["done"] else None, id="toggle:%d" % i,
            icon="object-select-symbolic" if t["done"] else None,
            end=[ui.button("copy:%d" % i, icon="edit-copy-symbolic", style="plain", tooltip="Copy"),
                 ui.button("delete:%d" % i, icon="user-trash-symbolic", style="plain", tooltip="Delete")]))
    if not rows:
        rows = [ui.empty("Nothing here", text="Type above and press Enter.", icon="object-select-symbolic")]
    return ui.page(
        ui.entry("new", placeholder="A thing to do, then Enter"),
        ui.section("To do", *rows, meta="%d left of %d" % (left, len(todos))),
        ui.bar(ui.dropdown("show", SHOW, value=show),
               ui.button("clear", "Clear done", style="danger")),
    )


@app.on("new", "submit")
def add(ev):
    text = (ev.value or "").strip()
    if text:
        app.state["todos"].append({"text": text, "done": False})
        save()
    app.set("new", "")


@app.on("toggle:*", "click")
def toggle(ev):
    t = app.state["todos"][int(ev.id.split(":")[1])]
    t["done"] = not t["done"]
    save()


@app.on("copy:*")
def copy(ev):
    app.call("clipboard.copy", text=app.state["todos"][int(ev.id.split(":")[1])]["text"])


@app.on("delete:*")
def delete(ev):
    i = int(ev.id.split(":")[1])
    text = app.state["todos"][i]["text"]
    if app.call("dialog.confirm", title="Delete “%s”?" % text, ok="Delete", danger=True):
        del app.state["todos"][i]
        save()


@app.on("clear")
def clear(ev):
    done = [t for t in app.state["todos"] if t["done"]]
    if not done:
        return
    if app.call("dialog.confirm", title="Clear %d done?" % len(done), text="They go for good.", ok="Clear", danger=True):
        app.state["todos"] = [t for t in app.state["todos"] if not t["done"]]
        save()
        app.call("notify", title="Todo", body="Cleared %d done" % len(done))


app.run()
