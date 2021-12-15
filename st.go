package main

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"log"
	"math"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"unicode"
	"unicode/utf8"

	"github.com/qeedquan/go-media/posix"
	"golang.org/x/sys/unix"
)

const (
	ATTR_NULL       = 0
	ATTR_BOLD       = 1 << 0
	ATTR_FAINT      = 1 << 1
	ATTR_ITALIC     = 1 << 2
	ATTR_UNDERLINE  = 1 << 3
	ATTR_BLINK      = 1 << 4
	ATTR_REVERSE    = 1 << 5
	ATTR_INVISIBLE  = 1 << 6
	ATTR_STRUCK     = 1 << 7
	ATTR_WRAP       = 1 << 8
	ATTR_WIDE       = 1 << 9
	ATTR_WDUMMY     = 1 << 10
	ATTR_BOLD_FAINT = ATTR_BOLD | ATTR_FAINT
)

const (
	SEL_IDLE  = 0
	SEL_EMPTY = 1
	SEL_READY = 2
)

const (
	SEL_REGULAR     = 1
	SEL_RECTANGULAR = 2
)

const (
	SNAP_WORD = 1
	SNAP_LINE = 2
)

const (
	MODE_WRAP      = 1 << 0
	MODE_INSERT    = 1 << 1
	MODE_ALTSCREEN = 1 << 2
	MODE_CRLF      = 1 << 3
	MODE_ECHO      = 1 << 4
	MODE_PRINT     = 1 << 5
	MODE_UTF8      = 1 << 6
	MODE_SIXEL     = 1 << 7
)

const (
	CURSOR_SAVE = iota
	CURSOR_LOAD
)

const (
	CURSOR_DEFAULT  = 0
	CURSOR_WRAPNEXT = 1
	CURSOR_ORIGIN   = 2
)

const (
	CS_GRAPHIC0 = iota
	CS_GRAPHIC1
	CS_UK
	CS_USA
	CS_MULTI
	CS_GER
	CS_FIN
)

const (
	ESC_START      = 1
	ESC_CSI        = 2
	ESC_STR        = 4 // OSC, PM, APC
	ESC_ALTCHARSET = 8
	ESC_STR_END    = 16 // a final string was encountered
	ESC_TEST       = 32 // Enter in test mode
	ESC_UTF8       = 64
	ESC_DCS        = 128
)

const (
	ESC_BUF_SIZ = 128 * utf8.UTFMax
	ESC_ARG_SIZ = 16
	STR_BUF_SIZ = ESC_BUF_SIZ
	STR_ARG_SIZ = ESC_ARG_SIZ
)

type Glyph struct {
	u    rune
	mode uint
	fg   uint32
	bg   uint32
}

type Line []Glyph

type TCursor struct {
	attr  Glyph // current char attributes
	x, y  int
	state int
}

type Selection struct {
	mode int
	typ  int
	snap int

	// Selection variables:
	// nb – normalized coordinates of the beginning of the selection
	// ne – normalized coordinates of the end of the selection
	// ob – original coordinates of the beginning of the selection
	// oe – original coordinates of the end of the selection
	nb, ne, ob, oe struct{ x, y int }

	alt bool
}

// Internal representation of the screen
type Term struct {
	row      int     // nb row
	col      int     // nb col
	line     []Line  // screen
	alt      []Line  // alternate screen
	dirty    []bool  // dirtyness of lines
	c        TCursor // cursor
	ocx      int     // old cursor col
	ocy      int     // old cursor row
	top      int     // top scroll limit
	bot      int     // bottom scroll limit
	mode     int     // terminal mode flags
	esc      int     // escape state flags
	trantbl  [4]byte // charset table translation
	charset  int     // current charset
	icharset int     // selected charset for sequence
	tabs     []bool
	tc       [2]TCursor
	buf      [32768]byte
	buflen   int
	rdy      chan struct{}
}

// CSI Escape sequence structs
// ESC '[' [[ [<priv>] <arg> [;]] <mode> [<mode>]]
type CSIEscape struct {
	buf  [ESC_BUF_SIZ]byte // raw string
	len  int               // raw string length
	priv bool
	arg  [ESC_ARG_SIZ]int // arguments
	narg int              // nb of args
	mode [2]int
}

// STR Escape sequence structs
// ESC type [[ [<priv>] <arg> [;]] <mode>] ESC '\'
type STREscape struct {
	typ  int                 // ESC type
	buf  [STR_BUF_SIZ]byte   // raw string
	len  int                 // raw string length
	args [STR_ARG_SIZ][]byte // arguments
	narg int
}

var (
	term      Term
	sel       Selection
	csiescseq CSIEscape
	strescseq STREscape
	cmdfile   *os.File
	iofile    *os.File
	pid       int
)

func truecolor(r, g, b int) int32 {
	return (1<<24 | int32(r)<<16 | int32(g)<<8 | int32(b))
}

func iscontrolc0(c rune) bool {
	return (0 <= c && c <= 0x1f) || c == '\177'
}

func iscontrolc1(c rune) bool {
	return 0x80 <= c && c <= 0x9f
}

func iscontrol(c rune) bool {
	return iscontrolc0(c) || iscontrolc1(c)
}

func wcschr(w []rune, u rune) int {
	for i := range w {
		if u == w[i] {
			return i
		}
	}
	return -1
}

func isdelim(u rune) bool {
	return u != 0 && wcschr(worddelimiters, u) >= 0
}

func sigchld(exe *exec.Cmd) {
	err := exe.Wait()
	if err != nil {
		log.Fatalf("child died: %v", err)
	}
	os.Exit(0)
}

func selinit() {
	sel.mode = SEL_IDLE
	sel.snap = 0
	sel.ob.x = -1
}

func tlinelen(y int) int {
	i := term.col

	if term.line[y][i-1].mode&ATTR_WRAP != 0 {
		return i
	}

	for i > 0 && term.line[y][i-1].u == ' ' {
		i--
	}

	return i
}

func selstart(col, row, snap int) {
	selclear()
	sel.mode = SEL_EMPTY
	sel.typ = SEL_REGULAR
	sel.alt = term.mode&MODE_ALTSCREEN != 0
	sel.snap = snap
	sel.oe.x, sel.ob.x = col, col
	sel.oe.y, sel.ob.y = row, row
	selnormalize()

	if sel.snap != 0 {
		sel.mode = SEL_READY
	}
	tsetdirt(sel.nb.y, sel.ne.y)
}

func selextend(col, row, typ int, done bool) {
	if sel.mode == SEL_IDLE {
		return
	}
	if done && sel.mode == SEL_EMPTY {
		selclear()
		return
	}
	oldey := sel.oe.y
	oldex := sel.oe.x
	oldsby := sel.nb.y
	oldsey := sel.ne.y
	oldtype := sel.typ

	sel.oe.x = col
	sel.oe.y = row
	selnormalize()
	sel.typ = typ

	if oldey != sel.oe.y || oldex != sel.oe.x || oldtype != sel.typ || sel.mode == SEL_EMPTY {
		tsetdirt(min(sel.nb.y, oldsby), max(sel.ne.y, oldsey))
	}

	sel.mode = SEL_READY
	if done {
		sel.mode = SEL_IDLE
	}
}

func selnormalize() {
	if sel.typ == SEL_REGULAR && sel.ob.y != sel.oe.y {
		if sel.ob.y < sel.oe.y {
			sel.nb.x = sel.ob.x
			sel.ne.x = sel.oe.x
		} else {
			sel.nb.x = sel.oe.x
			sel.ne.x = sel.ob.x
		}
	} else {
		sel.nb.x = min(sel.ob.x, sel.oe.x)
		sel.ne.x = max(sel.ob.x, sel.oe.x)
	}
	sel.nb.y = min(sel.ob.y, sel.oe.y)
	sel.ne.y = max(sel.ob.y, sel.oe.y)

	selsnap(&sel.nb.x, &sel.nb.y, -1)
	selsnap(&sel.ne.x, &sel.ne.y, +1)

	/* expand selection over line breaks */
	if sel.typ == SEL_RECTANGULAR {
		return
	}
	i := tlinelen(sel.nb.y)
	if i < sel.nb.x {
		sel.nb.x = i
	}
	if tlinelen(sel.ne.y) <= sel.ne.x {
		sel.ne.x = term.col - 1
	}
}

