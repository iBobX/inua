#include "inua_vt.h"

#include <ghostty/vt.h>

#include <stdbool.h>
#include <string.h>

struct inua_vt {
  GhosttyTerminal term;
  GhosttyRenderState render;
  GhosttyRenderStateRowIterator rows;
  GhosttyRenderStateRowCells cells;
  GhosttyKeyEncoder keys;
  GhosttyKeyEvent key;
};

int inua_vt_abi(void) { return INUA_VT_ABI; }

inua_vt *inua_vt_new(uint16_t cols, uint16_t rows) {
  uint8_t *p = ghostty_alloc(NULL, sizeof(inua_vt));
  if (p == NULL) return NULL;
  inua_vt *t = (inua_vt *)p;
  memset(t, 0, sizeof(*t));
  if (ghostty_terminal_new(NULL, &t->term, cols, rows) != GHOSTTY_SUCCESS ||
      ghostty_render_state_new(NULL, &t->render) != GHOSTTY_SUCCESS ||
      ghostty_render_state_row_iterator_new(NULL, &t->rows) != GHOSTTY_SUCCESS ||
      ghostty_render_state_row_cells_new(NULL, &t->cells) != GHOSTTY_SUCCESS ||
      ghostty_key_encoder_new(NULL, &t->keys) != GHOSTTY_SUCCESS ||
      ghostty_key_event_new(NULL, &t->key) != GHOSTTY_SUCCESS) {
    inua_vt_free(t);
    return NULL;
  }
  return t;
}

void inua_vt_free(inua_vt *t) {
  if (t == NULL) return;
  ghostty_key_event_free(t->key);
  ghostty_key_encoder_free(t->keys);
  ghostty_render_state_row_cells_free(t->cells);
  ghostty_render_state_row_iterator_free(t->rows);
  ghostty_render_state_free(t->render);
  ghostty_terminal_free(t->term);
  ghostty_free(NULL, (uint8_t *)t, sizeof(inua_vt));
}

void inua_vt_write(inua_vt *t, const uint8_t *data, size_t len) {
  ghostty_terminal_vt_write(t->term, data, len);
}

int inua_vt_resize(inua_vt *t, uint16_t cols, uint16_t rows) {
  return ghostty_terminal_resize(t->term, cols, rows, 0, 0);
}

int inua_vt_text(inua_vt *t, int unwrap, uint8_t **out, size_t *len) {
  GhosttyFormatterTerminalOptions opts = GHOSTTY_INIT_SIZED(GhosttyFormatterTerminalOptions);
  opts.emit = GHOSTTY_FORMATTER_FORMAT_PLAIN;
  opts.trim = true;
  opts.unwrap = unwrap != 0;
  GhosttyFormatter f;
  GhosttyResult r = ghostty_formatter_terminal_new(NULL, &f, t->term, opts);
  if (r != GHOSTTY_SUCCESS) return r;
  r = ghostty_formatter_format_alloc(f, NULL, out, len);
  ghostty_formatter_free(f);
  return r;
}

void inua_vt_free_bytes(uint8_t *p, size_t len) { ghostty_free(NULL, p, len); }

static int get_string(inua_vt *t, GhosttyTerminalData what, const uint8_t **ptr, size_t *len) {
  GhosttyString s = {0};
  GhosttyResult r = ghostty_terminal_get(t->term, what, &s);
  *ptr = s.ptr;
  *len = s.len;
  return r;
}

int inua_vt_title(inua_vt *t, const uint8_t **ptr, size_t *len) {
  return get_string(t, GHOSTTY_TERMINAL_DATA_TITLE, ptr, len);
}

int inua_vt_pwd(inua_vt *t, const uint8_t **ptr, size_t *len) {
  return get_string(t, GHOSTTY_TERMINAL_DATA_PWD, ptr, len);
}

int inua_vt_cursor(inua_vt *t, uint16_t *x, uint16_t *y) {
  GhosttyResult r = ghostty_terminal_get(t->term, GHOSTTY_TERMINAL_DATA_CURSOR_X, x);
  if (r != GHOSTTY_SUCCESS) return r;
  return ghostty_terminal_get(t->term, GHOSTTY_TERMINAL_DATA_CURSOR_Y, y);
}

