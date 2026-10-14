// Pomodoro: a workshop example in JavaScript. A timer that counts down
// with inua's timer events, and says so when it ends. Runs with Node, or
// with Bun: run = ["bun", "main.js"] in app.toml.
const { app, ui } = require("inua");

const LENGTHS = { "25 minutes": 25 * 60, "15 minutes": 15 * 60, "5 minutes": 5 * 60, "1 minute": 60 };

const clock = s => `${String(Math.floor(s / 60)).padStart(2, "0")}:${String(s % 60).padStart(2, "0")}`;

app.init(async () => {
  app.state.length = await app.store.get("length", "25 minutes");
  app.state.left = LENGTHS[app.state.length];
  app.state.running = false;
  app.state.done = await app.store.get("done", 0);
});

app.view(state => ui.page(
  ui.text(clock(state.left), { style: "heading" }),
  ui.text(state.running ? "Working" : state.left === LENGTHS[state.length] ? "Ready" : "Paused", { style: "sub" }),
  ui.section("Timer",
    ui.row("Length", { end: [ui.dropdown("length", Object.keys(LENGTHS), { value: state.length })] }),
    ui.row("Tell me when it ends", { sub: "A notification", end: [ui.switch("notify", true)] }),
    ui.row(`${state.done} done`, { sub: "Kept by inua between runs" })),
  ui.bar(ui.button("reset", "Reset"),
    ui.button("start", state.running ? "Pause" : "Start", { style: "primary" })),
));

app.on("length", "change", async ev => {
  await app.store.set("length", ev.value);
  if (!app.state.running) app.state.left = LENGTHS[ev.value];
});

app.on("start", async () => {
  app.state.running = !app.state.running;
  await app.timer("tick", app.state.running ? 1 : 0);
});

app.on("reset", async () => {
  app.state.running = false;
  await app.timer("tick", 0);
  app.state.left = LENGTHS[app.state.length];
});

app.on("tick", async () => {
  if (!app.state.running) return;
  app.state.left -= 1;
  if (app.state.left > 0) return;
  app.state.running = false;
  app.state.left = LENGTHS[app.state.length];
  app.state.done += 1;
  await app.timer("tick", 0);
  await app.store.set("done", app.state.done);
  if (app.state.notify !== false) await app.call("notify", { title: "Pomodoro", body: "Time's up: take a break." });
});

app.run();
