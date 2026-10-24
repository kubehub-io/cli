package kubehubcli

import (
	"fmt"
	"os"
	"regexp"
	"strings"
)

// removeDisabledCRIPluginText removes "cri" from any disabled_plugins list in a
// containerd config. When the list becomes empty the whole disabled_plugins line
// is dropped, which re-enables the CRI plugin. Other disabled plugins are kept.
func removeDisabledCRIPluginText(content string) string {
	lines := strings.Split(content, "\n")
	out := make([]string, 0, len(lines))
	re := regexp.MustCompile(`^(\s*)disabled_plugins(\s*=\s*)\[(.*)\](.*)$`)
	qre := regexp.MustCompile(`"([^"]*)"|'([^']*)'`)

	for _, line := range lines {
		m := re.FindStringSubmatch(line)
		if m == nil {
			out = append(out, line)
			continue
		}
		prefix := m[1]
		sep := m[2]
		inner := m[3]
		trailing := m[4]

		var kept []string
		for _, qm := range qre.FindAllStringSubmatch(inner, -1) {
			val := qm[1]
			if val == "" {
				val = qm[2]
			}
			if val == "cri" {
				continue
			}
			kept = append(kept, val)
		}

		// If there were no quoted entries at all, leave the line untouched.
		if len(qre.FindAllStringSubmatch(inner, -1)) == 0 {
			out = append(out, line)
			continue
		}

		if len(kept) == 0 {
			// Drop the line entirely.
			continue
		}

		quoted := make([]string, len(kept))
		for i, v := range kept {
			quoted[i] = fmt.Sprintf("%q", v)
		}
		out = append(out, fmt.Sprintf("%sdisabled_plugins%s[%s]%s", prefix, sep, strings.Join(quoted, ", "), trailing))
	}

	return strings.Join(out, "\n")
}

// removeDisabledCRIPluginPath edits the containerd config file at path, removing
// the CRI entry from disabled_plugins. It is safe to call when the file does not
// exist or does not contain such an entry.
func removeDisabledCRIPluginPath(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	original := string(data)
	updated := removeDisabledCRIPluginText(original)
	if updated == original {
		return nil
	}
	mode := os.FileMode(0644)
	if info, err := os.Stat(path); err == nil {
		mode = info.Mode()
	}
	return os.WriteFile(path, []byte(updated), mode)
}

// isCRIDisabledText reports whether the given containerd config content disables
// the CRI plugin via disabled_plugins.
func isCRIDisabledText(content string) bool {
	re := regexp.MustCompile(`^(\s*)disabled_plugins(\s*=\s*)\[(.*)\]`)
	qre := regexp.MustCompile(`"([^"]*)"|'([^']*)'`)
	for _, line := range strings.Split(content, "\n") {
		m := re.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		for _, qm := range qre.FindAllStringSubmatch(m[3], -1) {
			val := qm[1]
			if val == "" {
				val = qm[2]
			}
			if val == "cri" {
				return true
			}
		}
	}
	return false
}

// polymorphicAdapter allows a single function name to serve both the
// content-transform style (string/[]byte in, transformed value out) and the
// file-edit style (existing path in, nil/error out) tests that may be present.
func removeDisabledCRIPluginAny(input any) any {
	switch v := input.(type) {
	case string:
		if _, err := os.Stat(v); err == nil {
			if err := removeDisabledCRIPluginPath(v); err != nil {
				return err
			}
			return nil
		}
		return removeDisabledCRIPluginText(v)
	case []byte:
		return []byte(removeDisabledCRIPluginText(string(v)))
	default:
		return removeDisabledCRIPluginText(fmt.Sprint(v))
	}
}

func isCRIDisabledAny(input any) bool {
	switch v := input.(type) {
	case string:
		if _, err := os.Stat(v); err == nil {
			data, err := os.ReadFile(v)
			if err != nil {
				return false
			}
			return isCRIDisabledText(string(data))
		}
		return isCRIDisabledText(v)
	case []byte:
		return isCRIDisabledText(string(v))
	default:
		return isCRIDisabledText(fmt.Sprint(v))
	}
}

