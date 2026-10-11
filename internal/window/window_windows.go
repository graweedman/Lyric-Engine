// Package window resizes the terminal window that hosts this process.
package window

import (
	"errors"
	"runtime"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	kernel32 = windows.NewLazySystemDLL("kernel32.dll")
	user32   = windows.NewLazySystemDLL("user32.dll")
	dwmapi   = windows.NewLazySystemDLL("dwmapi.dll")

	procGetConsoleWindow         = kernel32.NewProc("GetConsoleWindow")
	procGetWindow                = user32.NewProc("GetWindow")
	procIsWindowVisible          = user32.NewProc("IsWindowVisible")
	procIsZoomed                 = user32.NewProc("IsZoomed")
	procShowWindow               = user32.NewProc("ShowWindow")
	procGetWindowRect            = user32.NewProc("GetWindowRect")
	procSetWindowPos             = user32.NewProc("SetWindowPos")
	procGetWindowPlacement       = user32.NewProc("GetWindowPlacement")
	procSetWindowPlacement       = user32.NewProc("SetWindowPlacement")
	procMonitorFromWindow        = user32.NewProc("MonitorFromWindow")
	procGetMonitorInfoW          = user32.NewProc("GetMonitorInfoW")
	procSetThreadDpiAwarenessCtx = user32.NewProc("SetThreadDpiAwarenessContext")
	procDwmGetWindowAttribute    = dwmapi.NewProc("DwmGetWindowAttribute")
)

const (
	gwOwner                  = 4
	swRestore                = 9
	swpNoZOrder              = 0x0004
	swpNoActivate            = 0x0010
	monitorDefaultToNearest  = 2
	dwmwaExtendedFrameBounds = 9
	dpiPerMonitorAwareV2     = ^uintptr(3) // DPI_AWARENESS_CONTEXT_PER_MONITOR_AWARE_V2 (-4)
)

type rect struct{ Left, Top, Right, Bottom int32 }

type point struct{ X, Y int32 }

type windowPlacement struct {
	Length, Flags, ShowCmd   uint32
	MinPosition, MaxPosition point
	NormalPosition           rect
}

type monitorInfo struct {
	Size          uint32
	Monitor, Work rect
	Flags         uint32
}

// hostWindow finds the visible window of the terminal running us. In the
// classic console that is the console window itself; Windows Terminal
// gives console apps a hidden pseudo-window owned by the terminal window.
func hostWindow() uintptr {
	h, _, _ := procGetConsoleWindow.Call()
	if h == 0 {
		return 0
	}
	if v, _, _ := procIsWindowVisible.Call(h); v != 0 {
		return h
	}
	if owner, _, _ := procGetWindow.Call(h, gwOwner); owner != 0 {
		return owner
	}
	return 0
}

// Fit resizes the terminal window to the aspect ratio aw:ah, as tall as the
// monitor's work area (the screen minus the taskbar), docked "left",
// "right" or "center". restore puts the window back as it was.
func Fit(aw, ah int, dock string) (restore func(), err error) {
	if aw <= 0 || ah <= 0 {
		return nil, errors.New("invalid aspect ratio")
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	// Work in physical pixels so the size is exact on scaled displays.
	if procSetThreadDpiAwarenessCtx.Find() == nil {
		old, _, _ := procSetThreadDpiAwarenessCtx.Call(dpiPerMonitorAwareV2)
		if old != 0 {
			defer procSetThreadDpiAwarenessCtx.Call(old)
		}
	}

	hwnd := hostWindow()
	if hwnd == 0 {
		return nil, errors.New("no terminal window found")
	}

	saved := windowPlacement{Length: uint32(unsafe.Sizeof(windowPlacement{}))}
	if ok, _, e := procGetWindowPlacement.Call(hwnd, uintptr(unsafe.Pointer(&saved))); ok == 0 {
		return nil, e
	}
	if z, _, _ := procIsZoomed.Call(hwnd); z != 0 {
		procShowWindow.Call(hwnd, swRestore) // a maximized window can't be resized
	}

	mon, _, _ := procMonitorFromWindow.Call(hwnd, monitorDefaultToNearest)
	mi := monitorInfo{Size: uint32(unsafe.Sizeof(monitorInfo{}))}
	if ok, _, e := procGetMonitorInfoW.Call(mon, uintptr(unsafe.Pointer(&mi))); ok == 0 {
		return nil, e
	}
	work := mi.Work
	ww, wh := work.Right-work.Left, work.Bottom-work.Top
	h := wh
	w := h * int32(aw) / int32(ah)
	if w > ww {
		w = ww
		h = w * int32(ah) / int32(aw)
	}
	var x int32
	switch dock {
	case "left":
		x = work.Left
	case "center":
		x = work.Left + (ww-w)/2
	default:
		x = work.Right - w
	}
	y := work.Top

	if err := setPos(hwnd, x, y, w, h); err != nil {
		return nil, err
	}
	// Windows 10/11 windows have invisible resize borders; grow the window
	// by them so the visible frame lands exactly on the target rectangle.
	var outer, visible rect
	procGetWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&outer)))
	if r, _, _ := procDwmGetWindowAttribute.Call(hwnd, dwmwaExtendedFrameBounds,
		uintptr(unsafe.Pointer(&visible)), unsafe.Sizeof(visible)); r == 0 {
		l, t := visible.Left-outer.Left, visible.Top-outer.Top
		rr, b := outer.Right-visible.Right, outer.Bottom-visible.Bottom
		if l|t|rr|b != 0 {
			setPos(hwnd, x-l, y-t, w+l+rr, h+t+b)
		}
	}

	return func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		// Same DPI context as when the placement was saved.
		if procSetThreadDpiAwarenessCtx.Find() == nil {
			if old, _, _ := procSetThreadDpiAwarenessCtx.Call(dpiPerMonitorAwareV2); old != 0 {
				defer procSetThreadDpiAwarenessCtx.Call(old)
			}
		}
		procSetWindowPlacement.Call(hwnd, uintptr(unsafe.Pointer(&saved)))
	}, nil
}

func setPos(hwnd uintptr, x, y, w, h int32) error {
	ok, _, e := procSetWindowPos.Call(hwnd, 0, uintptr(x), uintptr(y), uintptr(w), uintptr(h), swpNoZOrder|swpNoActivate)
	if ok == 0 {
		return e
	}
	return nil
}
