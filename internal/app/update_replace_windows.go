//go:build windows

package app

import (
	"fmt"
	"os"
	"os/exec"
)

const windowsUpdateScript = `param(
  [Parameter(Mandatory=$true)][string]$Source,
  [Parameter(Mandatory=$true)][string]$Target,
  [Parameter(Mandatory=$true)][string]$Script
)
for ($attempt = 0; $attempt -lt 50; $attempt++) {
  try {
    Move-Item -LiteralPath $Source -Destination $Target -Force -ErrorAction Stop
    Remove-Item -LiteralPath $Script -Force -ErrorAction SilentlyContinue
    exit 0
  } catch {
    Start-Sleep -Milliseconds 200
  }
}
exit 1
`

func replaceExecutable(source, target string) error {
	staged := target + ".update"
	script := target + ".update.ps1"
	_ = os.Remove(staged)
	_ = os.Remove(script)
	if err := os.Rename(source, staged); err != nil {
		return fmt.Errorf("stage verified update: %w", err)
	}
	if err := os.WriteFile(script, []byte(windowsUpdateScript), 0o600); err != nil {
		_ = os.Remove(staged)
		return fmt.Errorf("write replacement helper: %w", err)
	}
	cmd := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-File", script, "-Source", staged, "-Target", target, "-Script", script)
	if err := cmd.Start(); err != nil {
		_ = os.Remove(staged)
		_ = os.Remove(script)
		return fmt.Errorf("start replacement helper: %w", err)
	}
	return nil
}
