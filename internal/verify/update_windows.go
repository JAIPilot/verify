//go:build windows

package verify

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// Windows keeps a running executable locked. A separate PowerShell process
// replaces it after Verify exits; the next launch uses the new version.
func replaceExecutable(current, staged string) (bool, error) {
	script, err := os.CreateTemp(filepath.Dir(current), ".verify-update-*.ps1")
	if err != nil {
		return false, err
	}
	content := `param([string]$Current, [string]$Staged, [int]$ParentPid)
Wait-Process -Id $ParentPid -ErrorAction SilentlyContinue
for ($attempt = 0; $attempt -lt 60; $attempt++) {
  Start-Sleep -Milliseconds 500
  try {
    Move-Item -LiteralPath $Staged -Destination $Current -Force -ErrorAction Stop
    Remove-Item -LiteralPath $MyInvocation.MyCommand.Path -Force -ErrorAction SilentlyContinue
    exit 0
  } catch {}
}
Remove-Item -LiteralPath $Staged -Force -ErrorAction SilentlyContinue
Remove-Item -LiteralPath $MyInvocation.MyCommand.Path -Force -ErrorAction SilentlyContinue
exit 1
`
	if _, err = script.WriteString(content); err == nil {
		err = script.Close()
	} else {
		_ = script.Close()
	}
	if err != nil {
		_ = os.Remove(script.Name())
		return false, err
	}
	cmd := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-File", script.Name(), current, staged, fmt.Sprint(os.Getpid()))
	if err = cmd.Start(); err != nil {
		_ = os.Remove(script.Name())
		return false, err
	}
	_ = cmd.Process.Release()
	return true, nil
}