/* --- frames --- */

static uint32_t rgb(GhosttyColorRgb c) { return (uint32_t)c.r << 16 | (uint32_t)c.g << 8 | c.b; }

typedef struct {
  uint8_t *buf;
  size_t cap, len;
  bool full;
} out_buf;

static void put(out_buf *o, const void *p, size_t n) {
  if (o->len + n > o->cap) {
    o->full = true;
    return;
  }
  memcpy(o->buf + o->len, p, n);
  o->len += n;
}

static void put16(out_buf *o, uint16_t v) { put(o, &v, 2); }
static void put32(out_buf *o, uint32_t v) { put(o, &v, 4); }

/* A run being gathered for one row. */
typedef struct {
  uint16_t x, cells;
  uint32_t fg, bg;
  uint16_t attrs;
  uint8_t text[4096];
  size_t len;
} run;

static void put_run(out_buf *o, const run *r) {
  put16(o, r->x);
  put16(o, r->cells);
  put32(o, r->fg);
  put32(o, r->bg);
  put16(o, r->attrs);
  put16(o, (uint16_t)r->len);
  put(o, r->text, r->len);
}

/* One row's runs; returns how many were written. The row's last run
 * loses its trailing spaces when they draw nothing (default colours, no
 * attributes), and is left out when nothing else is in it. */
static uint16_t put_row(inua_vt *t, out_buf *o) {
  run cur = {0};
  bool open = false;
  uint16_t nruns = 0, x = 0;
  if (ghostty_render_state_row_get(t->rows, GHOSTTY_RENDER_STATE_ROW_DATA_CELLS, &t->cells) != GHOSTTY_SUCCESS)
    return 0;
  GhosttyRenderStateRowSelection sel = GHOSTTY_INIT_SIZED(GhosttyRenderStateRowSelection);
  bool has_sel = ghostty_render_state_row_get(t->rows, GHOSTTY_RENDER_STATE_ROW_DATA_SELECTION, &sel) == GHOSTTY_SUCCESS;
  while (ghostty_render_state_row_cells_next(t->cells)) {
    GhosttyCell raw = 0;
    ghostty_render_state_row_cells_get(t->cells, GHOSTTY_RENDER_STATE_ROW_CELLS_DATA_RAW, &raw);
    GhosttyCellWide wide = GHOSTTY_CELL_WIDE_NARROW;
    ghostty_cell_get(raw, GHOSTTY_CELL_DATA_WIDE, &wide);
    if (wide == GHOSTTY_CELL_WIDE_SPACER_TAIL) { /* the wide character's run counts it */
      if (open) cur.cells++;
      x++;
      continue;
    }
    uint32_t fg = INUA_VT_DEFAULT_COLOR, bg = INUA_VT_DEFAULT_COLOR;
    GhosttyColorRgb c;
    if (ghostty_render_state_row_cells_get(t->cells, GHOSTTY_RENDER_STATE_ROW_CELLS_DATA_FG_COLOR, &c) == GHOSTTY_SUCCESS) fg = rgb(c);
    if (ghostty_render_state_row_cells_get(t->cells, GHOSTTY_RENDER_STATE_ROW_CELLS_DATA_BG_COLOR, &c) == GHOSTTY_SUCCESS) bg = rgb(c);
    uint16_t attrs = 0;
    bool styled = false;
    ghostty_render_state_row_cells_get(t->cells, GHOSTTY_RENDER_STATE_ROW_CELLS_DATA_HAS_STYLING, &styled);
    if (styled) {
      GhosttyStyle st = GHOSTTY_INIT_SIZED(GhosttyStyle);
      ghostty_render_state_row_cells_get(t->cells, GHOSTTY_RENDER_STATE_ROW_CELLS_DATA_STYLE, &st);
      attrs |= st.bold ? INUA_VT_BOLD : 0;
      attrs |= st.italic ? INUA_VT_ITALIC : 0;
      attrs |= st.faint ? INUA_VT_FAINT : 0;
      attrs |= st.inverse ? INUA_VT_INVERSE : 0;
      attrs |= st.strikethrough ? INUA_VT_STRIKE : 0;
      attrs |= st.underline != 0 ? INUA_VT_UNDERLINE : 0;
      attrs |= st.invisible ? INUA_VT_INVISIBLE : 0;
      attrs |= st.overline ? INUA_VT_OVERLINE : 0;
    }
    if (has_sel && x >= sel.start_x && x <= sel.end_x) attrs |= INUA_VT_SELECTED;
    uint8_t text[64];
    GhosttyBuffer gb = {.ptr = text, .cap = sizeof(text), .len = 0};
    if (ghostty_render_state_row_cells_get(t->cells, GHOSTTY_RENDER_STATE_ROW_CELLS_DATA_GRAPHEMES_UTF8, &gb) != GHOSTTY_SUCCESS || gb.len == 0) {
      text[0] = ' ';
      gb.len = 1;
    }
    if (!open || cur.fg != fg || cur.bg != bg || cur.attrs != attrs || cur.len + gb.len > sizeof(cur.text)) {
      if (open) {
        put_run(o, &cur);
        nruns++;
      }
      memset(&cur, 0, sizeof(cur));
      cur.x = x;
      cur.fg = fg;
      cur.bg = bg;
      cur.attrs = attrs;
      open = true;
    }
    memcpy(cur.text + cur.len, text, gb.len);
    cur.len += gb.len;
    cur.cells++;
    x++;
  }
  if (open && cur.fg == INUA_VT_DEFAULT_COLOR && cur.bg == INUA_VT_DEFAULT_COLOR && cur.attrs == 0) {
    while (cur.len > 0 && cur.text[cur.len - 1] == ' ') {
      cur.len--;
      cur.cells--;
    }
  }
  if (open && cur.len > 0) {
    put_run(o, &cur);
    nruns++;
  }
  return nruns;
}

