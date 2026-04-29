//go:build windows

package install

import (
	"fmt"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows/registry"
)

// envKeyPath is the per-user PATH location. Writing here does not
// require admin privileges and matches what the standard "Environment
// Variables" dialog edits.
const envKeyPath = `Environment`

// addToPath appends dir to the user's persistent PATH if it isn't
// already there. Returns (added, rcPath, err); rcPath is always empty
// on Windows.
func addToPath(dir string) (bool, string, error) {
	k, err := registry.OpenKey(registry.CURRENT_USER, envKeyPath, registry.QUERY_VALUE|registry.SET_VALUE)
	if err != nil {
		return false, "", fmt.Errorf("open HKCU\\Environment: %w", err)
	}
	defer k.Close()

	current, valType, err := getPath(k)
	if err != nil {
		return false, "", err
	}
	if pathContains(current, dir) {
		return false, "", nil
	}

	updated := current
	if updated != "" && !strings.HasSuffix(updated, ";") {
		updated += ";"
	}
	updated += normaliseForWindows(dir)

	// Preserve the original value type. Most user PATHs are REG_EXPAND_SZ
	// (so "%USERPROFILE%" expands); fall back to that when the key is new.
	if valType == 0 {
		valType = registry.EXPAND_SZ
	}
	if err := setStringWithType(k, "Path", updated, valType); err != nil {
		return false, "", fmt.Errorf("write HKCU\\Environment\\Path: %w", err)
	}
	broadcastEnvironmentChange()
	return true, "", nil
}

// removeFromPath removes dir from the user's persistent PATH if present.
func removeFromPath(dir string) (bool, string, error) {
	k, err := registry.OpenKey(registry.CURRENT_USER, envKeyPath, registry.QUERY_VALUE|registry.SET_VALUE)
	if err != nil {
		return false, "", fmt.Errorf("open HKCU\\Environment: %w", err)
	}
	defer k.Close()

	current, valType, err := getPath(k)
	if err != nil {
		return false, "", err
	}
	parts := strings.Split(current, ";")
	dirNorm := strings.ToLower(strings.TrimRight(normaliseForWindows(dir), `\`))

	out := parts[:0]
	changed := false
	for _, p := range parts {
		if strings.ToLower(strings.TrimRight(p, `\`)) == dirNorm {
			changed = true
			continue
		}
		out = append(out, p)
	}
	if !changed {
		return false, "", nil
	}
	updated := strings.Join(out, ";")
	if valType == 0 {
		valType = registry.EXPAND_SZ
	}
	if err := setStringWithType(k, "Path", updated, valType); err != nil {
		return false, "", fmt.Errorf("write HKCU\\Environment\\Path: %w", err)
	}
	broadcastEnvironmentChange()
	return true, "", nil
}

// getPath reads HKCU\Environment\Path and reports its REG_* type so a
// subsequent write can preserve REG_EXPAND_SZ semantics.
func getPath(k registry.Key) (string, uint32, error) {
	v, t, err := k.GetStringValue("Path")
	if err == registry.ErrNotExist {
		return "", 0, nil
	}
	if err != nil {
		return "", 0, fmt.Errorf("read HKCU\\Environment\\Path: %w", err)
	}
	return v, t, nil
}

// setStringWithType writes a string value with the given REG_* type,
// since registry.Key.SetStringValue / SetExpandStringValue only cover
// REG_SZ and REG_EXPAND_SZ explicitly.
func setStringWithType(k registry.Key, name, value string, valType uint32) error {
	if valType == registry.EXPAND_SZ {
		return k.SetExpandStringValue(name, value)
	}
	return k.SetStringValue(name, value)
}

// pathContains reports whether dir is already present in a semicolon
// separated PATH string, comparing case-insensitively (Windows).
func pathContains(path, dir string) bool {
	dirNorm := strings.ToLower(strings.TrimRight(normaliseForWindows(dir), `\`))
	for _, p := range strings.Split(path, ";") {
		if strings.ToLower(strings.TrimRight(p, `\`)) == dirNorm {
			return true
		}
	}
	return false
}

// normaliseForWindows converts a forward-slash path into the
// backslash form the registry and shell expect.
func normaliseForWindows(p string) string {
	return strings.ReplaceAll(p, "/", `\`)
}

// broadcastEnvironmentChange tells any listening process that the
// user environment has changed; new shells will pick up the new PATH
// without a sign-out. SendMessageTimeout is best-effort — failures are
// silent because the registry write itself is the durable change.
func broadcastEnvironmentChange() {
	const (
		HWND_BROADCAST   = uintptr(0xffff)
		WM_SETTINGCHANGE = uintptr(0x001A)
		SMTO_ABORTIFHUNG = 0x0002
	)
	user32 := syscall.NewLazyDLL("user32.dll")
	proc := user32.NewProc("SendMessageTimeoutW")
	envPtr, _ := syscall.UTF16PtrFromString("Environment")
	var ret uintptr
	_, _, _ = proc.Call(
		HWND_BROADCAST,
		WM_SETTINGCHANGE,
		0,
		uintptr(unsafe.Pointer(envPtr)),
		SMTO_ABORTIFHUNG,
		5000,
		uintptr(unsafe.Pointer(&ret)),
	)
}
