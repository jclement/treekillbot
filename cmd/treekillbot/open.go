// Opening a finished document in the platform's default viewer.
package main

import (
	"os/exec"
	"runtime"

	"github.com/jclement/treekillbot/internal/ui"
)

// openURL asks the desktop to open a URL. It shares openFile's plumbing because
// every platform's opener takes either.
func openURL(console *ui.Console, url string) { openFile(console, url) }

// openFile asks the desktop to open a path.
//
// A failure here is reported and then ignored: the PDF was written, which is
// what was asked for, and a headless machine with no opener is not a build
// failure.
func openFile(console *ui.Console, path string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", path)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", path)
	default:
		cmd = exec.Command("xdg-open", path)
	}
	if err := cmd.Start(); err != nil {
		console.Warn("could not open " + path + ": " + err.Error())
		return
	}
	// Release the child rather than waiting: the viewer outlives us.
	_ = cmd.Process.Release()
}