func ttyresize(tw, th int) {
	w := unix.Winsize{
		Row:    uint16(term.row),
		Col:    uint16(term.col),
		Xpixel: uint16(tw),
		Ypixel: uint16(th),
	}
	cmdfd := int(cmdfile.Fd())
	err := unix.IoctlSetWinsize(cmdfd, unix.TIOCSWINSZ, &w)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Couldn't set window size: %v", err)
	}
}

func ttyhangup() {
	// Send SIGHUP to shell
	proc, err := os.FindProcess(pid)
	if err != nil {
		return
	}
	proc.Signal(syscall.SIGHUP)
}

func tattrset(attr uint) bool {
	for i := 0; i < term.row-1; i++ {
		for j := 0; j < term.col-1; j++ {
			if term.line[i][j].mode&attr != 0 {
				return true
			}
		}
	}
	return false
}

func tsetdirt(top, bot int) {
	top = clamp(top, 0, term.row-1)
	bot = clamp(bot, 0, term.row-1)

	for i := top; i <= bot; i++ {
		term.dirty[i] = true
	}
}

func tsetdirtattr(attr uint) {
	for i := 0; i < term.row-1; i++ {
		for j := 0; j < term.col-1; j++ {
			if term.line[i][j].mode&attr != 0 {
				tsetdirt(i, i)
				break
			}
		}
	}
}

func tfulldirt() {
	tsetdirt(0, term.row-1)
}

func tcursor(mode int) {
	alt := 0
	if term.mode&MODE_ALTSCREEN != 0 {
		alt = 1
	}

	if mode == CURSOR_SAVE {
		term.tc[alt] = term.c
	} else {
		term.c = term.tc[alt]
		tmoveto(term.tc[alt].x, term.tc[alt].y)
	}
}

func treset() {
	term.c = TCursor{
		attr: Glyph{
			mode: ATTR_NULL,
			fg:   defaultfg,
			bg:   defaultbg,
		},
		state: CURSOR_DEFAULT,
	}

	for i := 0; i < term.col; i++ {
		term.tabs[i] = false
	}
	for i := tabspaces; i < term.col; i += tabspaces {
		term.tabs[i] = true
	}
	term.top = 0
	term.bot = term.row - 1
	term.mode = MODE_WRAP | MODE_UTF8
	for i := range term.trantbl {
		term.trantbl[i] = CS_USA
	}

	for i := 0; i < 2; i++ {
		tmoveto(0, 0)
		tcursor(CURSOR_SAVE)
		tclearregion(0, 0, term.col-1, term.row-1)
		tswapscreen()
	}
}

func tnew(col, row int) {
	term = Term{
		c: TCursor{
			attr: Glyph{
				fg: defaultfg,
				bg: defaultbg,
			},
		},
		rdy: make(chan struct{}),
	}
	tresize(col, row)
	treset()
}

func ttywrite(s []byte, may_echo bool) {
	if may_echo && term.mode&MODE_ECHO != 0 {
		twrite(s, true)
	}

	if term.mode&MODE_CRLF == 0 {
		ttywriteraw(s)
		return
	}

	// This is similar to how the kernel handles ONLCR for ttys
	next := 0
	n := len(s)
	for i := 0; i < n; {
		if s[i] == '\r' {
			next = i + 1
			ttywriteraw([]byte("\r\n"))
		} else {
			next = bytes.IndexByte(s[i:n], '\r')
			if next == 0 {
				next = n
			}
			ttywriteraw(s[i : n-i])
		}
		n -= next - i
		i = next
	}
}

func ttywriteraw(s []byte) {
	// Remember that we are using a pty, which might be a modem line.
	// Writing too much will clog the line. That's why we are doing this
	// dance.
	// FIXME: Migrate the world to Plan 9.
	lim := 256
	for len(s) > 0 {
		n := min(len(s), lim)
		r, err := cmdfile.Write(s[:n])
		if err != nil {
			log.Fatalf("write error on tty: %v", err)
		}
		if r < n {
			term.rdy <- struct{}{}
			<-term.rdy
			ttyread()
		}
		s = s[r:]
	}
}

func tswapscreen() {
	term.line, term.alt = term.alt, term.line
	term.mode ^= MODE_ALTSCREEN
	tfulldirt()
}

func tscrolldown(orig, n int) {
	n = clamp(n, 0, term.bot-orig+1)

	tsetdirt(orig, term.bot-n)
	tclearregion(0, term.bot-n+1, term.col-1, term.bot)

	for i := term.bot; i >= orig+n; i-- {
		term.line[i], term.line[i-n] = term.line[i-n], term.line[i]
	}

	selscroll(orig, n)
}

func tscrollup(orig, n int) {
	n = clamp(n, 0, term.bot-orig+1)

	tclearregion(0, orig, term.col-1, orig+n-1)
	tsetdirt(orig+n, term.bot)

	for i := orig; i <= term.bot-n; i++ {
		term.line[i], term.line[i+n] = term.line[i+n], term.line[i]
	}
	selscroll(orig, -n)
}

func selscroll(orig, n int) {
	if sel.ob.x == -1 {
		return
	}

	if (sel.ob.y <= orig && orig <= term.bot) || (sel.oe.y <= orig && orig <= term.bot) {
		sel.ob.y += n
		sel.oe.y += n
		if sel.ob.y > term.bot || sel.oe.y < term.top {
			selclear()
			return
		}
		if sel.typ == SEL_RECTANGULAR {
			if sel.ob.y < term.top {
				sel.ob.y = term.top
			}
			if sel.oe.y > term.bot {
				sel.oe.y = term.bot
			}
		} else {
			if sel.ob.y < term.top {
				sel.ob.y = term.top
				sel.ob.x = 0
			}
			if sel.oe.y > term.bot {
				sel.oe.y = term.bot
				sel.oe.x = term.col
			}
		}
		selnormalize()
	}
}

func tnewline(first_col bool) {
	y := term.c.y
	if y == term.bot {
		tscrollup(term.top, 1)
	} else {
		y++
	}

	x := 0
	if !first_col {
		x = term.c.x
	}
	tmoveto(x, y)
}

func csiparse() {
	csiescseq.narg = 0
	p := csiescseq.buf[:csiescseq.len]
	if len(p) > 0 && p[0] == '?' {
		csiescseq.priv = true
		p = p[1:]
	}

	for len(p) > 0 {
		v, np, _ := posix.Strtol(string(p), 10)
		if np == 0 {
			v = 0
		}
		if v == math.MaxInt64 || v == math.MinInt64 {
			v = -1
		}

		csiescseq.arg[csiescseq.narg] = int(v)
		csiescseq.narg++

		p = p[np:]
		if len(p) > 0 {
			if p[0] != ';' || csiescseq.narg == ESC_ARG_SIZ {
				break
			}
			p = p[1:]
		}
	}

	for i := 0; i < 2; i++ {
		csiescseq.mode[i] = 0
		if i < len(p) {
			csiescseq.mode[i] = int(p[i])
			p = p[1:]
		}
	}
}

// for absolute user moves, when decom is set
func tmoveato(x, y int) {
	if term.c.state&CURSOR_ORIGIN != 0 {
		y += term.top
	}
	tmoveto(x, y)
}

func tmoveto(x, y int) {
	var miny, maxy int
	if term.c.state&CURSOR_ORIGIN != 0 {
		miny = term.top
		maxy = term.bot
	} else {
		miny = 0
		maxy = term.row - 1
	}
	term.c.state &^= CURSOR_WRAPNEXT
	term.c.x = clamp(x, 0, term.col-1)
	term.c.y = clamp(y, miny, maxy)
}

