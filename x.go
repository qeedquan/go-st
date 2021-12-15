package main

import (
	"flag"
	"fmt"
	"log"
	"math"
	"os"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/qeedquan/go-media/math/ga"
	"github.com/qeedquan/go-media/x11/fc"
	"github.com/qeedquan/go-media/x11/xft"
	"github.com/qeedquan/go-media/x11/xlib"
	"github.com/qeedquan/go-media/x11/xlib/xkb"
	"github.com/qeedquan/go-media/x11/xlib/xrender"
)

const (
	MODE_VISIBLE     = 1 << 0
	MODE_FOCUSED     = 1 << 1
	MODE_APPKEYPAD   = 1 << 2
	MODE_MOUSEBTN    = 1 << 3
	MODE_MOUSEMOTION = 1 << 4
	MODE_REVERSE     = 1 << 5
	MODE_KBDLOCK     = 1 << 6
	MODE_HIDE        = 1 << 7
	MODE_APPCURSOR   = 1 << 8
	MODE_MOUSESGR    = 1 << 9
	MODE_8BIT        = 1 << 10
	MODE_BLINK       = 1 << 11
	MODE_FBLINK      = 1 << 12
	MODE_FOCUS       = 1 << 13
	MODE_MOUSEX10    = 1 << 14
	MODE_MOUSEMANY   = 1 << 15
	MODE_BRCKTPASTE  = 1 << 16
	MODE_NUMLOCK     = 1 << 17
	MODE_MOUSE       = MODE_MOUSEBTN | MODE_MOUSEMOTION | MODE_MOUSEX10 | MODE_MOUSEMANY
)

const (
	FRC_NORMAL = iota
	FRC_ITALIC
	FRC_BOLD
	FRC_ITALICBOLD
)

const (
	XK_ANY_MOD    = ^uint(0)
	XK_NO_MOD     = 0
	XK_SWITCH_MOD = (1 << 13)
)

type Fontcache struct {
	font     *xft.Font
	flags    int
	unicodep rune
}

type (
	Draw          = *xft.Draw
	Color         = xft.Color
	GlyphFontSpec = xft.GlyphFontSpec
)

type Shortcut struct {
	mod    uint
	keysym xlib.KeySym
	funct  func(interface{})
	arg    interface{}
}

type MouseShortcut struct {
	b    uint
	mask uint
	s    string
}

type Key struct {
	k    xlib.KeySym
	mask uint
	s    string
	// three-valued logic variables: 0 indifferent, 1 on, -1 off
	appkey    int
	appcursor int
}

// Purely graphic info
type TermWindow struct {
	tw, th int // tty width and height
	w, h   int // window width and height
	ch     int // char height
	cw     int // char width
	mode   int // window state/mode flags
	cursor int // cursor style
}

type XWindow struct {
	dpy                                      *xlib.Display
	cmap                                     xlib.Colormap
	win                                      xlib.Window
	buf                                      xlib.Drawable
	specbuf                                  []GlyphFontSpec
	xembed, wmdeletewin, netwmname, netwmpid xlib.Atom
	xim                                      xlib.IM
	xic                                      xlib.IC
	draw                                     Draw
	vis                                      *xlib.Visual
	attrs                                    xlib.SetWindowAttributes
	scr                                      int
	isfixed                                  bool
	l, t                                     int
	gm                                       int
	qev                                      []xlib.Event
	mrpox, mrpoy                             int
}

type XSelection struct {
	xtarget            xlib.Atom
	primary, clipboard []byte
	tclick1, tclick2   time.Time
}

type Font struct {
	height    int
	width     int
	ascent    int
	descent   int
	badslant  bool
	badweight bool
	lbearing  int
	rbearing  int
	match     *xft.Font
	set       *fc.FontSet
	pattern   *fc.Pattern
}

type DC struct {
	col                        []Color
	font, bfont, ifont, ibfont Font
	gc                         xlib.GC
}

type Option struct {
	class   string
	cmd     []string
	embed   string
	font    string
	io      string
	line    string
	name    string
	title   string
	version bool
}

// XEMBED messages
const (
	XEMBED_FOCUS_IN  = 4
	XEMBED_FOCUS_OUT = 5
)

var handler = [xlib.LASTEvent]func(*xlib.Event){
	xlib.KeyPress:         kpress,
	xlib.ClientMessage:    cmessage,
	xlib.ConfigureNotify:  resize,
	xlib.VisibilityNotify: visibility,
	xlib.UnmapNotify:      unmap,
	xlib.Expose:           expose,
	xlib.FocusIn:          focus,
	xlib.FocusOut:         focus,
	xlib.MotionNotify:     bmotion,
	xlib.ButtonPress:      bpress,
	xlib.ButtonRelease:    brelease,
	// Uncomment if you want the selection to disappear when you select something
	// different in another window.
	// xlib.SelectionClear: selclear_,
	xlib.SelectionNotify: selnotify,
	// PropertyNotify is only turned on when there is some INCR transfer happening
	// for the selection retrieval
	xlib.PropertyNotify:   propnotify,
	xlib.SelectionRequest: selrequest,
}

var (
	// button event on startup: 3 = release
	oldbutton uint = 3

	dc   DC
	xw   XWindow
	xsel XSelection
	win  TermWindow

	opt Option

	frc             []Fontcache
	usedfont        string
	usedfontsize    float64
	defaultfontsize float64
)

func truered(x uint32) uint32 {
	return (x & 0xff0000) >> 8
}

func truegreen(x uint32) uint32 {
	return x & 0xff00
}

func trueblue(x uint32) uint32 {
	return (x & 0xff) << 8
}

func istruecol(x uint32) bool {
	return (1<<24)&x != 0
}

func gattrcmp(a, b *Glyph) bool {
	return a.mode != b.mode || a.fg != b.fg || a.bg != b.bg
}

func clipcopy(interface{}) {
	xsel.clipboard = nil
	if xsel.primary != nil {
		xsel.clipboard = append([]byte{}, xsel.primary...)
		clipboard := xlib.InternAtom(xw.dpy, "CLIPBOARD", false)
		xlib.SetSelectionOwner(xw.dpy, clipboard, xw.win, xlib.CurrentTime)
	}
}

func clippaste(interface{}) {
	clipboard := xlib.InternAtom(xw.dpy, "CLIPBOARD", false)
	xlib.ConvertSelection(xw.dpy, clipboard, xsel.xtarget, clipboard,
		xw.win, xlib.CurrentTime)
}

func selpaste(interface{}) {
	xlib.ConvertSelection(xw.dpy, xlib.XA_PRIMARY, xsel.xtarget, xlib.XA_PRIMARY,
		xw.win, xlib.CurrentTime)
}

func numlock(interface{}) {
	win.mode ^= MODE_NUMLOCK
}

func zoom(arg interface{}) {
	size := arg.(float64)
	zoomabs(usedfontsize + size)
}

func zoomabs(size float64) {
	xunloadfonts()
	xloadfonts(usedfont, size)
	cresize(0, 0)
	redraw()
	xhints()
}

func zoomreset(interface{}) {
	if defaultfontsize > 0 {
		zoomabs(defaultfontsize)
	}
}

func evcol(ev *xlib.Event) int {
	e := ev.Button()
	x := e.X() - borderpx
	x = ga.Clamp(x, 0, win.tw-1)
	return x / win.cw
}

func evrow(ev *xlib.Event) int {
	e := ev.Button()
	y := e.Y() - borderpx
	y = ga.Clamp(y, 0, win.th-1)
	return y / win.ch
}

