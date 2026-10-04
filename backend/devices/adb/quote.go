package adb

import "strings"

// QuoteArg wraps a string in single quotes suitable for POSIX sh on Android,
// escaping any internal single quotes.
func QuoteArg(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
