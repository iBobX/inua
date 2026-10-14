# A workshop app: inua draws the window this returns, in its look, and
# sends what the user does back as events. See docs/workshop.md.
from inua import app, ui


@app.init
def start():
    app.state["count"] = app.store.get("count", 0)


@app.view
def view(state):
    name = state.get("name") or "world"
    return ui.page(
        ui.section(
            "Hello",
            ui.row("Hello, %s" % name, sub="Type a name below; the view follows it."),
            ui.entry("name", placeholder="Your name"),
        ),
        ui.section(
            "Counter",
            ui.row("Clicked %d times" % state.get("count", 0), sub="Kept by inua between runs",
                   end=[ui.button("reset", icon="edit-undo-symbolic", style="plain", tooltip="Reset")]),
        ),
        ui.bar(ui.button("add", "Click me", style="primary")),
    )


@app.on("add")
def add(ev):
    app.state["count"] = app.state.get("count", 0) + 1
    app.store["count"] = app.state["count"]


@app.on("reset")
def reset(ev):
    app.state["count"] = 0
    app.store["count"] = 0


app.run()