func tsetchar(u rune, attr *Glyph, x, y int) {
	var vt100_0 = []string{ // 0x41 - 0x7e
		"↑", "↓", "→", "←", "█", "▚", "☃", // A - G
		"", "", "", "", "", "", "", "", // H - O
		"", "", "", "", "", "", "", "", // P - W
		"", "", "", "", "", "", "", " ", // X - _
		"◆", "▒", "␉", "␌", "␍", "␊", "°", "±", // ` - g
		"␤", "␋", "┘", "┐", "┌", "└", "┼", "⎺", // h - o
		"⎻", "─", "⎼", "⎽", "├", "┤", "┴", "┬", // p - w
		"│", "≤", "≥", "π", "≠", "£", "·", // x - ~
	}

	// The table is proudly stolen from rxvt.
	if term.trantbl[term.charset] == CS_GRAPHIC0 &&
		(0x41 <= u && u <= 0x7e) && vt100_0[u-0x41] != "" {
		u, _ = utf8.DecodeRuneInString(vt100_0[u-0x41])
	}

	if term.line[y][x].mode&ATTR_WIDE != 0 {
		if x+1 < term.col {
			term.line[y][x+1].u = ' '
			term.line[y][x+1].mode &^= ATTR_WDUMMY

		}
	} else if term.line[y][x].mode&ATTR_WDUMMY != 0 {
		term.line[y][x-1].u = ' '
		term.line[y][x-1].mode &^= ATTR_WIDE
	}

	term.dirty[y] = true
	term.line[y][x] = *attr
	term.line[y][x].u = u
}

func tclearregion(x1, y1, x2, y2 int) {
	if x1 > x2 {
		x1, x2 = x2, x1
	}
	if y1 > y2 {
		y1, y2 = y2, y1
	}

	x1 = clamp(x1, 0, term.col-1)
	x2 = clamp(x2, 0, term.col-1)
	y1 = clamp(y1, 0, term.row-1)
	y2 = clamp(y2, 0, term.row-1)

	for y := y1; y <= y2; y++ {
		term.dirty[y] = true
		for x := x1; x <= x2; x++ {
			gp := &term.line[y][x]
			if selected(x, y) {
				selclear()
			}
			gp.fg = term.c.attr.fg
			gp.bg = term.c.attr.bg
			gp.mode = 0
			gp.u = ' '
		}
	}
}

func tdeletechar(n int) {
	n = clamp(n, 0, term.col-term.c.x)

	dst := term.c.x
	src := term.c.x + n
	size := term.col - src
	line := term.line[term.c.y]

	copy(line[dst:dst+size], line[src:])
	tclearregion(term.col-n, term.c.y, term.col-1, term.c.y)
}

func tinsertblank(n int) {
	n = clamp(n, 0, term.col-term.c.x)

	dst := term.c.x + n
	src := term.c.x
	size := term.col - dst
	line := term.line[term.c.y]

	copy(line[dst:dst+size], line[src:])
	tclearregion(src, term.c.y, dst-1, term.c.y)
}

func tinsertblankline(n int) {
	if term.top <= term.c.y && term.c.y <= term.bot {
		tscrolldown(term.c.y, n)
	}
}

func tdeleteline(n int) {
	if term.top <= term.c.y && term.c.y <= term.bot {
		tscrollup(term.c.y, n)
	}
}

func tdefcolor(attr []int, npar *int) int32 {
	idx := int32(-1)
	switch attr[*npar+1] {
	case 2: // direct color in RGB space
		if *npar+4 >= len(attr) {
			fmt.Fprintf(os.Stderr, "erresc(38): Incorrect number of parameters (%d)\n", *npar)
			break
		}
		r := attr[*npar+2]
		g := attr[*npar+3]
		b := attr[*npar+4]
		*npar += 4
		if !(0 <= r && r <= 255) || !(0 <= g && g <= 255) || !(0 <= b && b <= 255) {
			fmt.Fprintf(os.Stderr, "erresc: bad rgb color (%u,%u,%u)\n", r, g, b)
		} else {
			idx = truecolor(r, g, b)
		}
	case 5: // indexed color
		if *npar+2 >= len(attr) {
			fmt.Fprintf(os.Stderr, "erresc(38): Incorrect number of parameters (%d)\n", *npar)
			break
		}
		*npar += 2
		if !(0 <= attr[*npar] && attr[*npar] <= 255) {
			fmt.Fprintf(os.Stderr, "erresc: bad fgcolor %d\n", attr[*npar])
		} else {
			idx = int32(attr[*npar])
		}
	case 0: // implemented defined (only foreground)
		fallthrough
	case 1: // transparent
		fallthrough
	case 3: // direct color in CMY space
		fallthrough
	case 4: // direct color in CMYK space
		fallthrough
	default:
		fmt.Fprintf(os.Stderr, "erresc(38): gfx attr %d unknown\n", attr[*npar])
	}
	return idx
}

func tsetattr(attr []int) {
	for i := 0; i < len(attr); i++ {
		switch attr[i] {
		case 0:
			term.c.attr.mode &^= (ATTR_BOLD |
				ATTR_FAINT |
				ATTR_ITALIC |
				ATTR_UNDERLINE |
				ATTR_BLINK |
				ATTR_REVERSE |
				ATTR_INVISIBLE |
				ATTR_STRUCK)
			term.c.attr.fg = defaultfg
			term.c.attr.bg = defaultbg
		case 1:
			term.c.attr.mode |= ATTR_BOLD
		case 2:
			term.c.attr.mode |= ATTR_FAINT
		case 3:
			term.c.attr.mode |= ATTR_ITALIC
		case 4:
			term.c.attr.mode |= ATTR_UNDERLINE
		case 5: // slow blink
			fallthrough
		case 6: // rapid blink
			term.c.attr.mode |= ATTR_BLINK
		case 7:
			term.c.attr.mode |= ATTR_REVERSE
		case 8:
			term.c.attr.mode |= ATTR_INVISIBLE
		case 9:
			term.c.attr.mode |= ATTR_STRUCK
		case 22:
			term.c.attr.mode &^= (ATTR_BOLD | ATTR_FAINT)
		case 23:
			term.c.attr.mode &^= ATTR_ITALIC
		case 24:
			term.c.attr.mode &^= ATTR_UNDERLINE
		case 25:
			term.c.attr.mode &^= ATTR_BLINK
		case 27:
			term.c.attr.mode &^= ATTR_REVERSE
		case 28:
			term.c.attr.mode &^= ATTR_INVISIBLE
		case 29:
			term.c.attr.mode &^= ATTR_STRUCK
		case 38:
			if idx := tdefcolor(attr, &i); idx >= 0 {
				term.c.attr.fg = uint32(idx)
			}
		case 39:
			term.c.attr.fg = defaultfg
		case 48:
			if idx := tdefcolor(attr, &i); idx >= 0 {
				term.c.attr.bg = uint32(idx)
			}
		case 49:
			term.c.attr.bg = defaultbg
		default:
			switch {
			case 30 <= attr[i] && attr[i] <= 37:
				term.c.attr.fg = uint32(attr[i] - 30)
			case 40 <= attr[i] && attr[i] <= 47:
				term.c.attr.bg = uint32(attr[i] - 40)
			case 90 <= attr[i] && attr[i] <= 97:
				term.c.attr.fg = uint32(attr[i] - 90 + 8)
			case 100 <= attr[i] && attr[i] <= 107:
				term.c.attr.bg = uint32(attr[i] - 100 + 8)
			default:
				fmt.Fprintf(os.Stderr, "erresc(default): gfx attr %d unknown\n", attr[i])
				csidump()
			}
		}
	}
}

