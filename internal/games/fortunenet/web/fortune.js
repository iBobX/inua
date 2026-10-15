// Small Fortune in a browser: joins the host's game over a WebSocket,
// shows its board in three.js (the host's print, pawns, buildings, dice)
// and plays the seat it is given. The host holds the game; the page shows
// what happened, one event after another, and sends its moves.

import * as THREE from 'three';
import { OrbitControls } from 'three/addons/OrbitControls.js';

const $ = (id) => document.getElementById(id);
let T = {}; // the page's words, in the host's language

// The players' colours and the streets' colour groups, the board's own.
const playerColors = ['#C23B22', '#2B4C7E', '#C88A1E', '#7B5EA7'];
const groupColors = [
  '#8A5A3A', '#6A9E73', '#6C8CAB', '#9E3D3F', '#C27D8B', '#2E3A5C', '#D49B39', '#7A4A7E',
  '#7A9758', '#D4836E', '#2F6C68', '#6E6A72', '#5C588C', '#C0485A', '#2D5D9F', '#BF4430',
  '#4A9BA8', '#8BA845', '#B5517E', '#4E5D6C', '#7D7A3A', '#8E354A', '#D3793D', '#6A4C9C',
];
const cardInks = ['#BF4430', '#3F7A3A', '#2B4C7E', '#7B5EA7', '#6E6A72', '#C88A1E', '#4A9BA8', '#C27D8B', '#2E3A5C', '#8E354A', '#D9A43A', '#5C588C'];

// fmt fills a format's verbs: %s, %d, and %[n]s for another order.
function fmt(s, ...args) {
  let i = 0;
  return String(s ?? '').replace(/%(?:\[(\d+)\])?([sd%])/g, (_, n, v) => {
    if (v === '%') return '%';
    if (n) i = Number(n) - 1;
    return String(args[i++]);
  });
}
const money = (n) => Number(n).toLocaleString('en-US');
const el = (tag, cls, text) => {
  const e = document.createElement(tag);
  if (cls) e.className = cls;
  if (text !== undefined) e.textContent = text;
  return e;
};
function store(key, value) {
  try {
    if (value === undefined) return localStorage.getItem(key);
    if (value === null) localStorage.removeItem(key);
    else localStorage.setItem(key, value);
  } catch { /* private windows: the seat is not remembered */ }
  return null;
}

// --- joining -------------------------------------------------------------

let code = location.hash.slice(1).toUpperCase().replace(/[^A-Z0-9]/g, '');
let ws = null, seat = -1, joined = false, bye = false, retry = 0, hidden = false;
const keyOf = () => `small-fortune:${code}`;

async function init() {
  try {
    T = await (await fetch('texts.json')).json();
  } catch { /* English, as written */ }
  for (const e of document.querySelectorAll('[data-t]')) {
    if (T[e.dataset.t]) e.textContent = T[e.dataset.t];
  }
  document.title = T.title || document.title;
  $('name').value = store('small-fortune:name') || '';
  $('code').hidden = !!code;
  $('code').required = !code;
  $('join-form').addEventListener('submit', (ev) => {
    ev.preventDefault();
    join($('name').value.trim());
  });
  $('watch-button').addEventListener('click', () => join(''));
  if (store(keyOf())) join(null); // back to the seat this browser had
  else $('join-form').hidden = false;
}

// join connects as name ('' to watch), or with the seat's key (null).
function join(name) {
  if (!code) code = $('code').value.trim().toUpperCase();
  if (name) store('small-fortune:name', name);
  $('join-form').hidden = true;
  connect(name ?? (store('small-fortune:name') || ''));
}

function connect(name) {
  ws = new WebSocket(`${location.protocol === 'https:' ? 'wss' : 'ws'}://${location.host}/ws`);
  ws.onopen = () => {
    retry = 0;
    ws.send(JSON.stringify({ t: 'join', code, name, key: store(keyOf()) || '' }));
  };
  ws.onmessage = (ev) => receive(JSON.parse(ev.data));
  ws.onclose = () => {
    if (bye || hidden) return;
    notice(T.reconnect || 'Connection lost: trying again…');
    setTimeout(() => connect(name), Math.min(8000, 800 * 2 ** retry++));
  };
}

function notice(text) {
  $('notice').textContent = text || '';
  if (game) $('slip-line').textContent = text || '';
}

function receive(m) {
  switch (m.t) {
    case 'hello':
      seat = m.seat;
      joined = true;
      if (m.key) store(keyOf(), m.key);
      notice('');
      break;
    case 'bye':
      bye = true;
      store(keyOf(), null);
      $('game').hidden = true;
      $('join').hidden = false;
      $('lobby').hidden = true;
      notice(m.text);
      break;
    case 'lobby':
      showLobby(m);
      break;
    case 'start':
      start(m);
      break;
    case 'events':
      queue.push(...m.events);
      break;
    case 'state':
      latest = m;
      fresh = true;
      if (!shown) shown = viewOf(m);
      break;
  }
}

