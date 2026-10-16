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
// cardInks are the cards' pigments, by kind (fortune_cards.go's cardInk).
const cardInks = ['#BF4430', '#5E843E', '#8E354A', '#4E5D6C', '#8A5A3A', '#D49B39', '#2F6C68', '#2D5D9F', '#7D7A3A', '#5C588C', '#4A9BA8', '#2E3A5C'];

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
  rung = '';
  endRung = false;
  $('join').hidden = true;
  $('game').hidden = false;
  $('me').textContent = seat >= 0 ? game.players[seat].name : (T.watching || 'Watching');
  if (rebuild) {
    board?.dispose();
    board = new Board(m);
  }
  board.resize();
  loadCards();
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
    if (curLength > 0.05) cueSounds(cur, dt);
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
  if (curLength > 0.05) { // shown, not hurried past: its sound
    if (e.Kind === EvDice) sound('dice');
    if (e.Kind === EvJump) sound('jump');
    if (e.Kind === EvCard) sound(e.Played ? 'cardPlay' : 'cardFlip');
  }
  if (e.Kind === EvNote) {
    log.unshift({ text: e.Text, good: e.Good, bad: e.Bad });
    log.length = Math.min(log.length, 7);
    renderLog();
  }
  if (e.Kind === EvDice) board.roll(e.Player);
  renderPanel();
}

function end(e) {
  const old = shown;
  shown = e.View;
  if (e.Kind === EvDice) board.dice(e.Dice);
  flash(old, shown);
  board.moneyFrom(old, shown);
  soundsFrom(old, shown);
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
  board.moneyFrom(old, shown);
  soundsFrom(old, shown);
  ring();
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
      if (p.kind === 'ai') status += T.ai;
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
  if (game.art) {
    const sign = el('img', 'sign');
    sign.src = `art/sign-${sq}.png`;
    sign.alt = '';
    head.append(sign);
  } else {
    const sw = el('span', 'swatch');
    if (q.kind === 'street') sw.style.setProperty('--group', groupColors[q.group]);
    head.append(sw);
  }
  const title = el('div');
  title.append(el('h4', '', q.name), el('div', 'kind', kindLine(q)));
  head.append(title);
  box.append(head);
  const o = v.Owner[sq];
  if (['street', 'pier', 'utility'].includes(q.kind)) {
    const owner = el('div', 'owner');
    if (o < 0) owner.textContent = fmt(T.forSale, q.price);
    else {
      owner.append(dot(o), ' ', o === seat ? T.youOwn : fmt(T.owns, game.players[o].name));
      if (q.kind !== 'utility') owner.append(' · ' + T.rentNow + ' ', el('b', '', '$' + money(rentOf(v, sq))));
    }
    box.append(owner);
  }

  // The rents, a tier to a cell, two to a row: the tier in icons (its
  // words on hover), what it charges; lit when it is how the square stands.
  const rents = el('div', 'rents');
  const cell = (icons, dice, amount, words, now) => {
    const c = el('span', now ? 'now' : '');
    c.title = words;
    const row = el('i');
    for (const name of icons) row.append(icon(name));
    c.append(row);
    if (dice) c.append(icon('dice'));
    c.append(el('b', '', amount));
    rents.append(c);
  };
  const count = (kind) => v.Owner.filter((w, i) => w === o && game.squares[i].kind === kind).length;
  const note = (...parts) => {
    const n = el('div', 'more');
    n.append(...parts);
    box.append(n);
  };
  switch (q.kind) {
    case 'street': {
      const whole = wholeGroup(v, sq);
      for (let lvl = 0; lvl <= 5; lvl++) {
        const icons = lvl === 0 ? ['lot'] : lvl === 5 ? ['hotel'] : Array(lvl).fill('house');
        const words = lvl === 0 ? T.tierBare : lvl === 5 ? T.tierHotel : T[`houses${lvl}`];
        cell(icons, false, '$' + money(rentAt(sq, lvl, whole)), words, o >= 0 && lvl === v.Houses[sq]);
      }
      box.append(rents);
      note(whole ? T.wholeGroup : T.withGroup);
      const group = [];
      game.squares.forEach((g, i) => {
        if (g.kind !== 'street' || g.group !== q.group || i === sq) return;
        if (group.length) group.push(',  ');
        if (v.Owner[i] >= 0) group.push(dot(v.Owner[i]), ' ');
        group.push(g.name);
      });
      note(T.group + ' ', ...group);
      break;
    }
    case 'pier':
      for (let k = 1; k <= 4; k++) cell(Array(k).fill('pier'), false, '$' + money(game.pierRents[k]), T[`piers${k}`], o >= 0 && k === count('pier'));
      box.append(rents);
      note(T.rentByPiers);
      break;
    case 'utility':
      [['power'], ['power', 'water']].forEach((icons, k) =>
        cell(icons, true, fmt(T.times, game.utilityTimes[k + 1]), T[`utilities${k + 1}`], o >= 0 && k + 1 === count('utility')));
      box.append(rents);
      note(T.rentByDice);
      break;
    case 'start': note(T.noteStart); break;
    case 'jail': note(game.oneDie ? T.noteJailSix : T.noteJailDouble); break;
    case 'fountain': note(fmt(T.noteFountain, v.Pot)); break;
    case 'gotojail': note(T.noteGoToJail); break;
    case 'chance': note(T.noteChance); break;
    case 'shop': note(T.noteShop); break;
    case 'tax': note(fmt(T.noteTax, q.price)); break;
  }

  // Build or sell the square in focus, in your turn.
  const mine = q.kind === 'street' && o === seat && seat >= 0;
  $('square-acts').hidden = !mine;
  if (mine) {
    const houses = v.Houses[sq];
    const build = $('build'), sell = $('sell');
    build.replaceChildren(icon(houses >= 4 ? 'hotel' : 'house'), houses >= 5 ? T.hotelBuilt : fmt(houses === 4 ? T.hotel : T.house, q.house));
    sell.replaceChildren(icon('sell'), fmt(T.sell, q.house / 2));
    build.disabled = !myTurn() || !(latest.build || []).includes(sq);
    sell.disabled = !myTurn() || !(latest.sell || []).includes(sq);
    build.onclick = () => send({ do: 'build', sq });
    sell.onclick = () => send({ do: 'sell', sq });
  }
}

// icon is one of the game's icons, in the text's colour.
function icon(name) {
  const i = el('span', 'icon');
  i.style.setProperty('--icon', `url(icons/fortune-${name}.svg)`);
  return i;
}

// rentAt is a street's rent with so many buildings (5 a hotel), with its
// whole group or not: bare doubled, built half again.
function rentAt(sq, buildings, whole) {
  const r = game.squares[sq].rents[buildings];
  if (!whole) return r;
  return buildings === 0 ? r * 2 : r + Math.floor(r / 2);
}

// wholeGroup reports whether a street's owner has its whole group.
function wholeGroup(v, sq) {
  const o = v.Owner[sq], q = game.squares[sq];
  return o >= 0 && game.squares.every((g, i) => g.kind !== 'street' || g.group !== q.group || v.Owner[i] === o);
}

