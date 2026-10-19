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

#define INUA_VT_ABI 13

typedef struct inua_vt inua_vt;

int inua_vt_abi(void);

/* A terminal of cols x rows; NULL when it cannot be made. */
inua_vt *inua_vt_new(uint16_t cols, uint16_t rows);
void inua_vt_free(inua_vt *t);

/* Bytes from the program, as they come. */
void inua_vt_write(inua_vt *t, const uint8_t *data, size_t len);

/* Makes the terminal answer the program's queries (device attributes,
 * cursor position, modes, colours, sizes, XTVERSION): the answers gather
 * as the program's bytes are written and are taken with
 * inua_vt_answers. Only one engine per program should answer: the one
 * that sees its output first. */
void inua_vt_answer(inua_vt *t);

/* The answers gathered since the last call; borrowed until the next
 * inua_vt_write. */
void inua_vt_answers(inua_vt *t, const uint8_t **ptr, size_t *len);

/* The shell's prompt marks (OSC 133) since the last call, for a terminal
 * that answers, borrowed until the next inua_vt_write, as records:
 *   u8 kind (1 prompt start, 2 input start, 3 command start, 4 command
 *   end), u8 has_exit, i32 exit, u16 len, len bytes: the command line,
 *   for a command start: the one the shell sent, else what was typed. */
void inua_vt_prompts(inua_vt *t, const uint8_t **ptr, size_t *len);

/* What is typed at the shell's prompt and not yet run: the cells marked
 * input after the last prompt, wrapped lines joined, a line each for a
 * command over several. *len 0 when the cursor is not at a prompt; *out
 * freed with inua_vt_free_bytes. */
int inua_vt_input(inua_vt *t, uint8_t **out, size_t *len);

/* The last n rows of the active screen as text, a line each, trailing
 * blanks trimmed: what a program shows at its bottom. *out freed with
 * inua_vt_free_bytes. */
int inua_vt_tail(inua_vt *t, uint16_t n, uint8_t **out, size_t *len);

/* The text the program last put on a clipboard (OSC 52, OSC 1337, OSC
 * 5522) since the last call, for a terminal that answers: returns 0 for
 * none, 1 for the clipboard, 2 for the primary selection, with the text
 * borrowed until the next inua_vt_write. Reading the clipboard is not
 * offered to programs. */
int inua_vt_clipboard(inua_vt *t, const uint8_t **ptr, size_t *len);

/* The default colours, 0x00RRGGBB, reported to programs that ask (OSC
 * 10, 11, 12; the colour scheme from the background). */
void inua_vt_colors(inua_vt *t, uint32_t fg, uint32_t bg, uint32_t cursor);

/* A cell's size in pixels, for size reports; 0 when not known. */
void inua_vt_cell_size(inua_vt *t, uint32_t width, uint32_t height);
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
  uint32_t mouse;          /* the program tracks the mouse */
  uint32_t alt;            /* the alternate screen is shown (vim, less) */
  /* The scrollable area in rows: all of it, where the viewport starts,
   * and the viewport's length. */
  uint32_t sb_total, sb_offset, sb_len;
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
#define INUA_VT_MATCH 512u          /* a search match */
#define INUA_VT_MATCH_CURRENT 1024u /* the search match selected */
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

/* Moves the viewport through the scrollback: kind 0 to the top, 1 to the
 * bottom (where the program writes), 2 by n rows (up is negative), 3 by n
 * prompts (the shell's OSC 133 marks). Frames then show the rows there;
 * new output keeps the viewport where it is unless it is at the bottom. */
void inua_vt_scroll(inua_vt *t, uint32_t kind, int32_t n);

/* Searching the screen and the scrollback for needle (ASCII letters in
 * any case); len 0 ends the search. Frames then mark the matches in view.
 * Returns 0, <0 on error. */
int inua_vt_search(inua_vt *t, const uint8_t *needle, size_t len);

/* Selects the next match (dir 1, toward older lines) or the previous
 * (-1), scrolling to it. Returns 1 when one is selected, 0 for none. */
int inua_vt_search_move(inua_vt *t, int32_t dir);

