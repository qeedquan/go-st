package main

import (
	"time"

	"github.com/qeedquan/go-media/x11/xlib"
	"github.com/qeedquan/go-media/x11/xlib/xc"
	"github.com/qeedquan/go-media/x11/xlib/xk"
)

const VERSION = "0.8.2"

// appearance
// font: see http://freedesktop.org/software/fontconfig/fontconfig-user.html
var font = "Liberation Mono:pixelsize=12:antialias=true:autohint=true"
var borderpx = 2

// What program is execed by st depends of these precedence rules:
// 1: program passed with -e
// 2: utmp option
// 3: SHELL environment variable
// 4: value of shell in /etc/passwd
// 5: value of shell in config.h
const shell = "/bin/sh"
const utmp = ""
const stty_args = "stty raw pass8 nl -echo -iexten -cstopb 38400"

// identification sequence returned in DA and DECID
var vtiden = []byte("\033[?6c")

// Kerning / character bounding-box multipliers
var cwscale = 1.0
var chscale = 1.0

// word delimiter string
// More advanced example: " `'\"()[]{}"
var worddelimiters = []rune(" ")

// selection timeouts (in milliseconds)
var doubleclicktimeout = 300 * time.Millisecond
var tripleclicktimeout = 600 * time.Millisecond

// alt screens
var allowaltscreen = true

// frames per second st should at maximum draw to the screen
var xfps time.Duration = 120
var actionfps time.Duration = 30

// blinking timeout (set to 0 to disable blinking) for the terminal blinking
// attribute.
var blinktimeout = 800 * time.Millisecond

// thickness of underline and bar cursors
var cursorthickness = 2

// bell volume. It must be a value between -100 and 100. Use 0 for disabling it
var bellvolume = 0

// default TERM value
var termname = "xterm-256color"

// spaces per tab
//
// When you are changing this value, don't forget to adapt the »it« value in
// the st.info and appropriately install the st.info in the environment where
// you use this st version.
//
//	it#$tabspaces,
//
// Secondly make sure your kernel is not expanding tabs. When running `stty
// -a` »tab0« should appear. You can tell the terminal to not expand tabs by
//  running following command:
//
//	stty tabs
var tabspaces = 8

// Terminal colors (16 first used in escape sequence)
var colorname = []string{
	// 8 normal colors
	"black",
	"red3",
	"green3",
	"yellow3",
	"blue2",
	"magenta3",
	"cyan3",
	"gray90",

	// 8 bright colors
	"gray50",
	"red",
	"green",
	"yellow",
	"#5c5cff",
	"magenta",
	"cyan",
	"white",

	// more colors can be added after 255 to use with DefaultXX
	256: "#cccccc",
	257: "#555555",
}

// Default colors (colorname index)
// foreground, background, cursor, reverse cursor
var defaultfg uint32 = 7
var defaultbg uint32 = 0
var defaultcs uint32 = 256
var defaultrcs uint32 = 257

// Default shape of cursor
// 2: Block ("█")
// 4: Underline ("_")
// 6: Bar ("|")
// 7: Snowman ("☃")
var cursorshape = 2

// Default columns and rows numbers
var cols = 80
var rows = 24

// Default colour and shape of the mouse cursor
var mouseshape = uint(xc.Xterm)
var mousefg = 7
var mousebg = 0

// Color used to display font attributes when fontconfig selected a font which
// doesn't match the ones requested.
var defaultattr uint32 = 11

// Internal mouse shortcuts.
// Beware that overloading Button1 will disable the selection.
var mshortcuts = []MouseShortcut{
	// button               mask            string
	{xlib.Button4, XK_ANY_MOD, "\031"},
	{xlib.Button5, XK_ANY_MOD, "\005"},
}

const (
	MODKEY  = xlib.Mod1Mask
	TERMMOD = (xlib.ControlMask | xlib.ShiftMask)
)

var shortcuts = []Shortcut{
	/* mask                 keysym          function        argument */
	{XK_ANY_MOD, xk.Break, sendbreak, 0},
	{xlib.ControlMask, xk.Print, toggleprinter, 0},
	{xlib.ShiftMask, xk.Print, printscreen, 0},
	{XK_ANY_MOD, xk.Print, printsel, 0},
	{TERMMOD, xk.Prior, zoom, +1.0},
	{TERMMOD, xk.Next, zoom, -1.0},
	{TERMMOD, xk.Home, zoomreset, 0.0},
	{TERMMOD, xk.C, clipcopy, 0},
	{TERMMOD, xk.V, clippaste, 0},
	{TERMMOD, xk.Y, selpaste, 0},
	{xlib.ShiftMask, xk.Insert, selpaste, 0},
	{TERMMOD, xk.Num_Lock, numlock, 0},
}

