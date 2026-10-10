Unicode true

####
## Auralis installer, based on the Wails v2.16 NSIS template.
##
## `wails build -nsis` regenerates wails_tools.nsh from wails.json on every build
## and keeps this file. To iterate on the installer by hand:
## > wails build -platform windows/amd64 -nsis
## > makensis -DARG_WAILS_AMD64_BINARY=..\..\bin\Auralis.exe project.nsi
####

## Install for the current user only: no admin prompt, files under
## %LocalAppData%\Programs\Auralis, and registry entries under HKCU. This matches
## the app, which keeps its data in ~/.auralis and registers auralis:// in HKCU.
!define WAILS_INSTALL_SCOPE "user"
!define REQUEST_EXECUTION_LEVEL "user"

!include "wails_tools.nsh"
!include "LogicLib.nsh"

# The version information for this two must consist of 4 parts
VIProductVersion "${INFO_PRODUCTVERSION}.0"
VIFileVersion    "${INFO_PRODUCTVERSION}.0"

VIAddVersionKey "CompanyName"     "${INFO_COMPANYNAME}"
VIAddVersionKey "FileDescription" "${INFO_PRODUCTNAME} Installer"
VIAddVersionKey "ProductVersion"  "${INFO_PRODUCTVERSION}"
VIAddVersionKey "FileVersion"     "${INFO_PRODUCTVERSION}"
VIAddVersionKey "LegalCopyright"  "${INFO_COPYRIGHT}"
VIAddVersionKey "ProductName"     "${INFO_PRODUCTNAME}"

# Enable HiDPI support. https://nsis.sourceforge.io/Reference/ManifestDPIAware
ManifestDPIAware true

!include "MUI.nsh"

!define MUI_ICON "..\icon.ico"
!define MUI_UNICON "..\icon.ico"
!define MUI_ABORTWARNING # This will warn the user if they exit from the installer.
!define MUI_FINISHPAGE_RUN "$INSTDIR\${PRODUCT_EXECUTABLE}"
!define MUI_FINISHPAGE_RUN_TEXT "Open ${INFO_PRODUCTNAME}"

!insertmacro MUI_PAGE_WELCOME
!insertmacro MUI_PAGE_DIRECTORY
!insertmacro MUI_PAGE_INSTFILES
!insertmacro MUI_PAGE_FINISH

!insertmacro MUI_UNPAGE_CONFIRM
!insertmacro MUI_UNPAGE_INSTFILES

!insertmacro MUI_LANGUAGE "English"

## The following two statements can be used to sign the installer and the uninstaller. The path to the binaries are provided in %1
#!uninstfinalize 'signtool --file "%1"'
#!finalize 'signtool --file "%1"'

Name "${INFO_PRODUCTNAME}"
OutFile "..\..\bin\${INFO_PROJECTNAME}-${ARCH}-installer.exe"
InstallDir "$LOCALAPPDATA\Programs\${INFO_PRODUCTNAME}"
# An upgrade goes back into the folder the user picked last time.
InstallDirRegKey HKCU "${UNINST_KEY}" "InstallLocation"
ShowInstDetails show

## Windows refuses to open a running .exe for writing, so a failed open means
## Auralis is still running from that folder. Sets $0 to 1 when it is running.
!macro AURALIS_IS_RUNNING_FUNC un
Function ${un}AuralisIsRunning
    StrCpy $0 0
    IfFileExists "$INSTDIR\${PRODUCT_EXECUTABLE}" 0 done
    ClearErrors
    FileOpen $1 "$INSTDIR\${PRODUCT_EXECUTABLE}" a
    IfErrors 0 +3
        StrCpy $0 1
        Goto done
    FileClose $1
    done:
FunctionEnd
!macroend
!insertmacro AURALIS_IS_RUNNING_FUNC ""
!insertmacro AURALIS_IS_RUNNING_FUNC "un."

## Called before files are replaced or removed. An upgrade or uninstall must not
## start while Auralis is open: it may be in the middle of downloads, and the
## running .exe cannot be overwritten.
!macro AURALIS_WAIT_FOR_CLOSE un
    ${Do}
        Call ${un}AuralisIsRunning
        ${IfThen} $0 == 0 ${|} ${ExitDo} ${|}
        # Nobody can answer a dialog during a silent install; fail with code 2.
        ${If} ${Silent}
            SetErrorLevel 2
            Abort
        ${EndIf}
        ${If} ${Cmd} `MessageBox MB_RETRYCANCEL|MB_ICONEXCLAMATION "${INFO_PRODUCTNAME} is still open. Close it, then click Retry.$\n$\nCancel stops here and changes nothing." IDCANCEL`
            Abort
        ${EndIf}
    ${Loop}
!macroend

Function .onInit
    !insertmacro wails.checkArchitecture
FunctionEnd

Section
    !insertmacro wails.setShellContext

    !insertmacro AURALIS_WAIT_FOR_CLOSE ""

    !insertmacro wails.webview2runtime

    SetOutPath $INSTDIR

    !insertmacro wails.files

    CreateShortcut "$SMPROGRAMS\${INFO_PRODUCTNAME}.lnk" "$INSTDIR\${PRODUCT_EXECUTABLE}"
    CreateShortCut "$DESKTOP\${INFO_PRODUCTNAME}.lnk" "$INSTDIR\${PRODUCT_EXECUTABLE}"

    !insertmacro wails.associateFiles
    !insertmacro wails.associateCustomProtocols

    !insertmacro wails.writeUninstaller
    WriteRegStr HKCU "${UNINST_KEY}" "InstallLocation" "$INSTDIR"
SectionEnd

Section "uninstall"
    !insertmacro wails.setShellContext

    !insertmacro AURALIS_WAIT_FOR_CLOSE "un."

    # WebView2 cache only. Library, settings and history in ~/.auralis are kept.
    RMDir /r "$AppData\${PRODUCT_EXECUTABLE}"

    RMDir /r $INSTDIR

    Delete "$SMPROGRAMS\${INFO_PRODUCTNAME}.lnk"
    Delete "$DESKTOP\${INFO_PRODUCTNAME}.lnk"

    !insertmacro wails.unassociateFiles
    !insertmacro wails.unassociateCustomProtocols
    # The app registers this legacy grant handler itself at startup.
    DeleteRegKey HKCU "Software\Classes\spotiflac"

    !insertmacro wails.deleteUninstaller
SectionEnd
