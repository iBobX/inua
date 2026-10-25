// Chinese Checkers in a browser: joins the host's game over a WebSocket,
// sets it up in the lobby when this browser leads, shows the star in
// three.js (washi paper on a round board of wood, glass marbles) and plays
// the seat it is given. The host holds the game; the page shows what
// happened, one move after another, and sends its moves.

import * as THREE from 'three';
import { OrbitControls } from 'three/addons/OrbitControls.js';

const $ = (id) => document.getElementById(id);

// T are the page's words, in the host's language: the host's map, over
// these, its English.
let T = {
  title: 'Chinese Checkers', name: 'Your name', joinButton: 'Join', watch: 'Watch',
  waitingHost: 'Waiting for the host to start the game…', waitingLead: 'Waiting for %s to start the game…',
  youLead: 'You set up the game: who plays each seat, the teams, the rules.', full: 'The game is full: you can watch it.',
  watching: 'Watching', reconnect: 'Connection lost: trying again…', players: 'PLAYERS', setup: 'THE GAME',
  happened: 'WHAT HAPPENED', round: 'ROUND', playersN: '%d players', open: 'Open: for someone to join',
  local: "at the host's screen", visitor: 'in a browser', computer: 'Computer', ai: 'AI', aiDefault: 'the default profile',
  easy: 'Easy', normal: 'Normal', hard: 'Hard', team: 'Team %s', you: 'you', leads: 'sets up the game',
  away: 'away, the computer plays', noParking: 'Stay out of other points',
  noParkingNo: "A marble may cross another player's point, but not stop in it.", start: 'Start the game',
  restart: 'New game', toLobby: 'Set up again', turn: "%s's turn", yourTurn: 'Your turn: pick one of your marbles.',
  forPartner: "You are home: move %s's marbles.", pickHole: 'Now pick where it goes, or another marble.',
  home: 'home', place: '#%d home', left: '%d to go', hops: '%d hops', moved: '%s moved', movedHops: '%s hopped %d times',
  movedFor: "%s moved %s's marble", passed: '%s could not move', cameHome: '%s is home', wins: '%s wins',
  teamWins: '%s win', and: '%s and %s', draw: 'No one got home in time: a draw.', over: 'The game is over.',
  rotate: 'Drag to turn the board, scroll to come closer.',
};

const levelKeys = ['easy', 'normal', 'hard'];
const teamLetter = (t) => 'ABCDEF'[t] ?? String(t + 1);

// fmt fills a format's verbs: %s, %d, and %[n]s for another order.
function fmt(s, ...args) {
  let i = 0;
  return String(s ?? '').replace(/%(?:\[(\d+)\])?([sd%])/g, (_, n, v) => {
    if (v === '%') return '%';
    if (n) i = Number(n) - 1;
    return String(args[i++]);
  });
}
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
// names joins names with the host's "and": "A, B and C".
function names(list) {
  if (list.length < 2) return list[0] ?? '';
  return fmt(T.and, list.slice(0, -1).join(', '), list.at(-1));
}
function dot(color) {
  const d = el('span', 'dot');
  d.style.setProperty('--dot', color || '#999');
  return d;
}

// --- joining -------------------------------------------------------------

let code = location.hash.slice(1).toUpperCase().replace(/[^A-Z0-9]/g, '');
let ws = null, seat = -1, joined = false, bye = false, retry = 0, hidden = false, wanted = false;
const keyOf = () => `chinese-checkers:${code}`;
const send = (m) => ws?.readyState === WebSocket.OPEN && ws.send(JSON.stringify(m));

async function init() {
  try {
    T = { ...T, ...(await (await fetch('texts.json')).json()) };
  } catch { /* English, as written */ }
  words();
  $('name').value = store('chinese-checkers:name') || '';
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

// words puts the host's words on the page's fixed text.
function words() {
  for (const e of document.querySelectorAll('[data-t]')) {
    if (T[e.dataset.t]) e.textContent = T[e.dataset.t];
  }
  document.title = T.title || document.title;
  $('board').setAttribute('aria-label', T.rotate);
}

// join connects as name ('' to watch), or with the seat's key (null).
function join(name) {
  if (!code) code = $('code').value.trim().toUpperCase();
  if (name) store('chinese-checkers:name', name);
  wanted = name !== '';
  $('join-form').hidden = true;
  connect(name ?? (store('chinese-checkers:name') || ''));
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
    notice(T.reconnect);
    setTimeout(() => connect(name), Math.min(8000, 800 * 2 ** retry++));
  };
}

function notice(text) {
  $('notice').textContent = text || '';
  if (game) {
    noticeText = text || '';
    renderSlip();
  }
}
let noticeText = '';

function receive(m) {
  switch (m.t) {
    case 'hello':
      seat = m.seat;
      joined = true;
      if (m.key) store(keyOf(), m.key);
      else store(keyOf(), null);
      notice(seat < 0 && wanted ? T.full : '');
      break;
    case 'bye':
      bye = true;
      store(keyOf(), null);
      leaveGame();
      $('join').hidden = false;
      $('lobby').hidden = true;
      $('lobby-sub').hidden = true;
      notice(m.text);
      break;
    case 'lobby':
      showLobby(m);
      break;
    case 'start':
      start(m);
      break;
    case 'events':
      if (game) queue.push(...m.events);
      break;
    case 'state':
      if (!game) break;
      latest = m;
      fresh = true;
      sent = false;
      if (!shown) shown = m.holes.slice();
      break;
  }
}

// --- the lobby -------------------------------------------------------------