int inua_vt_frame(inua_vt *t, int all, uint8_t *buf, size_t cap, size_t *len, inua_vt_frame_info *info) {
  GhosttyResult r = ghostty_render_state_update(t->render, t->term);
  if (r != GHOSTTY_SUCCESS) return r;
  memset(info, 0, sizeof(*info));
  uint16_t cols = 0, rows = 0;
  ghostty_render_state_get(t->render, GHOSTTY_RENDER_STATE_DATA_COLS, &cols);
  ghostty_render_state_get(t->render, GHOSTTY_RENDER_STATE_DATA_ROWS, &rows);
  info->cols = cols;
  info->rows = rows;
  GhosttyRenderStateDirty dirty = GHOSTTY_RENDER_STATE_DIRTY_FALSE;
  ghostty_render_state_get(t->render, GHOSTTY_RENDER_STATE_DATA_DIRTY, &dirty);
  info->dirty = (uint32_t)dirty;
  GhosttyRenderStateCursor cur = GHOSTTY_INIT_SIZED(GhosttyRenderStateCursor);
  if (ghostty_render_state_get(t->render, GHOSTTY_RENDER_STATE_DATA_CURSOR, &cur) == GHOSTTY_SUCCESS) {
    info->cursor_visible = cur.visible && cur.viewport_has_value;
    info->cursor_x = cur.viewport_x;
    info->cursor_y = cur.viewport_y;
    info->cursor_style = (uint32_t)cur.visual_style;
    info->cursor_wide = cur.wide_tail;
    info->password = cur.password_input;
  }
  GhosttyRenderStateColors colors = GHOSTTY_INIT_SIZED(GhosttyRenderStateColors);
  if (ghostty_render_state_get(t->render, GHOSTTY_RENDER_STATE_DATA_COLORS, &colors) == GHOSTTY_SUCCESS) {
    info->fg = rgb(colors.foreground);
    info->bg = rgb(colors.background);
  }
  bool every = all || dirty == GHOSTTY_RENDER_STATE_DIRTY_FULL;
  out_buf o = {.buf = buf, .cap = cap};
  if (ghostty_render_state_get(t->render, GHOSTTY_RENDER_STATE_DATA_ROW_ITERATOR, &t->rows) != GHOSTTY_SUCCESS)
    return GHOSTTY_INVALID_VALUE;
  uint16_t y = 0;
  for (;;) {
    if (every) {
      if (!ghostty_render_state_row_iterator_next(t->rows)) break;
    } else if (!ghostty_render_state_row_iterator_next_dirty(t->rows, &y)) {
      break;
    }
    if (every) {
      uint16_t vy = 0;
      ghostty_render_state_row_get(t->rows, GHOSTTY_RENDER_STATE_ROW_DATA_VIEWPORT_Y, &vy);
      y = vy;
    }
    size_t head = o.len;
    put16(&o, y);
    put16(&o, 0);
    uint16_t n = put_row(t, &o);
    if (!o.full) memcpy(o.buf + head + 2, &n, 2);
    info->nrows++;
    if (o.full) break;
  }
  if (o.full) {
    *len = cap * 2 + 65536;
    return GHOSTTY_OUT_OF_SPACE;
  }
  *len = o.len;
  /* Drawn: everything is clean until the terminal changes again. */
  if (ghostty_render_state_get(t->render, GHOSTTY_RENDER_STATE_DATA_ROW_ITERATOR, &t->rows) == GHOSTTY_SUCCESS) {
    bool clean = false;
    while (ghostty_render_state_row_iterator_next(t->rows))
      ghostty_render_state_row_set(t->rows, GHOSTTY_RENDER_STATE_ROW_OPTION_DIRTY, &clean);
  }
  GhosttyRenderStateDirty none = GHOSTTY_RENDER_STATE_DIRTY_FALSE;
  ghostty_render_state_set(t->render, GHOSTTY_RENDER_STATE_OPTION_DIRTY, &none);
  return GHOSTTY_SUCCESS;
}