function showLobby(m) {
  T = { ...T, ...m.text };
  seat = m.you;
  $('lobby').hidden = false;
  $('lobby-game').textContent = fmt(T.lobbyCity, m.city, m.rounds, money(m.cash)) +
    (m.rivals ? ' ' + (T[`rivals${m.rivals}`] || '') : '');
  const ul = $('seats');
  ul.replaceChildren();
  m.seats.forEach((s, i) => {
    const li = el('li');
    li.style.setProperty('--dot', playerColors[i] || '#999');
    const who = el('span', 'who', s.name + (i === m.you ? ` (${T.you || 'you'})` : ''));
    li.append(dot(i), who, el('span', 'note', s.kind === 'local' ? T.local : T.visitor));
    ul.append(li);
  });
  let note = m.you >= 0 ? T.waiting : m.room > 0 ? T.waiting : T.full;
  if (m.room > 0) note += ' ' + fmt(T.seatsOpen, m.room);
  $('lobby-note').textContent = note;
}

function dot(i) {
  const d = el('span', 'swatch-dot', '●');
  d.style.color = playerColors[i] || '#999';
  return d;
}

// --- the game ------------------------------------------------------------

let game = null;      // the start message: the board, cards, players
let latest = null;    // the last state
let shown = null;     // the board as shown: the state after the last event shown
let queue = [];       // events still to show
let cur = null, curT = 0, curLength = 0;
let log = [];
let selected = -1, picking = null; // a square picked; a card waiting for its square
let board = null;     // the 3D scene

function start(m) {
  T = { ...T, ...m.text };
  const rebuild = !game || game.city !== m.city;
  game = m;
  seat = m.you;
  queue = [];
  cur = null;
  shown = null;
  log = [];
  firstLook = true;
  $('join').hidden = true;
  $('game').hidden = false;
  $('me').textContent = seat >= 0 ? game.players[seat].name : (T.watching || 'Watching');
  if (rebuild) {
    board?.dispose();
    board = new Board(m);
  }
  board.resize();
}

// viewOf is a state's board, as an event's View has it.
function viewOf(s) {
  return {
    Cash: s.players.map((p) => p.cash), At: s.players.map((p) => p.at), InJail: s.players.map((p) => p.inJail),
    Out: s.players.map((p) => p.out), Hands: s.players.map((p) => p.cards), Owner: s.owner, Houses: s.houses, Pot: s.pot,
  };
}

// shownTurn is whose turn the board shows: the state runs ahead of the
// events still being shown.
const shownTurn = () => (cur ?? queue[0])?.Player ?? latest.turn;
const busy = () => cur !== null || queue.length > 0;
const myTurn = () => latest && seat >= 0 && latest.turn === seat && !['cpu', 'over'].includes(latest.phase) &&
  !latest.players[seat].away && !busy();
const send = (action) => ws?.readyState === WebSocket.OPEN && ws.send(JSON.stringify({ t: 'act', ...action }));

// How long each kind of event shows, in seconds: the light app's.
const EvDice = 0, EvWalk = 1, EvJump = 2, EvNote = 3, EvCard = 4;
function lengthOf(e) {
  switch (e.Kind) {
    case EvDice: return 0.55;
    case EvWalk: return 0.14 * e.Steps + 0.08;
    case EvJump: return 0.45;
    case EvCard: return e.Played ? 1.6 : 1.1;
    default: return e.Player === seat ? 0.3 : 0.75;
  }
}

// step plays the events out: each for its length, then the board as it
// left it; with many waiting (back after a while), quickly.
function step(dt) {
  if (!game) return;
  if (!cur && queue.length) {
    cur = queue.shift();
    curT = 0;
    curLength = queue.length > 24 ? 0.02 : lengthOf(cur);
    begin(cur);
  }
  if (cur) {
    curT += dt;
    board.animate(cur, Math.min(1, curT / curLength));
    if (curT >= curLength) {
      end(cur);
      cur = null;
      if (!queue.length) settle();
    }
  } else if (fresh && shown) {
    settle();
  }
}

let fresh = false; // a state not shown yet
function begin(e) {
  if (e.Kind === EvNote) {
    log.unshift({ text: e.Text, good: e.Good, bad: e.Bad });
    log.length = Math.min(log.length, 7);
    renderLog();
  }
  if (e.Kind === EvCard) {
    const who = game.players[e.Player]?.name ?? '';
    const name = e.Back ? T.aCard : game.cards[e.Card]?.name;
    toast(e.Played ? fmt(T.cardPlayed, who, name) : fmt(T.cardGot, who), cardInks[e.Card]);
  }
  if (e.Kind === EvDice) board.roll(e.Player);
  renderPanel();
}

function end(e) {
  const old = shown;
  shown = e.View;
  if (e.Kind === EvDice) board.dice(e.Dice);
  if (e.Kind === EvCard) toast('');
  flash(old, shown);
  board.update(shown);
  renderPanel();
}

// settle shows the game as it stands, once everything has been shown.
function settle() {
  if (!latest) return;
  const old = shown;
  shown = viewOf(latest);
  fresh = false;
  flash(old, shown);
  board.update(shown);
  if (latest.phase === 'roll' && latest.turn === seat) selected = -1;
  renderPanel();
}