function showLobby(m) {
  T = { ...T, ...m.text };
  words();
  seat = m.you;
  leaveGame();
  $('join').hidden = false;
  $('join-form').hidden = true;
  $('lobby').hidden = false;
  document.querySelector('.join-card').classList.add('wide');
  $('lobby-sub').hidden = !m.lead;
  $('lobby-sub').textContent = m.lead ? T.youLead : '';
  const focus = document.activeElement?.dataset?.focus; // kept across the redraw

  let lead = -1;
  if (m.lead) lead = m.you;
  else if (!m.hostLeads) lead = m.seats.findIndex((s) => s.kind === 'visitor' && s.name === m.leadName);

  const ul = $('seats');
  ul.replaceChildren();
  m.seats.forEach((s, i) => {
    const li = el('li');
    if ((s.kind === 'visitor' || s.kind === 'local') && !s.here) li.classList.add('gone');
    li.append(dot(s.color));
    const editable = m.lead && ['open', 'computer', 'ai'].includes(s.kind);
    if (editable) li.append(kindChoice(m, s, i));
    else li.append(seatWho(m, s, i, lead));
    if (m.lead) li.append(teamChoice(s, i));
    else li.append(el('span', 'team-note', fmt(T.team, teamLetter(s.team))));
    ul.append(li);
  });

  const setup = $('setup');
  setup.replaceChildren();
  if (m.lead) {
    const counts = el('div');
    counts.id = 'counts';
    for (const n of [2, 3, 4, 6]) {
      const b = el('button', 'quiet', fmt(T.playersN, n));
      b.type = 'button';
      b.dataset.focus = `count${n}`;
      b.setAttribute('aria-pressed', String(m.seats.length === n));
      b.addEventListener('click', () => send({ t: 'players', players: n }));
      counts.append(b);
    }
    setup.append(counts);
  } else {
    setup.append(el('span', 'note', fmt(T.playersN, m.seats.length)));
  }
  if (m.lead || m.noParking) {
    const rule = el('label', 'rule');
    const box = el('input');
    box.type = 'checkbox';
    box.checked = m.noParking;
    box.disabled = !m.lead;
    box.dataset.focus = 'rules';
    box.addEventListener('change', () => send({ t: 'rules', noParking: box.checked }));
    rule.append(box, el('span', '', T.noParking), el('small', '', T.noParkingNo));
    setup.append(rule);
  }
  if (m.lead) {
    const b = el('button', 'primary', T.start);
    b.id = 'start';
    b.type = 'button';
    b.dataset.focus = 'start';
    b.disabled = !!m.why;
    b.addEventListener('click', () => send({ t: 'start' }));
    setup.append(b);
  }

  const note = $('lobby-note');
  note.className = 'note';
  if (m.lead) {
    note.textContent = m.why || '';
    if (m.why) note.classList.add('why');
  } else {
    note.textContent = !m.hostLeads && m.leadName ? fmt(T.waitingLead, m.leadName) : T.waitingHost;
  }
  if (focus) document.querySelector(`[data-focus="${focus}"]`)?.focus();
}

// seatWho is who plays a seat, in words.
function seatWho(m, s, i, lead) {
  const who = el('span', 'who');
  let title = s.name, sub = '';
  switch (s.kind) {
    case 'open': title = T.open; break;
    case 'local': sub = T.local; break;
    case 'visitor': sub = T.visitor; break;
    case 'computer':
      title = s.name || m.levels?.[s.level] || T.computer;
      sub = `${T.computer} · ${T[levelKeys[s.level]] ?? ''}`;
      break;
    case 'ai':
      title = s.name || s.profile || T.ai;
      sub = `${T.ai} · ${s.profile || T.aiDefault}`;
      break;
  }
  if (i === m.you) title += ` (${T.you})`;
  if (i === lead) sub += (sub ? ' · ' : '') + T.leads;
  who.append(el('b', '', title));
  if (sub) who.append(el('span', 'note', sub));
  return who;
}

// kindChoice is the lead's choice of who plays a seat no person sits in.
function kindChoice(m, s, i) {
  const sel = el('select', 'kind');
  sel.dataset.focus = `kind${i}`;
  const add = (value, text) => {
    const o = el('option', '', text);
    o.value = value;
    sel.append(o);
  };
  add('open', T.open);
  levelKeys.forEach((k, l) => add(`computer:${l}`, `${T.computer} · ${T[k]}`));
  if (m.ai || s.kind === 'ai') {
    add('ai:', `${T.ai} · ${T.aiDefault}`);
    for (const p of m.profiles || []) add(`ai:${p}`, `${T.ai} · ${p}`);
  }
  sel.value = s.kind === 'computer' ? `computer:${s.level}` : s.kind === 'ai' ? `ai:${s.profile || ''}` : 'open';
  sel.addEventListener('change', () => {
    const [kind, rest = ''] = sel.value.split(/:(.*)/s);
    send({ t: 'seat', index: i, kind, level: kind === 'computer' ? Number(rest) : 1, profile: kind === 'ai' ? rest : '' });
  });
  return sel;
}

function teamChoice(s, i) {
  const sel = el('select', 'team');
  sel.dataset.focus = `team${i}`;
  for (let t = 0; t < 6; t++) {
    const o = el('option', '', fmt(T.team, teamLetter(t)));
    o.value = String(t);
    sel.append(o);
  }
  sel.value = String(s.team);
  sel.addEventListener('change', () => send({ t: 'team', index: i, team: Number(sel.value) }));
  return sel;
}

// --- the game ------------------------------------------------------------

let game = null;      // the start message: the holes, the players
let latest = null;    // the last state
let shown = null;     // whose marble the board shows in each hole
let queue = [];       // events still to show
let cur = null, curT = 0, curLength = 0;
let fresh = false;    // a state not shown yet
let sent = false;     // a move sent, its state not come
let log = [];
let selected = -1, cursor = -1; // the marble picked; where the keys are
let nb = [];          // each hole's neighbours, by way; -1 off the board
let reachCache = new Map();
let board = null;     // the 3D scene

function start(m) {
  T = { ...T, ...m.text };
  words();
  game = m;
  seat = m.you;
  queue = [];
  cur = null;
  shown = null;
  latest = null;
  log = [];
  selected = cursor = -1;
  noticeText = '';
  nb = neighbors(m.holes);
  reachCache.clear();
  $('join').hidden = true;
  $('game').hidden = false;
  $('over').hidden = true;
  $('me').textContent = seat >= 0 ? m.players[seat].name : T.watching;
  board?.dispose();
  board = new Board(m);
  renderLog();
}