/* How many matches the search has, and which is selected, from 1 (0 for
 * none); as of the last frame or move. */
void inua_vt_search_count(inua_vt *t, uint32_t *total, uint32_t *current);

/* Modifier bits for inua_vt_key, as libghostty's. */
#define INUA_VT_MOD_SHIFT 1u
#define INUA_VT_MOD_CTRL 2u
#define INUA_VT_MOD_ALT 4u
#define INUA_VT_MOD_SUPER 8u
#define INUA_VT_MOD_CAPS 16u
#define INUA_VT_MOD_NUM 32u

/* Encodes a key as the program wants it, in the modes it set (cursor
 * keys, the kitty keyboard protocol…): keysym is the key GDK reports
 * after the layout and its options (caps:escape makes Caps Lock Escape),
 * naming Enter, Escape, the arrows, the function and keypad keys; xkb is
 * the keycode, for the other keys;
 * action 0 release, 1 press, 2 repeat; utf8 the text the key makes;
 * unshifted the key's character without modifiers (0 for none); consumed
 * the modifiers the layout used up making the text. *len 0: nothing to
 * send; a release is sent only to a program that asked for releases
 * (the kitty protocol's report-events flag). */
int inua_vt_key(inua_vt *t, uint32_t keysym, uint32_t xkb, uint32_t mods, uint32_t action,
                const char *utf8, size_t utf8_len, uint32_t unshifted,
                uint32_t consumed, uint8_t *out, size_t cap, size_t *len);

/* Encodes a mouse event for a program that tracks the mouse, in the mode
 * and format it asked for: action 0 press, 1 release, 2 motion; button 0
 * none, 1 left, 2 right, 3 middle, 4 and 5 the wheel up and down; mods as
 * for keys; x, y the pointer in pixels from the widget's corner, which is
 * width x height with pad pixels around the cells of cell_w x cell_h;
 * pressed whether a button is held. *len 0: nothing to send (the mode
 * does not report it, or the pointer stayed in its cell). */
int inua_vt_mouse(inua_vt *t, uint32_t action, uint32_t button, uint32_t mods,
                  double x, double y, uint32_t width, uint32_t height,
                  uint32_t cell_w, uint32_t cell_h, uint32_t pad, uint32_t pressed,
                  uint8_t *out, size_t cap, size_t *len);

/* Selecting with the pointer, through Ghostty's gesture: kind 0 press, 1
 * release, 2 drag; x, y the cell in the viewport; px, py the pointer in
 * pixels from the cells' corner; time_ns for telling double and triple
 * clicks (repeat_ns and repeat_px apart at most); cell_w and height the
 * cells' width and the viewport's height in pixels. A selection made
 * becomes the terminal's, drawn in frames; a plain press clears it.
 * Returns 1 when there is a selection, 0 when not, <0 on error. */
int inua_vt_select(inua_vt *t, uint32_t kind, uint16_t x, uint16_t y,
                   double px, double py, uint64_t time_ns, uint64_t repeat_ns,
                   double repeat_px, uint32_t rectangle, uint32_t cell_w,
                   uint32_t height);

/* The selection's text: wrapped lines joined, trailing blanks trimmed.
 * *len 0 without a selection; *out freed with inua_vt_free_bytes. */
int inua_vt_selection_text(inua_vt *t, uint8_t **out, size_t *len);

void inua_vt_select_clear(inua_vt *t);

/* Pasting text the user chose (a keybind, a middle click): what to send
 * the program, in the modes it set: wrapped in bracketed paste (mode
 * 2004) when it asked, control bytes made spaces. Returns 0 with *out
 * (freed with inua_vt_free_bytes; *len 0 for nothing to paste), or -7
 * (rejected) when the text could run commands, that is a line break
 * outside a bracketed paste or the bracket's end inside one, and
 * allow_unsafe is 0: nothing to send until the user confirms. primary
 * says it came from the primary selection. */
int inua_vt_paste(inua_vt *t, const uint8_t *data, size_t len, uint32_t primary,
                  uint32_t allow_unsafe, uint8_t **out, size_t *out_len);

#endif