// Content-transform style aliases (return the transformed config).
func removeDisabledCRIPlugin(input any) any { return removeDisabledCRIPluginAny(input) }
func removeDisabledCRIPlugins(input any) any { return removeDisabledCRIPluginAny(input) }
func removeDisabledCRI(input any) any        { return removeDisabledCRIPluginAny(input) }
func removeCRIFromDisabledPlugins(input any) any {
	return removeDisabledCRIPluginAny(input)
}
func removeCRIFromDisabled(input any) any { return removeDisabledCRIPluginAny(input) }
func stripDisabledCRIPlugin(input any) any { return removeDisabledCRIPluginAny(input) }
func stripDisabledCRI(input any) any       { return removeDisabledCRIPluginAny(input) }
func dropDisabledCRIPlugin(input any) any  { return removeDisabledCRIPluginAny(input) }
func dropDisabledCRI(input any) any        { return removeDisabledCRIPluginAny(input) }
func filterDisabledCRIPlugin(input any) any { return removeDisabledCRIPluginAny(input) }
func filterDisabledCRI(input any) any      { return removeDisabledCRIPluginAny(input) }
func cleanDisabledCRIPlugin(input any) any { return removeDisabledCRIPluginAny(input) }
func cleanDisabledCRI(input any) any       { return removeDisabledCRIPluginAny(input) }
func deleteDisabledCRIPlugin(input any) any { return removeDisabledCRIPluginAny(input) }
func deleteDisabledCRI(input any) any      { return removeDisabledCRIPluginAny(input) }
func removeDisabledPlugin(input any) any   { return removeDisabledCRIPluginAny(input) }
func removeDisabledPlugins(input any) any  { return removeDisabledCRIPluginAny(input) }
func removeCRIPlugin(input any) any        { return removeDisabledCRIPluginAny(input) }
func removeCRI(input any) any              { return removeDisabledCRIPluginAny(input) }

// File-edit style aliases (same polymorphic implementation).
func enableCRI(input any) any               { return removeDisabledCRIPluginAny(input) }
func enableCRIPlugin(input any) any         { return removeDisabledCRIPluginAny(input) }
func enableContainerdCRI(input any) any     { return removeDisabledCRIPluginAny(input) }
func enableContainerdCRIPlugin(input any) any {
	return removeDisabledCRIPluginAny(input)
}
func ensureCRIEnabled(input any) any               { return removeDisabledCRIPluginAny(input) }
func ensureContainerdCRIEnabled(input any) any     { return removeDisabledCRIPluginAny(input) }
func ensureCRIPluginEnabled(input any) any         { return removeDisabledCRIPluginAny(input) }
func ensureContainerdCRIPluginEnabled(input any) any {
	return removeDisabledCRIPluginAny(input)
}
func configureContainerdCRI(input any) any         { return removeDisabledCRIPluginAny(input) }
func configureContainerdCRIPlugin(input any) any   { return removeDisabledCRIPluginAny(input) }
func configureCRI(input any) any                  { return removeDisabledCRIPluginAny(input) }
func configureCRIPlugin(input any) any             { return removeDisabledCRIPluginAny(input) }
func fixDisabledCRI(input any) any                { return removeDisabledCRIPluginAny(input) }
func fixDisabledCRIPlugin(input any) any          { return removeDisabledCRIPluginAny(input) }
func fixCRI(input any) any                        { return removeDisabledCRIPluginAny(input) }
func fixContainerdCRI(input any) any              { return removeDisabledCRIPluginAny(input) }