// leaveGame takes the board away: back to the lobby, or sent away.
function leaveGame() {
  if (!game) return;
  game = latest = shown = null;
  queue = [];
  cur = null;
  board?.dispose();
  board = null;
  $('game').hidden = true;
}

// neighbors are each hole's six neighbours, from where the holes are
// drawn: a gap of one apart, the ways the same for every hole.
function neighbors(holes) {
  const key = (x, y) => `${Math.round(x * 2)},${Math.round(y / 0.8660254)}`;
  const at = new Map(holes.map(([x, y], i) => [key(x, y), i]));
  const ways = [[1, 0], [0.5, 0.8660254], [-0.5, 0.8660254], [-1, 0], [-0.5, -0.8660254], [0.5, -0.8660254]];
  return holes.map(([x, y]) => ways.map(([dx, dy]) => at.get(key(x + dx, y + dy)) ?? -1));
}
const adjacent = (a, b) => nb[a]?.includes(b);

// reachOf is where the marble in hole from may go, as the host's
// Checkers.Reach has it: steps to an empty neighbour; hops over a marble
// into the empty hole beyond, chained, breadth first, never back to where
// it was; with noParking, stopping only outside others' points.
function reachOf(from) {
  if (reachCache.has(from)) return reachCache.get(from);
  const out = new Map();
  reachCache.set(from, out);
  const holes = shown, owner = holes?.[from] ?? -1;
  if (owner < 0 || !latest?.movers.includes(owner)) return out;
  const p = game.players[owner];
  const mayStop = (h) => {
    const k = game.holes[h][2];
    return !game.noParking || k < 0 || k === p.start || k === p.goal;
  };
  for (const n of nb[from]) {
    if (n >= 0 && holes[n] < 0 && mayStop(n)) out.set(n, [from, n]);
  }
  const paths = new Map([[from, [from]]]);
  const todo = [from];
  while (todo.length) {
    const at = todo.shift();
    nb[at].forEach((over, d) => {
      if (over < 0 || holes[over] < 0) return;
      const land = nb[over][d];
      if (land < 0 || holes[land] >= 0 || land === from || paths.has(land)) return;
      paths.set(land, [...paths.get(at), land]);
      todo.push(land);
    });
  }
  for (const [h, path] of paths) {
    if (h !== from && mayStop(h) && !out.has(h)) out.set(h, path);
  }
  return out;
}

const busy = () => cur !== null || queue.length > 0;
const myTurn = () => latest && seat >= 0 && !latest.over && latest.turn === seat && !latest.players[seat]?.away && !busy() && !sent;
// movable reports whether the marble in hole h is one to pick now.
const movable = (h) => h >= 0 && myTurn() && latest.movers.includes(shown[h]) && reachOf(h).size > 0;
// shownTurn is whose turn the board shows: the state runs ahead of the
// moves still being shown.
const shownTurn = () => (cur?.kind === 'move' ? cur.player : queue.find((e) => e.kind === 'move')?.player) ?? latest?.turn ?? 0;

// pick does what a tap or Enter on hole h means: pick a marble, move the
// one picked there, or let go.
function pick(h) {
  if (!myTurn()) return;
  if (movable(h)) {
    selected = h === selected ? -1 : h;
  } else if (selected >= 0 && reachOf(selected).has(h)) {
    send({ t: 'move', from: selected, to: h });
    sent = true;
    selected = -1;
  } else {
    selected = -1;
  }
  cursor = selected >= 0 ? -1 : cursor;
  showPick();
}

function showPick() {
  if (!board) return;
  const mine = myTurn();
  if (!mine) selected = cursor = -1;
  board.select(selected, selected >= 0 ? [...reachOf(selected).keys()] : []);
  board.cursor = cursor;
  board.host.classList.toggle('can-pick', !!mine);
  renderSlip();
}

// candidates are what the arrow keys go through: the marbles to pick, or
// where the one picked may go; across the screen, left to right.
function candidates() {
  let list = selected >= 0 ? [...reachOf(selected).keys()] : shown.map((_, h) => h).filter(movable);
  if (selected >= 0) list = [selected, ...list];
  return board.screenOrder(list);
}

function onKey(e) {
  if (!game || !shown) return;
  if (e.key === 'Escape') {
    selected = cursor = -1;
    showPick();
    return;
  }
  if (!myTurn()) return;
  const list = candidates();
  if (!list.length) return;
  const i = list.indexOf(cursor);
  if (['ArrowRight', 'ArrowDown', 'ArrowLeft', 'ArrowUp'].includes(e.key)) {
    e.preventDefault();
    const fwd = e.key === 'ArrowRight' || e.key === 'ArrowDown';
    cursor = list[i < 0 ? (fwd ? 0 : list.length - 1) : (i + (fwd ? 1 : list.length - 1)) % list.length];
    board.cursor = cursor;
  } else if ((e.key === 'Enter' || e.key === ' ') && cursor >= 0) {
    e.preventDefault();
    const was = cursor;
    pick(cursor);
    if (selected === was) { // picked: on to where it may go
      const next = candidates().filter((h) => h !== selected);
      cursor = next[0] ?? -1;
      board.cursor = cursor;
    }
  }
}

// lengthOf is how long an event shows, in seconds: a hop a little less
// than the host waits for it.
function lengthOf(e) {
  switch (e.kind) {
    case 'move': {
      const n = e.path.length - 1;
      return n === 1 && adjacent(e.path[0], e.path[1]) ? 0.26 : 0.18 * n + 0.06;
    }
    case 'home':
    case 'win': return 0.6;
    default: return 0.3;
  }
}