func mousesel(ev *xlib.Event, done bool) {
	e := ev.Button()
	seltype := SEL_REGULAR
	state := e.State() &^ (xlib.Button1Mask | forceselmod)
	for typ := 1; typ < len(selmasks); typ++ {
		if match(selmasks[typ], state) {
			seltype = typ
			break
		}
	}
	selextend(evcol(ev), evrow(ev), seltype, done)
	if done {
		setsel(getsel(), e.Time())
	}
}

func mousereport(ev *xlib.Event) {
	x := evcol(ev)
	y := evrow(ev)
	e := ev.Button()
	button := e.Button()
	state := e.State()

	// from urxvt
	if e.Type() == xlib.MotionNotify {
		if x == xw.mrpox && y == xw.mrpoy {
			return
		}
		if win.mode&MODE_MOUSEMOTION == 0 && win.mode&MODE_MOUSEMANY == 0 {
			return
		}
		// MOUSE_MOTION: no reporting if no button is pressed
		if win.mode&MODE_MOUSEMOTION != 0 && oldbutton == 3 {
			return
		}

		button = oldbutton + 32
		xw.mrpox = x
		xw.mrpoy = y
	} else {
		if win.mode&MODE_MOUSESGR == 0 && e.Type() == xlib.ButtonRelease {
			button = 3
		} else {
			button -= xlib.Button1
			if button >= 3 {
				button += 64 - 3
			}
		}
		if e.Type() == xlib.ButtonPress {
			oldbutton = button
			xw.mrpox = x
			xw.mrpoy = y
		} else if e.Type() == xlib.ButtonRelease {
			oldbutton = 3
			// MODE_MOUSEX10: no button release reporting
			if win.mode&MODE_MOUSEX10 != 0 {
				return
			}
			if button == 64 || button == 65 {
				return
			}
		}
	}

	if win.mode&MODE_MOUSEX10 == 0 {
		if state&xlib.ShiftMask != 0 {
			button += 4
		}
		if state&xlib.Mod4Mask != 0 {
			button += 8
		}
		if state&xlib.ControlMask != 0 {
			button += 16
		}
	}

	var str string
	if win.mode&MODE_MOUSESGR != 0 {
		m := 'M'
		if e.Type() == xlib.ButtonRelease {
			m = 'm'
		}
		str = fmt.Sprintf("\033[<%d;%d;%d%c", button, x+1, y+1, m)
	} else if x < 223 && y < 223 {
		str = fmt.Sprintf("\033[M%c%c%c", 32+button, 32+x+1, 32+y+1)
	} else {
		return
	}

	ttywrite([]byte(str), false)
}

func bpress(ev *xlib.Event) {
	e := ev.Button()
	if win.mode&MODE_MOUSE != 0 && e.State()&forceselmod == 0 {
		mousereport(ev)
		return
	}

	for _, ms := range mshortcuts {
		if e.Button() == ms.b && match(ms.mask, e.State()) {
			ttywrite([]byte(ms.s), true)
			return
		}
	}

	if e.Button() == xlib.Button1 {
		// If the user clicks below predefined timeouts specific
		// snapping behaviour is exposed.
		now := time.Now()
		snap := 0
		if now.Sub(xsel.tclick2) <= tripleclicktimeout {
			snap = SNAP_LINE
		} else if now.Sub(xsel.tclick1) <= doubleclicktimeout {
			snap = SNAP_WORD
		}
		xsel.tclick2 = xsel.tclick1
		xsel.tclick1 = now

		selstart(evcol(ev), evrow(ev), snap)
	}
}

func propnotify(ev *xlib.Event) {
	clipboard := xlib.InternAtom(xw.dpy, "CLIPBOARD", false)

	xpev := ev.Property()
	if xpev.State() == xlib.PropertyNewValue && (xpev.Atom() == xlib.XA_PRIMARY || xpev.Atom() == clipboard) {
		selnotify(ev)
	}
}

func selnotify(ev *xlib.Event) {
	incratom := xlib.InternAtom(xw.dpy, "INCR", false)

	property := xlib.Atom(xlib.None)
	switch ev.Type() {
	case xlib.SelectionNotify:
		e := ev.Selection()
		property = e.Property()
	case xlib.PropertyNotify:
		e := ev.Property()
		property = e.Atom()
	}

	if property == xlib.None {
		return
	}

	ofs := 0
	for {
		typ, format, nitems, rem, data, err := xlib.GetWindowProperty(xw.dpy, xw.win, property, ofs, 8192/4, false, xlib.AnyPropertyType)
		if err != nil {
			fmt.Fprintln(os.Stderr, "Clipboard allocation failed")
			return
		}

		if ev.Type() == xlib.PropertyNotify && nitems == 0 && rem == 0 {
			// If there is some PropertyNotify with no data, then
			// this is the signal of the selection owner that all
			// data has been transferred. We won't need to receive
			// PropertyNotify events anymore.
			xw.attrs.SetEventMask(xw.attrs.EventMask() &^ xlib.PropertyChangeMask)
			xlib.ChangeWindowAttributes(xw.dpy, xw.win, xlib.CWEventMask, &xw.attrs)
		}

		if typ == incratom {
			// Activate the PropertyNotify events so we receive
			// when the selection owner does send us the next
			// chunk of data.
			xw.attrs.SetEventMask(xw.attrs.EventMask() | xlib.PropertyChangeMask)
			xlib.ChangeWindowAttributes(xw.dpy, xw.win, xlib.CWEventMask, &xw.attrs)

			// Deleting the property is the transfer start signal.
			xlib.DeleteProperty(xw.dpy, xw.win, property)
			continue
		}

		// As seen in getsel:
		// Line endings are inconsistent in the terminal and GUI world
		// copy and pasting. When receiving some selection data,
		// replace all '\n' with '\r'.
		// FIXME: Fix the computer world.
		for repl := 0; repl < nitems*format/8; repl++ {
			if data[repl] == '\n' {
				data[repl] = '\r'
			}
		}

		if win.mode&MODE_BRCKTPASTE != 0 && ofs == 0 {
			ttywrite([]byte("\033[200~"), false)
		}
		ttywrite(data[:nitems*format/8], true)
		if win.mode&MODE_BRCKTPASTE != 0 && rem == 0 {
			ttywrite([]byte("\033[201~"), false)
		}

		// number of 32-bit chunks returned
		ofs += nitems * format / 32

		if rem <= 0 {
			break
		}
	}

	// Deleting the property again tells the selection owner to send the
	// next data chunk in the property.
	xlib.DeleteProperty(xw.dpy, xw.win, property)
}

func xclipcopy() {
	clipcopy(nil)
}

func selclear_(*xlib.Event) {
	selclear()
}