func tresize(col, row int) {
	minrow := min(row, term.row)
	mincol := min(col, term.col)

	if col < 1 || row < 1 {
		fmt.Fprintf(os.Stderr, "tresize: error resizing to %dx%d\n", col, row)
		return
	}

	// slide screen to keep cursor where we expect it -
	// tscrollup would work here, but we can optimize to
	// memmove because we're freeing the earlier lines

	// ensure that both src and dst are not NULL
	var line, alt []Line
	i := term.c.y - row
	if i > 0 {
		copy(term.line[:row], term.line[i:])
		copy(term.alt[:row], term.alt[i:])
	}

	// resize to new height
	line, alt, dirty, tabs := term.line, term.alt, term.dirty, term.tabs
	term.line = make([]Line, row)
	term.alt = make([]Line, row)
	term.dirty = make([]bool, row)
	term.tabs = make([]bool, col)
	copy(term.line, line)
	copy(term.alt, alt)
	copy(term.dirty, dirty)
	copy(term.tabs, tabs)

	// resize each row to new width, zero-pad if needed
	for i := 0; i < row; i++ {
		line, alt := term.line[i], term.alt[i]
		term.line[i] = make([]Glyph, col)
		term.alt[i] = make([]Glyph, col)
		copy(term.line[i], line)
		copy(term.alt[i], alt)
	}

	if col > term.col {
		bp := term.col
		for i := 0; i < col-term.col; i++ {
			term.tabs[bp+i] = false
		}
		for bp--; bp > len(term.tabs) && !term.tabs[bp]; {
			bp--
		}
		for bp += tabspaces; bp < col; bp += tabspaces {
			term.tabs[bp] = true
		}
	}
	// update terminal size
	term.col = col
	term.row = row
	// reset scrolling region
	tsetscroll(0, row-1)
	// make use of the LIMIT in tmoveto
	tmoveto(term.c.x, term.c.y)
	// Clearing both screens (it makes dirty all lines)
	c := term.c
	for i := 0; i < 2; i++ {
		if mincol < col && 0 < minrow {
			tclearregion(mincol, 0, col-1, minrow-1)
		}
		if 0 < col && minrow < row {
			tclearregion(0, minrow, col-1, row-1)
		}
		tswapscreen()
		tcursor(CURSOR_LOAD)
	}
	term.c = c
}

func resettitle() {
	xsettitle(nil)
}

func tsetscroll(t, b int) {
	t = clamp(t, 0, term.row-1)
	b = clamp(b, 0, term.row-1)
	if t > b {
		t, b = b, t
	}
	term.top = t
	term.bot = b
}

func tsetmode(priv, set bool, args []int) {
	for _, arg := range args {
		if priv {
			switch arg {
			case 1: // DECCKM -- Cursor key
				xsetmode(set, MODE_APPCURSOR)
			case 5: // DECSCNM -- Reverse video
				xsetmode(set, MODE_REVERSE)
			case 6: // DECOM -- Origin
				term.c.state &^= CURSOR_ORIGIN
				if set {
					term.c.state |= CURSOR_ORIGIN
				}
				tmoveato(0, 0)
			case 7: // DECAWM -- Auto wrap
				term.mode &^= MODE_WRAP
				if set {
					term.mode |= MODE_WRAP
				}
			case 0: // Error (IGNORED) */
			case 2: // DECANM -- ANSI/VT52 (IGNORED)
			case 3: // DECCOLM -- Column  (IGNORED)
			case 4: // DECSCLM -- Scroll (IGNORED)
			case 8: // DECARM -- Auto repeat (IGNORED)
			case 18: // DECPFF -- Printer feed (IGNORED)
			case 19: // DECPEX -- Printer extent (IGNORED)
			case 42: // DECNRCM -- National characters (IGNORED)
			case 12: // att610 -- Start blinking cursor (IGNORED)
			case 25: // DECTCEM -- Text Cursor Enable Mode
				xsetmode(!set, MODE_HIDE)
			case 9: // X10 mouse compatibility mode
				xsetpointermotion(false)
				xsetmode(false, MODE_MOUSE)
				xsetmode(set, MODE_MOUSEX10)
			case 1000: // 1000: report button press
				xsetpointermotion(false)
				xsetmode(false, MODE_MOUSE)
				xsetmode(set, MODE_MOUSEBTN)
			case 1002: // 1002: report motion on button press
				xsetpointermotion(false)
				xsetmode(false, MODE_MOUSE)
				xsetmode(set, MODE_MOUSEMOTION)
			case 1003: // 1003: enable all mouse motions
				xsetpointermotion(set)
				xsetmode(false, MODE_MOUSE)
				xsetmode(set, MODE_MOUSEMANY)
			case 1004: // 1004: send focus events to tty
				xsetmode(set, MODE_FOCUS)
			case 1006: // 1006: extended reporting mode
				xsetmode(set, MODE_MOUSESGR)
			case 1034:
				xsetmode(set, MODE_8BIT)
			case 1049: // swap screen & set/restore cursor as xterm
				if !allowaltscreen {
					break
				}
				if set {
					tcursor(CURSOR_SAVE)
				} else {
					tcursor(CURSOR_LOAD)
				}
				fallthrough
			case 47: // swap screen
				fallthrough
			case 1047:
				if !allowaltscreen {
					break
				}
				alt := term.mode&MODE_ALTSCREEN != 0
				if alt {
					tclearregion(0, 0, term.col-1, term.row-1)
				}
				// set is always 1 or 0
				if (set && !alt) || (!set && alt) {
					tswapscreen()
				}
				if arg != 1049 {
					break
				}
				fallthrough
			case 1048:
				if set {
					tcursor(CURSOR_SAVE)
				} else {
					tcursor(CURSOR_LOAD)
				}
			case 2004: // 2004: bracketed paste mode
				xsetmode(set, MODE_BRCKTPASTE)
				// Not implemented mouse modes. See comments there.
			case 1001: // mouse highlight mode; can hang the terminal by design when implemented.
				fallthrough
			case 1005: // UTF-8 mouse mode; will confuse applications not supporting UTF-8 and luit.
				fallthrough
			case 1015: // urxvt mangled mouse mode; incompatible and can be mistaken for other control codes.
			default:
				fmt.Fprintf(os.Stderr, "erresc: unknown private set/reset mode %d\n", arg)
			}
		} else {
			switch arg {
			case 0: // Error (IGNORED)
			case 2:
				xsetmode(set, MODE_KBDLOCK)
			case 4: // IRM -- Insertion-replacement
				term.mode &^= MODE_INSERT
				if set {
					term.mode |= MODE_INSERT
				}
			case 12: // SRM -- Send/Receive
				term.mode &^= MODE_ECHO
				if !set {
					term.mode |= MODE_ECHO
				}
			case 20: // LNM -- Linefeed/new line
				term.mode &^= MODE_CRLF
				if set {
					term.mode |= MODE_CRLF
				}
			default:
				fmt.Fprintf(os.Stderr, "erresc: unknown set/reset mode %d\n", args)
			}
		}
	}
}