// rentOf is what landing on a square costs, as the view stands; 0 for a
// utility, which charges by the dice.
function rentOf(v, sq) {
  const o = v.Owner[sq], q = game.squares[sq];
  if (o < 0) return 0;
  if (q.kind === 'street') return rentAt(sq, v.Houses[sq], wholeGroup(v, sq));
  if (q.kind === 'pier') return game.pierRents[v.Owner.filter((w, i) => w === o && game.squares[i].kind === 'pier').length];
  return 0;
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
      const b = cardFace(c, el('button', 'card shop-card'), true);
      b.disabled = !s.canShop?.[i];
      b.title = game.cards[c].text;
      b.onclick = () => send({ do: 'shop', card: i });
      offer.append(b);
    });
    $('leave-shop').onclick = () => send({ do: 'leave' });
  }
}

// cardFace fills e with card c as printed (the host's picture, with its
// price tag at a shop), else in words.
function cardFace(c, e, price) {
  const info = game.cards[c];
  if (game.art) {
    e.classList.add('art');
    const img = el('img');
    img.src = `art/card-${c}${price ? '-price' : ''}.png`;
    img.alt = info.name;
    e.append(img);
    return e;
  }
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
// --- shaders -----------------------------------------------------------------

// The pawns and dice look printed, as the light app's pictures round them
// are: three flat tones (toon), paper grain fixed to the piece, an ink
// outline. Buildings come and go dissolving in ink (dissolving).

const tones = (() => {
  const t = new THREE.DataTexture(new Uint8Array([96, 176, 255]), 3, 1, THREE.RedFormat);
  t.minFilter = t.magFilter = THREE.NearestFilter;
  t.needsUpdate = true;
  return t;
})();

const grain = `
varying vec3 vGrain;
float grainHash(vec3 p) {
  p = fract(p * 0.3183099 + 0.1) * 17.0;
  return fract(p.x * p.y * p.z * (p.x + p.y + p.z));
}`;

// toon is a material of three tones, its colour or map, with the paper's
// grain on it.
function toon(params) {
  const m = new THREE.MeshToonMaterial({ gradientMap: tones, ...params });
  m.onBeforeCompile = (sh) => {
    sh.vertexShader = 'varying vec3 vGrain;\n' + sh.vertexShader.replace('#include <begin_vertex>', '#include <begin_vertex>\n  vGrain = position;');
    sh.fragmentShader = grain + '\n' + sh.fragmentShader.replace('#include <dithering_fragment>',
      '#include <dithering_fragment>\n  gl_FragColor.rgb *= 0.92 + 0.08 * grainHash(floor(vGrain * 2.5));');
  };
  m.customProgramCacheKey = () => 'inua-toon';
  return m;
}

// The ink of the outlines: the inside of a mesh pushed out along its
// normals (smooth shapes), or of a copy a little larger (a die's box,
// whose corners would split).
const ink = new THREE.MeshBasicMaterial({ color: '#1f1b17', side: THREE.BackSide });
ink.onBeforeCompile = (sh) => {
  sh.vertexShader = sh.vertexShader.replace('#include <begin_vertex>', '#include <begin_vertex>\n  transformed += normalize(normal) * 1.1;');
};
ink.customProgramCacheKey = () => 'inua-ink';
const inkFlat = new THREE.MeshBasicMaterial({ color: '#1f1b17', side: THREE.BackSide });

// outlined gives mesh its ink outline; scale for a copy that much larger.
function outlined(mesh, scale) {
  const o = new THREE.Mesh(mesh.geometry, scale ? inkFlat : ink);
  if (scale) o.scale.setScalar(scale);
  mesh.add(o);
  return mesh;
}

// blobTexture is a soft ink blot, for the contact shadows.
let blobTex = null;
function blobTexture() {
  if (blobTex) return blobTex;
  const cv = document.createElement('canvas');
  cv.width = cv.height = 64;
  const g = cv.getContext('2d');
  const r = g.createRadialGradient(32, 32, 0, 32, 32, 32);
  r.addColorStop(0, 'rgba(31,27,23,0.42)');
  r.addColorStop(0.55, 'rgba(31,27,23,0.22)');
  r.addColorStop(1, 'rgba(31,27,23,0)');
  g.fillStyle = r;
  g.fillRect(0, 0, 64, 64);
  blobTex = new THREE.CanvasTexture(cv);
  return blobTex;
}

// dissolving is a sprite material of its own that shows as much of the
// picture as userData.shown says (0 none, 1 all): the rest gone in a
// noise of ink, its edge soaking dark.
function dissolving(base) {
  const m = base.clone();
  m.userData.shown = { value: 1 };
  m.onBeforeCompile = (sh) => {
    sh.uniforms.uShown = m.userData.shown;
    sh.fragmentShader = `uniform float uShown;
float inkRand(vec2 i) { return fract(sin(dot(i, vec2(127.1, 311.7))) * 43758.5453); }
float inkNoise(vec2 p) {
  vec2 i = floor(p), f = fract(p);
  f = f * f * (3.0 - 2.0 * f);
  return mix(mix(inkRand(i), inkRand(i + vec2(1, 0)), f.x), mix(inkRand(i + vec2(0, 1)), inkRand(i + vec2(1, 1)), f.x), f.y);
}
` + sh.fragmentShader.replace('#include <map_fragment>', `#include <map_fragment>
  float n = 0.65 * inkNoise(vMapUv * 9.0) + 0.35 * inkNoise(vMapUv * 23.0);
  float cut = uShown * 1.14 - 0.07;
  if (n > cut) discard;
  diffuseColor.rgb = mix(diffuseColor.rgb, vec3(0.12, 0.105, 0.09), smoothstep(cut - 0.09, cut, n) * 0.85);`);
  };
  m.customProgramCacheKey = () => 'inua-dissolve';
  return m;
}

class Board {
  constructor(m) {
    this.m = m;
    this.size = m.size;
    this.host = $('board');
    this.renderer = new THREE.WebGLRenderer({ antialias: true });
    this.renderer.setPixelRatio(Math.min(2, devicePixelRatio));
    // No shadow maps: the light app's pieces carry their own shadows, and
    // the pawns and dice an ink blot each (blob), lighter on a phone.
    this.host.append(this.renderer.domElement);

    this.scene = new THREE.Scene();
    this.scene.background = new THREE.Color('#e4dac4');
    this.camera = new THREE.PerspectiveCamera(38, 1, 5, 20000);
    // As the light app looks at it: 62° above the table, a board unit
    // about 1.2 pixels on a desktop's screen.
    const elev = 62 * Math.PI / 180, d = 820;
    this.camera.position.set(0, d * Math.sin(elev), d * Math.cos(elev));
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
    this.scene.add(sun);

    this.tiles = m.squares.map((q) => q.tile);
    this.addPaper();
    this.pictures = new Map(); // the pieces' pictures by name, as they load
    if (m.art) {
      for (const p of m.pieces || []) this.stand(p.name, p.x, p.y).then((s) => s && this.scene.add(s));
    }
    this.pawns = m.players.map((_, i) => this.addPawn(i));
    this.buildings = new Map();
    this.dieMeshes = [this.addDie(), this.addDie()]; // shown once rolled
    this.marks = new THREE.Group();
    this.scene.add(this.marks);
    this.rolling = -1;

    // The effects over the scene: money won and lost, cards got and
    // played (fortune_fx.go's), drawn each frame on a canvas of their own.
    this.fx = el('canvas', 'fx');
    this.host.append(this.fx);
    this.money = [];
    this.hover = -1;
    this.stateKey = '';

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
    // What changes on the print (paintTileState, paintMarks): drawn on a
    // layer just over it when it changes.
    this.stateCanvas = document.createElement('canvas');
    this.stateCanvas.width = this.stateCanvas.height = 2048;
    this.stateTex = new THREE.CanvasTexture(this.stateCanvas);
    this.stateTex.colorSpace = THREE.SRGBColorSpace;
    this.stateTex.anisotropy = this.renderer.capabilities.getMaxAnisotropy();
    const layer = new THREE.Mesh(new THREE.PlaneGeometry(s, s),
      new THREE.MeshStandardMaterial({ map: this.stateTex, transparent: true, depthWrite: false, roughness: 0.92 }));
    layer.rotation.x = -Math.PI / 2;
    layer.position.y = 0.25;
    layer.receiveShadow = true;
    this.scene.add(layer);
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
      g.restore();
    });
    return cv;
  }

  addPawn(i) {
    const color = playerColors[i];
    const pawn = new THREE.Group();
    const lacquer = toon({ color });
    const body = new THREE.Mesh(new THREE.CylinderGeometry(6.5, 10, 20, 24), lacquer);
    body.position.y = 10;
    const head = new THREE.Mesh(new THREE.SphereGeometry(8, 24, 16), toon({ color: '#f4e2c8' }));
    head.position.y = 27;
    const hair = new THREE.Mesh(new THREE.SphereGeometry(8.3, 24, 12, 0, Math.PI * 2, 0, Math.PI / 2.2), toon({ color: '#1f1b17' }));
    hair.position.y = 27.5;
    const ring = new THREE.Mesh(new THREE.TorusGeometry(9.5, 1.4, 8, 32), lacquer);
    ring.rotation.x = Math.PI / 2;
    ring.position.y = 1.4;
    for (const part of [body, head, ring]) pawn.add(outlined(part));
    pawn.add(hair);
    pawn.userData.slot = i;
    this.scene.add(pawn);
    this.blob(pawn, 30);
    return pawn;
  }

  // spot is where player i's pawn stands on sq (tokenAt): each player a
  // place of its own, clear of the print; in jail, behind the bars.
  spot(i, sq, jailed = false) {
    let x, y;
    if (sq % 20 === 0 && sq < 80) { // a corner
      [x, y] = jailed ? [-6 + 12 * (i % 2), 18 + 10 * Math.floor(i / 2)] : [-15 + 30 * (i % 2), -4 + 12 * Math.floor(i / 2)];
    } else {
      [x, y] = [(i - 1.5) * this.tiles[sq].along * 0.17, 10 + 8 * (i % 2)];
    }
    const [u, v] = this.at(sq, x, y);
    return this.world(u, v);
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
      return toon({ map: tex });
    });
    const die = outlined(new THREE.Mesh(new THREE.BoxGeometry(18, 18, 18), faces), 1.09);
    die.visible = false;
    this.scene.add(die);
    this.blob(die, 26);
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
      const from = this.spot(e.Player, e.From, shown.InJail[e.Player]), to = this.spot(e.Player, e.To, e.View.InJail[e.Player]);
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
    this.v = v;
    this.pawns.forEach((p, i) => {
      p.visible = !v.Out[i];
      p.position.copy(this.spot(i, v.At[i], v.InJail[i]));
      p.userData.home = p.position.clone();
    });
    v.Owner.forEach((o, sq) => {
      const key = `${o}:${v.Houses[sq]}`;
      if (key === '-1:0' && !this.buildings.has(sq)) return;
      const had = this.buildings.get(sq);
      if (had?.key === key) return;
      if (had) {
        const gone = had.group;
        const sprites = gone.children.filter((m) => m.isSprite);
        if (this.updated && sprites.length) { // pulled down or sold: it dissolves away
          let left = sprites.length;
          for (const sp of sprites) this.fade(sp, 1, 0, 0.55, () => --left || this.scene.remove(gone));
        } else {
          this.scene.remove(gone);
        }
        gone.traverse((m) => m.isSprite || m.geometry?.dispose()); // sprites share theirs
      }
      if (o < 0) return this.buildings.delete(sq);
      const group = this.building(sq, o, v.Houses[sq], this.updated);
      this.scene.add(group);
      this.buildings.set(sq, { key, group });
    });
    this.updated = true; // from now on, buildings come and go in ink
  }

  // blob lays an ink blot on the board under obj, a soft contact shadow
  // that follows it, smaller and fainter as it rises.
  blob(obj, size) {
    const b = new THREE.Mesh(new THREE.PlaneGeometry(size, size),
      new THREE.MeshBasicMaterial({ map: blobTexture(), transparent: true, depthWrite: false }));
    b.rotation.x = -Math.PI / 2;
    b.renderOrder = 1;
    this.scene.add(b);
    (this.blobs ??= []).push({ obj, b });
  }

  // shadows moves the blots under what casts them.
  shadows() {
    for (const { obj, b } of this.blobs) {
      const lift = Math.max(0, obj.position.y - (obj.geometry ? 9 : 0)); // a die rests at 9
      b.visible = obj.visible;
      b.position.set(obj.position.x, 0.4, obj.position.z);
      b.scale.setScalar(1 + lift / 70);
      b.material.opacity = Math.max(0.25, 1 - lift / 60);
    }
  }

  // fade dissolves a sprite in ink, from shown to shown (0 none, 1
  // whole) over dur seconds, then calls done.
  fade(sprite, from, to, dur, done) {
    const base = sprite.material;
    sprite.material = dissolving(base);
    sprite.material.userData.shown.value = from;
    (this.fades ??= []).push({ sprite, from, to, dur, t: 0, done: () => {
      if (to >= 1) { // whole again: back to the picture's own material
        sprite.material.dispose();
        sprite.material = base;
      }
      done?.();
    } });
  }

  stepFades(dt) {
    this.fades = (this.fades || []).filter((f) => {
      f.t += dt;
      const k = Math.min(1, f.t / f.dur);
      f.sprite.material.userData.shown.value = f.from + (f.to - f.from) * k * k * (3 - 2 * k);
      if (k < 1) return true;
      f.done?.();
      return false;
    });
  }

  // picture loads a piece's picture once: its texture, size and foot.
  picture(name) {
    if (!this.pictures.has(name)) {
      this.pictures.set(name, fetch(`art/${name}.png`).then(async (res) => {
        if (!res.ok) return null;
        const [w, h, footX, dv] = (res.headers.get('X-Piece') || '').split(' ').map(Number);
        const url = URL.createObjectURL(await res.blob());
        const tex = await new THREE.TextureLoader().loadAsync(url);
        URL.revokeObjectURL(url);
        tex.colorSpace = THREE.SRGBColorSpace;
        tex.anisotropy = this.renderer.capabilities.getMaxAnisotropy();
        return { material: new THREE.SpriteMaterial({ map: tex, transparent: true, depthWrite: false }), w, h, footX, dv };
      }).catch(() => null));
    }
    return this.pictures.get(name);
  }

  // stand is a sprite of picture name standing at board point x, y: as
  // the light app draws it, facing the view, its lowest edge on the board.
  async stand(name, x, y) {
    const pic = await this.picture(name);
    if (!pic?.w) return null;
    const s = new THREE.Sprite(pic.material);
    s.center.set(pic.footX / pic.w, 0);
    s.scale.set(pic.w, pic.h, 1);
    s.position.copy(this.world(x, y + pic.dv, 0));
    return s;
  }

  // building is a street's houses or hotel along its colour band: the
  // city's own (machiya, brownstones …) as the light app draws them, in
  // their owner's colour; else plain ones.
  building(sq, o, houses, appear) {
    const t = this.tiles[sq], g = new THREE.Group();
    if (this.m.art && houses) {
      const mid = -t.across / 2 + this.m.band / 2; // the band's middle line
      const along = Math.abs(t.tan[0]) >= Math.abs(t.tan[1]) ? 1 : 0;
      const put = (name, x) => {
        const [u, v] = this.at(sq, x, mid);
        this.stand(name, u, v).then((s) => {
          if (!s) return;
          g.add(s);
          if (appear) this.fade(s, 0, 1, 0.6); // built: it soaks into the paper
        });
      };
      if (houses >= 5) put(`hotel-${along}-${o}`, 0);
      else for (let k = 0; k < houses; k++) put(`house-${along}-${o}`, (k - (houses - 1) / 2) * Math.min(11.6, t.along / 4.2));
      return g;
    }
    const angle = -Math.atan2(t.tan[1], t.tan[0]);
    const color = new THREE.Color(playerColors[o]);
    if (!houses) return g;
    const band = -t.across / 2 + this.m.band / 2;
    const walls = new THREE.MeshStandardMaterial({ color: '#f6f0e0', roughness: 0.8 });
    const roof = new THREE.MeshStandardMaterial({ color, roughness: 0.55 });
    const put = (mesh, x, y, h) => {
      const [px, py] = this.at(sq, x, y);
      mesh.position.copy(this.world(px, py, h));
      mesh.rotation.y = angle;
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

  // drawState draws what changes on the print (paintTileState and
  // paintMarks): the owners' tint, a price or the rent now and the owner's
  // seal; the square under the pointer, the streets that may go up a
  // level this turn, the square picked. Only when that changed.
  drawState() {
    const v = this.v;
    if (!v) return;
    const build = myTurn() ? latest.build || [] : [];
    const key = JSON.stringify([v.Owner, v.Houses, this.hover, selected, build]);
    if (key === this.stateKey) return;
    this.stateKey = key;
    const n = this.stateCanvas.width, k = n / this.size;
    const g = this.stateCanvas.getContext('2d');
    g.setTransform(1, 0, 0, 1, 0, 0);
    g.clearRect(0, 0, n, n);
    g.scale(k, k);
    const path = (pts) => {
      g.beginPath();
      for (let i = 0; i < pts.length; i += 2) g.lineTo(pts[i], pts[i + 1]);
      g.closePath();
    };
    const mincho = '"Shippori Mincho", "Zen Old Mincho", "Noto Serif CJK JP", serif';
    this.m.squares.forEach((q, i) => {
      const t = q.tile, o = v.Owner[i];
      if (o >= 0) {
        path(t.outline);
        g.globalAlpha = 0.13;
        g.fillStyle = playerColors[o];
        g.fill();
        g.globalAlpha = 1;
      }
      if (!['street', 'pier', 'utility', 'tax'].includes(q.kind)) return;
      g.save();
      g.translate(t.center[0], t.center[1]);
      g.rotate(Math.atan2(t.in[0], -t.in[1]));
      g.font = `500 6.2px ${mincho}`;
      g.textAlign = 'center';
      g.textBaseline = 'bottom';
      let text = '$' + q.price, ink = '#8a8178';
      if (q.kind === 'tax') [text, ink] = [`pay $${q.price}`, '#bf4430'];
      else if (o >= 0 && q.kind === 'utility') [text, ink] = ['dice ×', '#1f1b17'];
      else if (o >= 0) [text, ink] = [`rent $${money(rentOf(v, i))}`, '#1f1b17'];
      g.fillStyle = ink;
      g.fillText(text, 0, t.across / 2 - 4);
      if (o >= 0) { // the owner's seal
        const sz = 9, top = -t.across / 2 + (q.kind === 'street' ? this.m.band : 0);
        const sx = t.along / 2 - sz - 2.5, sy = top + 2.5;
        g.fillStyle = playerColors[o];
        g.beginPath();
        g.roundRect(sx, sy, sz, sz, 1.5);
        g.fill();
        g.font = `700 5.5px ${mincho}`;
        g.textBaseline = 'middle';
        g.fillStyle = '#f1e9d6';
        g.fillText('一二三四'[o], sx + sz / 2, sy + sz / 2 + 0.3);
      }
      g.restore();
    });
    if (this.hover >= 0) {
      path(this.tiles[this.hover].outline);
      g.fillStyle = 'rgba(255,255,255,0.32)';
      g.fill();
    }
    for (const sq of build) {
      path(this.tiles[sq].outline);
      g.lineWidth = 2.2;
      g.setLineDash([5, 4]);
      g.strokeStyle = '#5e843e';
      g.stroke();
      g.setLineDash([]);
    }
    if (selected >= 0) {
      path(this.tiles[selected].outline);
      g.lineWidth = 3;
      g.lineJoin = 'round';
      g.strokeStyle = '#bf4430';
      g.stroke();
    }
    this.stateTex.needsUpdate = true;
  }

  // screen is where board point x, y at height h is on the screen, in CSS
  // pixels; unit how many pixels a board unit is there.
  screen(x, y, h = 0) {
    const p = this.world(x, y, h).project(this.camera);
    return [(p.x + 1) / 2 * this.w, (1 - p.y) / 2 * this.h];
  }
  unit(x, y) {
    const a = this.screen(x, y), b = this.screen(x + 1, y);
    return Math.hypot(b[0] - a[0], b[1] - a[1]);
  }

  // pawnAt is where player i's pawn is now on the board, and how high.
  pawnAt(i) {
    const p = this.pawns[i].position;
    return [p.x + this.size / 2, p.z + this.size / 2, p.y];
  }

  // moneyFrom starts the effects of cash changing between two views.
  moneyFrom(old, now) {
    now.Cash.forEach((c, i) => {
      if (old && c !== old.Cash[i]) this.money.push({ player: i, amount: c - old.Cash[i], t: 0, seed: Math.random() * 2 ** 32 >>> 0 });
    });
  }

  // jolt moves the pawns with the money they get: a hop up when they win,
  // a shake when they lose.
  jolt() {
    this.pawns.forEach((p, i) => {
      const home = p.userData.home;
      if (!home || (cur && cur.Player === i && [EvWalk, EvJump].includes(cur.Kind))) return;
      let dx = 0, lift = 0;
      for (const e of this.money) {
        if (e.player !== i) continue;
        if (e.amount > 0 && e.t < 0.4) lift += Math.sin(Math.PI * e.t / 0.4) * 7;
        if (e.amount < 0 && e.t < 0.5) dx += Math.sin(e.t * 55) * 1.8 * (1 - e.t / 0.5);
      }
      p.position.set(home.x + dx, home.y + lift, home.z);
    });
  }

  // drawFX draws the money won and lost at the pawns, and the card being
  // got or played.
  drawFX(dt) {
    const g = this.fx.getContext('2d'), r = Math.min(2, devicePixelRatio);
    g.setTransform(r, 0, 0, r, 0, 0);
    g.clearRect(0, 0, this.w, this.h);
    this.money = this.money.filter((e) => (e.t += dt) < fxTime);
    const stack = {};
    for (const e of this.money) {
      const [u, v, lift] = this.pawnAt(e.player);
      const n = stack[e.player] = (stack[e.player] ?? -1) + 1;
      const rnd = random(e.seed);
      if (e.amount > 0) this.gain(g, u, v, lift, e.t, rnd);
      else this.loss(g, u, v, lift, e.t, rnd);
      this.sum(g, u, v, lift + n * 14, e);
    }
    if (cur?.Kind === EvCard) this.cardFX(g, cur, curT);
    // The fountain's pot, over it.
    if (this.v) {
      const [fu, fv] = this.at(40, 0, 24);
      const [x, y] = this.screen(fu + 10, fv + 10, 30);
      g.font = `700 ${9.5 * this.unit(fu, fv) / 1.2}px "Shippori Mincho", "Noto Serif CJK JP", serif`;
      g.textAlign = 'center';
      g.textBaseline = 'middle';
      g.fillStyle = shu;
      g.fillText('$' + money(this.v.Pot), x, y);
    }
  }

  // ring strokes a circle of radius rad lying on the board at x, y.
  ring(g, x, y, rad, h = 0) {
    g.beginPath();
    for (let a = 0; a <= 32; a++) {
      const [sx, sy] = this.screen(x + Math.cos(a / 16 * Math.PI) * rad, y + Math.sin(a / 16 * Math.PI) * rad, h);
      g.lineTo(sx, sy);
    }
  }

  // gain: a gold pulse on the board, coins thrown up spinning, sparkles.
  gain(g, u, v, lift, t, rnd) {
    const k = this.unit(u, v);
    if (t < 0.9) {
      const f = t / 0.9;
      this.ring(g, u, v, 8 + f * 30);
      g.lineWidth = 2.5 * (1 - f) * k / 1.2;
      g.strokeStyle = rgba(gold, 1 - f);
      g.stroke();
    }
    const fade = t > 1.2 ? Math.max(0, 1 - (t - 1.2) / (fxTime - 1.2)) : 1;
    for (let i = 0; i < 11; i++) {
      const a = rnd() * 2 * Math.PI, sp = 10 + rnd() * 22, up = 60 + rnd() * 40, spin = 8 + rnd() * 10, delay = rnd() * 0.15;
      const tt = Math.max(0, t - delay);
      let z = lift + 26 + up * tt - 95 * tt * tt, bounce = false;
      if (z < 0) [z, bounce] = [0, true];
      const [x, y] = this.screen(u + Math.cos(a) * sp * tt, v + Math.sin(a) * sp * tt * 0.7, z);
      const h = 3.6 * k, w = bounce ? h : Math.abs(Math.cos(tt * spin)) * h;
      g.beginPath();
      g.ellipse(x, y, Math.max(w, 0.6), h, 0, 0, 2 * Math.PI);
      const grad = g.createLinearGradient(x - w, y - h, x + w, y + h);
      grad.addColorStop(0, rgba('#ffe79a', fade));
      grad.addColorStop(1, rgba(goldDeep, fade));
      g.fillStyle = grad;
      g.fill();
      g.lineWidth = 0.7;
      g.strokeStyle = rgba(goldDeep, fade);
      g.stroke();
    }
    for (let i = 0; i < 7; i++) {
      const a = rnd() * 2 * Math.PI, d = 6 + rnd() * 18, ph = rnd() * Math.PI, tw = Math.sin(t * 9 + ph), hz = rnd() * 24;
      if (tw < 0) continue;
      const [x, y] = this.screen(u + Math.cos(a) * d, v + Math.sin(a) * d * 0.6, lift + 18 + hz);
      star(g, x, y, 3.2 * tw * k, gold, fade);
    }
  }

  // loss: red ink splashed on the board, bills fluttering away.
  loss(g, u, v, lift, t, rnd) {
    const k = this.unit(u, v);
    const fade = t > 0.9 ? Math.max(0, 1 - (t - 0.9) / (fxTime - 0.9)) : 1;
    const grow = 1 - (1 - Math.min(1, t / 0.25)) ** 3;
    const splash = (count, dist, size, alpha) => {
      for (let i = 0; i < count; i++) {
        const a = rnd() * 2 * Math.PI, d = dist(), rr = size();
        this.ring(g, u + Math.cos(a) * d, v + Math.sin(a) * d, rr);
        g.fillStyle = rgba(shu, alpha * fade);
        g.fill();
      }
    };
    splash(9, () => rnd() * 14 * grow, () => (2 + rnd() * 6) * grow, 0.38);
    splash(6, () => (14 + rnd() * 12) * grow, () => 0.8 + rnd() * 1.2, 0.5);
    for (let i = 0; i < 7; i++) {
      const a = rnd() * 2 * Math.PI, sp = 12 + rnd() * 20, delay = rnd() * 0.2, rs = rnd(), fl = rnd();
      const tt = Math.max(0, t - delay);
      const z = Math.max(1, lift + 28 + 28 * tt - 34 * tt * tt);
      const [x, y] = this.screen(u + Math.cos(a) * sp * tt, v + Math.sin(a) * sp * tt * 0.6, z);
      const flutter = Math.cos(tt * (10 + fl * 6)), bw = 7 * k, bh = 3.6 * k;
      g.save();
      g.translate(x, y);
      g.rotate(a + tt * (4 + rs * 4));
      g.scale(1, 0.35 + 0.65 * Math.abs(flutter));
      g.fillStyle = rgba('#c9d9b8', fade);
      g.fillRect(-bw / 2, -bh / 2, bw, bh);
      g.lineWidth = 0.6;
      g.strokeStyle = rgba('#6e8c6a', fade);
      g.strokeRect(-bw / 2, -bh / 2, bw, bh);
      g.beginPath();
      g.arc(0, 0, bh * 0.28, 0, 2 * Math.PI);
      g.fillStyle = rgba('#6e8c6a', fade);
      g.fill();
      g.restore();
    }
  }

  // sum draws the money won or lost over the pawn: rising in gold, or
  // sinking in red with a shudder.
  sum(g, u, v, lift, e) {
    const t = e.t, a = t > 1.25 ? Math.max(0, 1 - (t - 1.25) / (fxTime - 1.25)) : 1;
    const pop = 1 + 0.6 * Math.max(0, 1 - t / 0.22);
    let text = '+$' + money(e.amount), col = '#c88a1e', z = lift + 44 + 22 * (1 - (1 - Math.min(1, t / fxTime)) ** 2), dx = 0;
    if (e.amount < 0) {
      text = '−$' + money(-e.amount);
      col = shu;
      z = lift + 54 - 18 * Math.min(1, t / fxTime);
      dx = Math.sin(t * 42) * 3 * Math.max(0, 1 - t / 0.5);
    }
    const [x, y] = this.screen(u, v, z), k = this.unit(u, v);
    g.font = `700 ${15 * pop * k / 1.2}px "Shippori Mincho", "Noto Serif CJK JP", serif`;
    g.textAlign = 'center';
    g.textBaseline = 'middle';
    g.fillStyle = rgba('#1f1b17', 0.55 * a);
    g.fillText(text, x + dx + 1, y + 1.2);
    g.fillStyle = rgba(col, a);
    g.fillText(text, x + dx, y);
  }

  // cardFX draws the card being got or played, t into it (paintCardFX).
  // Got by another: it rises off their pawn face down, shines, and settles
  // back. Got by you: it rises over your hand turning face up, shines, and
  // drops into it. Played: it rises to the middle of the view, large, rays
  // behind it, then flies to what it was played on and ripples there, or
  // bursts where it is.
  cardFX(g, e, t) {
    const [pu, pv, pz] = this.pawnAt(e.Player);
    let [fx, fy] = this.screen(pu, pv, pz + 26);
    const k1u = this.unit(pu, pv) / 1.2; // the light app's units, here
    const ink = cardInks[e.Card];
    const ease = (f) => { f = Math.max(0, Math.min(1, f)); return f * f * (3 - 2 * f); };
    if (!e.Played) {
      let lift = 70 * k1u, k0 = 0.3, k1 = 0.85, sink = 0;
      if (e.Player === seat) { // over the hand, clear of it, then down into it
        [fx, fy] = [this.w / 2, this.h - 150];
        [lift, k0, k1, sink] = [140, 0.6, 1.3, 130];
      }
      const rise = ease(t / 0.35);
      let y = fy - lift * rise, k = k0 + (k1 - k0) * rise;
      const turn = Math.PI * (1 - rise);
      if (t > 0.8) {
        const f = ease((t - 0.8) / 0.3);
        y = fy - lift + (lift + sink) * f;
        k = k1 - (k1 - k0) * f;
      }
      if (t > 0.3 && t < 0.9) {
        glow(g, fx, y, 70, washiGold, 0.55 * Math.sin((t - 0.3) / 0.6 * Math.PI));
        const rnd = random(e.Card * 7 + 1);
        for (let i = 0; i < 7; i++) {
          const a = rnd() * 2 * Math.PI, d = 40 + rnd() * 40 * (t - 0.3), s2 = 3 + rnd() * 3;
          star(g, fx + Math.cos(a) * d, y + Math.sin(a) * d * 0.8, s2, washiGold, 1 - (t - 0.3) / 0.6);
        }
      }
      drawCard(g, e.Back || Math.cos(turn) < 0 ? -1 : e.Card, fx, y, k, 0, Math.cos(turn));
      return;
    }
    const stage = [this.w / 2, this.h / 2 - 40];
    let to = stage, ripple = null;
    const target = this.m.cards[e.Card]?.target;
    if (e.Target >= 0 && target === 'player') {
      const [tu, tv, tz] = this.pawnAt(e.Target);
      to = this.screen(tu, tv, tz + 20);
      ripple = [tu, tv];
    } else if (e.Target >= 0 && target === 'square') {
      const c = this.tiles[e.Target].center;
      to = this.screen(c[0], c[1]);
      ripple = c;
    }
    if (t < 0.4) { // up to the stage, growing
      const f = ease(t / 0.4);
      drawCard(g, e.Card, fx + (stage[0] - fx) * f, fy + (stage[1] - fy) * f, 0.3 + 1.3 * f, -0.3 * (1 - f), 1);
    } else if (t < 1.0) { // shown, rays behind
      const f = (t - 0.4) / 0.6;
      glow(g, stage[0], stage[1], 150, ink, 0.45 * Math.sin(f * Math.PI));
      g.lineWidth = 5;
      for (let i = 0; i < 16; i++) {
        const a = i * Math.PI / 8 + t * 0.8, r0 = 70 + 30 * f, r1 = 120 + 80 * f;
        g.strokeStyle = rgba(i % 2 ? washiGold : ink, 0.5 * (1 - f));
        g.beginPath();
        g.moveTo(stage[0] + Math.cos(a) * r0, stage[1] + Math.sin(a) * r0);
        g.lineTo(stage[0] + Math.cos(a) * r1, stage[1] + Math.sin(a) * r1);
        g.stroke();
      }
      drawCard(g, e.Card, stage[0], stage[1], 1.6 + 0.06 * Math.sin(f * Math.PI * 2), 0, 1);
    } else { // off to the target, or a burst
      const f = ease((t - 1.0) / 0.45);
      if (!ripple) {
        for (let i = 0; i < 12; i++) {
          const a = i * Math.PI / 6, d = 40 + 120 * f;
          star(g, stage[0] + Math.cos(a) * d, stage[1] + Math.sin(a) * d, 6 * (1 - f) + 1, ink, 1 - f);
        }
        if (f < 1) drawCard(g, e.Card, stage[0], stage[1], 1.6 * (1 - f) + 0.01, 0, 1);
        return;
      }
      if (f < 1) drawCard(g, e.Card, stage[0] + (to[0] - stage[0]) * f, stage[1] + (to[1] - stage[1]) * f, 1.6 - 1.4 * f, f * 0.6, 1);
      const gr = (t - 1.3) / 0.3;
      if (gr > 0) {
        for (let i = 0; i < 3; i++) {
          this.ring(g, ripple[0], ripple[1], (gr + i * 0.25) * 50);
          g.lineWidth = 3;
          g.strokeStyle = rgba(ink, Math.max(0, 0.8 - gr * 0.6 - i * 0.2));
          g.stroke();
        }
      }
    }
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
    const squareAt = (e) => {
      const r = canvas.getBoundingClientRect();
      ndc.set(((e.clientX - r.left) / r.width) * 2 - 1, -((e.clientY - r.top) / r.height) * 2 + 1);
      ray.setFromCamera(ndc, this.camera);
      const hit = ray.intersectObject(this.paper)[0];
      if (!hit) return -1;
      const x = hit.point.x + this.size / 2, y = hit.point.z + this.size / 2;
      return this.tiles.findIndex((t) => inside(t.outline, x, y));
    };
    canvas.addEventListener('pointermove', (e) => {
      if (e.pointerType === 'mouse' && !e.buttons) this.hover = squareAt(e);
    });
    canvas.addEventListener('pointerleave', () => (this.hover = -1));
    canvas.addEventListener('pointerdown', (e) => (down = [e.clientX, e.clientY]));
    canvas.addEventListener('pointerup', (e) => {
      if (!down || Math.hypot(e.clientX - down[0], e.clientY - down[1]) > 6) return;
      const sq = squareAt(e);
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
    [this.w, this.h] = [w, h];
    const r = Math.min(2, devicePixelRatio);
    this.fx.width = w * r;
    this.fx.height = h * r;
    this.camera.aspect = w / h;
    this.camera.updateProjectionMatrix();
  }

  render(dt) {
    this.controls.update();
    // Keep the view over the board.
    const t = this.controls.target, lim = this.size / 2;
    const cx = Math.max(-lim, Math.min(lim, t.x)) - t.x, cz = Math.max(-lim, Math.min(lim, t.z)) - t.z;
    if (cx || cz) {
      t.x += cx; t.z += cz;
      this.camera.position.x += cx; this.camera.position.z += cz;
    }
    this.drawState();
    this.jolt();
    this.shadows();
    this.stepFades(dt);
    this.renderer.render(this.scene, this.camera);
    this.drawFX(dt);
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

// The effects' colours and how long money's effect plays (fortune_fx.go).
const gold = '#f2c24e', goldDeep = '#b07a12', shu = '#bf4430', washiGold = '#d9a43a';
const fxTime = 1.8;

// random is a seeded generator of numbers in [0, 1): an effect's own,
// the same each frame.
function random(seed) {
  let a = seed >>> 0;
  return () => {
    a = (a + 0x6d2b79f5) >>> 0;
    let t = a;
    t = Math.imul(t ^ (t >>> 15), t | 1);
    t ^= t + Math.imul(t ^ (t >>> 7), t | 61);
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
  };
}

// rgba is colour hex at alpha a.
function rgba(hex, a) {
  const n = parseInt(hex.slice(1), 16);
  return `rgba(${n >> 16},${(n >> 8) & 255},${n & 255},${Math.max(0, Math.min(1, a))})`;
}

// star draws a four-pointed sparkle.
function star(g, x, y, s, col, a) {
  g.beginPath();
  g.moveTo(x, y - s);
  g.quadraticCurveTo(x, y, x + s, y);
  g.quadraticCurveTo(x, y, x, y + s);
  g.quadraticCurveTo(x, y, x - s, y);
  g.quadraticCurveTo(x, y, x, y - s);
  g.fillStyle = rgba('#ffffff', a);
  g.fill();
  g.lineWidth = 0.6;
  g.strokeStyle = rgba(col, a);
  g.stroke();
}

// glow is a soft light of colour col, radius r, at x, y.
function glow(g, x, y, r, col, a) {
  const grad = g.createRadialGradient(x, y, 0, x, y, r);
  grad.addColorStop(0, rgba(col, a));
  grad.addColorStop(1, rgba(col, 0));
  g.fillStyle = grad;
  g.fillRect(x - r, y - r, 2 * r, 2 * r);
}

// cardImages are the cards' pictures, -1 the back, for the effects.
let cardImages = {};
function loadCards() {
  cardImages = {};
  if (!game.art) return;
  const load = (k, name) => {
    const img = new Image();
    img.src = `art/${name}.png`;
    cardImages[k] = img;
  };
  game.cards.forEach((_, k) => load(k, `card-${k}`));
  load(-1, 'card-back');
}

// drawCard draws card c (-1 its back) about x, y, k times its size (the
// light app's 70 by 100), turned by a and squeezed across by flip.
function drawCard(g, c, x, y, k, a, flip) {
  const img = cardImages[c];
  const w = 74 * k, h = 104 * k;
  g.save();
  g.translate(x, y);
  g.rotate(a);
  g.scale(Math.max(0.02, Math.abs(flip)), 1);
  if (img?.complete && img.naturalWidth) g.drawImage(img, -w / 2, -h / 2, w, h);
  else {
    g.fillStyle = c < 0 ? '#222f4c' : '#f7f1e3';
    g.strokeStyle = c < 0 ? '#222f4c' : cardInks[c];
    g.lineWidth = 2;
    g.beginPath();
    g.roundRect(-w / 2, -h / 2, w, h, 5 * k);
    g.fill();
    g.stroke();
  }
  g.restore();
}

// --- sound -------------------------------------------------------------------

// The light app's sounds (fortune_sound.go), made the same way: short
// tones, sweeps and noise, each fading out, mixed with a soft clip. They
// follow the board as it shows the game: the dice rattle and land, the
// pawn ticks on each square, money chimes or thuds. M or the speaker
// mutes them. A browser lets a page sound only after a click or a key.
let audio = null, muted = store('small-fortune:muted') === '1';
const SR = 48000, Sine = 0, Square = 1, Tri = 2, Noise = 3;

function wake() {
  if (!audio) {
    try { audio = new AudioContext({ sampleRate: SR }); } catch { return; }
  }
  if (audio.state === 'suspended') audio.resume();
}
addEventListener('pointerdown', wake);
addEventListener('keydown', wake);

const tone = (wave, delay, dur, f0, f1, vol) => ({ wave, delay, dur, f0, f1, vol });
const notes = (wave, step, dur, vol, ...fs) => fs.map((f, i) => tone(wave, i * step, dur, f, f, vol));

// voicesOf are a sound's voices; step is how far the pawn has come, for
// its ticks to climb a little.
function voicesOf(name, step) {
  const vs = [];
  switch (name) {
    case 'dice':
      for (let i = 0; i < 7; i++) {
        const at = i * 0.06 + Math.random() * 0.03;
        vs.push(tone(Noise, at, 0.018, 0, 0, 0.13), tone(Tri, at, 0.02, 900 + Math.random() * 700, 600, 0.07));
      }
      break;
    case 'diceLand':
      vs.push(tone(Noise, 0, 0.035, 0, 0, 0.2), tone(Tri, 0, 0.06, 240, 150, 0.22), tone(Noise, 0.07, 0.03, 0, 0, 0.16), tone(Tri, 0.07, 0.05, 260, 170, 0.18));
      break;
    case 'step': {
      const f = 480 + Math.min(step, 12) * 18;
      vs.push(tone(Tri, 0, 0.045, f, f * 0.8, 0.13), tone(Noise, 0, 0.012, 0, 0, 0.05));
      break;
    }
    case 'start': vs.push(...notes(Tri, 0.1, 0.16, 0.24, 784, 1047, 1319)); break;
    case 'jump': vs.push(tone(Sine, 0, 0.22, 300, 900, 0.18), tone(Sine, 0.22, 0.24, 900, 260, 0.18)); break;
    case 'coin': vs.push(...notes(Tri, 0.07, 0.14, 0.2, 1568, 2093), tone(Sine, 0.07, 0.25, 3136, 3136, 0.06)); break;
    case 'pay': vs.push(tone(Sine, 0, 0.16, 230, 150, 0.3), tone(Noise, 0, 0.03, 0, 0, 0.08)); break;
    case 'fountain': vs.push(tone(Noise, 0, 0.45, 0, 0, 0.08), ...notes(Tri, 0.07, 0.2, 0.22, 784, 988, 1175, 1568, 2093)); break;
    case 'buy': vs.push(tone(Noise, 0, 0.05, 0, 0, 0.22), tone(Square, 0, 0.09, 150, 90, 0.16), tone(Tri, 0.06, 0.12, 660, 660, 0.12)); break;
    case 'build':
      for (const at of [0, 0.13]) vs.push(tone(Noise, at, 0.025, 0, 0, 0.18), tone(Tri, at, 0.05, 720, 520, 0.18));
      break;
    case 'hotel':
      for (const at of [0, 0.11, 0.22]) vs.push(tone(Noise, at, 0.025, 0, 0, 0.18), tone(Tri, at, 0.05, 620, 440, 0.18));
      vs.push(...notes(Tri, 0.1, 0.25, 0.22, 1047, 1319, 1568));
      break;
    case 'raze': vs.push(tone(Noise, 0, 0.3, 0, 0, 0.14), tone(Square, 0, 0.3, 300, 80, 0.1)); break;
    case 'cardFlip': vs.push(tone(Noise, 0.1, 0.05, 0, 0, 0.12), tone(Tri, 0.1, 0.09, 600, 1300, 0.14)); break;
    case 'cardPlay': vs.push(tone(Sine, 0, 0.42, 380, 1500, 0.17), ...notes(Sine, 0.08, 0.2, 0.12, 1568, 2093, 2637)); break;
    case 'cardHit': vs.push(tone(Sine, 0, 0.14, 900, 280, 0.28), tone(Noise, 0, 0.03, 0, 0, 0.1)); break;
    case 'jail': vs.push(tone(Noise, 0, 0.06, 0, 0, 0.2), tone(Square, 0, 0.12, 120, 90, 0.18), ...notes(Square, 0.18, 0.24, 0.14, 392, 330, 262)); break;
    case 'bankrupt': vs.push(tone(Square, 0, 0.9, 330, 80, 0.18)); break;
    case 'win': vs.push(...notes(Tri, 0.13, 0.22, 0.28, 523, 659, 784, 1047), tone(Tri, 0.52, 0.8, 1319, 1319, 0.28)); break;
    case 'lose': vs.push(...notes(Tri, 0.26, 0.36, 0.24, 392, 330, 262, 196)); break;
    case 'yourTurn': vs.push(...notes(Sine, 0.12, 0.32, 0.16, 880, 1175)); break;
  }
  return vs;
}

// sound plays one of the game's sounds, made now.
function sound(name, step = 0) {
  if (muted || !audio || audio.state !== 'running') return;
  const vs = voicesOf(name, step);
  const n = Math.ceil(Math.max(...vs.map((v) => v.delay + v.dur)) * SR) + 1;
  const mix = new Float32Array(n);
  for (const v of vs) {
    let phase = 0;
    const start = Math.round(v.delay * SR), len = Math.round(v.dur * SR);
    for (let i = 0; i < len && start + i < n; i++) {
      const t = i / SR, k = t / v.dur;
      phase += (v.f0 + (v.f1 - v.f0) * k) / SR;
      phase -= Math.floor(phase);
      let x;
      switch (v.wave) {
        case Sine: x = Math.sin(2 * Math.PI * phase); break;
        case Square: x = phase > 0.5 ? -0.6 : 0.6; break;
        case Tri: x = 4 * Math.abs(phase - 0.5) - 1; break;
        default: x = Math.random() * 2 - 1;
      }
      const env = Math.min(1, t / 0.003) * (1 - k) * (1 - k); // a quick attack, then a fade
      mix[start + i] += x * env * v.vol;
    }
  }
  for (let i = 0; i < n; i++) mix[i] = Math.tanh(mix[i] * 0.9) * 0.55; // soft clipping
  const buf = audio.createBuffer(1, n, SR);
  buf.copyToChannel(mix, 0);
  const src = audio.createBufferSource();
  src.buffer = buf;
  src.connect(audio.destination);
  src.start();
}

// cueSounds plays what the event being shown sounds as it goes, before
// its time moves on by dt: the dice landing, each hop, passing Start, a
// played card landing.
function cueSounds(e, dt) {
  const crossed = (at) => curT < at && curT + dt >= at;
  if (e.Kind === EvDice && crossed(curLength - 0.1)) sound('diceLand');
  if (e.Kind === EvWalk) {
    (e.Path || []).forEach((sq, n) => {
      if (crossed((n + 1) * curLength / e.Path.length - 0.04)) {
        sound('step', n);
        if (sq === 0) sound('start');
      }
    });
  }
  if (e.Kind === EvCard && e.Played && crossed(1.15)) sound('cardHit');
}

// soundsFrom plays what changed between two views of the board:
// buildings up or down, deeds sealed, money, the fountain emptied, jail, a
// player out.
function soundsFrom(old, now) {
  if (!old || old.Cash.length !== now.Cash.length) return;
  let built = false, hotel = false, razed = false, bought = false, gain = false, loss = false;
  now.Houses.forEach((h, sq) => {
    const was = old.Houses[sq];
    if (h > was && h === 5) hotel = true;
    else if (h > was) built = true;
    else if (h < was && now.Owner[sq] >= 0) razed = true;
    if (now.Owner[sq] >= 0 && now.Owner[sq] !== old.Owner[sq]) bought = true;
  });
  for (let i = 0; i < now.Cash.length; i++) {
    gain ||= now.Cash[i] > old.Cash[i];
    loss ||= now.Cash[i] < old.Cash[i];
    if (now.Out[i] && !old.Out[i]) return sound('bankrupt');
    if (now.InJail[i] && !old.InJail[i]) sound('jail');
  }
  if (hotel) sound('hotel');
  else if (built) sound('build');
  else if (razed) sound('raze');
  else if (bought) sound('buy');
  if (old.Pot > 0 && now.Pot === 0 && gain) sound('fountain');
  else if (gain && !bought) sound('coin');
  else if (loss && !bought && !built && !hotel) sound('pay');
}

// ring sounds what the board, come to rest, stands at: the game's end,
// won or lost, once; your turn come round again.
let rung = '', endRung = false;
function ring() {
  if (latest.phase === 'over' && !endRung) {
    endRung = true;
    sound(seat < 0 || latest.winner === seat ? 'win' : 'lose');
  } else if (latest.phase === 'roll' && latest.turn === seat && `${latest.round}` !== rung) {
    rung = `${latest.round}`;
    sound('yourTurn');
  }
}

function setMuted(m) {
  muted = m;
  store('small-fortune:muted', m ? '1' : null);
  const b = $('mute');
  b.replaceChildren(el('span', 'icon'));
  b.firstChild.style.setProperty('--icon', `url(icons/audio-volume-${m ? 'muted' : 'high'}.svg)`);
  b.title = m ? T.soundOff || 'Sound off' : T.soundOn || 'Sound on';
}
$('mute').addEventListener('click', () => setMuted(!muted));
addEventListener('keydown', (e) => {
  if ((e.key === 'm' || e.key === 'M') && !['INPUT', 'TEXTAREA'].includes(document.activeElement?.tagName)) setMuted(!muted);
});
setMuted(muted);

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
    board.render(dt);
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
