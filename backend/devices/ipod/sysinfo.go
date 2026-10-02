package ipod

import (
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
)

type sysInfo struct {
	ModelNum  string
	Firewire  []byte
	FirewireH string
}

func readSysInfo(path string) sysInfo {
	data, err := os.ReadFile(path)
	if err != nil {
		return sysInfo{}
	}
	return parseSysInfo(string(data))
}

func parseSysInfo(text string) sysInfo {
	if strings.Contains(text, "<key>") {
		return parsePlistSysInfo(text)
	}
	info := sysInfo{}
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(strings.TrimRight(line, "\r"))
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, val, ok := splitKV(line)
		if !ok {
			continue
		}
		switch strings.ToLower(key) {
		case "modelnumstr", "modelnum":
			info.ModelNum = val
		case "firewireguid", "firewireid", "firewareguid":
			raw, hexText := decodeFirewire(val)
			info.Firewire = raw
			info.FirewireH = hexText
		}
	}
	return info
}

func splitKV(line string) (string, string, bool) {
	for _, sep := range []string{":", "="} {
		if i := strings.Index(line, sep); i > 0 {
			return strings.TrimSpace(line[:i]), strings.TrimSpace(line[i+len(sep):]), true
		}
	}
	return "", "", false
}

func loadSysInfo(root string) sysInfo {
	primary := readSysInfo(filepath.Join(root, "iPod_Control", "Device", "SysInfo"))
	extended := readSysInfo(filepath.Join(root, "iPod_Control", "Device", "SysInfoExtended"))
	if extended.ModelNum != "" {
		primary.ModelNum = extended.ModelNum
	}
	if len(extended.Firewire) > 0 {
		primary.Firewire = extended.Firewire
		primary.FirewireH = extended.FirewireH
	}
	return primary
}

func parsePlistSysInfo(text string) sysInfo {
	info := sysInfo{}
	rest := text
	for {
		i := strings.Index(rest, "<key>")
		if i < 0 {
			break
		}
		rest = rest[i+5:]
		j := strings.Index(rest, "</key>")
		if j < 0 {
			break
		}
		key := strings.TrimSpace(rest[:j])
		rest = rest[j+6:]
		val, next := plistString(rest)
		rest = next
		switch strings.ToLower(key) {
		case "modelnumstr", "modelnum":
			info.ModelNum = val
		case "firewireguid", "firewireid", "firewareguid":
			raw, hexText := decodeFirewire(val)
			info.Firewire = raw
			info.FirewireH = hexText
		}
	}
	return info
}

func plistString(s string) (string, string) {
	s = strings.TrimLeft(s, " \t\r\n")
	for _, tag := range []string{"string", "integer"} {
		open := "<" + tag + ">"
		close := "</" + tag + ">"
		if strings.HasPrefix(s, open) {
			s = s[len(open):]
			j := strings.Index(s, close)
			if j < 0 {
				return "", ""
			}
			return s[:j], s[j+len(close):]
		}
	}
	return "", s
}

func decodeFirewire(val string) ([]byte, string) {
	s := strings.TrimSpace(val)
	s = strings.TrimPrefix(strings.TrimPrefix(s, "0x"), "0X")
	s = strings.ReplaceAll(s, " ", "")
	if len(s)%2 == 1 {
		s = "0" + s
	}
	raw, err := hex.DecodeString(s)
	if err != nil {
		return nil, ""
	}
	return raw, strings.ToUpper(hex.EncodeToString(raw))
}
