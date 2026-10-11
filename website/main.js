// inua website: colourways, languages, the drawing, copying the commands.
// Shared by the pages; each part runs only where its elements are.
(() => {
  // The colourways; their tokens are in style.css. These are only for the
  // swatches.
  const WAYS = {
    washi: { k: "和紙", app: "#F2EDE2", line: "#D6CFBF", accent: "#2F4A6D" },
    aojashin: { k: "青写真", app: "#F1F4F8", line: "#C8D4E4", accent: "#2463AE" },
    yoru: { k: "夜", app: "#0B1012", line: "#243134", accent: "#9FC3C9" },
    suimen: { k: "水面", app: "#041417", line: "#12403F", accent: "#22F2D0" },
    rinko: { k: "燐光", app: "#03100E", line: "#0F3A32", accent: "#7CFFC4" },
    bunko: { k: "分光", app: "#07080F", line: "#1C2342", accent: "#1FE5FF" },
    denko: { k: "電光", app: "#0B0A1A", line: "#2A2046", accent: "#4DF3FF" },
  };
  const root = document.documentElement, ways = document.getElementById("ways");

  // remember: the user chose it (saved); otherwise it only applies now.
  // Light or dark, for the screenshots taken in each (data-tone).
  const LIGHT = { washi: 1, aojashin: 1 };
  function use(name, remember) {
    root.dataset.way = name;
    root.dataset.tone = LIGHT[name] ? "light" : "dark";
    document.querySelector('meta[name="theme-color"]').content = WAYS[name].app;
    if (ways) for (const b of ways.children) b.classList.toggle("on", b.dataset.w === name);
    if (remember) try { localStorage.setItem("inua-way", name); } catch (e) {}
  }
  for (const [name, w] of Object.entries(ways ? WAYS : {})) {
    const b = document.createElement("button");
    b.type = "button"; b.className = "way"; b.dataset.w = name;
    b.innerHTML = `<i style="background:${w.app};border-color:${w.line};--a:${w.accent}"></i>${w.k}<small>${name}</small>`;
    b.addEventListener("click", () => use(name, true));
    ways.appendChild(b);
  }
  // ?way=yoru for this visit, then a colourway the user picked, else paper
  // by day and night in dark mode, following the system as it changes.
  const dark = matchMedia("(prefers-color-scheme: dark)");
  const chosenWay = () => { try { const w = localStorage.getItem("inua-way"); return WAYS[w] ? w : null; } catch (e) { return null; } };
  const linkWay = new URLSearchParams(location.search).get("way");
  use(WAYS[linkWay] ? linkWay : chosenWay() || (dark.matches ? "yoru" : "washi"));
  dark.addEventListener("change", () => { if (!chosenWay() && !WAYS[linkWay]) use(dark.matches ? "yoru" : "washi"); });

  // --- languages (strings in i18n.js) ---
  // Chinese and Korean typefaces, loaded only when chosen.
  const FONTS = { "zh-Hant": "Noto+Sans+TC:wght@300;400;500", "zh-Hans": "Noto+Sans+SC:wght@300;400;500", ko: "Noto+Sans+KR:wght@300;400;500" };
  const ALIAS = { kr: "ko", "zh-tw": "zh-Hant", "zh-hk": "zh-Hant", "zh-mo": "zh-Hant", "zh-hant": "zh-Hant", "zh-cn": "zh-Hans", "zh-sg": "zh-Hans", "zh-hans": "zh-Hans", zh: "zh-Hans" };
  let T = I18N.en;

  function pick(tag) {
    if (!tag) return null;
    const t = tag.toLowerCase();
    if (ALIAS[t]) return ALIAS[t];
    for (const k of Object.keys(I18N)) if (k.toLowerCase() === t) return k;
    const base = t.split("-")[0];
    if (base === "zh") return /hant|tw|hk|mo/.test(t) ? "zh-Hant" : "zh-Hans";
    return I18N[base] ? base : null;
  }
  function speak(lang, remember) {
    T = I18N[lang];
    root.lang = lang;
    root.classList.remove("i18n-wait");
    // A page's own title and description: <body data-page="apps"> uses
    // apps_title and apps_desc.
    const page = document.body.dataset.page ? document.body.dataset.page + "_" : "";
    document.title = T[page + "title"];
    document.querySelector('meta[name="description"]').content = T[page + "desc"];
    document.querySelectorAll("[data-i18n]").forEach(el => { el.innerHTML = T[el.dataset.i18n]; });
    document.querySelectorAll("[data-i18n-aria]").forEach(el => el.setAttribute("aria-label", T[el.dataset.i18nAria]));
    if (FONTS[lang] && !document.getElementById("font-" + lang)) {
      const l = document.createElement("link");
      l.id = "font-" + lang; l.rel = "stylesheet";
      l.href = `https://fonts.googleapis.com/css2?family=${FONTS[lang]}&display=swap`;
      document.head.appendChild(l);
    }
    document.getElementById("langName").textContent = T.name;
    for (const li of list.children) li.setAttribute("aria-selected", li.dataset.l === lang);
    if (remember) try { localStorage.setItem("inua-lang", lang); } catch (e) {}
  }
  // The picker: a button and a listbox, with the keys a listbox has.
  const btn = document.getElementById("langBtn"), list = document.getElementById("langList");
  const codes = Object.keys(I18N);
  let active = 0;
  for (const code of codes) {
    const li = document.createElement("li");
    li.role = "option"; li.id = "lang-" + code; li.dataset.l = code; li.lang = code;
    li.textContent = I18N[code].name;
    li.addEventListener("click", () => { speak(code, true); closeList(true); });
    li.addEventListener("mousemove", () => highlight(codes.indexOf(code)));
    list.appendChild(li);
  }
  function highlight(i) {
    active = (i + codes.length) % codes.length;
    [...list.children].forEach((li, j) => li.classList.toggle("active", j === active));
    list.setAttribute("aria-activedescendant", list.children[active].id);
  }
  function openList() {
    list.classList.add("open"); btn.setAttribute("aria-expanded", "true");
    highlight(Math.max(0, codes.indexOf(root.lang))); list.focus();
  }
  function closeList(refocus) {
    list.classList.remove("open"); btn.setAttribute("aria-expanded", "false");
    if (refocus) btn.focus();
  }
  btn.addEventListener("click", () => (list.classList.contains("open") ? closeList(true) : openList()));
  btn.addEventListener("keydown", e => { if (e.key === "ArrowDown" || e.key === "ArrowUp") { e.preventDefault(); openList(); } });
  list.addEventListener("keydown", e => {
    const keys = { ArrowDown: () => highlight(active + 1), ArrowUp: () => highlight(active - 1),
      Home: () => highlight(0), End: () => highlight(codes.length - 1),
      Enter: () => { speak(codes[active], true); closeList(true); }, " ": () => { speak(codes[active], true); closeList(true); },
      Escape: () => closeList(true), Tab: () => closeList(false) };
    if (keys[e.key]) { if (e.key !== "Tab") e.preventDefault(); keys[e.key](); }
  });
  document.addEventListener("click", e => { if (!e.target.closest("#lang")) closeList(false); });
  // ?lang= for this visit, then a language the user picked, then the
  // browser's languages in order. Detection is never saved, so the page
  // follows the browser, and changes with it, until the user picks one.
  const browserLang = () => {
    for (const l of navigator.languages || [navigator.language]) { const k = pick(l); if (k) return k; }
    return "en";
  };
  const chosenLang = () => { try { return pick(localStorage.getItem("inua-lang")); } catch (e) { return null; } };
  speak(pick(new URLSearchParams(location.search).get("lang")) || chosenLang() || browserLang());
  addEventListener("languagechange", () => { if (!chosenLang()) speak(browserLang()); });

  // The clock in the drawing.
  const clock = document.getElementById("clock");
  if (clock) {
  const tick = () => {
    const d = new Date();
    const day = d.toLocaleDateString("en-US", { weekday: "short", month: "short", day: "numeric" }).replace(",", "").toUpperCase();
    clock.textContent = `${day}  ${String(d.getHours()).padStart(2, "0")}:${String(d.getMinutes()).padStart(2, "0")}`;
  };
  tick(); setInterval(tick, 20000);
  }

  // Now and then, focus moves to the other window, as it would under your hands.
  if (clock && !matchMedia("(prefers-reduced-motion: reduce)").matches) {
    const ws = document.getElementById("wsOn"), a = document.getElementById("focusA"), b = document.getElementById("focusB");
    let right = false;
    setInterval(() => {
      right = !right;
      ws.style.transform = right ? "translateX(9px)" : "none";
      a.setAttribute("opacity", right ? 0 : 1);
      b.setAttribute("opacity", right ? 1 : 0);
    }, 6000);
  }

  // Copy the commands.
  const copy = document.getElementById("copy");
  if (copy) copy.addEventListener("click", async () => {
    // The commands without the comment lines.
    const text = document.getElementById("cmds").innerText.split("\n").filter(l => l.trim() && !l.trim().startsWith("#")).join("\n");
    try { await navigator.clipboard.writeText(text); copy.textContent = T.copied; setTimeout(() => (copy.textContent = T.copy), 1600); } catch (e) {}
  });

  // Sections settle in as they come into view.
  const io = new IntersectionObserver(es => es.forEach(e => { if (e.isIntersecting) { e.target.classList.add("in"); io.unobserve(e.target); } }), { threshold: 0.15 });
  document.querySelectorAll(".rv").forEach(el => io.observe(el));

  // The apps page's stage: its windows drift at their own depths as the
  // page scrolls by.
  const stage = document.querySelector(".stage");
  if (stage && !matchMedia("(prefers-reduced-motion: reduce)").matches) {
    const wins = [...stage.querySelectorAll("[data-depth]")];
    let queued = false;
    const drift = () => {
      queued = false;
      const r = stage.getBoundingClientRect();
      const t = (r.top + r.height / 2 - innerHeight / 2) / innerHeight; // 0 when centred
      for (const w of wins) w.style.setProperty("--drift", (t * w.dataset.depth * -60).toFixed(1) + "px");
    };
    addEventListener("scroll", () => { if (!queued) { queued = true; requestAnimationFrame(drift); } }, { passive: true });
    drift();
  }
})();
