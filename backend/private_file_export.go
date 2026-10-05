package backend

// RestrictPrivateFilePath applies owner-only hardening (0600 plus the ACL
// tightening from file_acl_windows.go) to path. Packages under backend/
// (credentials, taste) reuse this so the hardening stays in one place.
func RestrictPrivateFilePath(path string) error {
	return restrictPrivateFile(path)
}