// flash colours the cash that changed, for a moment.
let flashes = {}, flashTimer = 0;
function flash(old, now) {
  if (!old || !now) return;
  now.Cash.forEach((c, i) => {
    if (c !== old.Cash[i]) flashes[i] = { up: c > old.Cash[i], until: performance.now() + 900 };
  });
}

let toastTimer = 0;
function toast(text, ink) {
  const t = $('toast');
  t.hidden = !text;
  t.textContent = text;
  t.style.borderColor = ink || '';
  clearTimeout(toastTimer);
  if (text) toastTimer = setTimeout(() => (t.hidden = true), 1800);
}

// --- the panel -------------------------------------------------------------

function renderPanel() {
  if (!game || !latest || !shown) return;
  const s = latest, v = shown, now = performance.now();
  $('round').textContent = `${s.round} / ${game.rounds}`;
  $('pot').textContent = '$' + money(v.Pot);

  const ul = $('players');
  ul.replaceChildren();
  s.players.forEach((p, i) => {
    const li = el('li');
    li.style.setProperty('--dot', playerColors[i]);
    if (i === shownTurn() && s.phase !== 'over') li.classList.add('turn');
    if (v.Out[i]) li.classList.add('out');
    let status = fmt(T.worth, money(worth(v, i)));
    if (v.Out[i]) status = T.bankrupt;
    else {
      if (v.InJail[i]) status += T.inJail;
      if (v.Hands[i] > 0) status += T[`cards${v.Hands[i]}`] || ` · ${v.Hands[i]}`;
      if (p.rentFree) status += T.rentFree;
      if (p.turtle > 0) status += T.turtle;
      if (p.asleep) status += T.asleep;
      if (p.away) status += T.away;
    }
    const name = el('span', 'name', p.name + (i === seat ? ` (${T.you})` : ''));
    const cash = el('span', 'cash', '$' + money(v.Cash[i]));
    const f = flashes[i];
    if (f && f.until > now) cash.classList.add(f.up ? 'gain' : 'loss');
    li.append(name, cash, el('span', 'status', status));
    ul.append(li);
  });
  if (Object.values(flashes).some((f) => f.until > now)) {
    clearTimeout(flashTimer);
    flashTimer = setTimeout(renderPanel, 950); // the cash back to ink
  }

  renderSquare();
  renderActions();
  renderHand();
  renderSlip();
  renderOver();
}

// worth is a player's cash, property and buildings, as the view has them.
function worth(v, i) {
  let w = v.Cash[i];
  v.Owner.forEach((o, sq) => {
    if (o === i) w += game.squares[sq].price + v.Houses[sq] * (game.squares[sq].house || 0);
  });
  return w;
}

function focusSquare() {
  if (selected >= 0) return selected;
  const who = seat >= 0 && !shown.Out[seat] ? seat : latest.turn;
  return shown.At[who];
}

function renderSquare() {
  const sq = focusSquare(), q = game.squares[sq], v = shown;
  const box = $('square');
  box.replaceChildren();
  const head = el('div', 'head');
  const sw = el('span', 'swatch');
  if (q.kind === 'street') sw.style.setProperty('--group', groupColors[q.group]);
  const title = el('div');
  title.append(el('h4', '', q.name), el('div', 'kind', kindLine(q)));
  head.append(sw, title);
  box.append(head);
  const o = v.Owner[sq];
  if (['street', 'pier', 'utility'].includes(q.kind)) {
    const owner = el('div', 'owner');
    if (o < 0) owner.textContent = fmt(T.forSale, q.price);
    else {
      const d = dot(o);
      owner.append(d, ' ', o === seat ? T.youOwn : fmt(T.owns, game.players[o].name));
      if (q.kind !== 'utility') owner.append(' · ' + T.rentNow + ' ', el('b', '', '$' + money(latest.rent[sq])));
    }
    box.append(owner);
  }
  if (q.kind === 'street') {
    const rents = el('div', 'rents');
    q.rents.forEach((r, b) => {
      const label = b === 0 ? T.tierBare : b === 5 ? T.tierHotel : T[`houses${b}`];
      const cell = el('span', o >= 0 && v.Houses[sq] === b ? 'now' : '', label);
      cell.append(el('b', '', '$' + money(r)));
      rents.append(cell);
    });
    box.append(rents);
  }
  if (q.kind === 'utility') box.append(el('div', 'more', T.rentByDice));
  if (q.kind === 'pier') box.append(el('div', 'more', T.rentByPiers));

  // Build or sell the square in focus, in your turn.
  const mine = q.kind === 'street' && o === seat && seat >= 0;
  $('square-acts').hidden = !mine;
  if (mine) {
    const houses = v.Houses[sq];
    const build = $('build'), sell = $('sell');
    build.textContent = houses >= 5 ? T.hotelBuilt : fmt(houses === 4 ? T.hotel : T.house, q.house);
    sell.textContent = fmt(T.sell, q.house / 2);
    build.disabled = !myTurn() || !(latest.build || []).includes(sq);
    sell.disabled = !myTurn() || !(latest.sell || []).includes(sq);
    build.onclick = () => send({ do: 'build', sq });
    sell.onclick = () => send({ do: 'sell', sq });
  }
}

