# SSH Hosts: a workshop example. The hosts of ~/.ssh/config on the left,
# filtered by what is typed; the one chosen on the right, to connect to
# in the terminal of Settings → Launcher, or to copy its command.
import os

from inua import app, ui, InuaError


def read_hosts():
    path = os.path.expanduser("~/.ssh/config")
    hosts, cur = [], None
    try:
        lines = open(path, encoding="utf-8").read().splitlines()
    except OSError:
        return []
    for line in lines:
        line = line.strip()
        if not line or line.startswith("#"):
            continue
        key, _, value = line.partition(" ")
        key, value = key.lower(), value.strip()
        if key == "host":
            cur = None
            for name in value.split():
                if "*" not in name and "?" not in name:
                    cur = {"name": name, "hostname": name, "user": "", "port": "22"}
                    hosts.append(cur)
        elif cur is not None and key in ("hostname", "user", "port"):
            cur[key] = value
    return hosts


@app.init
def start():
    app.state["hosts"] = read_hosts()
    app.state["chosen"] = None
    app.state["note"] = ""


@app.view
def view(state):
    hosts = state["hosts"]
    if not hosts:
        return ui.page(ui.empty("No hosts", text="Hosts in ~/.ssh/config show here.", icon="pass-terminal-window-symbolic"))
    q = (state.get("q") or "").lower()
    rows = [ui.row(h["name"], sub=(h["user"] + "@" if h["user"] else "") + h["hostname"],
                   id="host:" + h["name"], selected=h["name"] == state["chosen"])
            for h in hosts if q in h["name"].lower() or q in h["hostname"].lower()]
    chosen = next((h for h in hosts if h["name"] == state["chosen"]), None)
    if chosen:
        detail = ui.box(
            ui.text(chosen["name"], style="heading"),
            ui.facts([("Host", chosen["hostname"]), ("User", chosen["user"] or "—"), ("Port", chosen["port"])]),
            ui.box(ui.button("connect", "Connect", style="primary"),
                   ui.button("copy", "Copy command", icon="edit-copy-symbolic"), horizontal=True),
            ui.text(state["note"], style="sub"),
            spacing=14)
    else:
        detail = ui.empty("Choose a host", icon="pass-terminal-window-symbolic")
    return ui.page(
        ui.columns(
            ui.scroll(ui.search("q", placeholder="Filter"), *(rows or [ui.text("No host matches.", style="sub")]), width=260),
            ui.scroll(detail),
        ),
        scroll=False,
    )


@app.on("host:*", "click")
def choose(ev):
    app.state["chosen"] = ev.id[len("host:"):]
    app.state["note"] = ""


@app.on("connect")
def connect(ev):
    try:
        app.call("terminal.open", argv=["ssh", app.state["chosen"]])
        app.state["note"] = "Opened in the terminal."
    except InuaError as e:
        app.state["note"] = str(e)


@app.on("copy")
def copy(ev):
    app.call("clipboard.copy", text="ssh " + app.state["chosen"])
    app.state["note"] = "Copied."


app.run()
