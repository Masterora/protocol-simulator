Unicode true
!include "MUI2.nsh"
!include "LogicLib.nsh"
!include "WinVer.nsh"
!include "x64.nsh"
!include "WordFunc.nsh"

Name "protocol-simulator"
OutFile "${SETUP_OUTPUT}"
InstallDir "$LOCALAPPDATA\Programs\protocol-simulator"
InstallDirRegKey HKCU "Software\protocol-simulator" "InstallLocation"
RequestExecutionLevel user
SetCompressor /SOLID lzma
SetDatablockOptimize on
ShowInstDetails show
ShowUninstDetails show
BrandingText "protocol-simulator"
!define MUI_ICON "${APP_ICON}"
!define MUI_UNICON "${APP_ICON}"
VIProductVersion "${APP_VERSION}.0"
VIAddVersionKey /LANG=1033 "ProductName" "protocol-simulator Setup"
VIAddVersionKey /LANG=1033 "FileDescription" "protocol-simulator installer"
VIAddVersionKey /LANG=1033 "FileVersion" "${APP_VERSION}"
VIAddVersionKey /LANG=1033 "ProductVersion" "${APP_VERSION}"
VIAddVersionKey /LANG=1033 "OriginalFilename" "protocol-simulator-Setup.exe"
VIAddVersionKey /LANG=1033 "LegalCopyright" "protocol-simulator"

!define UNINSTALL_KEY "Software\Microsoft\Windows\CurrentVersion\Uninstall\protocol-simulator"
!define WEBVIEW_KEY "Software\Microsoft\EdgeUpdate\Clients\{F3017226-FE2A-4295-8BDF-00C3A9A7E4C5}"
!define MUI_ABORTWARNING
!insertmacro MUI_PAGE_WELCOME
!insertmacro MUI_PAGE_INSTFILES
!insertmacro MUI_PAGE_FINISH
!insertmacro MUI_UNPAGE_CONFIRM
!insertmacro MUI_UNPAGE_INSTFILES
!insertmacro MUI_LANGUAGE "SimpChinese"

Var InstanceMutex
Var RuntimePresent

!macro Init prefix initName
Function ${initName}
  SetShellVarContext current
  SetRegView 32
  ${IfNot} ${AtLeastWin10}
  ${OrIfNot} ${RunningX64}
    MessageBox MB_OK|MB_ICONSTOP "需要 Windows 10/11 64 位系统。" /SD IDOK
    SetErrorLevel 2
    Quit
  ${EndIf}
  System::Call 'kernel32::CreateMutexW(p 0, i 0, w "Local\protocol-simulator-Setup") p .r0 ?e'
  Pop $1
  StrCpy $InstanceMutex $0
  ${If} $InstanceMutex == 0
    SetErrorLevel 2
    Quit
  ${EndIf}
  ${If} $1 == 183
    MessageBox MB_OK|MB_ICONEXCLAMATION "另一个安装或卸载进程正在运行。" /SD IDOK
    SetErrorLevel 2
    Quit
  ${EndIf}
  !if "${prefix}" == "un."
    ReadRegStr $2 HKCU "Software\protocol-simulator" "InstallLocation"
    ${If} $2 != $INSTDIR
      MessageBox MB_OK|MB_ICONSTOP "卸载目录与安装记录不一致，请从 Windows 的应用列表卸载。" /SD IDOK
      SetErrorLevel 2
      Quit
    ${EndIf}
  !endif
  Call ${prefix}CheckApplicationClosed
FunctionEnd

Function ${prefix}CheckApplicationClosed
  IfFileExists "$INSTDIR\protocol-simulator.exe" 0 done
  System::Call 'kernel32::CreateFileW(w "$INSTDIR\protocol-simulator.exe", i 0xC0000000, i 0, p 0, i 3, i 128, p 0) p .r0'
  IntCmp $0 -1 in_use opened opened
in_use:
    MessageBox MB_OK|MB_ICONEXCLAMATION "请先保存工程并关闭此安装目录中的所有应用窗口，再重新运行安装或卸载。如已关闭，请检查目录权限。" /SD IDOK
    SetErrorLevel 2
    Quit
opened:
  System::Call 'kernel32::CloseHandle(p r0)'
done:
FunctionEnd
!macroend
!insertmacro Init "" ".onInit"
!insertmacro Init "un." "un.onInit"