// State bits to ignore when matching key or button events.  By default,
// numlock (Mod2Mask) and keyboard layout (XK_SWITCH_MOD) are ignored.
var ignoremod uint = xlib.Mod2Mask | XK_SWITCH_MOD

// Override mouse-select while mask is active (when MODE_MOUSE is set).
// Note that if you want to use ShiftMask with selmasks, set this to an other
// modifier, set to 0 to not use it.
var forceselmod uint = xlib.ShiftMask

// Selection types' masks.
// Use the same masks as usual.
// Button1Mask is always unset, to make masks match between ButtonPress.
// ButtonRelease and MotionNotify.
// If no match is found, regular selection is used.
var selmasks = []uint{
	SEL_RECTANGULAR: xlib.Mod1Mask,
}

// If you want keys other than the X11 function keys (0xFD00 - 0xFFFF)
// to be mapped below, add them to this array.
var mappedkeys = []xlib.KeySym{}

// This is the huge key array which defines all compatibility to the Linux
// world. Please decide about changes wisely.
var key = []Key{
	// keysym           mask            string      appkey appcursor
	{xk.KP_Home, xlib.ShiftMask, "\033[2J", 0, -1},
	{xk.KP_Home, xlib.ShiftMask, "\033[1;2H", 0, +1},
	{xk.KP_Home, XK_ANY_MOD, "\033[H", 0, -1},
	{xk.KP_Home, XK_ANY_MOD, "\033[1~", 0, +1},
	{xk.KP_Up, XK_ANY_MOD, "\033Ox", +1, 0},
	{xk.KP_Up, XK_ANY_MOD, "\033[A", 0, -1},
	{xk.KP_Up, XK_ANY_MOD, "\033OA", 0, +1},
	{xk.KP_Down, XK_ANY_MOD, "\033Or", +1, 0},
	{xk.KP_Down, XK_ANY_MOD, "\033[B", 0, -1},
	{xk.KP_Down, XK_ANY_MOD, "\033OB", 0, +1},
	{xk.KP_Left, XK_ANY_MOD, "\033Ot", +1, 0},
	{xk.KP_Left, XK_ANY_MOD, "\033[D", 0, -1},
	{xk.KP_Left, XK_ANY_MOD, "\033OD", 0, +1},
	{xk.KP_Right, XK_ANY_MOD, "\033Ov", +1, 0},
	{xk.KP_Right, XK_ANY_MOD, "\033[C", 0, -1},
	{xk.KP_Right, XK_ANY_MOD, "\033OC", 0, +1},
	{xk.KP_Prior, xlib.ShiftMask, "\033[5;2~", 0, 0},
	{xk.KP_Prior, XK_ANY_MOD, "\033[5~", 0, 0},
	{xk.KP_Begin, XK_ANY_MOD, "\033[E", 0, 0},
	{xk.KP_End, xlib.ControlMask, "\033[J", -1, 0},
	{xk.KP_End, xlib.ControlMask, "\033[1;5F", +1, 0},
	{xk.KP_End, xlib.ShiftMask, "\033[K", -1, 0},
	{xk.KP_End, xlib.ShiftMask, "\033[1;2F", +1, 0},
	{xk.KP_End, XK_ANY_MOD, "\033[4~", 0, 0},
	{xk.KP_Next, xlib.ShiftMask, "\033[6;2~", 0, 0},
	{xk.KP_Next, XK_ANY_MOD, "\033[6~", 0, 0},
	{xk.KP_Insert, xlib.ShiftMask, "\033[2;2~", +1, 0},
	{xk.KP_Insert, xlib.ShiftMask, "\033[4l", -1, 0},
	{xk.KP_Insert, xlib.ControlMask, "\033[L", -1, 0},
	{xk.KP_Insert, xlib.ControlMask, "\033[2;5~", +1, 0},
	{xk.KP_Insert, XK_ANY_MOD, "\033[4h", -1, 0},
	{xk.KP_Insert, XK_ANY_MOD, "\033[2~", +1, 0},
	{xk.KP_Delete, xlib.ControlMask, "\033[M", -1, 0},
	{xk.KP_Delete, xlib.ControlMask, "\033[3;5~", +1, 0},
	{xk.KP_Delete, xlib.ShiftMask, "\033[2K", -1, 0},
	{xk.KP_Delete, xlib.ShiftMask, "\033[3;2~", +1, 0},
	{xk.KP_Delete, XK_ANY_MOD, "\033[P", -1, 0},
	{xk.KP_Delete, XK_ANY_MOD, "\033[3~", +1, 0},
	{xk.KP_Multiply, XK_ANY_MOD, "\033Oj", +2, 0},
	{xk.KP_Add, XK_ANY_MOD, "\033Ok", +2, 0},
	{xk.KP_Enter, XK_ANY_MOD, "\033OM", +2, 0},
	{xk.KP_Enter, XK_ANY_MOD, "\r", -1, 0},
	{xk.KP_Subtract, XK_ANY_MOD, "\033Om", +2, 0},
	{xk.KP_Decimal, XK_ANY_MOD, "\033On", +2, 0},
	{xk.KP_Divide, XK_ANY_MOD, "\033Oo", +2, 0},
	{xk.KP_0, XK_ANY_MOD, "\033Op", +2, 0},
	{xk.KP_1, XK_ANY_MOD, "\033Oq", +2, 0},
	{xk.KP_2, XK_ANY_MOD, "\033Or", +2, 0},
	{xk.KP_3, XK_ANY_MOD, "\033Os", +2, 0},
	{xk.KP_4, XK_ANY_MOD, "\033Ot", +2, 0},
	{xk.KP_5, XK_ANY_MOD, "\033Ou", +2, 0},
	{xk.KP_6, XK_ANY_MOD, "\033Ov", +2, 0},
	{xk.KP_7, XK_ANY_MOD, "\033Ow", +2, 0},
	{xk.KP_8, XK_ANY_MOD, "\033Ox", +2, 0},
	{xk.KP_9, XK_ANY_MOD, "\033Oy", +2, 0},
	{xk.Up, xlib.ShiftMask, "\033[1;2A", 0, 0},
	{xk.Up, xlib.Mod1Mask, "\033[1;3A", 0, 0},
	{xk.Up, xlib.ShiftMask | xlib.Mod1Mask, "\033[1;4A", 0, 0},
	{xk.Up, xlib.ControlMask, "\033[1;5A", 0, 0},
	{xk.Up, xlib.ShiftMask | xlib.ControlMask, "\033[1;6A", 0, 0},
	{xk.Up, xlib.ControlMask | xlib.Mod1Mask, "\033[1;7A", 0, 0},
	{xk.Up, xlib.ShiftMask | xlib.ControlMask | xlib.Mod1Mask, "\033[1;8A", 0, 0},
	{xk.Up, XK_ANY_MOD, "\033[A", 0, -1},
	{xk.Up, XK_ANY_MOD, "\033OA", 0, +1},
	{xk.Down, xlib.ShiftMask, "\033[1;2B", 0, 0},
	{xk.Down, xlib.Mod1Mask, "\033[1;3B", 0, 0},
	{xk.Down, xlib.ShiftMask | xlib.Mod1Mask, "\033[1;4B", 0, 0},
	{xk.Down, xlib.ControlMask, "\033[1;5B", 0, 0},
	{xk.Down, xlib.ShiftMask | xlib.ControlMask, "\033[1;6B", 0, 0},
	{xk.Down, xlib.ControlMask | xlib.Mod1Mask, "\033[1;7B", 0, 0},
	{xk.Down, xlib.ShiftMask | xlib.ControlMask | xlib.Mod1Mask, "\033[1;8B", 0, 0},
	{xk.Down, XK_ANY_MOD, "\033[B", 0, -1},
	{xk.Down, XK_ANY_MOD, "\033OB", 0, +1},
	{xk.Left, xlib.ShiftMask, "\033[1;2D", 0, 0},
	{xk.Left, xlib.Mod1Mask, "\033[1;3D", 0, 0},
	{xk.Left, xlib.ShiftMask | xlib.Mod1Mask, "\033[1;4D", 0, 0},
	{xk.Left, xlib.ControlMask, "\033[1;5D", 0, 0},
	{xk.Left, xlib.ShiftMask | xlib.ControlMask, "\033[1;6D", 0, 0},
	{xk.Left, xlib.ControlMask | xlib.Mod1Mask, "\033[1;7D", 0, 0},
	{xk.Left, xlib.ShiftMask | xlib.ControlMask | xlib.Mod1Mask, "\033[1;8D", 0, 0},
	{xk.Left, XK_ANY_MOD, "\033[D", 0, -1},
	{xk.Left, XK_ANY_MOD, "\033OD", 0, +1},
	{xk.Right, xlib.ShiftMask, "\033[1;2C", 0, 0},
	{xk.Right, xlib.Mod1Mask, "\033[1;3C", 0, 0},
	{xk.Right, xlib.ShiftMask | xlib.Mod1Mask, "\033[1;4C", 0, 0},
	{xk.Right, xlib.ControlMask, "\033[1;5C", 0, 0},
	{xk.Right, xlib.ShiftMask | xlib.ControlMask, "\033[1;6C", 0, 0},
	{xk.Right, xlib.ControlMask | xlib.Mod1Mask, "\033[1;7C", 0, 0},
	{xk.Right, xlib.ShiftMask | xlib.ControlMask | xlib.Mod1Mask, "\033[1;8C", 0, 0},
	{xk.Right, XK_ANY_MOD, "\033[C", 0, -1},
	{xk.Right, XK_ANY_MOD, "\033OC", 0, +1},
	{xk.ISO_Left_Tab, xlib.ShiftMask, "\033[Z", 0, 0},
	{xk.Return, xlib.Mod1Mask, "\033\r", 0, 0},
	{xk.Return, XK_ANY_MOD, "\r", 0, 0},
	{xk.Insert, xlib.ShiftMask, "\033[4l", -1, 0},
	{xk.Insert, xlib.ShiftMask, "\033[2;2~", +1, 0},
	{xk.Insert, xlib.ControlMask, "\033[L", -1, 0},
	{xk.Insert, xlib.ControlMask, "\033[2;5~", +1, 0},
	{xk.Insert, XK_ANY_MOD, "\033[4h", -1, 0},
	{xk.Insert, XK_ANY_MOD, "\033[2~", +1, 0},
	{xk.Delete, xlib.ControlMask, "\033[M", -1, 0},
	{xk.Delete, xlib.ControlMask, "\033[3;5~", +1, 0},
	{xk.Delete, xlib.ShiftMask, "\033[2K", -1, 0},
	{xk.Delete, xlib.ShiftMask, "\033[3;2~", +1, 0},
	{xk.Delete, XK_ANY_MOD, "\033[P", -1, 0},
	{xk.Delete, XK_ANY_MOD, "\033[3~", +1, 0},
	{xk.BackSpace, XK_NO_MOD, "\177", 0, 0},
	{xk.BackSpace, xlib.Mod1Mask, "\033\177", 0, 0},
	{xk.Home, xlib.ShiftMask, "\033[2J", 0, -1},
	{xk.Home, xlib.ShiftMask, "\033[1;2H", 0, +1},
	{xk.Home, XK_ANY_MOD, "\033[H", 0, -1},
	{xk.Home, XK_ANY_MOD, "\033[1~", 0, +1},
	{xk.End, xlib.ControlMask, "\033[J", -1, 0},
	{xk.End, xlib.ControlMask, "\033[1;5F", +1, 0},
	{xk.End, xlib.ShiftMask, "\033[K", -1, 0},
	{xk.End, xlib.ShiftMask, "\033[1;2F", +1, 0},
	{xk.End, XK_ANY_MOD, "\033[4~", 0, 0},
	{xk.Prior, xlib.ControlMask, "\033[5;5~", 0, 0},
	{xk.Prior, xlib.ShiftMask, "\033[5;2~", 0, 0},
	{xk.Prior, XK_ANY_MOD, "\033[5~", 0, 0},
	{xk.Next, xlib.ControlMask, "\033[6;5~", 0, 0},
	{xk.Next, xlib.ShiftMask, "\033[6;2~", 0, 0},
	{xk.Next, XK_ANY_MOD, "\033[6~", 0, 0},
	{xk.F1, XK_NO_MOD, "\033OP", 0, 0},
	{xk.F1 /* F13 */, xlib.ShiftMask, "\033[1;2P", 0, 0},
	{xk.F1 /* F25 */, xlib.ControlMask, "\033[1;5P", 0, 0},
	{xk.F1 /* F37 */, xlib.Mod4Mask, "\033[1;6P", 0, 0},
	{xk.F1 /* F49 */, xlib.Mod1Mask, "\033[1;3P", 0, 0},
	{xk.F1 /* F61 */, xlib.Mod3Mask, "\033[1;4P", 0, 0},
	{xk.F2, XK_NO_MOD, "\033OQ", 0, 0},
	{xk.F2 /* F14 */, xlib.ShiftMask, "\033[1;2Q", 0, 0},
	{xk.F2 /* F26 */, xlib.ControlMask, "\033[1;5Q", 0, 0},
	{xk.F2 /* F38 */, xlib.Mod4Mask, "\033[1;6Q", 0, 0},
	{xk.F2 /* F50 */, xlib.Mod1Mask, "\033[1;3Q", 0, 0},
	{xk.F2 /* F62 */, xlib.Mod3Mask, "\033[1;4Q", 0, 0},
	{xk.F3, XK_NO_MOD, "\033OR", 0, 0},
	{xk.F3 /* F15 */, xlib.ShiftMask, "\033[1;2R", 0, 0},
	{xk.F3 /* F27 */, xlib.ControlMask, "\033[1;5R", 0, 0},
	{xk.F3 /* F39 */, xlib.Mod4Mask, "\033[1;6R", 0, 0},
	{xk.F3 /* F51 */, xlib.Mod1Mask, "\033[1;3R", 0, 0},
	{xk.F3 /* F63 */, xlib.Mod3Mask, "\033[1;4R", 0, 0},
	{xk.F4, XK_NO_MOD, "\033OS", 0, 0},
	{xk.F4 /* F16 */, xlib.ShiftMask, "\033[1;2S", 0, 0},
	{xk.F4 /* F28 */, xlib.ControlMask, "\033[1;5S", 0, 0},
	{xk.F4 /* F40 */, xlib.Mod4Mask, "\033[1;6S", 0, 0},
	{xk.F4 /* F52 */, xlib.Mod1Mask, "\033[1;3S", 0, 0},
	{xk.F5, XK_NO_MOD, "\033[15~", 0, 0},
	{xk.F5 /* F17 */, xlib.ShiftMask, "\033[15;2~", 0, 0},
	{xk.F5 /* F29 */, xlib.ControlMask, "\033[15;5~", 0, 0},
	{xk.F5 /* F41 */, xlib.Mod4Mask, "\033[15;6~", 0, 0},
	{xk.F5 /* F53 */, xlib.Mod1Mask, "\033[15;3~", 0, 0},
	{xk.F6, XK_NO_MOD, "\033[17~", 0, 0},
	{xk.F6 /* F18 */, xlib.ShiftMask, "\033[17;2~", 0, 0},
	{xk.F6 /* F30 */, xlib.ControlMask, "\033[17;5~", 0, 0},
	{xk.F6 /* F42 */, xlib.Mod4Mask, "\033[17;6~", 0, 0},
	{xk.F6 /* F54 */, xlib.Mod1Mask, "\033[17;3~", 0, 0},
	{xk.F7, XK_NO_MOD, "\033[18~", 0, 0},
	{xk.F7 /* F19 */, xlib.ShiftMask, "\033[18;2~", 0, 0},
	{xk.F7 /* F31 */, xlib.ControlMask, "\033[18;5~", 0, 0},
	{xk.F7 /* F43 */, xlib.Mod4Mask, "\033[18;6~", 0, 0},
	{xk.F7 /* F55 */, xlib.Mod1Mask, "\033[18;3~", 0, 0},
	{xk.F8, XK_NO_MOD, "\033[19~", 0, 0},
	{xk.F8 /* F20 */, xlib.ShiftMask, "\033[19;2~", 0, 0},
	{xk.F8 /* F32 */, xlib.ControlMask, "\033[19;5~", 0, 0},
	{xk.F8 /* F44 */, xlib.Mod4Mask, "\033[19;6~", 0, 0},
	{xk.F8 /* F56 */, xlib.Mod1Mask, "\033[19;3~", 0, 0},
	{xk.F9, XK_NO_MOD, "\033[20~", 0, 0},
	{xk.F9 /* F21 */, xlib.ShiftMask, "\033[20;2~", 0, 0},
	{xk.F9 /* F33 */, xlib.ControlMask, "\033[20;5~", 0, 0},
	{xk.F9 /* F45 */, xlib.Mod4Mask, "\033[20;6~", 0, 0},
	{xk.F9 /* F57 */, xlib.Mod1Mask, "\033[20;3~", 0, 0},
	{xk.F10, XK_NO_MOD, "\033[21~", 0, 0},
	{xk.F10 /* F22 */, xlib.ShiftMask, "\033[21;2~", 0, 0},
	{xk.F10 /* F34 */, xlib.ControlMask, "\033[21;5~", 0, 0},
	{xk.F10 /* F46 */, xlib.Mod4Mask, "\033[21;6~", 0, 0},
	{xk.F10 /* F58 */, xlib.Mod1Mask, "\033[21;3~", 0, 0},
	{xk.F11, XK_NO_MOD, "\033[23~", 0, 0},
	{xk.F11 /* F23 */, xlib.ShiftMask, "\033[23;2~", 0, 0},
	{xk.F11 /* F35 */, xlib.ControlMask, "\033[23;5~", 0, 0},
	{xk.F11 /* F47 */, xlib.Mod4Mask, "\033[23;6~", 0, 0},
	{xk.F11 /* F59 */, xlib.Mod1Mask, "\033[23;3~", 0, 0},
	{xk.F12, XK_NO_MOD, "\033[24~", 0, 0},
	{xk.F12 /* F24 */, xlib.ShiftMask, "\033[24;2~", 0, 0},
	{xk.F12 /* F36 */, xlib.ControlMask, "\033[24;5~", 0, 0},
	{xk.F12 /* F48 */, xlib.Mod4Mask, "\033[24;6~", 0, 0},
	{xk.F12 /* F60 */, xlib.Mod1Mask, "\033[24;3~", 0, 0},
	{xk.F13, XK_NO_MOD, "\033[1;2P", 0, 0},
	{xk.F14, XK_NO_MOD, "\033[1;2Q", 0, 0},
	{xk.F15, XK_NO_MOD, "\033[1;2R", 0, 0},
	{xk.F16, XK_NO_MOD, "\033[1;2S", 0, 0},
	{xk.F17, XK_NO_MOD, "\033[15;2~", 0, 0},
	{xk.F18, XK_NO_MOD, "\033[17;2~", 0, 0},
	{xk.F19, XK_NO_MOD, "\033[18;2~", 0, 0},
	{xk.F20, XK_NO_MOD, "\033[19;2~", 0, 0},
	{xk.F21, XK_NO_MOD, "\033[20;2~", 0, 0},
	{xk.F22, XK_NO_MOD, "\033[21;2~", 0, 0},
	{xk.F23, XK_NO_MOD, "\033[23;2~", 0, 0},
	{xk.F24, XK_NO_MOD, "\033[24;2~", 0, 0},
	{xk.F25, XK_NO_MOD, "\033[1;5P", 0, 0},
	{xk.F26, XK_NO_MOD, "\033[1;5Q", 0, 0},
	{xk.F27, XK_NO_MOD, "\033[1;5R", 0, 0},
	{xk.F28, XK_NO_MOD, "\033[1;5S", 0, 0},
	{xk.F29, XK_NO_MOD, "\033[15;5~", 0, 0},
	{xk.F30, XK_NO_MOD, "\033[17;5~", 0, 0},
	{xk.F31, XK_NO_MOD, "\033[18;5~", 0, 0},
	{xk.F32, XK_NO_MOD, "\033[19;5~", 0, 0},
	{xk.F33, XK_NO_MOD, "\033[20;5~", 0, 0},
	{xk.F34, XK_NO_MOD, "\033[21;5~", 0, 0},
	{xk.F35, XK_NO_MOD, "\033[23;5~", 0, 0},
}

// Printable characters in ASCII, used to estimate the advance width
// of single wide characters.
const ascii_printable = " !\"#$%&'()*+,-./0123456789:;<=>?" +
	"@ABCDEFGHIJKLMNOPQRSTUVWXYZ[\\]^_" +
	"`abcdefghijklmnopqrstuvwxyz{|}~"