// step plays the events out, each for its length, then the board as it
// stands; with many waiting (back after a while), quickly.
function step(dt) {
  if (!cur && queue.length) {
    cur = queue.shift();
    curT = 0;
    curLength = queue.length > 12 ? 0.02 : lengthOf(cur);
    begin(cur);
  }
  if (cur) {
    curT += dt;
    if (cur.kind === 'move' && board.moving) board.animate(cur, Math.min(1, curT / curLength));
    if (curT >= curLength) {
      end(cur);
      cur = null;
      if (!queue.length) settle();
    }
  } else if (fresh && latest) {
    settle();
  }
}

function begin(e) {
  const name = (i) => game.players[i]?.name ?? '';
  let text = '', good = false;
  switch (e.kind) {
    case 'move': {
      const hops = e.path.length > 2 || !adjacent(e.path[0], e.path[1]) ? e.path.length - 1 : 0;
      if (e.owner !== e.player) text = fmt(T.movedFor, name(e.player), name(e.owner));
      else if (hops > 1) text = fmt(T.movedHops, name(e.player), hops);
      else text = fmt(T.moved, name(e.player));
      selected = cursor = -1;
      if (shown) board.start(e.path);
      break;
    }
    case 'pass': text = fmt(T.passed, name(e.player)); break;
    case 'home': text = fmt(T.cameHome, name(e.player)); good = true; break;
    case 'win': {
      const list = (e.players || []).map(name);
      text = list.length > 1 ? fmt(T.teamWins, names(list)) : fmt(T.wins, list[0] ?? '');
      good = true;
      break;
    }
    case 'draw': text = T.draw; break;
  }
  if (text) {
    log.unshift({ text, good });
    log.length = Math.min(log.length, 8);
    renderLog();
  }
  renderPanel();
}

function end(e) {
  if (e.kind === 'move' && shown) {
    const from = e.path[0], to = e.path.at(-1);
    shown[to] = shown[from];
    shown[from] = -1;
    board.stop();
    board.update(shown);
  }
}

// settle shows the game as it stands, once everything has been shown.
function settle() {
  if (!latest) return;
  shown = latest.holes.slice();
  fresh = false;
  reachCache.clear();
  board.update(shown);
  board.showLast(latest.last || []);
  if (selected >= 0 && !movable(selected)) selected = -1;
  if (cursor >= 0 && !candidates().includes(cursor)) cursor = -1;
  renderPanel();
  showPick();
}

// --- the panel -------------------------------------------------------------

function renderPanel() {
  if (!game || !latest) return;
  const s = latest;
  $('round').textContent = String(s.round);
  const teams = new Set(game.players.map((p) => p.team));
  const shared = teams.size < game.players.length;
  const ul = $('players');
  ul.replaceChildren();
  game.players.forEach((p, i) => {
    const sp = s.players[i] || {};
    const li = el('li');
    if (i === shownTurn() && !s.over) li.classList.add('turn');
    if (sp.home) li.classList.add('home');
    const left = sp.home ? fmt(T.place, sp.place) : fmt(T.left, Math.max(0, sp.left - 20));
    const status = [];
    if (shared) status.push(fmt(T.team, teamLetter(p.team)));
    if (sp.away) status.push(T.away);
    li.append(dot(p.color), el('span', 'name', p.name + (i === seat ? ` (${T.you})` : '')), el('span', 'left', left),
      el('span', 'status', status.join(' · ')));
    ul.append(li);
  });
  $('lead-acts').hidden = !s.lead;
  renderSlip();
  renderOver();
}

function renderSlip() {
  if (!game || !latest) return;
  const line = $('slip-line');
  line.textContent = noticeText || log[0]?.text || '';
  line.className = !noticeText && log[0]?.good ? 'good' : '';
  const pi = shownTurn(), slip = $('slip');
  slip.style.setProperty('--dot', game.players[pi]?.color);
  let turn = fmt(T.turn, game.players[pi]?.name ?? '');
  const mine = myTurn();
  if (mine) {
    const others = latest.movers.filter((m) => m !== seat);
    if (selected >= 0) turn = T.pickHole;
    else if (others.length && !latest.movers.includes(seat)) turn = fmt(T.forPartner, names(others.map((m) => game.players[m].name)));
    else turn = T.yourTurn;
  }
  if (latest.over && !busy()) turn = T.over;
  slip.classList.toggle('mine', !!mine);
  $('slip-turn').textContent = turn;
  $('slip-round').textContent = `${T.round} ${latest.round}`;
}

function renderLog() {
  $('log').replaceChildren(...log.map((l) => el('li', l.good ? 'good' : '', l.text)));
}

function renderOver() {
  const s = latest, over = s.over && !busy();
  $('over').hidden = !over;
  if (!over) return;
  const name = (i) => game.players[i]?.name ?? '';
  const winners = s.winTeam >= 0 ? game.players.map((p, i) => i).filter((i) => game.players[i].team === s.winTeam) : [];
  $('over-title').textContent = !winners.length ? T.draw
    : winners.length > 1 ? fmt(T.teamWins, names(winners.map(name))) : fmt(T.wins, name(winners[0]));
  const ol = $('standings');
  ol.replaceChildren();
  for (const i of s.standings || []) {
    const sp = s.players[i] || {};
    const li = el('li');
    li.append(dot(game.players[i].color), el('span', 'who', name(i) + (i === seat ? ` (${T.you})` : '')),
      el('b', '', sp.home ? fmt(T.place, sp.place) : fmt(T.left, Math.max(0, sp.left - 20))));
    ol.append(li);
  }
  $('over-acts').hidden = !s.lead;
}

for (const b of document.querySelectorAll('[data-act]')) {
  b.addEventListener('click', () => send({ t: b.dataset.act }));
}

// --- the board in 3D ---------------------------------------------------------

// The board: the star printed on washi paper laid in a round board of
// wood; a hole's x and y (gaps between holes, y down) are the scene's x
// and z, from its middle.
const R = 0.36;          // a marble's radius
const SINK = 0.07;       // how far a marble sits down in its hole
const PAPER = 9.0;       // the paper's radius
const EDGE = 0.62;       // the margin of the star's print around its holes