func selrequest(ev *xlib.Event) {
	xsre := ev.SelectionRequest()

	var xev xlib.SelectionEvent
	xev.SetType(xlib.SelectionNotify)
	xev.SetRequestor(xsre.Requestor())
	xev.SetSelection(xsre.Selection())
	xev.SetTarget(xsre.Target())
	xev.SetTime(xsre.Time())
	if xsre.Property() == xlib.None {
		xsre.SetProperty(xsre.Target())
	}

	// reject
	xev.SetProperty(xlib.None)

	xa_targets := xlib.InternAtom(xw.dpy, "TARGETS", false)
	if xsre.Target() == xa_targets {
		// respond with the supported type
		string_ := xsel.xtarget
		xlib.ChangeProperty(xsre.Display(), xsre.Requestor(), xsre.Property(),
			xlib.XA_ATOM, 32, xlib.PropModeReplace, string_)
		xev.SetProperty(xsre.Property())
	} else if xsre.Target() == xsel.xtarget || xsre.Target() == xlib.XA_STRING {
		// xith XA_STRING non ascii characters may be incorrect in the
		// requestor. It is not our problem, use utf8.
		var seltext []byte
		clipboard := xlib.InternAtom(xw.dpy, "CLIPBOARD", false)
		if xsre.Selection() == xlib.XA_PRIMARY {
			seltext = xsel.primary
		} else if xsre.Selection() == clipboard {
			seltext = xsel.clipboard
		} else {
			fmt.Fprintf(os.Stderr, "Unhandled clipboard selection 0x%lx\n", xsre.Selection())
			return
		}

		if seltext != nil {
			xlib.ChangeProperty(xsre.Display(), xsre.Requestor(),
				xsre.Property(), xsre.Target(),
				8, xlib.PropModeReplace, seltext)
			xev.SetProperty(xsre.Property())
		}
	}

	// all done, send a notification to the listener
	err := xlib.SendEvent(xsre.Display(), xsre.Requestor(), true, 0, xev.Cast())
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error sending SelectionNotify event: %v", err)
	}
}

func setsel(str []byte, t xlib.Time) {
	if str == nil {
		return
	}

	xsel.primary = str
	xlib.SetSelectionOwner(xw.dpy, xlib.XA_PRIMARY, xw.win, t)
	if xlib.GetSelectionOwner(xw.dpy, xlib.XA_PRIMARY) != xw.win {
		selclear()
	}
}

func xsetsel(str []byte) {
	setsel(str, xlib.CurrentTime)
}

func brelease(ev *xlib.Event) {
	e := ev.Button()
	if win.mode&MODE_MOUSE != 0 && e.State()&forceselmod == 0 {
		mousereport(ev)
		return
	}

	button := e.Button()
	if button == xlib.Button2 {
		selpaste(nil)
	} else if button == xlib.Button1 {
		mousesel(ev, true)
	}
}

func bmotion(ev *xlib.Event) {
	e := ev.Button()
	if win.mode&MODE_MOUSE != 0 && e.State()&forceselmod == 0 {
		mousereport(ev)
		return
	}

	mousesel(ev, false)
}

func cresize(width, height int) {
	if width != 0 {
		win.w = width
	}
	if height != 0 {
		win.h = height
	}

	col := (win.w - 2*borderpx) / win.cw
	row := (win.h - 2*borderpx) / win.ch
	col = max(1, col)
	row = max(1, row)

	tresize(col, row)
	xresize(col, row)
	ttyresize(win.tw, win.th)
}

func xresize(col, row int) {
	win.tw = col * win.cw
	win.th = row * win.ch

	xlib.FreePixmap(xw.dpy, xw.buf)
	xw.buf = xlib.Drawable(xlib.CreatePixmap(xw.dpy, xlib.Drawable(xw.win), win.w, win.h,
		xlib.DefaultDepth(xw.dpy, xw.scr)))
	xft.DrawChange(xw.draw, xw.buf)
	xclear(0, 0, win.w, win.h)

	// resize to new width
	xw.specbuf = append(make([]GlyphFontSpec, col), xw.specbuf...)
}

func sixd_to_16bit(x int) uint16 {
	if x == 0 {
		return 0
	}
	return uint16(0x3737 + 0x2828*x)
}

func xloadcolor(i int, name string, ncolor *Color) bool {
	var color xrender.Color
	color.SetAlpha(0xffff)

	if name == "" {
		// 256 color
		if 16 <= i && i <= 255 {
			if i < 6*6*6+16 {
				// same colors as xterm
				color.SetRed(sixd_to_16bit(((i - 16) / 36) % 6))
				color.SetGreen(sixd_to_16bit(((i - 16) / 6) % 6))
				color.SetBlue(sixd_to_16bit(((i - 16) / 1) % 6))
			} else {
				// greyscale
				value := 0x0808 + 0x0a0a*(uint16(i)-(6*6*6+16))
				color.SetRed(value)
				color.SetGreen(value)
				color.SetBlue(value)
			}
			return xft.ColorAllocValue(xw.dpy, xw.vis, xw.cmap, &color, ncolor)
		} else {
			name = colorname[i]
		}
	}

	return xft.ColorAllocName(xw.dpy, xw.vis, xw.cmap, name, ncolor)
}

func xloadcols() {
	for i := range dc.col {
		xft.ColorFree(xw.dpy, xw.vis, xw.cmap, &dc.col[i])
	}
	dc.col = make([]Color, max(len(colorname), 256))

	for i := range dc.col {
		if !xloadcolor(i, "", &dc.col[i]) {
			if colorname[i] != "" {
				log.Fatalf("could not allocate color '%s'", colorname[i])
			} else {
				log.Fatal("could not allocate color", i)
			}
		}
	}
}

func xsetcolorname(x int, name string) bool {
	var ncolor Color

	if !(0 <= x && x <= len(dc.col)) {
		return true
	}

	if !xloadcolor(x, name, &ncolor) {
		return true
	}

	xft.ColorFree(xw.dpy, xw.vis, xw.cmap, &dc.col[x])
	dc.col[x] = ncolor

	return false
}

// Absolute coordinates.
func xclear(x1, y1, x2, y2 int) {
	col := defaultbg
	if win.mode&MODE_REVERSE != 0 {
		col = defaultfg
	}
	xft.DrawRect(xw.draw, &dc.col[col], x1, y1, x2-x1, y2-y1)
}

func xhints() {
	var class xlib.ClassHint
	resname := opt.name
	if resname == "" {
		resname = termname
	}
	resclass := opt.class
	if resclass == "" {
		resclass = termname
	}
	class.SetResName(resname)
	class.SetResClass(resclass)

	var wm xlib.WMHints
	wm.SetFlags(xlib.InputHint)
	wm.SetInput(1)

	sizeh := xlib.AllocSizeHints()
	sizeh.SetFlags(xlib.PSize | xlib.PResizeInc | xlib.PBaseSize | xlib.PMinSize)
	sizeh.SetHeight(win.h)
	sizeh.SetWidth(win.w)
	sizeh.SetHeightInc(win.ch)
	sizeh.SetWidthInc(win.cw)
	sizeh.SetBaseHeight(2 * borderpx)
	sizeh.SetBaseWidth(2 * borderpx)
	sizeh.SetMinHeight(win.ch + 2*borderpx)
	sizeh.SetMinWidth(win.cw + 2*borderpx)
	if xw.isfixed {
		sizeh.SetFlags(sizeh.Flags() | xlib.PMaxSize)
		sizeh.SetMinWidth(win.w)
		sizeh.SetMaxWidth(win.w)
		sizeh.SetMinHeight(win.h)
		sizeh.SetMaxHeight(win.h)
	}
	if xw.gm&(xlib.XValue|xlib.YValue) != 0 {
		sizeh.SetFlags(sizeh.Flags() | xlib.USPosition | xlib.PWinGravity)
		sizeh.SetX(xw.l)
		sizeh.SetY(xw.t)
		sizeh.SetWinGravity(xgeommasktogravity(xw.gm))
	}
	xlib.SetWMProperties(xw.dpy, xw.win, nil, nil, nil, sizeh, &wm, &class)
	sizeh.Free()
}

func xgeommasktogravity(mask int) int {
	switch mask & (xlib.XNegative | xlib.YNegative) {
	case 0:
		return xlib.NorthWestGravity
	case xlib.XNegative:
		return xlib.NorthEastGravity
	case xlib.YNegative:
		return xlib.SouthWestGravity
	}
	return xlib.SouthEastGravity
}