func csihandle() {
	unknown := func() {
		fmt.Fprintf(os.Stderr, "erresc: unknown csi ")
		csidump()
	}

	switch csiescseq.mode[0] {
	default:
		unknown()
	case '@': // ICH -- Insert <n> blank char
		if csiescseq.arg[0] == 0 {
			csiescseq.arg[0] = 1
		}
		tinsertblank(csiescseq.arg[0])
	case 'A': // CUU -- Cursor <n> Up
		if csiescseq.arg[0] == 0 {
			csiescseq.arg[0] = 1
		}
		tmoveto(term.c.x, term.c.y-csiescseq.arg[0])
	case 'B': // CUD -- Cursor <n> Down
		fallthrough
	case 'e': // VPR --Cursor <n> Down
		if csiescseq.arg[0] == 0 {
			csiescseq.arg[0] = 1
		}
		tmoveto(term.c.x, term.c.y+csiescseq.arg[0])
	case 'i': // MC -- Media Copy
		switch csiescseq.arg[0] {
		case 0:
			tdump()
		case 1:
			tdumpline(term.c.y)
		case 2:
			tdumpsel()
		case 4:
			term.mode &^= MODE_PRINT
		case 5:
			term.mode |= MODE_PRINT
		}
	case 'c': // DA -- Device Attributes
		if csiescseq.arg[0] == 0 {
			ttywrite(vtiden, false)
		}
	case 'C': // CUF -- Cursor <n> Forward
		fallthrough
	case 'a': // HPR -- Cursor <n> Forward
		if csiescseq.arg[0] == 0 {
			csiescseq.arg[0] = 1
		}
		tmoveto(term.c.x+csiescseq.arg[0], term.c.y)
	case 'D': // CUB -- Cursor <n> Backward
		if csiescseq.arg[0] == 0 {
			csiescseq.arg[0] = 1
		}
		tmoveto(term.c.x-csiescseq.arg[0], term.c.y)
	case 'E': // CNL -- Cursor <n> Down and first col
		if csiescseq.arg[0] == 0 {
			csiescseq.arg[0] = 1
		}
		tmoveto(0, term.c.y+csiescseq.arg[0])
	case 'F': // CPL -- Cursor <n> Up and first col
		if csiescseq.arg[0] == 0 {
			csiescseq.arg[0] = 1
		}
		tmoveto(0, term.c.y-csiescseq.arg[0])
	case 'g': // TBC -- Tabulation clear
		switch csiescseq.arg[0] {
		case 0: // clear current tab stop
			term.tabs[term.c.x] = false
		case 3: // clear all the tabs
			for i := range term.tabs {
				term.tabs[i] = false
			}
		default:
			unknown()
		}
	case 'G': // CHA -- Move to <col>
		fallthrough
	case '`': // HPA
		if csiescseq.arg[0] == 0 {
			csiescseq.arg[0] = 1
		}
		tmoveto(csiescseq.arg[0]-1, term.c.y)
	case 'H': // CUP -- Move to <row> <col
		fallthrough
	case 'f': // HVP
		if csiescseq.arg[0] == 0 {
			csiescseq.arg[0] = 1
		}
		if csiescseq.arg[1] == 0 {
			csiescseq.arg[1] = 1
		}
		tmoveato(csiescseq.arg[1]-1, csiescseq.arg[0]-1)
	case 'I': // CHT -- Cursor Forward Tabulation <n> tab stops
		if csiescseq.arg[0] == 0 {
			csiescseq.arg[0] = 1
		}
		tputtab(csiescseq.arg[0])
	case 'J': // ED -- Clear screen
		switch csiescseq.arg[0] {
		case 0: // below
			tclearregion(term.c.x, term.c.y, term.col-1, term.c.y)
			if term.c.y < term.row-1 {
				tclearregion(0, term.c.y+1, term.col-1, term.row-1)
			}
		case 1: // above
			if term.c.y > 1 {
				tclearregion(0, 0, term.col-1, term.c.y-1)
			}
			tclearregion(0, term.c.y, term.c.x, term.c.y)
		case 2: // all
			tclearregion(0, 0, term.col-1, term.row-1)
		default:
			unknown()
		}
	case 'K': // EL -- Clear line
		switch csiescseq.arg[0] {
		case 0: // right
			tclearregion(term.c.x, term.c.y, term.col-1, term.c.y)
		case 1: // left
			tclearregion(0, term.c.y, term.c.x, term.c.y)
		case 2: // right
			tclearregion(0, term.c.y, term.col-1, term.c.y)
		}
	case 'S': // SU -- Scroll <n> line up
		if csiescseq.arg[0] == 0 {
			csiescseq.arg[0] = 1
		}
		tscrollup(term.top, csiescseq.arg[0])
	case 'T': // SD -- Scroll <n> line down
		if csiescseq.arg[0] == 0 {
			csiescseq.arg[0] = 1
		}
		tscrolldown(term.top, csiescseq.arg[0])
	case 'L': // IL -- Insert <n> blank lines
		if csiescseq.arg[0] == 0 {
			csiescseq.arg[0] = 1
		}
		tinsertblankline(csiescseq.arg[0])
	case 'l': // RM -- Reset Mode
		tsetmode(csiescseq.priv, false, csiescseq.arg[:csiescseq.narg])
	case 'M': // DL -- Delete <n> lines TODO
		if csiescseq.arg[0] == 0 {
			csiescseq.arg[0] = 1
		}
		tdeleteline(csiescseq.arg[0])
	case 'X': // ECH -- Erase <n> char
		if csiescseq.arg[0] == 0 {
			csiescseq.arg[0] = 1
		}
		tclearregion(term.c.x, term.c.y, term.c.x+csiescseq.arg[0]-1, term.c.y)
	case 'P': // DCH -- Delete <n> char
		if csiescseq.arg[0] == 0 {
			csiescseq.arg[0] = 1
		}
		tdeletechar(csiescseq.arg[0])
	case 'Z': // CBT -- Cursor Backward Tabulation <n> tab stops
		if csiescseq.arg[0] == 0 {
			csiescseq.arg[0] = 1
		}
		tputtab(-csiescseq.arg[0])
	case 'd': // VPA -- Move to <row>
		if csiescseq.arg[0] == 0 {
			csiescseq.arg[0] = 1
		}
		tmoveato(term.c.x, csiescseq.arg[0]-1)
	case 'h': // SM -- Set terminal mode
		tsetmode(csiescseq.priv, true, csiescseq.arg[:csiescseq.narg])
	case 'm': // SGR -- Terminal attribute (color)
		tsetattr(csiescseq.arg[:csiescseq.narg])
	case 'n': // DSR – Device Status Report (cursor position)
		if csiescseq.arg[0] == 6 {
			buf := fmt.Sprintf("\033[%d;%dR", term.c.y+1, term.c.x+1)
			ttywrite([]byte(buf), false)
		}
	case 'r': // DECSTBM -- Set Scrolling Region
		if csiescseq.priv {
			unknown()
		} else {
			if csiescseq.arg[0] == 0 {
				csiescseq.arg[0] = 1
			}
			if csiescseq.arg[1] == 0 {
				csiescseq.arg[1] = term.row
			}
			tsetscroll(csiescseq.arg[0]-1, csiescseq.arg[1]-1)
			tmoveato(0, 0)
		}
	case 's': // DECSC -- Save cursor position (ANSI.SYS)
		tcursor(CURSOR_SAVE)
	case 'u': // DECRC -- Restore cursor position (ANSI.SYS)
		tcursor(CURSOR_LOAD)
	case ' ':
		switch csiescseq.mode[1] {
		case 'q': // DECSCUSR -- Set Cursor Style
			if xsetcursor(csiescseq.arg[0]) {
				unknown()
			}
		default:
			unknown()
		}
	}
}

func csidump() {
	fmt.Fprintf(os.Stderr, "ESC[")
	for i := 0; i < csiescseq.len; i++ {
		c := csiescseq.buf[i] & 0xff
		switch {
		case unicode.IsPrint(rune(c)):
			fmt.Fprintf(os.Stderr, "%c", c)
		case c == '\n':
			fmt.Fprintf(os.Stderr, "(\\n)")
		case c == '\r':
			fmt.Fprintf(os.Stderr, "(\\r)")
		case c == 0x1b:
			fmt.Fprintf(os.Stderr, "(\\e)")
		default:
			fmt.Fprintf(os.Stderr, "(%02x)", c)
		}
	}
	fmt.Fprintf(os.Stderr, "\n")
}

func csireset() {
	csiescseq = CSIEscape{}
}

func strhandle() {
	term.esc &^= (ESC_STR_END | ESC_STR)
	strparse()
	par := 0
	narg := strescseq.narg
	if narg > 0 {
		par, _ = strconv.Atoi(string(strescseq.args[0]))
	}

	var p []byte
	switch strescseq.typ {
	case ']': // OSC -- Operating System Command
		switch par {
		case 0, 1, 2:
			if narg > 1 {
				xsettitle(strescseq.args[1])
			}
			return
		case 52:
			if narg > 2 {
				dec, err := base64.StdEncoding.DecodeString(string(strescseq.args[2]))
				if err == nil {
					xsetsel(dec)
					xclipcopy()
				} else {
					fmt.Fprintln(os.Stderr, "erresc: invalid base64")
				}
			}
			return
		case 4: /* color set */
			if narg < 3 {
				break
			}
			p = strescseq.args[2]
			fallthrough
		case 104: // color reset, here p = NULL
			j := -1
			if narg > 1 {
				j, _ = strconv.Atoi(string(strescseq.args[1]))
			}
			if xsetcolorname(j, string(p)) {
				if par == 104 && narg <= 1 {
					// color reset without parameter
					return
				}
				fmt.Fprintf(os.Stderr, "erresc: invalid color j=%d, p=%q\n", j, p)
			} else {
				// TODO if defaultbg color is changed, borders
				// are dirty
				redraw()
			}
			return
		}
	case 'k': // old title set compatibility
		xsettitle(strescseq.args[0])
		return
	case 'P': // DCS -- Device Control String
		term.mode |= ESC_DCS
		fallthrough
	case '_': // APC -- Application Program Command
		fallthrough
	case '^': // PM -- Privacy Message
		return
	}

	fmt.Fprintf(os.Stderr, "erresc: unknown str ")
	strdump()
}