function kindLine(q) {
  switch (q.kind) {
    case 'street': return fmt(T.street, q.price, q.house);
    case 'pier': return fmt(T.pier, q.price);
    case 'utility': return fmt(T.utility, q.price);
    case 'chance': return T.chance;
    case 'shop': return T.shop;
    case 'tax': return fmt(T.tax, q.price);
    default: return T.corner;
  }
}

function renderActions() {
  const s = latest, p = $('primary'), q = $('secondary');
  q.hidden = true;
  p.disabled = true;
  p.onclick = q.onclick = null;
  if (seat < 0) {
    p.hidden = true;
    return;
  }
  p.hidden = false;
  if (s.phase === 'over' || !myTurn()) {
    p.textContent = s.phase === 'over' ? fmt(T.won, game.players[s.winner]?.name ?? '') :
      s.turn === seat && busy() ? '…' : fmt(T.playing, s.players[s.turn].name);
    return;
  }
  p.disabled = false;
  const me = s.players[seat];
  switch (s.phase) {
    case 'roll':
      p.textContent = me.inJail ? (game.oneDie ? T.rollSix : T.rollDouble) : T.roll;
      p.onclick = () => { selected = -1; send({ do: 'roll' }); };
      if (me.inJail) {
        q.hidden = false;
        q.textContent = T.bail;
        q.disabled = !s.bail;
        q.onclick = () => send({ do: 'bail' });
      }
      break;
    case 'buy':
      p.textContent = fmt(T.buy, game.squares[me.at].price);
      p.onclick = () => send({ do: 'buy' });
      q.hidden = false;
      q.disabled = false;
      q.textContent = T.pass;
      q.onclick = () => send({ do: 'pass' });
      break;
    case 'shop':
      p.textContent = T.leave;
      p.onclick = () => send({ do: 'leave' });
      break;
    case 'end':
      p.textContent = T.end;
      p.onclick = () => send({ do: 'end' });
      break;
  }
}

let handShown = [];
function renderHand() {
  const s = latest;
  const bar = $('hand-bar'), hand = $('hand');
  const cards = seat >= 0 ? s.hand || [] : [];
  bar.hidden = cards.length === 0;
  const can = myTurn() && s.phase !== 'shop';
  $('hand-caption').textContent = fmt(can ? T.handPlay : T.hand, cards.length, game.handMax);
  const was = handShown;
  handShown = cards.slice();
  hand.replaceChildren();
  cards.forEach((c, i) => {
    const b = cardFace(c, el('button', 'card'));
    if (!was.includes(c) && was.length) b.classList.add('new');
    b.disabled = !can || !s.plays || !s.plays[i];
    b.title = game.cards[c].text;
    b.onclick = () => playCard(i);
    hand.append(b);
  });

  // The shop, while you are at one.
  const shopping = s.phase === 'shop' && s.turn === seat && !busy();
  $('shop').hidden = !shopping;
  if (shopping) {
    const offer = $('offer');
    offer.replaceChildren();
    s.offer.forEach((c, i) => {
      const b = cardFace(c, el('button', 'card'));
      b.disabled = !s.canShop?.[i];
      b.title = game.cards[c].text;
      b.onclick = () => send({ do: 'shop', card: i });
      offer.append(b);
    });
    $('leave-shop').onclick = () => send({ do: 'leave' });
  }
}

function cardFace(c, e) {
  const info = game.cards[c];
  e.style.setProperty('--ink-card', cardInks[c]);
  e.append(el('b', '', info.name), el('small', '', info.text), el('span', 'price', '$' + info.price));
  return e;
}

// playCard plays card i: on nothing at once; else on the player, the
// steps or the square picked.
function playCard(i) {
  const c = latest.hand[i], kind = game.cards[c].target, targets = latest.plays[i] || [];
  if (kind === 'none') return send({ do: 'card', card: i, target: targets[0] ?? 0 });
  if (kind === 'square') {
    picking = { card: i, targets };
    $('pick-hint').hidden = false;
    $('pick-hint').querySelector('span').textContent = T.pickSquare;
    board.highlight(targets);
    return;
  }
  const choices = $('picker-choices');
  choices.replaceChildren();
  $('picker-title').textContent = kind === 'player' ? T.onWhom : T.howMany;
  for (const t of targets) {
    const b = el('button', 'quiet', kind === 'player' ? game.players[t].name : String(t));
    if (kind === 'player') b.prepend(dot(t), ' ');
    b.onclick = () => { $('picker').hidden = true; send({ do: 'card', card: i, target: t }); };
    choices.append(b);
  }
  $('picker').hidden = false;
}

function cancelPick() {
  picking = null;
  $('pick-hint').hidden = true;
  $('picker').hidden = true;
  board?.highlight([]);
}