func xunloadfont(f *Font) {
	xft.FontClose(xw.dpy, f.match)
	fc.PatternDestroy(f.pattern)
	if f.set != nil {
		fc.FontSetDestroy(f.set)
	}
}

func xloadfont(f *Font, pattern *fc.Pattern) bool {
	// Manually configure instead of calling XftMatchFont
	// so that we can use the configured pattern for
	// "missing glyph" lookups.
	configured := fc.PatternDuplicate(pattern)
	if configured == nil {
		return true
	}

	fc.ConfigSubstitute(nil, configured, fc.MatchPattern)
	xft.DefaultSubstitute(xw.dpy, xw.scr, (*xft.Pattern)(configured))

	match, _ := fc.FontMatch(nil, configured)
	if match == nil {
		fc.PatternDestroy(configured)
		return true
	}

	f.match = xft.FontOpenPattern(xw.dpy, (*xft.Pattern)(match))
	if f.match == nil {
		fc.PatternDestroy(configured)
		fc.PatternDestroy(match)
		return true
	}

	result, wantattr := xft.PatternGetInteger((*xft.Pattern)(pattern), "slant", 0)
	if result == xft.ResultMatch {
		// Check if xft was unable to find a font with the appropriate
		// slant but gave us one anyway. Try to mitigate.
		result, haveattr := xft.PatternGetInteger(f.match.Pattern(), "slant", 0)
		if result != xft.ResultMatch || haveattr < wantattr {
			f.badslant = true
			fmt.Fprintln(os.Stderr, "font slant does not match")
		}
	}

	result, wantattr = xft.PatternGetInteger((*xft.Pattern)(pattern), "weight", 0)
	if result == xft.ResultMatch {
		result, haveattr := xft.PatternGetInteger(f.match.Pattern(), "weight", 0)
		if result != xft.ResultMatch || haveattr != wantattr {
			f.badweight = false
			fmt.Fprintln(os.Stderr, "font weight does not match")
		}
	}

	var extents xft.GlyphInfo
	xft.TextExtentsUtf8(xw.dpy, f.match, ascii_printable, &extents)

	f.set = nil
	f.pattern = configured

	f.ascent = f.match.Ascent()
	f.descent = f.match.Descent()
	f.lbearing = 0
	f.rbearing = f.match.MaxAdvanceWidth()

	f.height = f.ascent + f.descent
	f.width = divceil(extents.XOff(), len(ascii_printable))

	return false
}

func xloadfonts(fontstr string, fontsize float64) {
	var pattern *fc.Pattern
	if strings.HasPrefix(fontstr, "-") {
		pattern = (*fc.Pattern)(xft.XlfdParse(fontstr, false, false))
	} else {
		pattern = fc.NameParse(fontstr)
	}

	if pattern == nil {
		log.Fatal("can't open font ", fontstr)
	}

	if fontsize > 1 {
		fc.PatternDel(pattern, fc.PIXEL_SIZE)
		fc.PatternDel(pattern, fc.SIZE)
		fc.PatternAddDouble(pattern, fc.PIXEL_SIZE, fontsize)
		usedfontsize = fontsize
	} else {
		if match, fontval := fc.PatternGetDouble(pattern, fc.PIXEL_SIZE, 0); match == fc.ResultMatch {
			usedfontsize = fontval
		} else if match, _ := fc.PatternGetDouble(pattern, fc.SIZE, 0); match == fc.ResultMatch {
			usedfontsize = -1
		} else {
			// Default font size is 12, if none given. This is to
			// have a known usedfontsize value.
			fc.PatternAddDouble(pattern, fc.PIXEL_SIZE, 12)
			usedfontsize = 12
		}
		defaultfontsize = usedfontsize
	}
	if xloadfont(&dc.font, pattern) {
		log.Fatal("can't open font ", fontstr)
	}

	if usedfontsize < 0 {
		_, fontval := fc.PatternGetDouble((*fc.Pattern)(dc.font.match.Pattern()), fc.PIXEL_SIZE, 0)
		usedfontsize = fontval
		if fontsize == 0 {
			defaultfontsize = fontval
		}
	}

	// Setting character width and height.
	win.cw = int(math.Ceil(float64(dc.font.width) * cwscale))
	win.ch = int(math.Ceil(float64(dc.font.height) * chscale))

	fc.PatternDel(pattern, fc.SLANT)
	fc.PatternAddInteger(pattern, fc.SLANT, fc.SLANT_ITALIC)
	if xloadfont(&dc.ifont, pattern) {
		log.Fatal("can't open font ", fontstr)
	}

	fc.PatternDel(pattern, fc.WEIGHT)
	fc.PatternAddInteger(pattern, fc.WEIGHT, fc.WEIGHT_BOLD)
	if xloadfont(&dc.ibfont, pattern) {
		log.Fatal("can't open font ", fontstr)
	}

	fc.PatternDel(pattern, fc.SLANT)
	fc.PatternAddInteger(pattern, fc.SLANT, fc.SLANT_ROMAN)
	if xloadfont(&dc.bfont, pattern) {
		log.Fatal("can't open font ", fontstr)
	}

	fc.PatternDestroy(pattern)
}

func xunloadfonts() {
	// Free the loaded fonts in the font cache.
	for i := range frc {
		xft.FontClose(xw.dpy, frc[i].font)
	}

	xunloadfont(&dc.font)
	xunloadfont(&dc.bfont)
	xunloadfont(&dc.ifont)
	xunloadfont(&dc.ibfont)
}

func ximopen(dpy *xlib.Display) {
	xw.xim = xlib.OpenIM(dpy, nil, "", "")
	if xw.xim == nil {
		xlib.SetLocaleModifiers("@im=local")
		xw.xim = xlib.OpenIM(dpy, nil, "", "")
	}
	if xw.xim == nil {
		xlib.SetLocaleModifiers("@im=")
		xw.xim = xlib.OpenIM(dpy, nil, "", "")
	}
	if xw.xim == nil {
		log.Fatal("XOpenIM failed. Could not open input device.")
	}

	result := xlib.SetIMValues(xw.xim, []xlib.IMValue{
		{xlib.NDestroyCallback, ximdestroy},
	})
	if result != "" {
		log.Fatal("XSetIMValues failed. Could not set input method value.")
	}

	xw.xic = xlib.CreateIC(xw.xim, []xlib.IMValue{
		{xlib.NInputStyle, xlib.IMPreeditNothing | xlib.IMStatusNothing},
		{xlib.NClientWindow, xw.win},
		{xlib.NFocusWindow, xw.win},
	})
	if xw.xic == nil {
		log.Fatal("XCreateIC failed. Could not obtain input method.")
	}
}

func ximinstantiate(dpy *xlib.Display, call xlib.Pointer) {
	ximopen(dpy)
	xlib.UnregisterIMInstantiateCallback(xw.dpy, nil, "", "",
		"ximinstantiate", ximinstantiate)
}

func ximdestroy(xim xlib.IM, call xlib.Pointer) {
	xw.xim = nil
	xlib.RegisterIMInstantiateCallback(xw.dpy, nil, "", "",
		"ximinstantiate", ximinstantiate)
}