func strparse() {
	strescseq.narg = 0

	p := strescseq.buf[:strescseq.len]
	if len(p) == 0 {
		return
	}

	toks := strings.Split(string(p), ";")
	for _, t := range toks {
		if strescseq.narg < STR_ARG_SIZ {
			strescseq.args[strescseq.narg] = []byte(t)
			strescseq.narg++
		} else {
			break
		}
	}
}

func strdump() {
	fmt.Fprintf(os.Stderr, "ESC%c", strescseq.typ)
	for i := 0; i < strescseq.len; i++ {
		c := rune(strescseq.buf[i] & 0xff)
		switch {
		case c == 0:
			fmt.Fprintf(os.Stderr, "\n")
			return
		case unicode.IsPrint(c):
			fmt.Fprint(os.Stderr, c)
		case c == '\n':
			fmt.Fprintf(os.Stderr, "(\\n)")
		case c == '\r':
			fmt.Fprintf(os.Stderr, "(\\r)")
		case c == 0x1b:
			fmt.Fprintf(os.Stderr, "(\\e)")
		default:
			fmt.Fprintf(os.Stderr, "(%02x)", c)
		}
	}
	fmt.Fprintf(os.Stderr, "ESC\\\n")
}

func selected(x, y int) bool {
	if sel.mode == SEL_EMPTY || sel.ob.x == -1 || sel.alt != (term.mode&MODE_ALTSCREEN != 0) {
		return false
	}

	if sel.typ == SEL_RECTANGULAR {
		return sel.nb.y <= y && y <= sel.ne.y && sel.nb.x <= x && x <= sel.ne.x
	}

	return sel.nb.y <= y && y <= sel.ne.y &&
		(y != sel.nb.y || x >= sel.nb.x) &&
		(y != sel.ne.y || x <= sel.ne.x)
}

func selsnap(x, y *int, direction int) {
	switch sel.snap {
	case SNAP_WORD:
		// Snap around if the word wraps around at the end or
		// beginning of a line.
		prevgp := &term.line[*y][*x]
		prevdelim := isdelim(prevgp.u)

		var xt, yt int
		for {
			newx := *x + direction
			newy := *y
			if 0 <= newx && newx <= term.col-1 {
				newy += direction
				newx = (newx + term.col) % term.col
				if !(0 <= newy && newy <= term.row-1) {
					break
				}

				if direction > 0 {
					yt = *y
					xt = *x
				} else {
					yt = newy
					xt = newx
				}
				if term.line[yt][xt].mode&ATTR_WRAP == 0 {
					break
				}
			}

			if newx >= tlinelen(newy) {
				break
			}

			gp := &term.line[newy][newx]
			delim := isdelim(gp.u)
			if (gp.mode&ATTR_WDUMMY) == 0 && (delim != prevdelim || (delim && gp.u != prevgp.u)) {
				break
			}

			*x = newx
			*y = newy
			prevgp = gp
			prevdelim = delim
		}

	case SNAP_LINE:
		// Snap around if the the previous line or the current one
		// has set ATTR_WRAP at its end. Then the whole next or
		// previous line will be selected.
		*x = term.col - 1
		if direction < 0 {
			*x = 0
		}
		if direction < 0 {
			for ; *y > 0; *y += direction {
				if term.line[*y-1][term.col-1].mode&ATTR_WRAP == 0 {
					break
				}
			}
		} else if direction > 0 {
			for ; *y < term.row-1; *y += direction {
				if term.line[*y][term.col-1].mode&ATTR_WRAP == 0 {
					break
				}
			}
		}
	}
}

func getsel() []byte {
	if sel.ob.x == -1 {
		return nil
	}

	bufsize := (term.col + 1) * (sel.ne.y - sel.nb.y + 1) * utf8.UTFMax
	str := make([]byte, bufsize)
	ptr := str

	// append every set & selected glyph to the selection
	for y := sel.nb.y; y <= sel.ne.y; y++ {
		linelen := tlinelen(y)
		if linelen == 0 {
			ptr[0], ptr = '\n', ptr[1:]
			continue
		}

		gp := term.line[y]
		gpi := 0
		lastx := 0
		if sel.typ == SEL_RECTANGULAR {
			gpi = sel.nb.x
			lastx = sel.ne.x
		} else {
			if sel.nb.y == y {
				gpi = sel.nb.x
			}
			if sel.ne.y == y {
				lastx = sel.ne.x
			} else {
				lastx = term.col - 1
			}
		}

		lasti := min(lastx, linelen-1)
		for lasti >= gpi && gp[lasti].u == ' ' {
			lasti--
		}

		for ; gpi <= lasti; gpi++ {
			if gp[gpi].mode&ATTR_WDUMMY != 0 {
				continue
			}

			ul := utf8.EncodeRune(ptr, gp[gpi].u)
			ptr = ptr[ul:]
		}

		// Copy and pasting of line endings is inconsistent
		// in the inconsistent terminal and GUI world.
		// The best solution seems like to produce '\n' when
		// something is copied from st and convert '\n' to
		// '\r', when something to be pasted is received by st.
		// FIXME: Fix the computer world.
		if (y < sel.ne.y || lastx >= linelen) && gp[lasti].mode&ATTR_WRAP == 0 {
			ptr[0], ptr = '\n', ptr[1:]
		}
	}
	return bytes.TrimRight(str, "\x00")
}

func selclear() {
	if sel.ob.x == -1 {
		return
	}
	sel.mode = SEL_IDLE
	sel.ob.x = -1
	tsetdirt(sel.nb.y, sel.ne.y)
}

func stty(args []string) {
	command := stty_args + strings.Join(args, " ")
	cmd := exec.Command(command)
	err := cmd.Run()
	if err != nil {
		fmt.Fprintln(os.Stderr, "Couldn't call stty: %v", err)
	}
}

func setenv(env []string, key, val string) []string {
	str := fmt.Sprintf("%s=%s", key, val)
	for i := range env {
		if strings.HasPrefix(env[i], key) {
			env[i] = str
			return env
		}
	}
	env = append(env, str)
	return env
}

func unsetenv(env []string, key string) []string {
	for i := range env {
		if strings.HasPrefix(env[i], key) {
			copy(env[i:], env[i+1:])
			env = env[:len(env)-1]
			break
		}
	}
	return env
}

func execsh(s *os.File, cmd string, args []string) {
	usr, err := posix.Getpwuid(posix.Geteuid())
	if err != nil {
		log.Fatal("can't get user info: %v", err)
	}

	sh := usr.Shell
	if sh == "" {
		sh = os.Getenv("SHELL")
	}
	if sh == "" {
		sh = cmd
	}

	prog := sh
	if len(args) > 0 {
		prog = args[0]
		args = args[1:]
	} else if utmp != "" {
		prog = utmp
	}

	env := os.Environ()
	env = unsetenv(env, "COLUMNS")
	env = unsetenv(env, "LINES")
	env = unsetenv(env, "TERMCAP")
	env = setenv(env, "LOGNAME", usr.Name)
	env = setenv(env, "USER", usr.Name)
	env = setenv(env, "SHELL", sh)
	env = setenv(env, "HOME", usr.Dir)
	env = setenv(env, "TERM", termname)

	exe := exec.Command(prog, args...)
	exe.Stdin = s
	exe.Stdout = s
	exe.Stderr = s
	exe.Env = env
	exe.ExtraFiles = []*os.File{s}
	exe.SysProcAttr = &syscall.SysProcAttr{
		Setsid:  true,
		Setctty: true,
	}
	err = exe.Start()
	if err != nil {
		log.Fatalf("can't start shell: %v", err)
	}

	go sigchld(exe)
}

func ttynew(line, cmd, out string, args []string) *os.File {
	var err error
	if out != "" {
		term.mode |= MODE_PRINT
		if out == "-" {
			iofile = os.Stdout
		} else {
			iofile, err = os.OpenFile(out, os.O_WRONLY|os.O_CREATE, 0666)
			if err != nil {
				log.Fatal(err)
			}
		}
	}

	if line != "" {
		cmdfile, err = os.OpenFile(line, os.O_RDWR, 0644)
		if err != nil {
			log.Fatal(err)
		}
		syscall.Dup2(int(cmdfile.Fd()), int(os.Stdin.Fd()))
		stty(args)
		return cmdfile
	}

	// seems to work fine on linux, openbsd and freebsd
	m, s, err := posix.Openpty(nil, nil)
	if err != nil {
		log.Fatalf("openpty failed: %v", err)
	}

	execsh(s, cmd, args)
	cmdfile = m
	return cmdfile
}

