//go:build !windows

package main

import (
	"log"
	"os"

	"github.com/mattn/go-colorable"
)

func Setup() {
	colorable.EnableColorsStdout(nil)
	log.SetOutput(colorable.NewColorableStderr())
}
func Exit(code int) {
	os.Exit(code)
}