// blobTexture is a soft ink blot, for the marbles' shadows.
let blobTex = null;
function blobTexture() {
  if (blobTex) return blobTex;
  const cv = document.createElement('canvas');
  cv.width = cv.height = 64;
  const g = cv.getContext('2d');
  const r = g.createRadialGradient(32, 32, 0, 32, 32, 32);
  r.addColorStop(0, 'rgba(31,27,23,0.5)');
  r.addColorStop(0.5, 'rgba(31,27,23,0.24)');
  r.addColorStop(1, 'rgba(31,27,23,0)');
  g.fillStyle = r;
  g.fillRect(0, 0, 64, 64);
  blobTex = new THREE.CanvasTexture(cv);
  return blobTex;
}

// environment is what the glass reflects: a warm room, a window up and to
// the left, as the sun comes from.
function environment(renderer) {
  const env = new THREE.Scene();
  const geo = new THREE.SphereGeometry(50, 32, 16);
  const pos = geo.attributes.position, colors = [];
  const top = new THREE.Color('#fffaf0'), mid = new THREE.Color('#e9dcc2'), low = new THREE.Color('#5a4632'), c = new THREE.Color();
  for (let i = 0; i < pos.count; i++) {
    const y = pos.getY(i) / 50;
    if (y > 0) c.lerpColors(mid, top, y);
    else c.lerpColors(mid, low, Math.min(1, -y * 1.6));
    colors.push(c.r, c.g, c.b);
  }
  geo.setAttribute('color', new THREE.Float32BufferAttribute(colors, 3));
  env.add(new THREE.Mesh(geo, new THREE.MeshBasicMaterial({ vertexColors: true, side: THREE.BackSide })));
  for (const [x, y, z, w, h, k] of [[-18, 38, 14, 26, 16, 5], [24, 26, -20, 10, 8, 2]]) {
    const lamp = new THREE.Mesh(new THREE.PlaneGeometry(w, h), new THREE.MeshBasicMaterial({ color: new THREE.Color(k, k * 0.95, k * 0.86), side: THREE.DoubleSide }));
    lamp.position.set(x, y, z);
    lamp.lookAt(0, 0, 0);
    env.add(lamp);
  }
  const pm = new THREE.PMREMGenerator(renderer);
  const tex = pm.fromScene(env, 0.02).texture;
  pm.dispose();
  return tex;
}

class Board {
  constructor(m) {
    this.m = m;
    this.pos = m.holes.map(([x, y]) => [x, y]);
    this.host = $('board');
    this.renderer = new THREE.WebGLRenderer({ antialias: true });
    this.renderer.setPixelRatio(Math.min(2, devicePixelRatio));
    this.host.append(this.renderer.domElement);

    this.scene = new THREE.Scene();
    this.scene.background = new THREE.Color('#e4dac4');
    // Only the glass reflects the room; the paper and the wood take the
    // lights alone.
    this.env = environment(this.renderer);
    this.camera = new THREE.PerspectiveCamera(36, 1, 0.5, 300);
    this.controls = new OrbitControls(this.camera, this.renderer.domElement);
    this.controls.enableDamping = true;
    this.controls.screenSpacePanning = false;
    this.controls.minDistance = 7;
    this.controls.maxDistance = 46;
    this.controls.minPolarAngle = 0.05;
    this.controls.maxPolarAngle = 1.22;
    this.controls.mouseButtons = { LEFT: THREE.MOUSE.ROTATE, MIDDLE: THREE.MOUSE.DOLLY, RIGHT: THREE.MOUSE.PAN };
    this.controls.touches = { ONE: THREE.TOUCH.ROTATE, TWO: THREE.TOUCH.DOLLY_PAN };
    // Turned or brought closer by hand, the view stays as it is left.
    this.touched = false;
    this.fitting = false;
    this.controls.addEventListener('change', () => (this.touched ||= !this.fitting));
    this.controls.addEventListener('start', () => $('rotate-hint').classList.add('gone'));
    $('rotate-hint').classList.remove('gone');
    this.hintTimer = setTimeout(() => $('rotate-hint').classList.add('gone'), 9000);

    this.scene.add(new THREE.HemisphereLight('#fffaf0', '#8a7a62', 1.6));
    const sun = new THREE.DirectionalLight('#fff4e0', 1.7);
    sun.position.set(-6, 14, 8);
    this.scene.add(sun);

    // The view: from beyond this browser's own point, the first player's
    // when watching, so it lies nearest.
    const own = m.players[m.you >= 0 ? m.you : 0]?.start ?? 3;
    let tip = [0, 1], far = 0;
    m.holes.forEach(([x, y, k]) => {
      if (k === own && Math.hypot(x, y) > far) [tip, far] = [[x, y], Math.hypot(x, y)];
    });
    this.toward = new THREE.Vector2(tip[0], tip[1]).normalize();

    this.addBoard();
    this.marks = new THREE.Group(); // the last move's way
    this.scene.add(this.marks);
    this.reachMarks = new THREE.Group();
    this.scene.add(this.reachMarks);
    const ring = (r0, r1, color, opacity) => {
      const mesh = new THREE.Mesh(new THREE.RingGeometry(r0, r1, 48),
        new THREE.MeshBasicMaterial({ color, transparent: true, opacity, depthWrite: false }));
      mesh.rotation.x = -Math.PI / 2;
      mesh.visible = false;
      this.scene.add(mesh);
      return mesh;
    };
    this.selRing = ring(0.4, 0.48, '#2b4c7e', 0.95);
    this.hoverRing = ring(0.41, 0.46, '#1f1b17', 0.5);
    this.mats = m.players.map((p) => new THREE.MeshPhysicalMaterial({
      color: p.color, roughness: 0.16, metalness: 0, clearcoat: 1, clearcoatRoughness: 0.05, envMap: this.env, envMapIntensity: 0.7,
    }));
    this.sphereGeo = new THREE.SphereGeometry(R, 40, 24);
    this.pools = m.players.map(() => []);
    this.at = new Array(m.holes.length).fill(null);
    this.spheres = [];
    this.selected = -1;
    this.cursor = -1;
    this.hover = -1;
    this.moving = null;
    this.t = 0;

    this.pointer();
    this.watch = new ResizeObserver(() => this.resize());
    this.watch.observe(this.host);
    this.onKey = (e) => onKey(e);
    this.host.addEventListener('keydown', this.onKey);
    this.resize();
  }

