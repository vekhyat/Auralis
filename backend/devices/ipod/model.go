package ipod

import "strings"

// Checksum is the scheme a stock iPod uses to accept or reject its database.
type Checksum int

const (
	ChecksumNone Checksum = iota
	ChecksumHash58
	ChecksumHash72
	ChecksumHashAB
	ChecksumSQLite
	ChecksumUnknown
)

func (c Checksum) String() string {
	switch c {
	case ChecksumNone:
		return "none"
	case ChecksumHash58:
		return "hash58"
	case ChecksumHash72:
		return "hash72"
	case ChecksumHashAB:
		return "hashAB"
	case ChecksumSQLite:
		return "sqlite"
	default:
		return "unknown"
	}
}

func (c Checksum) writable() bool {
	return c == ChecksumNone || c == ChecksumHash58
}

var generationLabel = map[string]string{
	"FIRST":     "iPod (1st generation)",
	"SECOND":    "iPod (2nd generation)",
	"THIRD":     "iPod (3rd generation)",
	"FOURTH":    "iPod (4th generation)",
	"PHOTO":     "iPod photo",
	"MINI_1":    "iPod mini (1st generation)",
	"MINI_2":    "iPod mini (2nd generation)",
	"SHUFFLE_1": "iPod shuffle (1st generation)",
	"SHUFFLE_2": "iPod shuffle (2nd generation)",
	"SHUFFLE_3": "iPod shuffle (3rd generation)",
	"SHUFFLE_4": "iPod shuffle (4th generation)",
	"NANO_1":    "iPod nano (1st generation)",
	"NANO_2":    "iPod nano (2nd generation)",
	"NANO_3":    "iPod nano (3rd generation)",
	"NANO_4":    "iPod nano (4th generation)",
	"NANO_5":    "iPod nano (5th generation)",
	"NANO_6":    "iPod nano (6th generation)",
	"VIDEO_1":   "iPod (5th generation)",
	"VIDEO_2":   "iPod (5.5 generation)",
	"CLASSIC_1": "iPod classic",
	"CLASSIC_2": "iPod classic (2008)",
	"CLASSIC_3": "iPod classic (2009)",
	"TOUCH_1":   "iPod touch (1st generation)",
	"TOUCH_2":   "iPod touch (2nd generation)",
	"TOUCH_3":   "iPod touch (3rd generation)",
	"TOUCH_4":   "iPod touch (4th generation)",
	"IPHONE_1":  "iPhone",
	"IPHONE_2":  "iPhone 3G",
	"IPHONE_3":  "iPhone 3GS",
	"IPHONE_4":  "iPhone 4",
	"IPAD_1":    "iPad",
	"MOBILE":    "iPod",
}

func checksumForGeneration(gen string) Checksum {
	switch gen {
	case "CLASSIC_1", "CLASSIC_2", "CLASSIC_3", "NANO_3", "NANO_4":
		return ChecksumHash58
	case "NANO_5", "TOUCH_1", "TOUCH_2", "TOUCH_3", "IPHONE_1", "IPHONE_2", "IPHONE_3":
		return ChecksumHash72
	case "NANO_6", "TOUCH_4", "IPHONE_4", "IPAD_1":
		return ChecksumHashAB
	case "":
		return ChecksumUnknown
	default:
		return ChecksumNone
	}
}

func usesShuffleSD(gen string) bool {
	return gen == "SHUFFLE_1" || gen == "SHUFFLE_2"
}

// LookupModel resolves a SysInfo ModelNumStr such as "MB147" or "B147".
func LookupModel(modelNum string) (code, generation, name string, ok bool) {
	clean := digitsAndLetters(modelNum)
	if clean == "" {
		return "", "", "iPod", false
	}
	candidates := []string{clean}
	if len(clean) > 4 {
		candidates = append(candidates, clean[len(clean)-4:])
	}
	for _, candidate := range candidates {
		gen, found := modelGeneration[candidate]
		if !found {
			gen, found = modelGeneration[strings.ToLower(candidate)]
		}
		if !found {
			continue
		}
		label := generationLabel[gen]
		if label == "" {
			label = "iPod"
		}
		return candidate, gen, label, true
	}
	label := "iPod"
	if len(clean) > 4 {
		clean = clean[len(clean)-4:]
	}
	return clean, "", label + " " + clean, false
}

func digitsAndLetters(s string) string {
	var b strings.Builder
	for _, r := range strings.ToUpper(s) {
		if (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	return b.String()
}