func xinit(cols, rows int) {
	xw.dpy = xlib.OpenDisplay("")
	if xw.dpy == nil {
		log.Fatal("can't open display")
	}
	xw.scr = xlib.DefaultScreen(xw.dpy)
	xw.vis = xlib.DefaultVisual(xw.dpy, xw.scr)

	// font
	err := fc.Init()
	if err != nil {
		log.Fatal("could not init fontconfig")
	}

	usedfont = opt.font
	if usedfont == "" {
		usedfont = font
	}
	xloadfonts(usedfont, 0)

	// colors
	xw.cmap = xlib.DefaultColormap(xw.dpy, xw.scr)
	xloadcols()

	// adjust fixed window geometry
	win.w = 2*borderpx + cols*win.cw
	win.h = 2*borderpx + rows*win.ch
	if xw.gm&xlib.XNegative != 0 {
		xw.l += xlib.DisplayWidth(xw.dpy, xw.scr) - win.w - 2
	}
	if xw.gm&xlib.YNegative != 0 {
		xw.t += xlib.DisplayHeight(xw.dpy, xw.scr) - win.h - 2
	}

	// events
	xw.attrs.SetBackgroundPixel(dc.col[defaultbg].Pixel())
	xw.attrs.SetBorderPixel(dc.col[defaultbg].Pixel())
	xw.attrs.SetBitGravity(xlib.NorthWestGravity)
	xw.attrs.SetEventMask(xlib.FocusChangeMask | xlib.KeyPressMask | xlib.KeyReleaseMask |
		xlib.ExposureMask | xlib.VisibilityChangeMask | xlib.StructureNotifyMask |
		xlib.ButtonMotionMask | xlib.ButtonPressMask | xlib.ButtonReleaseMask)
	xw.attrs.SetColormap(xw.cmap)

	parent := xlib.RootWindow(xw.dpy, xw.scr)
	embed, err := strconv.ParseInt(opt.embed, 0, 64)
	if err == nil {
		parent = xlib.Window(embed)
	}

	xw.win = xlib.CreateWindow(xw.dpy, parent, xw.l, xw.t,
		win.w, win.h, 0, xlib.DefaultDepth(xw.dpy, xw.scr), xlib.InputOutput,
		xw.vis, xlib.CWBackPixel|xlib.CWBorderPixel|xlib.CWBitGravity|
			xlib.CWEventMask|xlib.CWColormap, &xw.attrs)

	var gcvalues xlib.GCValues
	dc.gc = xlib.CreateGC(xw.dpy, xlib.Drawable(parent), xlib.GCGraphicsExposures, &gcvalues)
	xw.buf = xlib.Drawable(xlib.CreatePixmap(xw.dpy, xlib.Drawable(xw.win), win.w, win.h,
		xlib.DefaultDepth(xw.dpy, xw.scr)))
	xlib.SetForeground(xw.dpy, dc.gc, dc.col[defaultbg].Pixel())
	xlib.FillRectangle(xw.dpy, xw.buf, dc.gc, 0, 0, win.w, win.h)

	// font spec buffer
	xw.specbuf = make([]GlyphFontSpec, cols)

	// Xft rendering context
	xw.draw = xft.DrawCreate(xw.dpy, xw.buf, xw.vis, xw.cmap)

	// input methods
	ximopen(xw.dpy)

	// white cursor, black outline
	cursor := xlib.CreateFontCursor(xw.dpy, mouseshape)
	xlib.DefineCursor(xw.dpy, xw.win, cursor)

	var xmousefg, xmousebg xlib.Color
	err = xlib.ParseColor(xw.dpy, xw.cmap, colorname[mousefg], &xmousefg)
	if err == nil {
		xmousefg.SetRed(0xffff)
		xmousefg.SetGreen(0xffff)
		xmousefg.SetBlue(0xffff)
	}

	err = xlib.ParseColor(xw.dpy, xw.cmap, colorname[mousebg], &xmousebg)
	if err == nil {
		xmousebg.SetRed(0x0000)
		xmousebg.SetGreen(0x0000)
		xmousebg.SetBlue(0x0000)
	}
	xlib.RecolorCursor(xw.dpy, cursor, &xmousefg, &xmousebg)

	xw.xembed = xlib.InternAtom(xw.dpy, "_XEMBED", false)
	xw.wmdeletewin = xlib.InternAtom(xw.dpy, "WM_DELETE_WINDOW", false)
	xw.netwmname = xlib.InternAtom(xw.dpy, "_NET_WM_NAME", false)
	xlib.SetWMProtocols(xw.dpy, xw.win, []xlib.Atom{xw.wmdeletewin})

	thispid := os.Getpid()
	xw.netwmpid = xlib.InternAtom(xw.dpy, "_NET_WM_PID", false)
	xlib.ChangeProperty(xw.dpy, xw.win, xw.netwmpid, xlib.XA_CARDINAL, 32,
		xlib.PropModeReplace, thispid)

	win.mode = MODE_NUMLOCK
	resettitle()
	xlib.MapWindow(xw.dpy, xw.win)
	xhints()
	xlib.Sync(xw.dpy, false)

	xsel.tclick1 = time.Now()
	xsel.tclick2 = time.Now()
	xsel.primary = nil
	xsel.clipboard = nil
	xsel.xtarget = xlib.InternAtom(xw.dpy, "UTF8_STRING", false)
	if xsel.xtarget == xlib.None {
		xsel.xtarget = xlib.XA_STRING
	}

	xw.qev = make([]xlib.Event, 0, 256)
}

func xmakeglyphfontspecs(specs []xft.GlyphFontSpec, glyphs []Glyph, len_, x, y int) int {
	font := &dc.font
	prevmode := uint(math.MaxUint16)
	frcflags := FRC_NORMAL
	runewidth := float64(win.cw)

	winx := float64(borderpx + x*win.cw)
	winy := float64(borderpx + y*win.ch)

	xp := float64(winx)
	yp := winy + float64(font.ascent)
	numspecs := 0
	for i := 0; i < len_; i++ {
		// Fetch rune and mode for current glyph.
		rune := glyphs[i].u
		mode := glyphs[i].mode

		// Skip dummy wide-character spacing.
		if mode == ATTR_WDUMMY {
			continue
		}

		// Determine font for glyph if different from previous glyph.
		if prevmode != mode {
			prevmode = mode
			font = &dc.font
			frcflags = FRC_NORMAL
			runewidth := float64(win.cw)
			if mode&ATTR_WIDE != 0 {
				runewidth *= 2
			}

			if (mode&ATTR_ITALIC) != 0 && (mode&ATTR_BOLD) != 0 {
				font = &dc.ibfont
				frcflags = FRC_ITALICBOLD
			} else if mode&ATTR_ITALIC != 0 {
				font = &dc.ifont
				frcflags = FRC_ITALIC
			} else if mode&ATTR_BOLD != 0 {
				font = &dc.bfont
				frcflags = FRC_BOLD
			}
			yp = winy + float64(font.ascent)
		}

		// Lookup character index with default font.
		glyphidx := xft.CharIndex(xw.dpy, font.match, rune)
		if glyphidx != 0 {
			specs[numspecs].SetFont(font.match)
			specs[numspecs].SetGlyph(glyphidx)
			specs[numspecs].SetX(int(xp))
			specs[numspecs].SetY(int(yp))
			xp += runewidth
			numspecs++
			continue
		}

		// Fallback on font cache, search the font cache for match.
		f := 0
		for ; f < len(frc); f++ {
			glyphidx = xft.CharIndex(xw.dpy, frc[f].font, rune)
			// Everything correct.
			if glyphidx != 0 && frc[f].flags == frcflags {
				break
			}
			// We got a default font for a not found glyph.
			if glyphidx == 0 && frc[f].flags == frcflags && frc[f].unicodep == rune {
				break
			}
		}

		// Nothing was found. Use fontconfig to find matching font.
		if f >= len(frc) {
			fcsets := make([]*fc.FontSet, 1)
			if font.set == nil {
				font.set, _, _ = fc.FontSort(nil, font.pattern, true)
			}
			fcsets[0] = font.set

			// Nothing was found in the cache. Now use
			// some dozen of Fontconfig calls to get the
			// font for one single character.
			// Xft and fontconfig are design failures.
			fcpattern := fc.PatternDuplicate(font.pattern)
			fccharset := fc.CharSetCreate()

			fc.CharSetAddChar(fccharset, rune)
			fc.PatternAddCharSet(fcpattern, fc.CHARSET, fccharset)
			fc.PatternAddBool(fcpattern, fc.SCALABLE, true)

			fc.ConfigSubstitute(nil, fcpattern, fc.MatchPattern)
			fc.DefaultSubstitute(fcpattern)

			fontpattern, _ := fc.FontSetMatch(nil, fcsets, fcpattern)
			frcfont := xft.FontOpenPattern(xw.dpy, (*xft.Pattern)(fontpattern))
			if frcfont == nil {
				log.Fatal("XftFontOpenPattern failed seeking fallback font")
			}
			frc = append(frc, Fontcache{
				font:     frcfont,
				flags:    frcflags,
				unicodep: rune,
			})

			glyphidx = xft.CharIndex(xw.dpy, frcfont, rune)
			f = len(frc) - 1

			fc.PatternDestroy(fcpattern)
			fc.CharSetDestroy(fccharset)
		}

		specs[numspecs].SetFont(frc[f].font)
		specs[numspecs].SetGlyph(glyphidx)
		specs[numspecs].SetX(int(xp))
		specs[numspecs].SetY(int(yp))
		xp += runewidth
		numspecs++
	}
	return numspecs
}