/* --- keys --- */

static const struct {
  uint32_t xkb;
  GhosttyKey key;
} keymap[] = {
#include "inua_keymap.inc"
};

static GhosttyKey key_for_xkb(uint32_t xkb) {
  for (size_t i = 0; i < sizeof(keymap) / sizeof(keymap[0]); i++)
    if (keymap[i].xkb == xkb) return keymap[i].key;
  return GHOSTTY_KEY_UNIDENTIFIED;
}

int inua_vt_key(inua_vt *t, uint32_t xkb, uint32_t mods, uint32_t action,
                const char *utf8, size_t utf8_len, uint32_t unshifted,
                uint32_t consumed, uint8_t *out, size_t cap, size_t *len) {
  *len = 0;
  /* A release means something only to a program that asked for them. */
  if (action == GHOSTTY_KEY_ACTION_RELEASE) {
    uint8_t flags = 0;
    ghostty_terminal_get(t->term, GHOSTTY_TERMINAL_DATA_KITTY_KEYBOARD_FLAGS, &flags);
    if ((flags & GHOSTTY_KITTY_KEY_REPORT_EVENTS) == 0) return GHOSTTY_SUCCESS;
  }
  ghostty_key_encoder_setopt_from_terminal(t->keys, t->term);
  ghostty_key_event_set_action(t->key, (GhosttyKeyAction)action);
  ghostty_key_event_set_key(t->key, key_for_xkb(xkb));
  ghostty_key_event_set_mods(t->key, (GhosttyMods)mods);
  ghostty_key_event_set_consumed_mods(t->key, (GhosttyMods)consumed);
  ghostty_key_event_set_utf8(t->key, utf8_len ? utf8 : NULL, utf8_len);
  ghostty_key_event_set_unshifted_codepoint(t->key, unshifted);
  ghostty_key_event_set_composing(t->key, false);
  *len = 0;
  return ghostty_key_encoder_encode(t->keys, t->key, (char *)out, cap, len);
}