func ttyread() int {
	written := twrite(term.buf[:term.buflen], false)
	term.buflen -= written
	copy(term.buf[:term.buflen], term.buf[written:])
	return term.buflen
}

func tputc(u rune) {
	var c [utf8.UTFMax]byte
	var width, len_ int
	control := iscontrol(u)
	if term.mode&MODE_UTF8 == 0 && term.mode&MODE_SIXEL == 0 {
		c[0] = byte(u)
		width, len_ = 1, 1
	} else {
		len_ = utf8.EncodeRune(c[:], u)
		if width = posix.Wcwidth(u); !control && width == -1 {
			// UTF_INVALID
			copy(c[:], []byte("\357\277\275"))
			width = 1
		}
	}

	if term.mode&MODE_PRINT != 0 {
		tprinter(c[:len_])
	}

	// STR sequence must be checked before anything else
	// because it uses all following characters until it
	// receives a ESC, a SUB, a ST or any other C1 control
	// character.
	if term.esc&ESC_STR != 0 {
		if u == '\a' || u == 030 || u == 032 || u == 033 || iscontrolc1(u) {
			term.esc &^= (ESC_START | ESC_STR | ESC_DCS)
			if term.mode&MODE_SIXEL != 0 {
				// TODO: render sixel
				term.mode &^= MODE_SIXEL
				return
			}
			term.esc |= ESC_STR_END
			goto check_control_code
		}

		if term.mode&MODE_SIXEL != 0 {
			// TODO: implement sixel mode
			return
		}
		if term.esc&ESC_DCS != 0 && strescseq.len == 0 && u == 'q' {
			term.mode |= MODE_SIXEL
		}

		if strescseq.len+len_ >= len(strescseq.buf)-1 {
			// Here is a bug in terminals. If the user never sends
			// some code to stop the str or esc command, then st
			// will stop responding. But this is better than
			// silently failing with unknown characters. At least
			// then users will report back
			// In this case users ever get fixed, here is the code:
			// term.esc 0
			// strhandle()
			return
		}
		copy(strescseq.buf[strescseq.len:], c[:len_])
		strescseq.len += len_
		return
	}

check_control_code:
	// Actions of control codes must be performed as soon they arrive
	// because they can be embedded inside a control sequence, and
	// they must not cause conflicts with sequences.
	if control {
		tcontrolcode(u)
		// control codes are not shown ever
		return
	} else if term.esc&ESC_START != 0 {
		if term.esc&ESC_CSI != 0 {
			csiescseq.buf[csiescseq.len], csiescseq.len = byte(u), csiescseq.len+1
			if (0x40 <= u && u <= 0x7E) || csiescseq.len >= len(csiescseq.buf)-1 {
				term.esc = 0
				csiparse()
				csihandle()
			}
			return
		} else if term.esc&ESC_UTF8 != 0 {
			tdefutf8(u)
		} else if term.esc&ESC_ALTCHARSET != 0 {
			tdeftran(u)
		} else if term.esc&ESC_TEST != 0 {
			tdectest(u)
		} else {
			if !eschandle(u) {
				return
			}
			// sequence already finished
		}
		term.esc = 0
		// All characters which form part of a sequence are not printed
		return
	}
	if sel.ob.x != -1 && sel.ob.y <= term.c.y && term.c.y <= sel.oe.y {
		selclear()
	}

	gp := &term.line[term.c.y][term.c.x]
	gpu := term.line[term.c.y][term.c.x:]
	if term.mode&MODE_WRAP != 0 && term.c.state&CURSOR_WRAPNEXT != 0 {
		gp.mode |= ATTR_WRAP
		tnewline(true)
		gp = &term.line[term.c.y][term.c.x]
		gpu = term.line[term.c.y][term.c.x:]
	}

	if term.mode&MODE_INSERT != 0 && term.c.x+width < term.col {
		copy(gpu[width:], gpu[:term.col-term.c.x-width])
	}

	if term.c.x+width > term.col {
		tnewline(true)
		gp = &term.line[term.c.y][term.c.x]
		gpu = term.line[term.c.y][term.c.x:]
	}
	tsetchar(u, &term.c.attr, term.c.x, term.c.y)

	if width == 2 {
		gp.mode |= ATTR_WIDE
		if term.c.x+1 < term.col {
			gpu[1].u = 0
			gpu[1].mode = ATTR_WDUMMY
		}
	}
	if term.c.x+width < term.col {
		tmoveto(term.c.x+width, term.c.y)
	} else {
		term.c.state |= CURSOR_WRAPNEXT
	}
}

func strreset() {
	strescseq = STREscape{}
}

func sendbreak(interface{}) {
	err := posix.Tcsendbreak(int(cmdfile.Fd()), 0)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error sending break: %v", err)
	}
}

func tprinter(s []byte) {
	if iofile == nil {
		return
	}
	_, err := iofile.Write(s)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error writing to output file")
		iofile.Close()
		iofile = nil
	}
}

func toggleprinter(interface{}) {
	term.mode ^= MODE_PRINT
}

func printscreen(interface{}) {
	tdump()
}

func printsel(interface{}) {
	tdumpsel()
}

func tdumpsel() {
	ptr := getsel()
	if ptr != nil {
		tprinter(ptr)
	}
}

func tdumpline(n int) {
	var buf [utf8.UTFMax]byte
	bp := term.line[n]
	end := min(tlinelen(n), term.col) - 1
	if end > 0 || bp[0].u != ' ' {
		for i := 0; i <= end && bp[i].u != ' '; i++ {
			buflen := utf8.EncodeRune(buf[:], bp[i].u)
			tprinter(buf[:buflen])
		}
	}
	tprinter([]byte("\n"))
}

func tdump() {
	for i := 0; i < term.row; i++ {
		tdumpline(i)
	}
}

func tputtab(n int) {
	x := term.c.x
	if n > 0 {
		for x < term.col && n != 0 {
			n--
			for x++; x < term.col && !term.tabs[x]; x++ {
				// nothing
			}
		}
	} else if n < 0 {
		for x > 0 && n != 0 {
			n++
			for x--; x > 0 && !term.tabs[x]; x-- {
				// nothing
			}
		}
	}
	term.c.x = clamp(x, 0, term.col-1)
}

func tdefutf8(ascii rune) {
	if ascii == 'G' {
		term.mode |= MODE_UTF8
	} else if ascii == '@' {
		term.mode &^= MODE_UTF8
	}
}

func tdeftran(ascii rune) {
	cs := "0B"
	vcs := []byte{CS_GRAPHIC0, CS_USA}
	if p := strings.IndexRune(cs, ascii); p < 0 {
		fmt.Fprintf(os.Stderr, "esc unhandled charset: ESC ( %c\n", ascii)
	} else {
		term.trantbl[term.icharset] = vcs[p]
	}
}

func tdectest(c rune) {
	// DEC screen alignment test.
	if c == '8' {
		for x := 0; x < term.col; x++ {
			for y := 0; y < term.row; y++ {
				tsetchar('E', &term.c.attr, x, y)
			}
		}
	}
}

func tstrsequence(c rune) {
	strreset()

	switch c {
	case 0x90: // DCS -- Device Control String
		c = 'P'
		term.esc |= ESC_DCS
	case 0x9f: // APC -- Application Program Command
		c = '_'
	case 0x9e: // PM -- Privacy Message
		c = '^'
	case 0x9d: // OSC -- Operating System Command
		c = ']'
	}
	strescseq.typ = int(c)
	term.esc |= ESC_STR
}