func xdrawglyphfontspecs(specs []xft.GlyphFontSpec, base Glyph, len_, x, y int) {
	charlen := len_
	if base.mode&ATTR_WIDE != 0 {
		charlen *= 2
	}
	winx := borderpx + x*win.cw
	winy := borderpx + y*win.ch
	width := charlen * win.cw

	// Fallback on color display for attributes not supported by the font
	if base.mode&ATTR_ITALIC != 0 && base.mode&ATTR_BOLD != 0 {
		if dc.ibfont.badslant || dc.ibfont.badweight {
			base.fg = defaultattr
		}
	} else if (base.mode&ATTR_ITALIC != 0 && dc.ifont.badslant) || (base.mode&ATTR_BOLD != 0 && dc.bfont.badweight) {
		base.fg = defaultattr
	}

	var fg, bg *Color
	var revfg, revbg, truefg, truebg Color
	var colfg, colbg xrender.Color
	if istruecol(base.fg) {
		colfg.SetAlpha(0xffff)
		colfg.SetRed(uint16(truered(base.fg)))
		colfg.SetGreen(uint16(truegreen(base.fg)))
		colfg.SetBlue(uint16(trueblue(base.fg)))
		xft.ColorAllocValue(xw.dpy, xw.vis, xw.cmap, &colfg, &truefg)
		fg = &truefg
	} else {
		fg = &dc.col[base.fg]
	}

	if istruecol(base.bg) {
		colbg.SetAlpha(0xffff)
		colbg.SetRed(uint16(truered(base.bg)))
		colbg.SetGreen(uint16(truegreen(base.bg)))
		colbg.SetBlue(uint16(trueblue(base.bg)))
		xft.ColorAllocValue(xw.dpy, xw.vis, xw.cmap, &colfg, &truebg)
		bg = &truebg
	} else {
		bg = &dc.col[base.bg]
	}

	// Change basic system colors [0-7] to bright system colors [8-15]
	if base.mode&ATTR_BOLD_FAINT == ATTR_BOLD && (0 <= base.fg && base.fg <= 7) {
		fg = &dc.col[base.fg+8]
	}

	if win.mode&MODE_REVERSE != 0 {
		if fg == &dc.col[defaultfg] {
			fg = &dc.col[defaultbg]
		} else {
			fgcolor := fg.Color()
			colfg.SetRed(^fgcolor.Red())
			colfg.SetGreen(^fgcolor.Green())
			colfg.SetBlue(^fgcolor.Blue())
			colfg.SetAlpha(fgcolor.Alpha())
			xft.ColorAllocValue(xw.dpy, xw.vis, xw.cmap, &colfg, &revfg)
			fg = &revfg
		}

		if bg == &dc.col[defaultbg] {
			bg = &dc.col[defaultfg]
		} else {
			bgcolor := bg.Color()
			colbg.SetRed(^bgcolor.Red())
			colbg.SetGreen(^bgcolor.Green())
			colbg.SetBlue(^bgcolor.Blue())
			colbg.SetAlpha(bgcolor.Alpha())
			xft.ColorAllocValue(xw.dpy, xw.vis, xw.cmap, &colbg, &revbg)
			bg = &revbg
		}
	}

	if (base.mode & ATTR_BOLD_FAINT) == ATTR_FAINT {
		fgcolor := fg.Color()
		colfg.SetRed(fgcolor.Red() / 2)
		colfg.SetGreen(fgcolor.Green() / 2)
		colfg.SetBlue(fgcolor.Blue() / 2)
		colfg.SetAlpha(fgcolor.Alpha())
		xft.ColorAllocValue(xw.dpy, xw.vis, xw.cmap, &colfg, &revfg)
		fg = &revfg
	}

	if base.mode&ATTR_REVERSE != 0 {
		fg, bg = bg, fg
	}

	if base.mode&ATTR_BLINK != 0 && win.mode&MODE_BLINK != 0 {
		fg = bg
	}

	if base.mode&ATTR_INVISIBLE != 0 {
		fg = bg
	}

	// Intelligent cleaning up of the borders
	if x == 0 {
		wy := 0
		wh := 0
		if y != 0 {
			wy = winy
		}
		if winy+win.ch >= borderpx+win.th {
			wh = win.h
		}
		xclear(0, wy, borderpx, winy+win.ch+wh)
	}

	if winx+width >= borderpx+win.tw {
		wy := 0
		wh := winy + win.ch
		if y != 0 {
			wy = winy
		}
		if winy+win.ch >= borderpx+win.th {
			wh = win.h
		}
		xclear(winx+width, wy, win.w, wh)
	}

	if y == 0 {
		xclear(winx, 0, winx+width, borderpx)
	}
	if winy+win.ch >= borderpx+win.th {
		xclear(winx, winy+win.ch, winx+width, win.h)
	}
	// Clean up the region we want to draw to.
	xft.DrawRect(xw.draw, bg, winx, winy, width, win.ch)

	// Set the clip region because Xft is sometimes dirty
	var r xlib.Rectangle
	r.SetX(0)
	r.SetY(0)
	r.SetHeight(win.ch)
	r.SetWidth(width)
	xft.DrawSetClipRectangles(xw.draw, winx, winy, []xlib.Rectangle{r})

	// Render the glyphs.
	xft.DrawGlyphFontSpec(xw.draw, fg, specs[:len_])

	// Render underline and strikethrough.
	if base.mode&ATTR_UNDERLINE != 0 {
		xft.DrawRect(xw.draw, fg, winx, winy+dc.font.ascent+1, width, 1)
	}

	if base.mode&ATTR_STRUCK != 0 {
		xft.DrawRect(xw.draw, fg, winx, winy+2*dc.font.ascent/3, width, 1)
	}

	// Reset clip to none.
	xft.DrawSetClip(xw.draw, nil)
}