function renderSlip() {
  const s = latest;
  const line = $('slip-line');
  line.textContent = log[0]?.text || '';
  line.className = log[0]?.good ? 'good' : log[0]?.bad ? 'bad' : '';
  const pi = shownTurn();
  $('slip').style.setProperty('--dot', playerColors[pi]);
  $('slip-turn').textContent = pi === seat ? T.yourTurn : fmt(T.turnOf, s.players[pi]?.name ?? '');
  $('slip-round').textContent = fmt(T.roundOf, s.round, game.rounds);
}

function renderLog() {
  const ul = $('log');
  ul.replaceChildren(...log.map((l) => el('li', l.good ? 'good' : l.bad ? 'bad' : '', l.text)));
}

function renderOver() {
  const s = latest, over = s.phase === 'over' && !busy();
  $('over').hidden = !over;
  if (!over) return;
  const w = s.winner;
  $('over-title').textContent = w === seat ? T.youWon : fmt(T.won, s.players[w]?.name ?? '');
  const ol = $('standings');
  ol.replaceChildren();
  s.players.map((p, i) => ({ p, i })).sort((a, b) => b.p.worth - a.p.worth).forEach(({ p, i }) => {
    const li = el('li');
    li.style.setProperty('--dot', playerColors[i]);
    li.append(el('span', '', p.out ? fmt(T.bankruptOf, p.name) : p.name), el('b', '', '$' + money(p.worth)));
    ol.append(li);
  });
}

// --- the board in 3D ---------------------------------------------------------

// Board is the scene: the print on a slab of wood, the pawns, the
// buildings, the dice; board units, x right and y down the print, are the
// scene's x and z, from its middle.
class Board {
  constructor(m) {
    this.m = m;
    this.size = m.size;
    this.host = $('board');
    this.renderer = new THREE.WebGLRenderer({ antialias: true });
    this.renderer.setPixelRatio(Math.min(2, devicePixelRatio));
    this.renderer.shadowMap.enabled = true;
    this.renderer.shadowMap.type = THREE.PCFSoftShadowMap;
    this.host.append(this.renderer.domElement);

    this.scene = new THREE.Scene();
    this.scene.background = new THREE.Color('#e4dac4');
    this.camera = new THREE.PerspectiveCamera(38, 1, 5, 20000);
    this.camera.position.set(0, this.size * 0.62, this.size * 0.52);
    this.controls = new OrbitControls(this.camera, this.renderer.domElement);
    this.controls.enableDamping = true;
    this.controls.screenSpacePanning = false;
    this.controls.minDistance = 260;
    this.controls.maxDistance = this.size * 1.5;
    this.controls.minPolarAngle = 0.15;
    this.controls.maxPolarAngle = 1.15;
    this.controls.mouseButtons = { LEFT: THREE.MOUSE.PAN, MIDDLE: THREE.MOUSE.DOLLY, RIGHT: THREE.MOUSE.ROTATE };
    this.controls.touches = { ONE: THREE.TOUCH.PAN, TWO: THREE.TOUCH.DOLLY_ROTATE };
    this.controls.addEventListener('start', () => (this.touched = performance.now()));
    this.touched = -1e9;

    this.scene.add(new THREE.HemisphereLight('#fffaf0', '#8a7a62', 1.6));
    const sun = new THREE.DirectionalLight('#fff4e0', 2.2);
    sun.position.set(-this.size * 0.3, this.size * 0.9, this.size * 0.5);
    sun.castShadow = true;
    sun.shadow.mapSize.set(2048, 2048);
    const r = this.size * 0.6;
    Object.assign(sun.shadow.camera, { left: -r, right: r, top: r, bottom: -r, near: 10, far: this.size * 3 });
    this.scene.add(sun);

    this.tiles = m.squares.map((q) => q.tile);
    this.addPaper();
    this.pawns = m.players.map((_, i) => this.addPawn(i));
    this.buildings = new Map();
    this.dieMeshes = [this.addDie(), this.addDie()]; // shown once rolled
    this.marks = new THREE.Group();
    this.scene.add(this.marks);
    this.rolling = -1;

    this.pointer();
    this.onResize = () => this.resize();
    addEventListener('resize', this.onResize);
    this.resize();
  }

  // world is a board point at height h, in the scene.
  world(x, y, h = 0) {
    return new THREE.Vector3(x - this.size / 2, h, y - this.size / 2);
  }

  // at is a point of square sq in its own frame: x along the track, y
  // across it, outwards positive.
  at(sq, x, y) {
    const t = this.tiles[sq];
    return [t.center[0] + t.tan[0] * x - t.in[0] * y, t.center[1] + t.tan[1] * x - t.in[1] * y];
  }

  addPaper() {
    const s = this.size;
    const slab = new THREE.Mesh(new THREE.BoxGeometry(s, 14, s), new THREE.MeshStandardMaterial({ color: '#7a5a3e', roughness: 0.8 }));
    slab.position.y = -7.2;
    slab.receiveShadow = true;
    this.scene.add(slab);
    const mat = new THREE.MeshStandardMaterial({ color: '#ffffff', roughness: 0.92 });
    this.paper = new THREE.Mesh(new THREE.PlaneGeometry(s, s), mat);
    this.paper.rotation.x = -Math.PI / 2;
    this.paper.receiveShadow = true;
    this.scene.add(this.paper);
    const fallback = new THREE.CanvasTexture(this.drawPlain());
    fallback.colorSpace = THREE.SRGBColorSpace;
    fallback.anisotropy = this.renderer.capabilities.getMaxAnisotropy();
    mat.map = fallback;
    if (this.m.print) {
      new THREE.TextureLoader().load(`board.jpg?${Date.now()}`, (tex) => {
        tex.colorSpace = THREE.SRGBColorSpace;
        tex.anisotropy = this.renderer.capabilities.getMaxAnisotropy();
        mat.map = tex;
        mat.needsUpdate = true;
        fallback.dispose();
      });
    }
  }