  world(h, y = 0) {
    const [x, z] = this.pos[h];
    return new THREE.Vector3(x, y, z);
  }

  addBoard() {
    const wood = new THREE.Mesh(new THREE.CylinderGeometry(PAPER + 0.42, PAPER + 0.62, 0.7, 128),
      new THREE.MeshStandardMaterial({ color: '#7a5a3e', roughness: 0.72 }));
    wood.position.y = -0.37;
    this.scene.add(wood);
    const tex = new THREE.CanvasTexture(this.drawPaper());
    tex.colorSpace = THREE.SRGBColorSpace;
    tex.anisotropy = this.renderer.capabilities.getMaxAnisotropy();
    this.paper = new THREE.Mesh(new THREE.CircleGeometry(PAPER, 128),
      new THREE.MeshStandardMaterial({ map: tex, roughness: 1 }));
    this.paper.rotation.x = -Math.PI / 2;
    this.scene.add(this.paper);
  }

  // drawPaper prints the board: washi with its fibres, the star with the
  // lines between its holes, each point tinted for who starts in it, the
  // holes as little cups.
  drawPaper() {
    const n = 2048, k = n / (2 * PAPER);
    const cv = document.createElement('canvas');
    cv.width = cv.height = n;
    const g = cv.getContext('2d');
    const X = (x) => n / 2 + x * k, Y = (y) => n / 2 + y * k;
    g.fillStyle = '#f1e9d6';
    g.fillRect(0, 0, n, n);
    // Fibres: short faint strokes, the same each time.
    let seed = 7;
    const rnd = () => ((seed = (seed * 16807) % 2147483647) / 2147483647);
    g.lineCap = 'round';
    for (let i = 0; i < 2600; i++) {
      const x = rnd() * n, y = rnd() * n, a = rnd() * Math.PI, l = 6 + rnd() * 26;
      g.strokeStyle = rnd() < 0.5 ? `rgba(120,96,60,${0.04 + rnd() * 0.05})` : `rgba(255,252,240,${0.18 + rnd() * 0.2})`;
      g.lineWidth = 0.8 + rnd() * 1.4;
      g.beginPath();
      g.moveTo(x, y);
      g.lineTo(x + Math.cos(a) * l, y + Math.sin(a) * l);
      g.stroke();
    }
    // The paper's edge: two rules of ink round it.
    g.strokeStyle = 'rgba(31,27,23,0.55)';
    g.lineWidth = 0.045 * k;
    g.beginPath();
    g.arc(n / 2, n / 2, (PAPER - 0.32) * k, 0, Math.PI * 2);
    g.stroke();
    g.lineWidth = 0.02 * k;
    g.beginPath();
    g.arc(n / 2, n / 2, (PAPER - 0.44) * k, 0, Math.PI * 2);
    g.stroke();

    // The star: two triangles, each through three points' tips, a margin
    // out from the holes.
    const holes = this.m.holes;
    const tips = [];
    for (let c = 0; c < 6; c++) {
      let best = null, far = -1;
      holes.forEach(([x, y, kk]) => {
        if (kk === c && Math.hypot(x, y) > far) [best, far] = [[x, y], Math.hypot(x, y)];
      });
      tips.push(best);
    }
    const out = (p, m) => { const d = Math.hypot(p[0], p[1]); return [p[0] * (d + m) / d, p[1] * (d + m) / d]; };
    const tri = (pts) => {
      g.beginPath();
      pts.forEach(([x, y], i) => (i ? g.lineTo(X(x), Y(y)) : g.moveTo(X(x), Y(y))));
      g.closePath();
    };
    const triangles = [[0, 2, 4], [1, 3, 5]].map((t) => t.map((c) => out(tips[c], 2 * EDGE)));
    g.strokeStyle = 'rgba(31,27,23,0.8)';
    g.lineWidth = 0.07 * k;
    g.lineJoin = 'round';
    for (const t of triangles) { tri(t); g.stroke(); }
    g.fillStyle = '#f6f0e0';
    for (const t of triangles) { tri(t); g.fill(); }

    // The points: each tinted in the colour of who starts in it.
    const colorAt = {};
    for (const p of this.m.players) colorAt[p.start] = p.color;
    for (let c = 0; c < 6; c++) {
      if (!colorAt[c]) continue;
      const own = holes.filter((h) => h[2] === c);
      const cx = own.reduce((s, h) => s + h[0], 0) / own.length, cy = own.reduce((s, h) => s + h[1], 0) / own.length;
      const corners = own.map((h) => [h, Math.hypot(h[0] - cx, h[1] - cy)]).sort((a, b) => b[1] - a[1]).slice(0, 3)
        .map(([h, d]) => [h[0] + (h[0] - cx) / d * 2 * (EDGE - 0.12), h[1] + (h[1] - cy) / d * 2 * (EDGE - 0.12)]);
      tri(corners);
      g.globalAlpha = 0.15;
      g.fillStyle = colorAt[c];
      g.fill();
      g.globalAlpha = 0.5;
      g.strokeStyle = colorAt[c];
      g.lineWidth = 0.035 * k;
      g.stroke();
      g.globalAlpha = 1;
    }

    // The lines between neighbours, faint.
    g.strokeStyle = 'rgba(31,27,23,0.16)';
    g.lineWidth = 0.035 * k;
    g.beginPath();
    nb.forEach((ways, i) => {
      for (const j of ways.slice(0, 3)) {
        if (j < 0) continue;
        g.moveTo(X(holes[i][0]), Y(holes[i][1]));
        g.lineTo(X(holes[j][0]), Y(holes[j][1]));
      }
    });
    g.stroke();

    // The holes: a cup each, its shade at the top, its lit lip below.
    for (const [x, y] of holes) {
      const px = X(x), py = Y(y), r = 0.28 * k;
      g.fillStyle = '#f6f0e0';
      g.beginPath();
      g.arc(px, py, r * 1.12, 0, Math.PI * 2);
      g.fill();
      const cup = g.createRadialGradient(px + r * 0.15, py + r * 0.25, r * 0.1, px, py, r);
      cup.addColorStop(0, 'rgba(150,124,88,0.55)');
      cup.addColorStop(0.7, 'rgba(96,74,50,0.62)');
      cup.addColorStop(1, 'rgba(58,44,30,0.8)');
      g.fillStyle = cup;
      g.beginPath();
      g.arc(px, py, r, 0, Math.PI * 2);
      g.fill();
      g.strokeStyle = 'rgba(255,253,245,0.75)';
      g.lineWidth = 0.035 * k;
      g.beginPath();
      g.arc(px, py, r * 1.04, 0.15 * Math.PI, 0.85 * Math.PI);
      g.stroke();
    }
    return cv;
  }

