//go:build windows

package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"unsafe"

	webview2 "github.com/jchv/go-webview2"
	"golang.org/x/sys/windows"

	"nova/launcher/internal/settings"
)

const createNoWindow = 0x08000000

var (
	user32         = windows.NewLazySystemDLL("user32.dll")
	procShowWindow = user32.NewProc("ShowWindow")
	procMessageBox = user32.NewProc("MessageBoxW")
	kernel32       = windows.NewLazySystemDLL("kernel32.dll")
	procMemStatus  = kernel32.NewProc("GlobalMemoryStatusEx")
)

func hideConsole(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow}
}

func totalMemoryMB() int {
	var st struct {
		Length               uint32
		MemoryLoad           uint32
		TotalPhys            uint64
		AvailPhys            uint64
		TotalPageFile        uint64
		AvailPageFile        uint64
		TotalVirtual         uint64
		AvailVirtual         uint64
		AvailExtendedVirtual uint64
	}
	st.Length = uint32(unsafe.Sizeof(st))
	if r, _, _ := procMemStatus.Call(uintptr(unsafe.Pointer(&st))); r == 0 {
		return 8192
	}
	return int(st.TotalPhys / (1024 * 1024))
}

func messageBox(title, text string) {
	t, _ := windows.UTF16PtrFromString(title)
	m, _ := windows.UTF16PtrFromString(text)
	procMessageBox.Call(0, uintptr(unsafe.Pointer(m)), uintptr(unsafe.Pointer(t)), 0x10)
}

func shellOpen(target string) error {
	verb, _ := windows.UTF16PtrFromString("open")
	file, err := windows.UTF16PtrFromString(target)
	if err != nil {
		return err
	}
	return windows.ShellExecute(0, verb, file, nil, nil, windows.SW_SHOWNORMAL)
}

func main() {
	exe, _ := os.Executable()
	exeDir := filepath.Dir(exe)
	dataDir := settings.DataDir()
	app := NewApp(dataDir, exeDir)
	defer app.Shutdown()

	w := webview2.NewWithOptions(webview2.WebViewOptions{
		Debug:     os.Getenv("NOVA_DEBUG") == "1",
		AutoFocus: true,
		DataPath:  filepath.Join(dataDir, "webview"),
		WindowOptions: webview2.WindowOptions{
			Title:  appName,
			Width:  1100,
			Height: 700,
			Center: true,
		},
	})
	if w == nil {
		messageBox(appName, "Не удалось открыть окно: нужен Microsoft Edge WebView2 Runtime.\n"+
			"Установите его с https://go.microsoft.com/fwlink/p/?LinkId=2124703 и запустите лаунчер снова.")
		return
	}
	defer w.Destroy()
	w.SetSize(960, 600, webview2.HintMin)

	hwnd := uintptr(w.Window())
	app.Minimize = func() { w.Dispatch(func() { procShowWindow.Call(hwnd, 6) }) } // SW_MINIMIZE
	app.Restore = func() { w.Dispatch(func() { procShowWindow.Call(hwnd, 9) }) }  // SW_RESTORE
	app.OpenPath = shellOpen

	// All calls are asynchronous: the result comes back through window.__nativeDone.
	_ = w.Bind("nativeCall", func(id int, method string, args json.RawMessage) {
		go func() {
			res, err := app.Call(method, args)
			payload := map[string]any{"ok": err == nil, "result": res}
			if err != nil {
				payload["error"] = err.Error()
			}
			b, _ := json.Marshal(payload)
			w.Dispatch(func() { w.Eval("window.__nativeDone(" + strconv.Itoa(id) + "," + string(b) + ")") })
		}()
	})
	w.SetHtml(indexHTML)
	w.Run()
}