  // drawPlain draws a plain board, until the print comes or without one:
  // the squares and their colour bands on the paper.
  drawPlain() {
    const n = 2048, k = n / this.size;
    const cv = document.createElement('canvas');
    cv.width = cv.height = n;
    const g = cv.getContext('2d');
    g.fillStyle = '#f1e9d6';
    g.fillRect(0, 0, n, n);
    const path = (pts) => {
      g.beginPath();
      for (let i = 0; i < pts.length; i += 2) g.lineTo(pts[i] * k, pts[i + 1] * k);
      g.closePath();
    };
    this.m.squares.forEach((q, sq) => {
      const t = q.tile;
      path(t.outline);
      g.fillStyle = '#f6f0e0';
      g.fill();
      g.strokeStyle = '#1f1b17';
      g.lineWidth = 1.5;
      g.stroke();
      if (t.band && q.kind === 'street') {
        path(t.band);
        g.fillStyle = groupColors[q.group];
        g.fill();
      }
      g.save();
      g.translate(t.center[0] * k, t.center[1] * k);
      g.rotate(Math.atan2(t.in[0], -t.in[1]));
      g.fillStyle = '#1f1b17';
      g.font = `${Math.round(11 * k)}px serif`;
      g.textAlign = 'center';
      g.fillText(q.name.slice(0, 12), 0, 14 * k, t.along * k * 0.95);
      if (q.price) g.fillText('$' + q.price, 0, 28 * k);
      g.restore();
    });
    return cv;
  }

  addPawn(i) {
    const color = playerColors[i];
    const pawn = new THREE.Group();
    const lacquer = new THREE.MeshStandardMaterial({ color, roughness: 0.35 });
    const body = new THREE.Mesh(new THREE.CylinderGeometry(6.5, 10, 20, 24), lacquer);
    body.position.y = 10;
    const head = new THREE.Mesh(new THREE.SphereGeometry(8, 24, 16), new THREE.MeshStandardMaterial({ color: '#f4e2c8', roughness: 0.6 }));
    head.position.y = 27;
    const hair = new THREE.Mesh(new THREE.SphereGeometry(8.3, 24, 12, 0, Math.PI * 2, 0, Math.PI / 2.2), new THREE.MeshStandardMaterial({ color: '#1f1b17', roughness: 0.5 }));
    hair.position.y = 27.5;
    const ring = new THREE.Mesh(new THREE.TorusGeometry(9.5, 1.4, 8, 32), lacquer);
    ring.rotation.x = Math.PI / 2;
    ring.position.y = 1.4;
    for (const part of [body, head, hair, ring]) {
      part.castShadow = true;
      pawn.add(part);
    }
    pawn.userData.slot = i;
    this.scene.add(pawn);
    return pawn;
  }

  // spot is where player i's pawn stands on sq: side by side when several do.
  spot(i, sq) {
    const offs = [[-15, -6], [15, -6], [-15, 14], [15, 14]];
    const [x, y] = this.at(sq, offs[i][0], offs[i][1]);
    return this.world(x, y);
  }

  addDie() {
    const faces = [3, 4, 1, 6, 2, 5].map((v) => {
      const cv = document.createElement('canvas');
      cv.width = cv.height = 128;
      const g = cv.getContext('2d');
      g.fillStyle = '#fbf7ec';
      g.fillRect(0, 0, 128, 128);
      const pips = { 1: [[64, 64]], 2: [[34, 34], [94, 94]], 3: [[30, 30], [64, 64], [98, 98]], 4: [[34, 34], [94, 34], [34, 94], [94, 94]],
        5: [[30, 30], [98, 30], [64, 64], [30, 98], [98, 98]], 6: [[34, 28], [94, 28], [34, 64], [94, 64], [34, 100], [94, 100]] }[v];
      g.fillStyle = v === 1 ? '#bf4430' : '#1f1b17';
      for (const [x, y] of pips) {
        g.beginPath();
        g.arc(x, y, v === 1 ? 16 : 11, 0, Math.PI * 2);
        g.fill();
      }
      const tex = new THREE.CanvasTexture(cv);
      tex.colorSpace = THREE.SRGBColorSpace;
      return new THREE.MeshStandardMaterial({ map: tex, roughness: 0.5 });
    });
    const die = new THREE.Mesh(new THREE.BoxGeometry(18, 18, 18), faces);
    die.castShadow = true;
    die.visible = false;
    this.scene.add(die);
    return die;
  }

