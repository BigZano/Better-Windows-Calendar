#define MyAppName "PyCalendar"
#define MyAppVersion "0.1.0"
#define MyAppPublisher "Bret Zanotelli"
#define MyAppURL "https://github.com/BigZano/Better-Windows-Calendar"
#define MyAppExeName "pycalendar.exe"

[Setup]
AppId={{B7E3A4D2-9C1F-4E6B-8A5D-0F2C7B3E9A1D}
AppName={#MyAppName}
AppVersion={#MyAppVersion}
AppVerName={#MyAppName} {#MyAppVersion}
AppPublisher={#MyAppPublisher}
AppPublisherURL={#MyAppURL}
AppSupportURL={#MyAppURL}/issues
AppUpdatesURL={#MyAppURL}/releases
DefaultDirName={autopf}\{#MyAppName}
DefaultGroupName={#MyAppName}
AllowNoIcons=yes
LicenseFile=
; Uncomment and set path once you have an icon: SetupIconFile=..\..\assets\icon.ico
OutputDir=..\..\dist
OutputBaseFilename=PyCalendarSetup-v{#MyAppVersion}
Compression=lzma2/ultra64
SolidCompression=yes
WizardStyle=modern
PrivilegesRequired=admin
ArchitecturesInstallIn64BitMode=x64compatible

[Languages]
Name: "english"; MessagesFile: "compiler:Default.isl"

[Tasks]
Name: "desktopicon"; Description: "{cm:CreateDesktopIcon}"; GroupDescription: "{cm:AdditionalIcons}"; Flags: unchecked
Name: "autostart"; Description: "Start PyCalendar when Windows starts"; GroupDescription: "Startup:"; Flags: unchecked

[Files]
Source: "..\..\dist\pycalendar.exe"; DestDir: "{app}"; Flags: ignoreversion
; Add any extra files here (e.g. icon, README)

[Icons]
Name: "{group}\{#MyAppName}"; Filename: "{app}\{#MyAppExeName}"
Name: "{group}\Uninstall {#MyAppName}"; Filename: "{uninstallexe}"
Name: "{commondesktop}\{#MyAppName}"; Filename: "{app}\{#MyAppExeName}"; Tasks: desktopicon

[Registry]
; Autostart via registry (matches what the app's own autostart module writes)
Root: HKCU; Subkey: "Software\Microsoft\Windows\CurrentVersion\Run"; \
  ValueType: string; ValueName: "PyCalendar"; ValueData: """{app}\{#MyAppExeName}"""; \
  Flags: uninsdeletevalue; Tasks: autostart

; --- .ics file association (ProgID + association only) ---
; Windows 10/11 UserChoice blocks forcing the *default* handler programmatically,
; so we register a ProgID and advertise it for .ics; the user opts in via
; "Open with" / Default Apps. HKA routes to HKLM under the admin install (or HKCU
; for a per-user install). uninsdeletekey/value undo everything on uninstall.
;
; ProgID: friendly type name + open command. "%1" is the double-clicked file's
; path, handed to the running tray via single-instance IPC (or a fresh tray).
Root: HKA; Subkey: "Software\Classes\PyCalendar.ics"; \
  ValueType: string; ValueName: ""; ValueData: "iCalendar event"; \
  Flags: uninsdeletekey
Root: HKA; Subkey: "Software\Classes\PyCalendar.ics\DefaultIcon"; \
  ValueType: string; ValueName: ""; ValueData: "{app}\{#MyAppExeName},0"; \
  Flags: uninsdeletekey
Root: HKA; Subkey: "Software\Classes\PyCalendar.ics\shell\open\command"; \
  ValueType: string; ValueName: ""; ValueData: """{app}\{#MyAppExeName}"" ""%1"""; \
  Flags: uninsdeletekey
; Associate the .ics extension with our ProgID without seizing the default:
; OpenWithProgids adds PyCalendar.ics to the "Open with" list for .ics files.
Root: HKA; Subkey: "Software\Classes\.ics\OpenWithProgids"; \
  ValueType: string; ValueName: "PyCalendar.ics"; ValueData: ""; \
  Flags: uninsdeletevalue

[Run]
Filename: "{app}\{#MyAppExeName}"; Description: "{cm:LaunchProgram,{#StringChange(MyAppName, '&', '&&')}}"; \
  Flags: nowait postinstall skipifsilent

[UninstallRun]
; Ask the app to clean up keychain entries and remove autostart before the installer
; removes the executable.
Filename: "{app}\{#MyAppExeName}"; Parameters: "--mode uninstall"; \
  Flags: runhidden waituntilterminated