func tcontrolcode(ascii rune) {
	switch ascii {
	case '\t': // HT
		tputtab(1)
		return
	case '\b': // BS
		tmoveto(term.c.x-1, term.c.y)
		return
	case '\r': // CR
		tmoveto(0, term.c.y)
		return
	case '\f': // LF
		fallthrough
	case '\v': // VT
		fallthrough
	case '\n': // LF
		// go to first col if the mode is set
		tnewline(term.mode&MODE_CRLF != 0)
		return
	case '\a': // BEL
		if term.esc&ESC_STR_END != 0 {
			// backwards compatibility to xterm
			strhandle()
		} else {
			xbell()
		}
	case '\033': // ESC
		csireset()
		term.esc &^= (ESC_CSI | ESC_ALTCHARSET | ESC_TEST)
		term.esc |= ESC_START
		return
	case '\016': // SO (LS1 -- Locking shift 1)
	case '\017': // SI (LS0 -- Locking shift 0)
		term.charset = int(1 - (ascii - '\016'))
		return
	case '\032': // SUB
		tsetchar('?', &term.c.attr, term.c.x, term.c.y)
		fallthrough
	case '\030': // CAN
		csireset()
	case '\005': // ENQ (IGNORED)
		fallthrough
	case '\000': // NUL (IGNORED)
		fallthrough
	case '\021': // XON (IGNORED)
		fallthrough
	case '\023': // XOFF (IGNORED)
		fallthrough
	case 0177: // DEL (IGNORED)
		return
	case 0x80: // TODO: PAD
		fallthrough
	case 0x81: // TODO: HOP
		fallthrough
	case 0x82: // TODO: BPH
		fallthrough
	case 0x83: // TODO: NBH
		fallthrough
	case 0x84: // TODO: IND
	case 0x85: // NEL -- Next line
		tnewline(true) // always go to first col
	case 0x86: // TODO: SSA
		fallthrough
	case 0x87: // TODO: ESA
	case 0x88: // HTS -- Horizontal tab stop
		term.tabs[term.c.x] = true
	case 0x89: // TODO: HTJ
		fallthrough
	case 0x8a: // TODO: VTS
		fallthrough
	case 0x8b: // TODO: PLD
		fallthrough
	case 0x8c: // TODO: PLU
		fallthrough
	case 0x8d: // TODO: RI
		fallthrough
	case 0x8e: // TODO: SS2
		fallthrough
	case 0x8f: // TODO: SS3
		fallthrough
	case 0x91: // TODO: PU1
		fallthrough
	case 0x92: // TODO: PU2
		fallthrough
	case 0x93: // TODO: STS
		fallthrough
	case 0x94: // TODO: CCH
		fallthrough
	case 0x95: // TODO: MW
		fallthrough
	case 0x96: // TODO: SPA
		fallthrough
	case 0x97: // TODO: EPA
		fallthrough
	case 0x98: // TODO: SOS
		fallthrough
	case 0x99: // TODO: SGCI
		break
	case 0x9a: // DECID -- Identify Terminal
		ttywrite(vtiden, false)
	case 0x9b: // TODO: CSI
		fallthrough
	case 0x9c: // TODO: ST
	case 0x90: // DCS -- Device Control String
		fallthrough
	case 0x9d: // OSC -- Operating System Command
		fallthrough
	case 0x9e: // PM -- Privacy Message
		fallthrough
	case 0x9f: // APC -- Application Program Command
		tstrsequence(ascii)
		return
	}
	// only CAN, SUB, \a and C1 chars interrupt a sequence
	term.esc &^= (ESC_STR_END | ESC_STR)
}

// returns 1 when the sequence is finished and it hasn't to read
// more characters for this sequence, otherwise 0
func eschandle(ascii rune) bool {
	switch ascii {
	case '[':
		term.esc |= ESC_CSI
		return false
	case '#':
		term.esc |= ESC_TEST
		return false
	case '%':
		term.esc |= ESC_UTF8
		return false
	case 'P': // DCS -- Device Control String
		fallthrough
	case '_': // APC -- Application Program Command
		fallthrough
	case '^': // PM -- Privacy Message
		fallthrough
	case ']': // OSC -- Operating System Command
		fallthrough
	case 'k': // old title set compatibility
		tstrsequence(ascii)
		return false
	case 'n': // LS2 -- Locking shift 2
	case 'o': // LS3 -- Locking shift 3
		term.charset = int(2 + (ascii - 'n'))
	case '(': // GZD4 -- set primary charset G0
		fallthrough
	case ')': // G1D4 -- set secondary charset G1
		fallthrough
	case '*': // G2D4 -- set tertiary charset G2
		fallthrough
	case '+': // G3D4 -- set quaternary charset G3
		term.icharset = int(ascii - '(')
		term.esc |= ESC_ALTCHARSET
		return false
	case 'D': // IND -- Linefeed
		if term.c.y == term.bot {
			tscrollup(term.top, 1)
		} else {
			tmoveto(term.c.x, term.c.y+1)
		}
	case 'E': // NEL -- Next line
		tnewline(true) /* always go to first col */
	case 'H': // HTS -- Horizontal tab stop
		term.tabs[term.c.x] = true
	case 'M': // RI -- Reverse index
		if term.c.y == term.top {
			tscrolldown(term.top, 1)
		} else {
			tmoveto(term.c.x, term.c.y-1)
		}
	case 'Z': // DECID -- Identify Terminal
		ttywrite(vtiden, false)
	case 'c': // RIS -- Reset to initial state
		treset()
		resettitle()
		xloadcols()
	case '=': // DECPAM -- Application keypad
		xsetmode(true, MODE_APPKEYPAD)
	case '>': // DECPNM -- Normal keypad
		xsetmode(false, MODE_APPKEYPAD)
	case '7': // DECSC -- Save Cursor
		tcursor(CURSOR_SAVE)
	case '8': // DECRC -- Restore Cursor
		tcursor(CURSOR_LOAD)
	case '\\': // ST -- String Terminator
		if term.esc&ESC_STR_END != 0 {
			strhandle()
		}
	default:
		char := '.'
		if unicode.IsPrint(ascii) {
			char = ascii
		}
		fmt.Fprintf(os.Stderr, "erresc: unknown sequence ESC 0x%02X '%c'\n", ascii, char)
	}
	return true
}

func twrite(buf []byte, show_ctrl bool) int {
	var (
		u        rune
		n        int
		charsize int
	)
	for ; n < len(buf); n += charsize {
		if term.mode&MODE_UTF8 != 0 && term.mode&MODE_SIXEL == 0 {
			// process a complete utf8 char
			u, charsize = utf8.DecodeRune(buf[n:])
			if charsize == 0 || u == utf8.RuneError {
				break
			}
		} else {
			u = rune(buf[n]) & 0xff
			charsize = 1
		}
		if show_ctrl && iscontrol(u) {
			if u&0x80 != 0 {
				u &= 0x7f
				tputc('^')
				tputc('[')
			} else if u != '\n' && u != '\r' && u != '\t' {
				u ^= 0x40
				tputc('^')
			}
		}
		tputc(u)
	}

	return n
}

func drawregion(x1, y1, x2, y2 int) {
	for y := y1; y < y2; y++ {
		if !term.dirty[y] {
			continue
		}

		term.dirty[y] = false
		xdrawline(term.line[y], x1, y, x2)
	}
}

func draw() {
	if !xstartdraw() {
		return
	}

	// adjust cursor position
	cx := term.c.x
	term.ocx = clamp(term.ocx, 0, term.col-1)
	term.ocy = clamp(term.ocy, 0, term.row-1)
	if term.line[term.ocy][term.ocx].mode&ATTR_WDUMMY != 0 {
		term.ocx--
	}
	if term.line[term.c.y][cx].mode&ATTR_WDUMMY != 0 {
		cx--
	}

	drawregion(0, 0, term.col, term.row)
	xdrawcursor(cx, term.c.y, term.line[term.c.y][cx],
		term.ocx, term.ocy, term.line[term.ocy][term.ocx])
	term.ocx = cx
	term.ocy = term.c.y
	xfinishdraw()
	xximspot(term.ocx, term.ocy)
}

func redraw() {
	tfulldirt()
	draw()
}

func trun() {
	for {
		nr, err := cmdfile.Read(term.buf[term.buflen:])
		if err != nil {
			log.Fatalf("couldn't read from shell: %v", err)
		}
		term.buflen += nr
		term.rdy <- struct{}{}
		<-term.rdy
	}
}