  // roll starts the dice tumbling beside player pi's pawn.
  roll(pi) {
    this.rolling = pi;
    const sq = shown.At[pi], t = this.tiles[sq];
    const [x, y] = this.at(sq, 0, -t.across / 2 - 42);
    this.dieMeshes.forEach((d, k) => {
      d.visible = k === 0 || !this.m.oneDie;
      d.position.copy(this.world(x + (k ? 14 : -14), y, 9));
    });
  }

  // dice shows the dice come to rest, each its value up.
  dice(values) {
    const up = { 1: [0, 0, 0], 6: [Math.PI, 0, 0], 2: [-Math.PI / 2, 0, 0], 5: [Math.PI / 2, 0, 0], 3: [0, 0, Math.PI / 2], 4: [0, 0, -Math.PI / 2] };
    this.dieMeshes.forEach((d, k) => {
      const v = values[k];
      if (!v) return;
      d.rotation.set(...up[v]);
      d.rotateOnWorldAxis(new THREE.Vector3(0, 1, 0), (k ? -0.3 : 0.25));
      d.position.y = 9;
    });
    this.rolling = -1;
  }

  // animate shows event e at f of its length: the dice tumbling, a pawn
  // hopping along its path or jumping, the view following it.
  animate(e, f) {
    const p = this.pawns[e.Player];
    if (e.Kind === EvDice) {
      this.dieMeshes.forEach((d, k) => {
        d.rotation.x += 0.4 + k * 0.1;
        d.rotation.z += 0.3;
        d.position.y = 9 + Math.abs(Math.sin(f * Math.PI * 3)) * 22 * (1 - f);
      });
    }
    if (e.Kind === EvWalk && e.Path?.length) {
      const hops = e.Path.length, h = Math.min(hops - 1e-6, f * hops), k = Math.floor(h), g = h - k;
      const from = this.spot(e.Player, k === 0 ? e.From : e.Path[k - 1]), to = this.spot(e.Player, e.Path[k]);
      p.position.lerpVectors(from, to, g);
      p.position.y = Math.sin(g * Math.PI) * 14;
      this.follow(p.position);
    }
    if (e.Kind === EvJump) {
      const from = this.spot(e.Player, e.From), to = this.spot(e.Player, e.To);
      p.position.lerpVectors(from, to, f);
      p.position.y = Math.sin(f * Math.PI) * 60;
      this.follow(p.position);
    }
  }

  // follow brings the view along with a pawn on the move, unless the
  // viewer has just moved it themselves.
  follow(pos) {
    if (performance.now() - this.touched < 4000) return;
    const d = this.aim(pos).sub(this.controls.target).multiplyScalar(0.08);
    this.controls.target.add(d);
    this.camera.position.add(d);
  }

  // aim is where the view looks to show pos: a third of the way from it
  // to the board's middle, so a pawn at the edge leaves the view on the
  // board.
  aim(pos) {
    return new THREE.Vector3(pos.x * 0.67, 0, pos.z * 0.67);
  }

  lookAt(sq) {
    const t = this.tiles[sq];
    const d = this.aim(this.world(t.center[0], t.center[1])).sub(this.controls.target);
    this.controls.target.add(d);
    this.camera.position.add(d);
  }

  // update stands the board as the view has it: the pawns, the owners'
  // marks, the buildings.
  update(v) {
    this.pawns.forEach((p, i) => {
      p.visible = !v.Out[i];
      p.position.copy(this.spot(i, v.At[i]));
    });
    v.Owner.forEach((o, sq) => {
      const key = `${o}:${v.Houses[sq]}`;
      const had = this.buildings.get(sq);
      if (had?.key === key) return;
      if (had) {
        this.scene.remove(had.group);
        had.group.traverse((m) => m.geometry?.dispose());
      }
      if (o < 0) return this.buildings.delete(sq);
      const group = this.building(sq, o, v.Houses[sq]);
      this.scene.add(group);
      this.buildings.set(sq, { key, group });
    });
  }

  // building is square sq's owner's mark along its outer edge, and on a
  // street its houses or hotel along the colour band.
  building(sq, o, houses) {
    const t = this.tiles[sq], g = new THREE.Group();
    const angle = -Math.atan2(t.tan[1], t.tan[0]);
    const color = new THREE.Color(playerColors[o]);
    const strip = new THREE.Mesh(new THREE.BoxGeometry(t.along * 0.86, 1.2, 5), new THREE.MeshStandardMaterial({ color }));
    const [sx, sy] = this.at(sq, 0, t.across / 2 - 5);
    strip.position.copy(this.world(sx, sy, 0.6));
    strip.rotation.y = angle;
    g.add(strip);
    if (!houses) return g;
    const band = -t.across / 2 + this.m.band / 2;
    const walls = new THREE.MeshStandardMaterial({ color: '#f6f0e0', roughness: 0.8 });
    const roof = new THREE.MeshStandardMaterial({ color, roughness: 0.55 });
    const put = (mesh, x, y, h) => {
      const [px, py] = this.at(sq, x, y);
      mesh.position.copy(this.world(px, py, h));
      mesh.rotation.y = angle;
      mesh.castShadow = true;
      g.add(mesh);
      return mesh;
    };
    if (houses >= 5) {
      put(new THREE.Mesh(new THREE.BoxGeometry(t.along * 0.7, 16, 15), walls), 0, band, 8);
      put(new THREE.Mesh(new THREE.BoxGeometry(t.along * 0.74, 3, 17), roof), 0, band, 17.5);
      return g;
    }
    const gap = t.along / 4.4;
    for (let k = 0; k < houses; k++) {
      const x = (k - (houses - 1) / 2) * gap;
      put(new THREE.Mesh(new THREE.BoxGeometry(10, 8, 10), walls), x, band, 4);
      const cone = put(new THREE.Mesh(new THREE.ConeGeometry(8.5, 7, 4), roof), x, band, 11.5);
      cone.rotation.y = angle + Math.PI / 4;
    }
    return g;
  }