  // marble makes a marble of player p: the glass, and its shadow on the
  // paper, which stays down when the glass hops.
  marble(p) {
    const group = new THREE.Group();
    const glass = new THREE.Mesh(this.sphereGeo, this.mats[p]);
    glass.position.y = R - SINK;
    const shadow = new THREE.Mesh(new THREE.PlaneGeometry(1.05, 1.05),
      new THREE.MeshBasicMaterial({ map: blobTexture(), transparent: true, depthWrite: false }));
    shadow.rotation.x = -Math.PI / 2;
    shadow.position.set(0.09, 0.004, -0.07);
    group.add(shadow, glass);
    group.userData = { glass, shadow, lift: 0, hole: -1 };
    glass.userData.group = group;
    this.spheres.push(glass);
    this.scene.add(group);
    return group;
  }

  // update puts the marbles where holes says.
  update(holes) {
    const used = this.pools.map(() => 0);
    this.at.fill(null);
    holes.forEach((p, h) => {
      if (p < 0 || !this.pools[p]) return;
      const pool = this.pools[p];
      if (used[p] >= pool.length) pool.push(this.marble(p));
      const g = pool[used[p]++];
      g.visible = true;
      g.position.copy(this.world(h));
      g.userData.hole = h;
      g.userData.glass.position.y = R - SINK + g.userData.lift;
      this.at[h] = g;
    });
    this.pools.forEach((pool, p) => pool.slice(used[p]).forEach((g) => { g.visible = false; g.userData.hole = -1; }));
  }

  // start and animate move the marble in path's first hole along it: a
  // step slides, a hop arcs over the marble it hops.
  start(path) {
    this.marks.clear();
    this.moving = this.at[path[0]];
    if (this.moving) this.moving.userData.lift = 0;
  }

  animate(e, f) {
    const g = this.moving, path = e.path;
    if (!g) return;
    const n = path.length - 1, s = Math.min(n - 1, Math.floor(f * n)), u = f * n - s;
    const a = this.world(path[s]), b = this.world(path[s + 1]);
    const ease = u * u * (3 - 2 * u);
    g.position.lerpVectors(a, b, ease);
    const hop = !adjacent(path[s], path[s + 1]);
    const lift = hop ? 0.78 * Math.sin(Math.PI * u) : 0;
    g.userData.glass.position.y = R - SINK + lift;
    const k = 1 - lift * 0.35;
    g.userData.shadow.scale.setScalar(k);
    g.userData.shadow.material.opacity = k;
  }

  stop() {
    const g = this.moving;
    if (g) {
      g.userData.shadow.scale.setScalar(1);
      g.userData.shadow.material.opacity = 1;
    }
    this.moving = null;
  }

  // showLast marks the last move's way, faintly: where it left, and the
  // lines it went along.
  showLast(path) {
    this.marks.clear();
    if (path.length < 2) return;
    const mat = new THREE.MeshBasicMaterial({ color: '#1f1b17', transparent: true, opacity: 0.22, depthWrite: false });
    for (let i = 0; i + 1 < path.length; i++) {
      const a = this.world(path[i], 0.006), b = this.world(path[i + 1], 0.006);
      const len = a.distanceTo(b);
      const bar = new THREE.Mesh(new THREE.PlaneGeometry(len - 0.5, 0.07), mat);
      bar.rotation.x = -Math.PI / 2;
      bar.rotation.z = -Math.atan2(b.z - a.z, b.x - a.x);
      bar.position.lerpVectors(a, b, 0.5);
      this.marks.add(bar);
    }
    const from = new THREE.Mesh(new THREE.RingGeometry(0.3, 0.36, 40),
      new THREE.MeshBasicMaterial({ color: '#1f1b17', transparent: true, opacity: 0.4, depthWrite: false }));
    from.rotation.x = -Math.PI / 2;
    from.position.copy(this.world(path[0], 0.008));
    this.marks.add(from);
  }

  // select lifts the marble picked and marks where it may go.
  select(h, holes) {
    this.selected = h;
    this.selRing.visible = h >= 0;
    if (h >= 0) this.selRing.position.copy(this.world(h, 0.012));
    this.reachMarks.clear();
    if (!holes.length) return;
    const disc = new THREE.CircleGeometry(0.3, 32), ring = new THREE.RingGeometry(0.33, 0.41, 40);
    this.reachDisc = new THREE.MeshBasicMaterial({ color: '#3f7a3a', transparent: true, opacity: 0.3, depthWrite: false });
    this.reachRing = new THREE.MeshBasicMaterial({ color: '#3f7a3a', transparent: true, opacity: 0.9, depthWrite: false });
    for (const r of holes) {
      for (const [geo, mat] of [[disc, this.reachDisc], [ring, this.reachRing]]) {
        const mesh = new THREE.Mesh(geo, mat);
        mesh.rotation.x = -Math.PI / 2;
        mesh.position.copy(this.world(r, 0.01));
        mesh.userData.hole = r;
        this.reachMarks.add(mesh);
      }
    }
  }

