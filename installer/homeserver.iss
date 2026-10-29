; HomeServer installer (Inno Setup 6). Build: ISCC.exe /DAppVersion=1.0.0 installer\homeserver.iss
#ifndef AppVersion
  #define AppVersion "0.0.0-dev"
#endif
#ifndef SourceDir
  #define SourceDir "..\dist"
#endif

[Setup]
; Constant AppId: installing a newer build over an older one upgrades in place.
AppId={{6E2F0D5A-3B7C-4C11-9E55-4A0B1C2D3E4F}
AppName=HomeServer
AppVersion={#AppVersion}
AppPublisher=Abhijith KM
AppPublisherURL=https://github.com/abhijithkm/homeserver
DefaultDirName={autopf}\HomeServer
DefaultGroupName=HomeServer
DisableProgramGroupPage=yes
PrivilegesRequired=admin
ArchitecturesAllowed=x64compatible
ArchitecturesInstallIn64BitMode=x64compatible
OutputDir=..\release
OutputBaseFilename=HomeServer-Setup-{#AppVersion}
Compression=lzma2
SolidCompression=yes
WizardStyle=modern
UninstallDisplayName=HomeServer
LicenseFile=..\LICENSE
CloseApplications=yes
RestartApplications=no
; Broadcast the PATH change so new terminals see it without a sign-out.
ChangesEnvironment=yes

[Tasks]
Name: "addtopath"; Description: "Add HomeServer to the system PATH (so you can run ""homeserver"" from any terminal)"; GroupDescription: "Convenience:"
Name: "firewall"; Description: "Allow other devices on my home network to connect (adds a Windows Firewall rule for Private networks only)"; GroupDescription: "Network access:"
Name: "tray"; Description: "Show a tray icon with server status when I sign in (optional, uses about 20 MB of RAM)"; GroupDescription: "Convenience:"; Flags: unchecked

[Files]
Source: "{#SourceDir}\homeserver.exe"; DestDir: "{app}"; Flags: ignoreversion
Source: "{#SourceDir}\homeserver-tray.exe"; DestDir: "{app}"; Flags: ignoreversion
Source: "..\README.md"; DestDir: "{app}"; Flags: ignoreversion
Source: "..\LICENSE"; DestDir: "{app}"; Flags: ignoreversion

[Registry]
Root: HKLM; Subkey: "SYSTEM\CurrentControlSet\Control\Session Manager\Environment"; ValueType: expandsz; ValueName: "Path"; ValueData: "{olddata};{app}"; Tasks: addtopath; Check: NeedsAddPath

[Icons]
Name: "{group}\HomeServer tray"; Filename: "{app}\homeserver-tray.exe"
Name: "{commonstartup}\HomeServer tray"; Filename: "{app}\homeserver-tray.exe"; Tasks: tray

[Run]
Filename: "{app}\homeserver.exe"; Parameters: "install"; Flags: runhidden waituntilterminated; StatusMsg: "Installing and starting the HomeServer service..."
Filename: "{app}\homeserver.exe"; Parameters: "firewall add"; Flags: runhidden waituntilterminated; Tasks: firewall; StatusMsg: "Configuring Windows Firewall (Private networks)..."
Filename: "{app}\homeserver-tray.exe"; Flags: nowait postinstall skipifsilent runasoriginaluser; Description: "Start the tray icon now"; Tasks: tray
Filename: "http://localhost:8686/"; Flags: shellexec postinstall skipifsilent runasoriginaluser; Description: "Open HomeServer to finish setup"

[UninstallDelete]
Type: filesandordirs; Name: "{app}\updates"

[UninstallRun]
Filename: "{app}\homeserver.exe"; Parameters: "uninstall"; Flags: runhidden waituntilterminated; RunOnceId: "RemoveService"
Filename: "{app}\homeserver.exe"; Parameters: "firewall remove"; Flags: runhidden waituntilterminated; RunOnceId: "RemoveFirewall"

[Code]
const
  EnvKey = 'SYSTEM\CurrentControlSet\Control\Session Manager\Environment';

// True when the install folder is not yet in the system PATH (no duplicates on upgrade).
function NeedsAddPath: Boolean;
var
  paths, dir: String;
begin
  if not RegQueryStringValue(HKLM, EnvKey, 'Path', paths) then
    paths := '';
  dir := ExpandConstant('{app}');
  paths := ';' + Lowercase(paths) + ';';
  Result := (Pos(';' + Lowercase(dir) + ';', paths) = 0) and
            (Pos(';' + Lowercase(dir) + '\;', paths) = 0);
end;

// Removes the install folder from the system PATH, leaving every other entry untouched.
procedure RemoveFromPath;
var
  paths, dir: String;
begin
  if not RegQueryStringValue(HKLM, EnvKey, 'Path', paths) then
    Exit;
  dir := ExpandConstant('{app}');
  paths := ';' + paths + ';';
  while StringChangeEx(paths, ';' + dir + ';', ';', True) > 0 do;
  while StringChangeEx(paths, ';' + dir + '\;', ';', True) > 0 do;
  if (Length(paths) > 0) and (paths[1] = ';') then
    Delete(paths, 1, 1);
  if (Length(paths) > 0) and (paths[Length(paths)] = ';') then
    Delete(paths, Length(paths), 1);
  RegWriteExpandStringValue(HKLM, EnvKey, 'Path', paths);
end;

function InitializeSetup(): Boolean;
var
  rc: Integer;
begin
  // Upgrade path: stop the running service and tray icon so files can be replaced.
  Exec(ExpandConstant('{sys}\net.exe'), 'stop HomeServer', '', SW_HIDE, ewWaitUntilTerminated, rc);
  Exec(ExpandConstant('{sys}\taskkill.exe'), '/F /IM homeserver-tray.exe', '', SW_HIDE, ewWaitUntilTerminated, rc);
  Result := True;
end;

procedure CurUninstallStepChanged(CurUninstallStep: TUninstallStep);
var
  dataDir: String;
  rc: Integer;
begin
  if CurUninstallStep = usUninstall then
    RemoveFromPath;
  if CurUninstallStep = usPostUninstall then
  begin
    dataDir := ExpandConstant('{commonappdata}\HomeServer');
    if DirExists(dataDir) then
      if MsgBox('Also delete HomeServer settings, accounts and logs?' + #13#10 + dataDir + #13#10#13#10 +
                'Your shared files are NOT affected. Choose No to keep your configuration for a later reinstall.',
                mbConfirmation, MB_YESNO or MB_DEFBUTTON2) = IDYES then
        Exec(ExpandConstant('{sys}\cmd.exe'), '/C rd /S /Q "' + dataDir + '"', '', SW_HIDE, ewWaitUntilTerminated, rc);
  end;
end;