  // highlight outlines squares: those a card may be played on.
  highlight(squares) {
    this.marks.clear();
    const mat = new THREE.LineBasicMaterial({ color: '#3f7a3a' });
    for (const sq of squares) {
      const o = this.tiles[sq].outline, pts = [];
      for (let i = 0; i < o.length; i += 2) pts.push(this.world(o[i], o[i + 1], 1.2));
      this.marks.add(new THREE.LineLoop(new THREE.BufferGeometry().setFromPoints(pts), mat));
    }
  }

  // pointer picks the square tapped: to show it, or to play a card on it.
  pointer() {
    const ray = new THREE.Raycaster(), ndc = new THREE.Vector2();
    let down = null;
    const canvas = this.renderer.domElement;
    canvas.addEventListener('pointerdown', (e) => (down = [e.clientX, e.clientY]));
    canvas.addEventListener('pointerup', (e) => {
      if (!down || Math.hypot(e.clientX - down[0], e.clientY - down[1]) > 6) return;
      const r = canvas.getBoundingClientRect();
      ndc.set(((e.clientX - r.left) / r.width) * 2 - 1, -((e.clientY - r.top) / r.height) * 2 + 1);
      ray.setFromCamera(ndc, this.camera);
      const hit = ray.intersectObject(this.paper)[0];
      if (!hit) return;
      const x = hit.point.x + this.size / 2, y = hit.point.z + this.size / 2;
      const sq = this.tiles.findIndex((t) => inside(t.outline, x, y));
      if (sq < 0) return;
      if (picking) {
        if (picking.targets.includes(sq)) send({ do: 'card', card: picking.card, target: sq });
        cancelPick();
        return;
      }
      selected = sq === selected ? -1 : sq;
      renderPanel();
    });
  }

  resize() {
    const w = this.host.clientWidth, h = this.host.clientHeight;
    if (!w || !h) return;
    this.renderer.setSize(w, h);
    this.camera.aspect = w / h;
    this.camera.updateProjectionMatrix();
  }

  render() {
    this.controls.update();
    // Keep the view over the board.
    const t = this.controls.target, lim = this.size / 2;
    const cx = Math.max(-lim, Math.min(lim, t.x)) - t.x, cz = Math.max(-lim, Math.min(lim, t.z)) - t.z;
    if (cx || cz) {
      t.x += cx; t.z += cz;
      this.camera.position.x += cx; this.camera.position.z += cz;
    }
    this.renderer.render(this.scene, this.camera);
  }

  dispose() {
    removeEventListener('resize', this.onResize);
    this.controls.dispose();
    this.renderer.dispose();
    this.renderer.domElement.remove();
  }
}

// inside reports whether x, y is inside the polygon of x, y pairs.
function inside(pts, x, y) {
  let in_ = false;
  for (let i = 0, j = pts.length - 2; i < pts.length; j = i, i += 2) {
    const xi = pts[i], yi = pts[i + 1], xj = pts[j], yj = pts[j + 1];
    if ((yi > y) !== (yj > y) && x < ((xj - xi) * (y - yi)) / (yj - yi) + xi) in_ = !in_;
  }
  return in_;
}

// --- the loop ---------------------------------------------------------------

let last = performance.now(), firstLook = true;
function frame(now) {
  const dt = Math.min(0.1, (now - last) / 1000);
  last = now;
  if (game && board) {
    if (shown && firstLook) {
      firstLook = false;
      board.update(shown);
      board.lookAt(shown.At[seat >= 0 ? seat : latest.turn]);
      renderPanel();
    }
    step(dt);
    board.render();
  }
  requestAnimationFrame(frame);
}

// A page left (another page, a closed tab, the back/forward cache) lets
// go of its seat at once, for the computer to play; shown again, it
// takes it back.
addEventListener('pagehide', () => {
  hidden = true;
  ws?.close();
});
addEventListener('pageshow', (e) => {
  if (e.persisted && joined && !bye) {
    hidden = false;
    connect(store('small-fortune:name') || '');
  }
});

$('pick-hint').querySelector('button').addEventListener('click', cancelPick);
$('picker-cancel').addEventListener('click', cancelPick);
addEventListener('keydown', (e) => e.key === 'Escape' && cancelPick());
init();
requestAnimationFrame(frame);
