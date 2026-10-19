/* inua's boundary with libghostty-vt: the few calls inua-term makes,
 * with pointer and integer arguments only, so Go reaches them through
 * purego (which passes no structs by value on Linux). Compiled with the
 * pinned libghostty-vt's own headers, so a change upstream fails here, at
 * build time, and not in a call from Go. INUA_VT_ABI changes whenever a
 * signature here does; internal/term/vt checks it when it loads. */
#ifndef INUA_VT_H
#define INUA_VT_H

#include <stddef.h>
#include <stdint.h>

#define INUA_VT_ABI 2

typedef struct inua_vt inua_vt;

int inua_vt_abi(void);

/* A terminal of cols x rows; NULL when it cannot be made. */
inua_vt *inua_vt_new(uint16_t cols, uint16_t rows);
void inua_vt_free(inua_vt *t);

/* Bytes from the program, as they come. */
void inua_vt_write(inua_vt *t, const uint8_t *data, size_t len);
int inua_vt_resize(inua_vt *t, uint16_t cols, uint16_t rows);

/* The screen as plain text, trailing blanks trimmed, soft-wrapped lines
 * joined when unwrap is set. *out is freed with inua_vt_free_bytes. */
int inua_vt_text(inua_vt *t, int unwrap, uint8_t **out, size_t *len);
void inua_vt_free_bytes(uint8_t *p, size_t len);

/* Borrowed until the next call that changes the terminal; len 0 for none. */
int inua_vt_title(inua_vt *t, const uint8_t **ptr, size_t *len);
int inua_vt_pwd(inua_vt *t, const uint8_t **ptr, size_t *len);

/* The cursor's column and row, 0-based. */
int inua_vt_cursor(inua_vt *t, uint16_t *x, uint16_t *y);

/* A frame to draw. All fields are 32-bit so Go mirrors the layout
 * without padding. Colours are 0x00RRGGBB. */
typedef struct {
  uint32_t cols, rows;
  uint32_t dirty;          /* 0 clean, 1 some rows, 2 everything */
  uint32_t cursor_visible; /* in the viewport and shown */
  uint32_t cursor_x, cursor_y;
  uint32_t cursor_style;   /* 0 bar, 1 block, 2 underline, 3 hollow block */
  uint32_t cursor_wide;    /* on the tail of a wide character */
  uint32_t password;       /* the program reads a password */
  uint32_t fg, bg;         /* the default colours */
  uint32_t nrows;          /* row records in the buffer */
} inua_vt_frame_info;

/* Run attribute bits. */
#define INUA_VT_BOLD 1u
#define INUA_VT_ITALIC 2u
#define INUA_VT_FAINT 4u
#define INUA_VT_INVERSE 8u
#define INUA_VT_STRIKE 16u
#define INUA_VT_UNDERLINE 32u
#define INUA_VT_INVISIBLE 64u
#define INUA_VT_OVERLINE 128u
#define INUA_VT_SELECTED 256u
/* fg or bg is the default colour, not the value given. */
#define INUA_VT_DEFAULT_COLOR 0x01000000u

/* Updates the frame from the terminal and writes the rows to draw (every
 * row when all is set or the whole screen changed, else the changed ones)
 * into buf, little-endian:
 *   row: u16 y, u16 runs
 *   run: u16 x, u16 cells, u32 fg, u32 bg, u16 attrs, u16 len, len bytes UTF-8
 * Empty cells are spaces; a wide character's run counts both its cells.
 * Trailing blank default cells are left out. Then the rows are marked
 * clean. Returns 0, or -3 (out of space) with *len a size to retry with,
 * nothing marked clean. */
int inua_vt_frame(inua_vt *t, int all, uint8_t *buf, size_t cap, size_t *len,
                  inua_vt_frame_info *info);

#endif
