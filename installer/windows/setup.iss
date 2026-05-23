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

[Run]
Filename: "{app}\{#MyAppExeName}"; Description: "{cm:LaunchProgram,{#StringChange(MyAppName, '&', '&&')}}"; \
  Flags: nowait postinstall skipifsilent

[UninstallRun]
; Ask the app to clean up keychain entries and remove autostart before the installer
; removes the executable.
Filename: "{app}\{#MyAppExeName}"; Parameters: "--mode uninstall"; \
  Flags: runhidden waituntilterminated
