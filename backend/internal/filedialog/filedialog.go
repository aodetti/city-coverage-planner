// Package filedialog opens the operating system's native file-chooser dialog so
// non-technical users can pick a database file visually instead of typing a
// path. Each platform is driven through a small command-line helper that is
// present on a normal desktop install, so the binary stays pure Go (no cgo):
//
//	macOS   -> osascript (AppleScript "choose file" / "choose file name")
//	Windows -> PowerShell + System.Windows.Forms Open/Save dialogs
//	Linux   -> zenity, falling back to kdialog
//
// Pick returns the chosen absolute path, or an empty string if the user
// cancelled. ErrUnavailable is returned when no dialog helper can be found.
package filedialog

import (
	"errors"
	"os/exec"
	"runtime"
	"strings"
)

// ErrUnavailable means no native file dialog helper exists on this system.
var ErrUnavailable = errors.New("no native file dialog is available on this system")

// Mode selects the kind of dialog.
const (
	ModeOpen = "open" // choose an existing file
	ModeNew  = "new"  // choose a location/name for a new file
)

// Pick opens a native dialog and returns the selected absolute path (or "" if
// the user cancelled).
func Pick(dialogMode string) (string, error) {
	switch runtime.GOOS {
	case "darwin":
		return pickWithAppleScript(dialogMode)
	case "windows":
		return pickWithPowerShell(dialogMode)
	default:
		return pickWithLinuxDialog(dialogMode)
	}
}

// interpretDialogResult trims the helper's stdout and treats a non-zero exit
// (how most helpers signal "cancelled") as an empty selection rather than an
// error.
func interpretDialogResult(commandOutput []byte, commandErr error) (string, error) {
	if commandErr != nil {
		var exitErr *exec.ExitError
		if errors.As(commandErr, &exitErr) {
			return "", nil
		}
		return "", commandErr
	}
	return strings.TrimSpace(string(commandOutput)), nil
}

func pickWithAppleScript(dialogMode string) (string, error) {
	var appleScript string
	if dialogMode == ModeNew {
		appleScript = `try
	return POSIX path of (choose file name with prompt "Elegí dónde crear la base de datos" default name "data.json")
on error number -128
	return ""
end try`
	} else {
		appleScript = `try
	return POSIX path of (choose file with prompt "Elegí la base de datos")
on error number -128
	return ""
end try`
	}
	return interpretDialogResult(exec.Command("osascript", "-e", appleScript).Output())
}

func pickWithPowerShell(dialogMode string) (string, error) {
	// The Open/Save dialog is otherwise created behind the browser window, so
	// the user never sees it and PowerShell blocks forever waiting for input —
	// the button appears "stuck". To force it to the front we build an owner
	// Form that is TopMost and actually shown (Opacity 0 keeps it invisible; a
	// 1x1 size and no taskbar entry keep it out of the way). A TopMost window
	// that is really shown renders above the (non-topmost) browser window, and
	// the dialog parented to it inherits that placement.
	var dialogSetup string
	if dialogMode == ModeNew {
		dialogSetup = `$d = New-Object System.Windows.Forms.SaveFileDialog;` +
			`$d.Title = 'Elegí dónde crear la base de datos';` +
			`$d.Filter = 'Base de datos (*.json)|*.json|Todos los archivos (*.*)|*.*';` +
			`$d.FileName = 'data.json';`
	} else {
		dialogSetup = `$d = New-Object System.Windows.Forms.OpenFileDialog;` +
			`$d.Title = 'Elegí la base de datos';` +
			`$d.Filter = 'Base de datos (*.json)|*.json|Todos los archivos (*.*)|*.*';`
	}
	powerShellScript := `Add-Type -AssemblyName System.Windows.Forms;` +
		`Add-Type -AssemblyName System.Drawing;` +
		`$owner = New-Object System.Windows.Forms.Form;` +
		`$owner.TopMost = $true;` +
		`$owner.ShowInTaskbar = $false;` +
		`$owner.FormBorderStyle = 'None';` +
		`$owner.StartPosition = 'CenterScreen';` +
		`$owner.Size = New-Object System.Drawing.Size(1,1);` +
		`$owner.Opacity = 0;` +
		`$owner.Show();` +
		`$owner.Activate();` +
		`[System.Windows.Forms.Application]::DoEvents();` +
		dialogSetup +
		`if ($d.ShowDialog($owner) -eq [System.Windows.Forms.DialogResult]::OK) { [Console]::Out.Write($d.FileName) };` +
		`$owner.Close();$owner.Dispose()`

	// Prefer Windows PowerShell, then PowerShell 7 (pwsh) as a fallback.
	for _, powerShellExecutable := range []string{"powershell", "pwsh"} {
		powerShellPath, lookupErr := exec.LookPath(powerShellExecutable)
		if lookupErr != nil {
			continue
		}
		return interpretDialogResult(exec.Command(powerShellPath,
			"-NoProfile", "-ExecutionPolicy", "Bypass", "-STA", "-Command", powerShellScript).Output())
	}
	return "", ErrUnavailable
}

func pickWithLinuxDialog(dialogMode string) (string, error) {
	if zenityPath, zenityLookupErr := exec.LookPath("zenity"); zenityLookupErr == nil {
		zenityArgs := []string{"--file-selection"}
		if dialogMode == ModeNew {
			zenityArgs = append(zenityArgs, "--save", "--confirm-overwrite",
				"--title=Elegí dónde crear la base de datos", "--filename=data.json")
		} else {
			zenityArgs = append(zenityArgs, "--title=Elegí la base de datos")
		}
		return interpretDialogResult(exec.Command(zenityPath, zenityArgs...).Output())
	}
	if kdialogPath, kdialogLookupErr := exec.LookPath("kdialog"); kdialogLookupErr == nil {
		var kdialogArgs []string
		if dialogMode == ModeNew {
			kdialogArgs = []string{"--getsavefilename", ".", "*.json"}
		} else {
			kdialogArgs = []string{"--getopenfilename", ".", "*.json"}
		}
		return interpretDialogResult(exec.Command(kdialogPath, kdialogArgs...).Output())
	}
	return "", ErrUnavailable
}
