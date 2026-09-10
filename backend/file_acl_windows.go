//go:build windows

package backend

import (
	"fmt"
	"runtime"

	"golang.org/x/sys/windows"
)

func restrictPrivateFileACL(path string) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("acl restrict panic: %v", recovered)
		}
	}()

	tokenUser, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return err
	}
	if tokenUser == nil || tokenUser.User.Sid == nil {
		return fmt.Errorf("missing process user SID")
	}

	sid, err := tokenUser.User.Sid.Copy()
	if err != nil {
		return err
	}
	runtime.KeepAlive(tokenUser)

	entries := []windows.EXPLICIT_ACCESS{{
		AccessPermissions: windows.GENERIC_ALL,
		AccessMode:        windows.SET_ACCESS,
		Inheritance:       windows.NO_INHERITANCE,
		Trustee: windows.TRUSTEE{
			TrusteeForm:  windows.TRUSTEE_IS_SID,
			TrusteeType:  windows.TRUSTEE_IS_USER,
			TrusteeValue: windows.TrusteeValueFromSID(sid),
		},
	}}
	acl, err := windows.ACLFromEntries(entries, nil)
	if err != nil {
		return err
	}

	err = windows.SetNamedSecurityInfo(
		path,
		windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION,
		nil,
		nil,
		acl,
		nil,
	)
	runtime.KeepAlive(sid)
	runtime.KeepAlive(entries)
	runtime.KeepAlive(acl)
	return err
}
