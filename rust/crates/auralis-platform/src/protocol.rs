//! Custom URL protocol registration (`auralis://` and legacy `spotiflac://`).
//!
//! Port of `backend/protocol_windows.go` and `backend/protocol_other.go`.

use crate::error::ProtocolError;
#[cfg(windows)]
use std::path::Path;

/// Registers the `auralis://` and legacy `spotiflac://` custom URL protocol schemes in the OS.
///
/// Port of `backend.RegisterAuralisProtocol()`.
#[cfg(windows)]
pub fn register_auralis_protocol() -> Result<(), ProtocolError> {
    register_url_protocol("auralis", "Auralis Protocol")?;

    // Zarz's browser challenge page is server-rendered with SpotiFLAC branding;
    // grants are picked up by polling, but keep the legacy spotiflac:// handler
    // registered so old copied fallback links still open the app.
    if let Err(e) = register_url_protocol("spotiflac", "Auralis Protocol") {
        eprintln!("Could not register spotiflac:// grant handler: {}", e);
    }

    Ok(())
}

/// Registers a custom URL protocol scheme in the Windows registry under `HKCU\Software\Classes\<scheme>`.
#[cfg(windows)]
pub fn register_url_protocol(scheme: &str, description: &str) -> Result<(), ProtocolError> {
    let exe = std::env::current_exe().map_err(ProtocolError::Executable)?;
    let hkcu = winreg::RegKey::predef(winreg::enums::HKEY_CURRENT_USER);
    register_url_protocol_with_root(&hkcu, "Software\\Classes", scheme, description, &exe)
}

/// Registers a custom URL protocol scheme under a given root key and classes subpath.
///
/// This parameterization allows tests to execute against isolated test registry subtrees
/// without modifying real user associations in `HKCU\Software\Classes`.
#[cfg(windows)]
pub fn register_url_protocol_with_root(
    root: &winreg::RegKey,
    classes_subpath: &str,
    scheme: &str,
    description: &str,
    exe_path: &Path,
) -> Result<(), ProtocolError> {
    let scheme_path = format!("{}\\{}", classes_subpath, scheme);
    let (key, _) = root
        .create_subkey(&scheme_path)
        .map_err(ProtocolError::Registry)?;

    key.set_value("", &format!("URL:{}", description))
        .map_err(ProtocolError::Registry)?;
    key.set_value("URL Protocol", &"")
        .map_err(ProtocolError::Registry)?;

    let cmd_path = format!("{}\\{}\\shell\\open\\command", classes_subpath, scheme);
    let (cmd, _) = root
        .create_subkey(&cmd_path)
        .map_err(ProtocolError::Registry)?;

    let cmd_value = format!("\"{}\" \"%1\"", exe_path.display());
    cmd.set_value("", &cmd_value)
        .map_err(ProtocolError::Registry)?;

    Ok(())
}

/// Registers the `auralis://` and legacy `spotiflac://` custom URL protocol schemes (non-Windows stub).
///
/// Port of `backend/protocol_other.go`.
#[cfg(not(windows))]
pub fn register_auralis_protocol() -> Result<(), ProtocolError> {
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_protocol_registration() {
        #[cfg(windows)]
        {
            use winreg::enums::HKEY_CURRENT_USER;
            use winreg::RegKey;

            // Use an isolated test subkey under HKCU\Software\AuralisTestProtocol to ensure
            // the test never modifies the real user registry in HKCU\Software\Classes
            let hkcu = RegKey::predef(HKEY_CURRENT_USER);
            let test_root_path = "Software\\AuralisTestProtocol_TestClasses";

            let fake_exe = std::path::PathBuf::from("C:\\Program Files\\Auralis\\Auralis.exe");
            let scheme = "auralistest";
            let description = "Test Auralis Protocol";

            let reg_res = register_url_protocol_with_root(
                &hkcu,
                test_root_path,
                scheme,
                description,
                &fake_exe,
            );
            assert!(reg_res.is_ok(), "registration under test key succeeded");

            // Verify keys and values
            let scheme_key_path = format!("{}\\{}", test_root_path, scheme);
            let scheme_key = hkcu.open_subkey(&scheme_key_path).expect("open scheme key");
            let val: String = scheme_key.get_value("").expect("read default value");
            assert_eq!(val, format!("URL:{}", description));

            let proto_val: String = scheme_key
                .get_value("URL Protocol")
                .expect("read URL Protocol value");
            assert_eq!(proto_val, "");

            let cmd_key_path = format!("{}\\{}\\shell\\open\\command", test_root_path, scheme);
            let cmd_key = hkcu.open_subkey(&cmd_key_path).expect("open command key");
            let cmd_val: String = cmd_key.get_value("").expect("read command string");
            assert_eq!(cmd_val, format!("\"{}\" \"%1\"", fake_exe.display()));

            // Cleanup isolated test subtree
            let _ = hkcu.delete_subkey_all(test_root_path);
        }

        #[cfg(not(windows))]
        {
            let res = register_auralis_protocol();
            assert!(res.is_ok());
        }
    }
}
