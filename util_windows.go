//go:build windows

package main

import (
	"log"
	"os"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/mattn/go-colorable"
)

const (
	ENABLE_QUICK_EDIT_MODE = uint32(0x0040)
	ES_SYSTEM_REQUIRED     = uint32(0x00000001)
	ES_CONTINUOUS          = uint32(0x80000000)
)

var (
	kernel32                    = windows.NewLazyDLL("kernel32.dll")
	procGetConsoleMode          = kernel32.NewProc("GetConsoleMode")
	procSetConsoleMode          = kernel32.NewProc("SetConsoleMode")
	procSetThreadExecutionState = kernel32.NewProc("SetThreadExecutionState")
	previousMode                = uint32(0)
)

func Setup() {
	const threadState = ES_CONTINUOUS | ES_SYSTEM_REQUIRED
	procSetThreadExecutionState.Call(uintptr(threadState))
	disableQuickEditMode()
	colorable.EnableColorsStdout(nil)
	log.SetOutput(colorable.NewColorableStderr())
}
func Exit(code int) {
	procSetThreadExecutionState.Call(uintptr(ES_CONTINUOUS))
	resetConsoleMode()
	os.Exit(code)
}

func disableQuickEditMode() {
	h := os.Stdin.Fd()
	if r, _, _ := procGetConsoleMode.Call(h, uintptr(unsafe.Pointer(&previousMode))); r == 0 {
		previousMode = 0
		return
	}
	mode := previousMode & ^ENABLE_QUICK_EDIT_MODE

	procSetConsoleMode.Call(h, uintptr(mode))
}

func resetConsoleMode() {
	if previousMode == 0 {
		return
	}

	h := os.Stdin.Fd()
	procSetConsoleMode.Call(h, uintptr(previousMode))
}
