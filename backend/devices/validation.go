package devices

import (
	"fmt"
	"path"
	"path/filepath"
	"strings"

	"github.com/vekhyat/Auralis/backend/library"
	"github.com/vekhyat/Auralis/backend/syncengine"
)

func validateProfile(p DeviceProfile) error {
	valid := false
	for _, profile := range library.Profiles() {
		if profile.ID == p.ProfileID {
			valid = true
		}
	}
	if !valid {
		return fmt.Errorf("unknown library profile %q", p.ProfileID)
	}
	valid = false
	for _, mode := range syncengine.Modes {
		if p.FormatPolicy.Mode == mode {
			valid = true
		}
	}
	if !valid {
		return fmt.Errorf("unknown format policy %q", p.FormatPolicy.Mode)
	}
	if p.SelectionRules.RecentDays < 0 || p.SelectionRules.RecentDays > 36500 {
		return fmt.Errorf("recent days must be between 0 and 36500")
	}
	if strings.ContainsAny(p.TargetFolder, "\x00\r\n") {
		return fmt.Errorf("invalid target folder")
	}
	if strings.HasPrefix(p.DeviceID, "adb:") {
		root := path.Clean(p.TargetFolder)
		if !strings.HasPrefix(root, "/") || root == "/" || root == "/sdcard" || root == "/storage" {
			return fmt.Errorf("choose a dedicated absolute music folder")
		}
	} else {
		if !filepath.IsAbs(p.TargetFolder) {
			return fmt.Errorf("target folder must be absolute")
		}
		volume := filepath.VolumeName(p.TargetFolder)
		if filepath.Clean(p.TargetFolder) == volume+string(filepath.Separator) {
			return fmt.Errorf("choose a music folder below the volume root")
		}
	}
	return nil
}