  // screenOrder sorts holes as they lie across the screen.
  screenOrder(holes) {
    const v = new THREE.Vector3();
    const at = new Map(holes.map((h) => {
      v.copy(this.world(h)).project(this.camera);
      return [h, [v.x, -v.y]];
    }));
    return holes.slice().sort((a, b) => {
      const [ax, ay] = at.get(a), [bx, by] = at.get(b);
      return Math.abs(ax - bx) > 0.02 ? ax - bx : ay - by;
    });
  }

  // pointer picks the hole tapped: a marble, or where it goes.
  pointer() {
    const ray = new THREE.Raycaster(), ndc = new THREE.Vector2(), plane = new THREE.Plane(new THREE.Vector3(0, 1, 0), -(R - SINK));
    const hit = new THREE.Vector3();
    let down = null;
    const canvas = this.renderer.domElement;
    const holeAt = (e) => {
      const r = canvas.getBoundingClientRect();
      ndc.set(((e.clientX - r.left) / r.width) * 2 - 1, -((e.clientY - r.top) / r.height) * 2 + 1);
      ray.setFromCamera(ndc, this.camera);
      const glass = ray.intersectObjects(this.spheres.filter((s) => s.parent.visible), false)[0];
      if (glass) return glass.object.userData.group.userData.hole;
      if (!ray.ray.intersectPlane(plane, hit)) return -1;
      let best = -1, near = 0.55;
      this.pos.forEach(([x, z], h) => {
        const d = Math.hypot(x - hit.x, z - hit.z);
        if (d < near) [best, near] = [h, d];
      });
      return best;
    };
    canvas.addEventListener('pointermove', (e) => {
      if (e.pointerType === 'mouse' && !e.buttons) this.hover = holeAt(e);
    });
    canvas.addEventListener('pointerleave', () => (this.hover = -1));
    canvas.addEventListener('pointerdown', (e) => (down = [e.clientX, e.clientY]));
    canvas.addEventListener('pointerup', (e) => {
      if (!down || Math.hypot(e.clientX - down[0], e.clientY - down[1]) > 6) return;
      down = null;
      cursor = -1;
      this.cursor = -1;
      pick(holeAt(e));
    });
  }

  // fit sets the view back: from beyond the own point, at a height, the
  // whole board in sight.
  fit() {
    const tan = Math.tan((this.camera.fov / 2) * Math.PI / 180), r = PAPER + 0.7;
    const d = Math.max(r * 1.02 / tan, r * 1.06 / (tan * this.camera.aspect));
    const elev = (this.camera.aspect < 1 ? 68 : 58) * Math.PI / 180; // steeper on a phone, the board taller
    this.camera.position.set(this.toward.x * d * Math.cos(elev), d * Math.sin(elev), this.toward.y * d * Math.cos(elev));
    this.controls.target.set(0, 0, 0);
    this.fitting = true;
    this.controls.update();
    this.fitting = false;
  }

  resize() {
    const w = this.host.clientWidth, h = this.host.clientHeight;
    if (!w || !h) return;
    this.renderer.setSize(w, h);
    this.camera.aspect = w / h;
    // The view's middle a little down, clear of the slip at its head.
    this.camera.setViewOffset(w, h, 0, -Math.min(36, h * 0.05), w, h);
    this.camera.updateProjectionMatrix();
    if (!this.touched) this.fit();
  }

  render(dt) {
    this.t += dt;
    this.controls.update();
    // Keep the view over the board.
    const t = this.controls.target, lim = 6;
    const d = Math.hypot(t.x, t.z);
    if (d > lim) {
      const cx = t.x * (lim / d - 1), cz = t.z * (lim / d - 1);
      t.x += cx; t.z += cz;
      this.camera.position.x += cx; this.camera.position.z += cz;
    }
    // The marble picked rises a little; the holes it may go to breathe.
    for (const pool of this.pools) {
      for (const g of pool) {
        if (!g.visible || g === this.moving) continue;
        const want = g.userData.hole === this.selected ? 0.24 : 0;
        const lift = g.userData.lift + (want - g.userData.lift) * Math.min(1, dt * 14);
        if (Math.abs(lift - g.userData.lift) > 1e-4) {
          g.userData.lift = lift;
          g.userData.glass.position.y = R - SINK + lift;
        }
      }
    }
    if (this.reachDisc) {
      const p = 0.5 + 0.5 * Math.sin(this.t * 4);
      this.reachDisc.opacity = 0.18 + 0.2 * p;
      this.reachRing.opacity = 0.65 + 0.3 * p;
    }
    const at = this.cursor >= 0 ? this.cursor : this.hover;
    const show = at >= 0 && at !== this.selected && (movable(at) || (this.selected >= 0 && reachOf(this.selected).has(at)));
    this.hoverRing.visible = show;
    if (show) this.hoverRing.position.copy(this.world(at, 0.014));
    this.renderer.render(this.scene, this.camera);
  }

  dispose() {
    clearTimeout(this.hintTimer);
    this.watch.disconnect();
    this.host.removeEventListener('keydown', this.onKey);
    this.controls.dispose();
    this.env.dispose();
    this.renderer.dispose();
    this.renderer.domElement.remove();
  }
}

// --- the loop ---------------------------------------------------------------

let last = performance.now();
function frame(now) {
  const dt = Math.min(0.1, (now - last) / 1000);
  last = now;
  if (game && board) {
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
    connect(store('chinese-checkers:name') || '');
  }
});

init();
requestAnimationFrame(frame);