func xdrawglyph(g Glyph, x, y int) {
	spec := make([]xft.GlyphFontSpec, 1)
	glyph := []Glyph{g}
	numspecs := xmakeglyphfontspecs(spec, glyph, 1, x, y)
	xdrawglyphfontspecs(spec, g, numspecs, x, y)
}

func xdrawcursor(cx, cy int, g Glyph, ox, oy int, og Glyph) {
	// remove the old cursor
	if selected(ox, oy) {
		og.mode ^= ATTR_REVERSE
	}
	xdrawglyph(og, ox, oy)

	if win.mode&MODE_HIDE != 0 {
		return
	}

	// Select the right color for the right mode.
	g.mode &= ATTR_BOLD | ATTR_ITALIC | ATTR_UNDERLINE | ATTR_STRUCK | ATTR_WIDE

	var drawcol Color
	if win.mode&MODE_REVERSE != 0 {
		g.mode |= ATTR_REVERSE
		g.bg = defaultfg
		if selected(cx, cy) {
			drawcol = dc.col[defaultcs]
			g.fg = defaultrcs
		} else {
			drawcol = dc.col[defaultrcs]
			g.fg = defaultcs
		}
	} else {
		if selected(cx, cy) {
			g.fg = defaultfg
			g.bg = defaultrcs
		} else {
			g.fg = defaultbg
			g.bg = defaultcs
		}
		drawcol = dc.col[g.bg]
	}

	// draw the new one
	if win.mode&MODE_FOCUSED != 0 {
		switch win.cursor {
		case 7: // st extension: snowman (U+2603)
			g.u = 0x2603
			fallthrough
		case 0: // Blinking Block
			fallthrough
		case 1: // Blinking Block (Default)
			fallthrough
		case 2: // Steady Block
			xdrawglyph(g, cx, cy)
		case 3: // Blinking Underline
			fallthrough
		case 4: // Steady Underline
			xft.DrawRect(xw.draw, &drawcol,
				borderpx+cx*win.cw,
				borderpx+(cy+1)*win.ch-cursorthickness,
				win.cw, cursorthickness)
		case 5: // Blinking bar
			fallthrough
		case 6: // Steady bar
			xft.DrawRect(xw.draw, &drawcol,
				borderpx+cx*win.cw,
				borderpx+cy*win.ch,
				cursorthickness, win.ch)
		}
	} else {
		xft.DrawRect(xw.draw, &drawcol,
			borderpx+cx*win.cw,
			borderpx+cy*win.ch,
			win.cw-1, 1)
		xft.DrawRect(xw.draw, &drawcol,
			borderpx+cx*win.cw,
			borderpx+cy*win.ch,
			1, win.ch-1)
		xft.DrawRect(xw.draw, &drawcol,
			borderpx+(cx+1)*win.cw-1,
			borderpx+cy*win.ch,
			1, win.ch-1)
		xft.DrawRect(xw.draw, &drawcol,
			borderpx+cx*win.cw,
			borderpx+(cy+1)*win.ch-1,
			win.cw, 1)
	}
}

func xsetcursor(cursor int) bool {
	if cursor == 0 {
		cursor = 1
	}
	if !(0 <= cursor && cursor <= 6) {
		return true
	}
	win.cursor = cursor
	return false
}

func match(mask, state uint) bool {
	return mask == XK_ANY_MOD || mask == (state & ^ignoremod)
}

func kmap(k xlib.KeySym, state uint) []byte {
	// Check for mapped keys out of X11 function keys.
	var i int
	for i = range mappedkeys {
		if mappedkeys[i] == k {
			break
		}
	}
	if i == len(mappedkeys) {
		if k&0xFFFF < 0xFD00 {
			return nil
		}
	}

	for _, kp := range key {
		if kp.k != k {
			continue
		}

		if !match(kp.mask, state) {
			continue
		}

		if win.mode&MODE_APPKEYPAD != 0 {
			if kp.appkey < 0 {
				continue
			}
		} else {
			if kp.appkey > 0 {
				continue
			}
		}

		if win.mode&MODE_NUMLOCK != 0 && kp.appkey == 2 {
			continue
		}

		if win.mode&MODE_APPCURSOR != 0 {
			if kp.appcursor < 0 {
				continue
			}
		} else {
			if kp.appcursor > 0 {
				continue
			}
		}

		return []byte(kp.s)
	}

	return nil
}

func kpress(ev *xlib.Event) {
	if win.mode&MODE_KBDLOCK != 0 {
		return
	}

	e := ev.Key()
	str, ksym, _ := xlib.XmbLookupString(xw.xic, (*xlib.KeyPressedEvent)(e))
	// 1. shortcuts
	for _, bp := range shortcuts {
		if ksym == bp.keysym && match(bp.mod, e.State()) {
			bp.funct(bp.arg)
			return
		}
	}

	// 2. custom keys from config.h
	if customkey := kmap(ksym, e.State()); customkey != nil {
		ttywrite(customkey, true)
		return
	}

	// 3. composed string from input method
	if len(str) == 0 {
		return
	}

	buf := make([]byte, 32)
	copy(buf, str)
	if len(str) == 1 && e.State()&xlib.Mod1Mask != 0 {
		if win.mode&MODE_8BIT != 0 {
			if buf[0] < 0177 {
				c := buf[0] | 0x80
				n := utf8.EncodeRune(buf, rune(c))
				buf = buf[:n]
			} else {
				buf = buf[:1]
			}
		} else {
			buf[1] = buf[0]
			buf[0] = '\033'
			buf = buf[:2]
		}
	} else {
		buf = buf[:len(str)]
	}
	ttywrite(buf, true)
}

func xsetenv() {
	os.Setenv("WINDOWID", fmt.Sprintf("%d", xw.win))
}

func xsettitle(p []byte) {
	var prop xlib.TextProperty
	if p == nil {
		p = []byte(opt.title)
	}
	xlib.UTF8TextListToTextProperty(xw.dpy, []string{string(p)}, xlib.UTF8StringStyle, &prop)
	xlib.SetWMName(xw.dpy, xw.win, &prop)
	xlib.SetTextProperty(xw.dpy, xw.win, &prop, xw.netwmname)
	prop.Free()
}

func xstartdraw() bool {
	return win.mode&MODE_VISIBLE != 0
}

func xdrawline(line Line, x1, y1, x2 int) {
	specs := xw.specbuf

	numspecs := xmakeglyphfontspecs(specs, line[x1:], x2-x1, x1, y1)

	var base Glyph
	i, ox := 0, 0
	for x := x1; x < x2 && i < numspecs; x++ {
		new_ := line[x]
		if new_.mode&ATTR_WDUMMY != 0 {
			continue
		}
		if selected(x, y1) {
			new_.mode ^= ATTR_REVERSE
		}
		if i > 0 && gattrcmp(&base, &new_) {
			xdrawglyphfontspecs(specs, base, i, ox, y1)
			specs = specs[i:]
			numspecs -= i
			i = 0
		}
		if i == 0 {
			ox = x
			base = new_
		}
		i++
	}
	if i > 0 {
		xdrawglyphfontspecs(specs, base, i, ox, y1)
	}
}

func xfinishdraw() {
	xlib.CopyArea(xw.dpy, xw.buf, xlib.Drawable(xw.win), dc.gc, 0, 0, win.w, win.h, 0, 0)
	col := defaultfg
	if win.mode&MODE_REVERSE != 0 {
		col = defaultbg
	}
	xlib.SetForeground(xw.dpy, dc.gc, dc.col[col].Pixel())
}

