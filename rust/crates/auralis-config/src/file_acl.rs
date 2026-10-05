//! Access control and private file permission hardening.

use std::io;
use std::path::Path;

/// Restricts file access permissions so that only the current user can access it.
#[cfg(target_os = "windows")]
pub fn restrict_private_file(file_path: &Path) -> io::Result<()> {
    use std::os::windows::ffi::OsStrExt;
    use windows_sys::Win32::Foundation::{CloseHandle, LocalFree, GENERIC_ALL, HANDLE};
    use windows_sys::Win32::Security::Authorization::{
        SetEntriesInAclW, SetNamedSecurityInfoW, EXPLICIT_ACCESS_W,
        SE_FILE_OBJECT, SET_ACCESS, TRUSTEE_IS_SID, TRUSTEE_IS_USER, TRUSTEE_W,
    };
    use windows_sys::Win32::Security::{
        GetTokenInformation, TokenUser, DACL_SECURITY_INFORMATION,
        PROTECTED_DACL_SECURITY_INFORMATION, TOKEN_QUERY, TOKEN_USER,
    };
    use windows_sys::Win32::System::Threading::{GetCurrentProcess, OpenProcessToken};

    let path_wide: Vec<u16> = file_path
        .as_os_str()
        .encode_wide()
        .chain(std::iter::once(0))
        .collect();

    unsafe {
        let process = GetCurrentProcess();
        let mut token: HANDLE = std::ptr::null_mut();
        if OpenProcessToken(process, TOKEN_QUERY, &mut token) == 0 {
            return Err(io::Error::last_os_error());
        }

        struct HandleGuard(HANDLE);
        impl Drop for HandleGuard {
            fn drop(&mut self) {
                if !self.0.is_null() {
                    unsafe {
                        CloseHandle(self.0);
                    }
                }
            }
        }
        let _token_guard = HandleGuard(token);

        let mut length_needed: u32 = 0;
        let _ = GetTokenInformation(token, TokenUser, std::ptr::null_mut(), 0, &mut length_needed);
        if length_needed == 0 {
            return Err(io::Error::last_os_error());
        }

        let mut user_buf: Vec<u8> = vec![0u8; length_needed as usize];
        if GetTokenInformation(
            token,
            TokenUser,
            user_buf.as_mut_ptr() as *mut _,
            length_needed,
            &mut length_needed,
        ) == 0
        {
            return Err(io::Error::last_os_error());
        }

        let token_user = &*(user_buf.as_ptr() as *const TOKEN_USER);
        let user_sid = token_user.User.Sid;

        let explicit_access = EXPLICIT_ACCESS_W {
            grfAccessPermissions: GENERIC_ALL,
            grfAccessMode: SET_ACCESS,
            grfInheritance: 0, // NO_INHERITANCE
            Trustee: TRUSTEE_W {
                pMultipleTrustee: std::ptr::null_mut(),
                MultipleTrusteeOperation: 0,
                TrusteeForm: TRUSTEE_IS_SID,
                TrusteeType: TRUSTEE_IS_USER,
                ptstrName: user_sid as *mut u16,
            },
        };

        let mut acl: *mut windows_sys::Win32::Security::ACL = std::ptr::null_mut();
        let ret = SetEntriesInAclW(1, &explicit_access, std::ptr::null_mut(), &mut acl);
        if ret != 0 {
            return Err(io::Error::from_raw_os_error(ret as i32));
        }

        struct AclGuard(*mut windows_sys::Win32::Security::ACL);
        impl Drop for AclGuard {
            fn drop(&mut self) {
                if !self.0.is_null() {
                    unsafe {
                        LocalFree(self.0 as *mut _);
                    }
                }
            }
        }
        let _acl_guard = AclGuard(acl);

        let sec_info = DACL_SECURITY_INFORMATION | PROTECTED_DACL_SECURITY_INFORMATION;
        let set_res = SetNamedSecurityInfoW(
            path_wide.as_ptr() as *mut u16,
            SE_FILE_OBJECT,
            sec_info,
            std::ptr::null_mut(),
            std::ptr::null_mut(),
            acl,
            std::ptr::null_mut(),
        );

        if set_res != 0 {
            return Err(io::Error::from_raw_os_error(set_res as i32));
        }

        Ok(())
    }
}

#[cfg(not(target_os = "windows"))]
pub fn restrict_private_file(file_path: &Path) -> io::Result<()> {
    use std::os::unix::fs::PermissionsExt;
    let permissions = std::fs::Permissions::from_mode(0o600);
    std::fs::set_permissions(file_path, permissions)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_restrict_private_file() {
        let temp = tempfile::tempdir().unwrap();
        let file_path = temp.path().join("secure.txt");
        std::fs::write(&file_path, "secret content").unwrap();

        let res = restrict_private_file(&file_path);
        assert!(res.is_ok(), "Failed to restrict file: {:?}", res);

        let content = std::fs::read_to_string(&file_path).unwrap();
        assert_eq!(content, "secret content");
    }
}