Function CheckWebView2
  StrCpy $RuntimePresent 0
  ReadRegStr $0 HKCU "${WEBVIEW_KEY}" "pv"
  ${If} $0 != ""
    ${VersionCompare} "$0" "0.0.0.0" $1
    ${If} $1 == 1
      StrCpy $RuntimePresent 1
      Return
    ${EndIf}
  ${EndIf}
  ReadRegStr $0 HKLM "${WEBVIEW_KEY}" "pv"
  ${If} $0 != ""
    ${VersionCompare} "$0" "0.0.0.0" $1
    ${If} $1 == 1
      StrCpy $RuntimePresent 1
    ${EndIf}
  ${EndIf}
FunctionEnd

Function EnsureWebView2
  Call CheckWebView2
  ${If} $RuntimePresent == 1
    Return
  ${EndIf}
  DetailPrint "正在安装 Microsoft Edge WebView2 Runtime，需要连接网络…"
  InitPluginsDir
  SetOutPath "$PLUGINSDIR"
  File /oname=MicrosoftEdgeWebview2Setup.exe "${WEBVIEW_BOOTSTRAPPER}"
  ExecWait '"$PLUGINSDIR\MicrosoftEdgeWebview2Setup.exe" /silent /install' $0
  DetailPrint "WebView2 安装退出码：$0"
  StrCpy $2 20
runtime_wait:
  Call CheckWebView2
  ${If} $RuntimePresent == 1
    Return
  ${EndIf}
  IntOp $2 $2 - 1
  ${If} $2 > 0
    Sleep 500
    Goto runtime_wait
  ${EndIf}
  ${If} $RuntimePresent != 1
    MessageBox MB_OK|MB_ICONSTOP "WebView2 Runtime 未安装成功。请检查网络或由管理员安装 WebView2 后重试。应用安装尚未执行，已有工程和旧版程序保留。" /SD IDOK
    SetErrorLevel 3
    Quit
  ${EndIf}
FunctionEnd

Section "安装"
  Call CheckApplicationClosed
  Call EnsureWebView2
  ClearErrors
  !include "${INSTALL_FILES}"
  ${If} ${Errors}
    MessageBox MB_OK|MB_ICONSTOP "程序文件未完整写入，请检查磁盘空间和目录权限后重新安装。" /SD IDOK
    SetErrorLevel 4
    Quit
  ${EndIf}
  SetOutPath "$INSTDIR"
  WriteUninstaller "$INSTDIR\Uninstall.exe"
  WriteRegStr HKCU "Software\protocol-simulator" "InstallLocation" "$INSTDIR"
  WriteRegStr HKCU "${UNINSTALL_KEY}" "DisplayName" "protocol-simulator"
  WriteRegStr HKCU "${UNINSTALL_KEY}" "DisplayVersion" "${APP_VERSION}"
  WriteRegStr HKCU "${UNINSTALL_KEY}" "InstallLocation" "$INSTDIR"
  WriteRegStr HKCU "${UNINSTALL_KEY}" "DisplayIcon" "$INSTDIR\protocol-simulator.exe"
  WriteRegStr HKCU "${UNINSTALL_KEY}" "UninstallString" '$\"$INSTDIR\Uninstall.exe$\"'
  WriteRegStr HKCU "${UNINSTALL_KEY}" "QuietUninstallString" '$\"$INSTDIR\Uninstall.exe$\" /S'
  WriteRegDWORD HKCU "${UNINSTALL_KEY}" "NoModify" 1
  WriteRegDWORD HKCU "${UNINSTALL_KEY}" "NoRepair" 1
  CreateDirectory "$SMPROGRAMS\protocol-simulator"
  CreateShortcut "$SMPROGRAMS\protocol-simulator\protocol-simulator.lnk" "$INSTDIR\protocol-simulator.exe"
  CreateShortcut "$SMPROGRAMS\protocol-simulator\卸载.lnk" "$INSTDIR\Uninstall.exe"
  CreateShortcut "$DESKTOP\protocol-simulator.lnk" "$INSTDIR\protocol-simulator.exe"
SectionEnd

Section "Uninstall"
  Call un.CheckApplicationClosed
  !include "${UNINSTALL_FILES}"
  Delete "$DESKTOP\protocol-simulator.lnk"
  Delete "$SMPROGRAMS\protocol-simulator\protocol-simulator.lnk"
  Delete "$SMPROGRAMS\protocol-simulator\卸载.lnk"
  RMDir "$SMPROGRAMS\protocol-simulator"
  DeleteRegKey HKCU "${UNINSTALL_KEY}"
  DeleteRegKey HKCU "Software\protocol-simulator"
  Delete "$INSTDIR\Uninstall.exe"
  ; No recursive removal: user projects, examples and WebView profile survive.
  RMDir "$INSTDIR"
SectionEnd