func xximspot(x, y int) {
	var spot xlib.Point
	spot.SetX(borderpx + x*win.cw)
	spot.SetY(borderpx + (y+1)*win.ch)
	attr := xlib.VaCreateNestedList([]xlib.ICValue{
		{xlib.NSpotLocation, &spot},
	})

	xlib.SetICValues(xw.xic, []xlib.ICValue{
		{xlib.NPreeditAttributes, attr},
	})
}

func expose(*xlib.Event) {
	redraw()
}

func visibility(ev *xlib.Event) {
	e := ev.Visibility()
	win.mode &^= MODE_VISIBLE
	if e.State() != xlib.VisibilityFullyObscured {
		win.mode |= MODE_VISIBLE
	}
}

func unmap(*xlib.Event) {
	win.mode &^= MODE_VISIBLE
}

func xsetpointermotion(set bool) {
	mask := xw.attrs.EventMask() &^ xlib.PointerMotionMask
	if set {
		mask |= xlib.PointerMotionMask
	}
	xw.attrs.SetEventMask(mask)
	xlib.ChangeWindowAttributes(xw.dpy, xw.win, xlib.CWEventMask, &xw.attrs)
}

func xsetmode(set bool, flags int) {
	mode := win.mode
	win.mode &^= flags
	if set {
		win.mode |= flags
	}
	if (win.mode & MODE_REVERSE) != (mode & MODE_REVERSE) {
		redraw()
	}
}

func xseturgency(add bool) {
	h := xlib.GetWMHints(xw.dpy, xw.win)
	f := h.Flags() &^ xlib.UrgencyHint
	if add {
		f |= xlib.UrgencyHint
	}
	h.SetFlags(f)
	xlib.SetWMHints(xw.dpy, xw.win, h)
	h.Free()
}

func xbell() {
	if win.mode&MODE_FOCUSED == 0 {
		xseturgency(true)
	}
	if bellvolume != 0 {
		xkb.Bell(xw.dpy, xw.win, bellvolume, 0)
	}
}

func focus(ev *xlib.Event) {
	e := ev.Focus()

	if e.Mode() == xlib.NotifyGrab {
		return
	}

	if ev.Type() == xlib.FocusIn {
		xlib.SetICFocus(xw.xic)
		win.mode |= MODE_FOCUSED
		xseturgency(false)
		if win.mode&MODE_FOCUS != 0 {
			ttywrite([]byte("\033[I"), false)
		}
	} else {
		xlib.UnsetICFocus(xw.xic)
		win.mode &^= MODE_FOCUSED
		if win.mode&MODE_FOCUS != 0 {
			ttywrite([]byte("\033[O"), false)
		}
	}
}

func cmessage(ev *xlib.Event) {
	e := ev.Client()
	l := e.Long()
	if e.MessageType() == xw.xembed && e.Format() == 32 {
		if l[1] == XEMBED_FOCUS_IN {
			win.mode |= MODE_FOCUSED
			xseturgency(false)
		} else if l[1] == XEMBED_FOCUS_OUT {
			win.mode &^= MODE_FOCUSED
		}
	} else if xlib.Atom(l[0]) == xw.wmdeletewin {
		ttyhangup()
		os.Exit(0)
	}
}

func resize(ev *xlib.Event) {
	e := ev.Configure()
	if e.Width() == win.w && e.Height() == win.h {
		return
	}
	cresize(e.Width(), e.Height())
}

func xrun() {
	var ev xlib.Event
	for xlib.Pending(xw.dpy) > 0 {
		xlib.NextEvent(xw.dpy, &ev)
		if xlib.FilterEvent(&ev, xlib.None) {
			continue
		}
		xw.qev = append(xw.qev, ev)
	}
}

func run() {
	// Waiting for window mapping
	var ev xlib.Event
	w, h := win.w, win.h
loop:
	for {
		xlib.NextEvent(xw.dpy, &ev)
		if xlib.FilterEvent(&ev, xlib.None) {
			continue
		}

		// This XFilterEvent call is required because of XOpenIM. It
		// does filter out the key event and some client message for
		// the input method too.
		switch typ := ev.Type(); typ {
		case xlib.ConfigureNotify:
			c := ev.Configure()
			w = c.Width()
			h = c.Height()
		case xlib.MapNotify:
			break loop
		}
	}
	ttynew(opt.line, shell, opt.io, opt.cmd)
	cresize(w, h)

	blinkset := false
	last := time.Now()
	lastblink := last

	tv := time.NewTimer(1 * time.Second)
	xev := actionfps
	go trun()
	for {
		var fds int
		select {
		case <-term.rdy:
			fds = 1
		case <-tv.C:
		}

		xrun()
		switch fds {
		case 1:
			ttyread()
			term.rdy <- struct{}{}
			if blinktimeout != 0 {
				blinkset = tattrset(ATTR_BLINK)
				if !blinkset {
					win.mode &^= MODE_BLINK
				}
			}
		default:
			xev = actionfps
		}
		tv = time.NewTimer(1000 / xfps * time.Millisecond)

		now := time.Now()
		dodraw := false
		if blinktimeout != 0 && now.Sub(lastblink) > blinktimeout {
			tsetdirtattr(ATTR_BLINK)
			win.mode ^= MODE_BLINK
			lastblink = now
			dodraw = true
		}
		deltatime := now.Sub(last)
		fps := actionfps
		if xev != 0 {
			fps = xfps
		}
		if deltatime > 1000/fps {
			dodraw = true
			last = now
		}

		if dodraw {
			for _, ev := range xw.qev {
				if typ := ev.Type(); handler[typ] != nil {
					handler[typ](&ev)
				}
			}
			xw.qev = xw.qev[:0]

			draw()
			xlib.Flush(xw.dpy)

			if xev > 0 && fds == 2 {
				xev--
			}

			if fds == 0 {
				if blinkset {
					if now.Sub(lastblink) > blinktimeout {
						tv = time.NewTimer(1000 * time.Nanosecond)
					} else {
						tv = time.NewTimer((blinktimeout - now.Sub(lastblink)) * time.Nanosecond)
					}
				}
			}
		}
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: st [options] cmd ...")
	flag.PrintDefaults()
	os.Exit(2)
}

func main() {
	log.SetPrefix("")
	log.SetFlags(0)
	xw.l, xw.t = 0, 0
	xw.isfixed = false
	win.cursor = cursorshape
	flag.BoolVar(&allowaltscreen, "a", !allowaltscreen, "disable alt screen")
	flag.BoolVar(&xw.isfixed, "i", xw.isfixed, "fixed screen")
	flag.StringVar(&opt.line, "l", opt.line, "set line")
	flag.StringVar(&opt.name, "n", opt.name, "set name")
	flag.StringVar(&opt.title, "t", opt.title, "set title")
	flag.StringVar(&opt.embed, "w", opt.embed, "set embed")
	flag.BoolVar(&opt.version, "v", opt.version, "show version")
	flag.Usage = usage
	flag.Parse()
	if opt.version {
		log.Fatal(VERSION)
	}

	if opt.title == "" {
		if opt.line != "" || len(opt.cmd) == 0 {
			opt.title = "st"
		} else {
			opt.title = opt.cmd[0]
		}
	}
	opt.cmd = flag.Args()
	err := xlib.InitThreads()
	if err != nil {
		log.Fatal(err)
	}
	xlib.SetLocaleModifiers("")
	cols = max(cols, 1)
	rows = max(rows, 1)
	tnew(cols, rows)
	xinit(cols, rows)
	xsetenv()
	selinit()
	run()
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func clamp(x, a, b int) int {
	return min(max(x, a), b)
}

func divceil(n, d int) int {
	return (n + d - 1) / d
}
