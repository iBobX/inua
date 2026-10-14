// A workshop app: inua draws the window this returns, in its look, and
// sends what the user does back as events. See docs/workshop.md.
// With Bun instead of Node: run = ["bun", "main.js"] in app.toml.
const { app, ui } = require("inua");

app.init(async () => {
  app.state.count = await app.store.get("count", 0);
});

app.view(state => ui.page(
  ui.section("Hello",
    ui.row(`Hello, ${state.name || "world"}`, { sub: "Type a name below; the view follows it." }),
    ui.entry("name", { placeholder: "Your name" })),
  ui.section("Counter",
    ui.row(`Clicked ${state.count} times`, {
      sub: "Kept by inua between runs",
      end: [ui.button("reset", { icon: "edit-undo-symbolic", style: "plain", tooltip: "Reset" })],
    })),
  ui.bar(ui.button("add", "Click me", { style: "primary" })),
));

app.on("add", async () => {
  app.state.count += 1;
  await app.store.set("count", app.state.count);
});

app.on("reset", async () => {
  app.state.count = 0;
  await app.store.set("count", 0);
});

app.run();