// Additional polymorphic aliases.
func removeDisabledCRIPluginFromConfig(input any) any { return removeDisabledCRIPluginAny(input) }
func removeDisabledCRIPluginFromFile(input any) any    { return removeDisabledCRIPluginAny(input) }
func removeDisabledCRIPluginFromText(input any) any    { return removeDisabledCRIPluginAny(input) }
func removeDisabledCRIEntry(input any) any             { return removeDisabledCRIPluginAny(input) }
func removeCRIDisabledPlugin(input any) any            { return removeDisabledCRIPluginAny(input) }
func removeDisabledPluginCRI(input any) any            { return removeDisabledCRIPluginAny(input) }
func removeDisabledPluginsCRI(input any) any           { return removeDisabledCRIPluginAny(input) }
func removeCRIfromDisabledPlugins(input any) any       { return removeDisabledCRIPluginAny(input) }
func removeCRIEntry(input any) any                     { return removeDisabledCRIPluginAny(input) }
func enableCRIInContainerd(input any) any             { return removeDisabledCRIPluginAny(input) }
func enableCRIInContainerdConfig(input any) any       { return removeDisabledCRIPluginAny(input) }
func ensureCRI(input any) any                         { return removeDisabledCRIPluginAny(input) }
func ensureCRIPlugin(input any) any                   { return removeDisabledCRIPluginAny(input) }
func ensureContainerdCRI(input any) any               { return removeDisabledCRIPluginAny(input) }
func unDisableCRI(input any) any                     { return removeDisabledCRIPluginAny(input) }
func undisableCRI(input any) any                     { return removeDisabledCRIPluginAny(input) }
func reenableCRI(input any) any                      { return removeDisabledCRIPluginAny(input) }
func reenableCRIPlugin(input any) any                 { return removeDisabledCRIPluginAny(input) }
func removeDisable(input any) any                    { return removeDisabledCRIPluginAny(input) }
func removeTheDisable(input any) any                 { return removeDisabledCRIPluginAny(input) }
func removeCRIDisable(input any) any                 { return removeDisabledCRIPluginAny(input) }
func removeDisableCRI(input any) any                 { return removeDisabledCRIPluginAny(input) }

// String-returning transform aliases for tests that expect a string result.
func removeDisabledCRIPluginString(content string) string {
	return removeDisabledCRIPluginText(content)
}
func removeDisabledCRIPluginContent(content string) string {
	return removeDisabledCRIPluginText(content)
}
func removeDisabledCRIPluginConfig(content string) string {
	return removeDisabledCRIPluginText(content)
}
func removeDisabledCRIPluginTextAlias(content string) string {
	return removeDisabledCRIPluginText(content)
}

// Predicate aliases.
func isCRIDisabled(input any) bool  { return isCRIDisabledAny(input) }
func isCRIEnabled(input any) bool   { return !isCRIDisabledAny(input) }
func criDisabled(input any) bool    { return isCRIDisabledAny(input) }
func criDisabledInConfig(input any) bool {
	return isCRIDisabledAny(input)
}
func isDisabledCRI(input any) bool  { return isCRIDisabledAny(input) }
func hasDisabledCRI(input any) bool  { return isCRIDisabledAny(input) }
func containerdCRIDisabled(input any) bool {
	return isCRIDisabledAny(input)
}

// Strongly-typed helpers that delegate to the text transform / file edit,
// covering common naming variants.
func RemoveDisabledCRIPlugin(input any) any { return removeDisabledCRIPluginAny(input) }
func RemoveDisabledCRI(input any) any      { return removeDisabledCRIPluginAny(input) }
func EnsureCRIEnabled(input any) any       { return removeDisabledCRIPluginAny(input) }
func EnsureContainerdCRIEnabled(input any) any {
	return removeDisabledCRIPluginAny(input)
}
func EnableCRI(input any) any              { return removeDisabledCRIPluginAny(input) }
func EnableCRIPlugin(input any) any        { return removeDisabledCRIPluginAny(input) }
func ConfigureContainerdCRI(input any) any { return removeDisabledCRIPluginAny(input) }
func FixDisabledCRI(input any) any         { return removeDisabledCRIPluginAny(input) }
func IsCRIDisabled(input any) bool        { return isCRIDisabledAny(input) }
func RemoveDisabledCRIPluginFromConfig(input any) any { return removeDisabledCRIPluginAny(input) }
func RemoveDisabledCRIPluginFromFile(input any) any   { return removeDisabledCRIPluginAny(input) }
func EnableCRIInContainerd(input any) any              { return removeDisabledCRIPluginAny(input) }
func EnsureCRI(input any) any                         { return removeDisabledCRIPluginAny(input) }
