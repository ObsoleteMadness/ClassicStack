; ClassicStack — Windows installer (Inno Setup 6.x).
;
; Installs every command-line tool (classicstack, classicstack-svc, csmount,
; csclient, csecho, csgetzones, csipxping, csnbp, csncpinfo, csnetsend,
; csnetview) into Program Files, and offers four independent opt-in tasks:
;   - service  register + start classicstack-svc as a Windows service
;   - npcap    silently install Npcap (needed for EtherTalk/IPX/NetBEUI, which
;              talk to Ethernet via raw pcap capture)
;   - winfsp   silently install WinFsp (needed for csmount to mount AFP/SMB/NCP
;              shares as local drives)
;   - tray     start classicstack-tray at sign-in (per-user, HKCU Run key) —
;              it monitors the Windows service over the control API if one is
;              installed, otherwise self-starts classicstack-svc.exe under
;              the signed-in user with its own config under %LOCALAPPDATA%
;              (see cmd/classicstack-tray/launcher_windows.go)
;
; Configuration (server.toml, extmap.conf, sample share folders) lives under
; CommonApplicationData (C:\ProgramData\ClassicStack) rather than per-user
; AppData or Program Files, since the service normally runs as LocalSystem
; and needs a machine-wide, writable location every account can reach — see
; cmd/classicstack-svc/main_windows.go's runService, which os.Chdir()s into
; that directory (the SCM always starts services with CWD = System32, so
; relative paths like extmap.conf's default would otherwise resolve there
; instead).
;
; Binaries are expected in ..\..\bin (scripts/build-local.sh's default
; output directory, or packaging\windows\build.ps1's) — see build.ps1 in this
; directory for a one-shot build+compile helper.
;
; Build: iscc ClassicStack.iss  (optionally /DMyAppVersion=1.2.3)

#define MyAppName "ClassicStack"
#define MyAppPublisher "ObsoleteMadness"
#define MyAppURL "https://github.com/ObsoleteMadness/ClassicStack"
#ifndef MyAppVersion
  #define MyAppVersion "0.0.0-dev"
#endif
#define MyAppExeName "classicstack.exe"
#define SourceBinDir "..\..\bin"
#define TemplatesDir "templates"
#define RedistDir "redist"
#define ConfigDirName "ClassicStack"
#define TrayExe SourceBinDir + "\classicstack-tray.exe"
#define NpcapInstaller RedistDir + "\npcap-installer.exe"
#define WinFspInstaller RedistDir + "\winfsp-installer.msi"

[Setup]
AppId={{87098CC0-A5E4-4A90-8260-2B5922F61927}}
AppName={#MyAppName}
AppVersion={#MyAppVersion}
AppPublisher={#MyAppPublisher}
AppPublisherURL={#MyAppURL}
AppSupportURL={#MyAppURL}
AppUpdatesURL={#MyAppURL}
DefaultDirName={autopf}\{#MyAppName}
DefaultGroupName={#MyAppName}
DisableProgramGroupPage=yes
UsePreviousAppDir=yes
UsePreviousTasks=yes
LicenseFile=..\..\LICENSE
OutputDir=Output
OutputBaseFilename=ClassicStack-Setup-{#MyAppVersion}
SetupIconFile=..\..\icons\classicstack.ico
UninstallDisplayIcon={app}\{#MyAppExeName}
Compression=lzma2/max
SolidCompression=yes
WizardStyle=modern
ArchitecturesAllowed=x64compatible
ArchitecturesInstallIn64BitMode=x64compatible
PrivilegesRequired=admin
CloseApplications=yes
RestartApplications=yes

[Languages]
Name: "english"; MessagesFile: "compiler:Default.isl"

[Tasks]
Name: "service"; Description: "Install and start the ClassicStack Windows service (runs at boot, before sign-in)"
Name: "addpath"; Description: "Add ClassicStack to the system PATH (so csclient, csmount, etc. work from any Command Prompt)"
#ifexist NpcapInstaller
Name: "npcap"; Description: "Install Npcap (required for EtherTalk/IPX/NetBEUI over Ethernet)"; Check: not IsNpcapInstalled
#endif
#ifexist WinFspInstaller
Name: "winfsp"; Description: "Install WinFsp (required for csmount to mount AFP/SMB/NCP shares as drives)"; Check: not IsWinFspInstalled
#endif
#ifexist TrayExe
Name: "tray"; Description: "Start ClassicStack Tray at sign-in (monitors the Windows service if installed, otherwise runs ClassicStack itself under your account)"; Flags: unchecked
#endif

[Files]
; Command-line tools — always installed.
Source: "{#SourceBinDir}\classicstack.exe";     DestDir: "{app}"; Flags: ignoreversion
Source: "{#SourceBinDir}\classicstack-svc.exe"; DestDir: "{app}"; Flags: ignoreversion
Source: "{#SourceBinDir}\csmount.exe";           DestDir: "{app}"; Flags: ignoreversion
Source: "{#SourceBinDir}\csclient.exe";          DestDir: "{app}"; Flags: ignoreversion
Source: "{#SourceBinDir}\csecho.exe";            DestDir: "{app}"; Flags: ignoreversion
Source: "{#SourceBinDir}\csgetzones.exe";        DestDir: "{app}"; Flags: ignoreversion
Source: "{#SourceBinDir}\csipxping.exe";         DestDir: "{app}"; Flags: ignoreversion
Source: "{#SourceBinDir}\csnbp.exe";             DestDir: "{app}"; Flags: ignoreversion
Source: "{#SourceBinDir}\csncpinfo.exe";         DestDir: "{app}"; Flags: ignoreversion
Source: "{#SourceBinDir}\csnetsend.exe";         DestDir: "{app}"; Flags: ignoreversion
Source: "{#SourceBinDir}\csnetview.exe";         DestDir: "{app}"; Flags: ignoreversion

#ifexist TrayExe
Source: "{#TrayExe}"; DestDir: "{app}"; Flags: ignoreversion
#endif

; Reference docs, copied alongside the binaries.
Source: "..\..\README.md";              DestDir: "{app}"; Flags: ignoreversion
Source: "..\..\server.toml.example";    DestDir: "{app}"; Flags: ignoreversion

; CommonApplicationData: seeded once, never overwritten on upgrade/reinstall
; so hand-edited config and web-admin Saves survive.
Source: "..\..\extmap.conf";                    DestDir: "{commonappdata}\{#ConfigDirName}"; Flags: onlyifdoesntexist
Source: "{#TemplatesDir}\server.toml";           DestDir: "{commonappdata}\{#ConfigDirName}"; Flags: onlyifdoesntexist
Source: "{#TemplatesDir}\Volumes\Public\*";      DestDir: "{commonappdata}\{#ConfigDirName}\Volumes\Public"; Flags: onlyifdoesntexist recursesubdirs createallsubdirs
Source: "{#TemplatesDir}\Volumes\SYS\*";         DestDir: "{commonappdata}\{#ConfigDirName}\Volumes\SYS";    Flags: onlyifdoesntexist recursesubdirs createallsubdirs
Source: "{#TemplatesDir}\Volumes\DOS\*";         DestDir: "{commonappdata}\{#ConfigDirName}\Volumes\DOS";    Flags: onlyifdoesntexist recursesubdirs createallsubdirs

; Redistributables — DestDir {tmp} auto-extracts these during the file-copy
; phase (before ssPostInstall runs them) and Setup cleans {tmp} up itself, so
; they never end up left behind in {app}.
#ifexist NpcapInstaller
Source: "{#NpcapInstaller}"; DestDir: "{tmp}"
#endif
#ifexist WinFspInstaller
Source: "{#WinFspInstaller}"; DestDir: "{tmp}"
#endif

[Dirs]
Name: "{commonappdata}\{#ConfigDirName}";         Permissions: users-modify
Name: "{commonappdata}\{#ConfigDirName}\Volumes"; Permissions: users-modify

[Icons]
Name: "{group}\ClassicStack"; Filename: "{app}\classicstack.exe"; Parameters: "-config ""{commonappdata}\{#ConfigDirName}\server.toml"""
Name: "{group}\ClassicStack Configuration"; Filename: "{win}\explorer.exe"; Parameters: """{commonappdata}\{#ConfigDirName}"""
Name: "{group}\Uninstall ClassicStack"; Filename: "{uninstallexe}"
#ifexist TrayExe
Name: "{group}\ClassicStack Tray"; Filename: "{app}\classicstack-tray.exe"; Tasks: tray
#endif

[Registry]
#ifexist TrayExe
Root: HKCU; Subkey: "Software\Microsoft\Windows\CurrentVersion\Run"; ValueType: string; ValueName: "ClassicStackTray"; ValueData: """{app}\classicstack-tray.exe"""; Tasks: tray; Flags: uninsdeletevalue
#endif

[Run]
#ifexist TrayExe
Filename: "{app}\classicstack-tray.exe"; Description: "Start ClassicStack Tray now"; Flags: nowait postinstall skipifsilent runasoriginaluser; Tasks: tray
#endif

[Code]
const
  ServiceName = 'ClassicStack';

// --- Third-party install detection ------------------------------------
// Npcap and WinFsp both register an Uninstall entry; scanning both the
// native and WOW6432Node Uninstall hives by DisplayName substring avoids
// hardcoding either project's exact key name/GUID, which changes per
// version.
function IsProductInstalled(const NameSubstring: string): Boolean;
var
  Bases: TArrayOfString;
  Keys: TArrayOfString;
  I, J: Integer;
  DisplayName: string;
begin
  Result := False;
  SetArrayLength(Bases, 2);
  Bases[0] := 'SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall';
  Bases[1] := 'SOFTWARE\WOW6432Node\Microsoft\Windows\CurrentVersion\Uninstall';
  for I := 0 to GetArrayLength(Bases) - 1 do
  begin
    if RegGetSubkeyNames(HKEY_LOCAL_MACHINE, Bases[I], Keys) then
    begin
      for J := 0 to GetArrayLength(Keys) - 1 do
      begin
        if RegQueryStringValue(HKEY_LOCAL_MACHINE, Bases[I] + '\' + Keys[J], 'DisplayName', DisplayName) then
        begin
          if Pos(Lowercase(NameSubstring), Lowercase(DisplayName)) > 0 then
          begin
            Result := True;
            Exit;
          end;
        end;
      end;
    end;
  end;
end;

function IsNpcapInstalled: Boolean;
begin
  Result := IsProductInstalled('Npcap');
end;

function IsWinFspInstalled: Boolean;
begin
  Result := IsProductInstalled('WinFsp');
end;

// --- System PATH -------------------------------------------------------
// Reopen any already-open Command Prompt/PowerShell window to pick this up;
// new ones read the registry fresh, so no WM_SETTINGCHANGE broadcast needed.
function GetSystemPath: string;
var
  Value: string;
begin
  if not RegQueryStringValue(HKEY_LOCAL_MACHINE, 'SYSTEM\CurrentControlSet\Control\Session Manager\Environment', 'Path', Value) then
    Value := '';
  Result := Value;
end;

procedure EnvAddPath(const Dir: string);
var
  Path: string;
begin
  Path := GetSystemPath;
  if (Path <> '') and (Pos(Lowercase(';' + Dir + ';'), Lowercase(';' + Path + ';')) > 0) then
    Exit; // already present
  if (Path <> '') and (Path[Length(Path)] <> ';') then
    Path := Path + ';';
  Path := Path + Dir;
  RegWriteExpandStringValue(HKEY_LOCAL_MACHINE, 'SYSTEM\CurrentControlSet\Control\Session Manager\Environment', 'Path', Path);
end;

procedure EnvRemovePath(const Dir: string);
var
  Path, Needle: string;
  P: Integer;
begin
  Path := GetSystemPath;
  if Path = '' then
    Exit;
  Needle := Dir + ';';
  P := Pos(Lowercase(Needle), Lowercase(Path));
  if P > 0 then
  begin
    Delete(Path, P, Length(Needle));
  end
  else
  begin
    Needle := ';' + Dir;
    P := Pos(Lowercase(Needle), Lowercase(Path));
    if P > 0 then
      Delete(Path, P, Length(Needle))
    else if Lowercase(Path) = Lowercase(Dir) then
      Path := '';
  end;
  RegWriteExpandStringValue(HKEY_LOCAL_MACHINE, 'SYSTEM\CurrentControlSet\Control\Session Manager\Environment', 'Path', Path);
end;

// --- Windows service -----------------------------------------------------
procedure StopAndRemoveExistingService;
var
  ResultCode: Integer;
begin
  // Idempotent teardown of any previous registration: releases the file lock
  // on classicstack-svc.exe so [Files] can overwrite it, and clears the way
  // for a clean `install` below. Errors here just mean there was nothing to
  // tear down (fresh install) — ignored either way.
  Exec('sc.exe', 'stop ' + ServiceName, '', SW_HIDE, ewWaitUntilTerminated, ResultCode);
  Sleep(1500);
  Exec('sc.exe', 'delete ' + ServiceName, '', SW_HIDE, ewWaitUntilTerminated, ResultCode);
end;

procedure InstallAndStartService(const ConfigPath: string);
var
  ResultCode: Integer;
  ExePath: string;
begin
  ExePath := ExpandConstant('{app}\classicstack-svc.exe');
  if not Exec(ExePath, 'install -config "' + ConfigPath + '"', '', SW_HIDE, ewWaitUntilTerminated, ResultCode) or (ResultCode <> 0) then
  begin
    MsgBox('Installing the ClassicStack service failed (exit code ' + IntToStr(ResultCode) + '). ' +
           'You can retry later from an elevated Command Prompt:'#13#10 +
           '"' + ExePath + '" install -config "' + ConfigPath + '"', mbError, MB_OK);
    Exit;
  end;
  if not Exec(ExePath, 'start', '', SW_HIDE, ewWaitUntilTerminated, ResultCode) or (ResultCode <> 0) then
    MsgBox('The ClassicStack service was installed but did not start (exit code ' + IntToStr(ResultCode) + '). ' +
           'Check Event Viewer (Application log, source "ClassicStack") or run:'#13#10 +
           '"' + ExePath + '" start', mbError, MB_OK);
end;

procedure ConfigureFirewall(Add: Boolean);
var
  ResultCode: Integer;
  Params: string;
begin
  if Add then
    Params := 'advfirewall firewall add rule name="ClassicStack" dir=in action=allow ' +
      'program="' + ExpandConstant('{app}\classicstack-svc.exe') + '" enable=yes profile=any ' +
      'description="ClassicStack AppleTalk/AFP/SMB/NCP file and network services"'
  else
    Params := 'advfirewall firewall delete rule name="ClassicStack"';
  Exec('netsh.exe', Params, '', SW_HIDE, ewWaitUntilTerminated, ResultCode);
end;

// --- Starter config -------------------------------------------------------
// Rewrites the __VOLUMES__ placeholder in the seeded server.toml to this
// install's actual Volumes path, forward-slashed to avoid TOML backslash
// escaping. A no-op (Exists check) on upgrades where server.toml already had
// this done, or was hand-edited and no longer contains the placeholder.
procedure ResolveVolumesPlaceholder;
var
  ConfigFile, Contents, VolumesPath: string;
begin
  ConfigFile := ExpandConstant('{commonappdata}\{#ConfigDirName}\server.toml');
  if not FileExists(ConfigFile) then
    Exit;
  if not LoadStringFromFile(ConfigFile, Contents) then
    Exit;
  if Pos('__VOLUMES__', Contents) = 0 then
    Exit;
  VolumesPath := ExpandConstant('{commonappdata}\{#ConfigDirName}\Volumes');
  StringChangeEx(VolumesPath, '\', '/', True);
  StringChangeEx(Contents, '__VOLUMES__', VolumesPath, True);
  SaveStringToFile(ConfigFile, Contents, False);
end;

// --- Wizard/step wiring ----------------------------------------------------
procedure CurStepChanged(CurStep: TSetupStep);
var
  ConfigPath: string;
  ResultCode: Integer;
begin
  if CurStep = ssInstall then
  begin
    StopAndRemoveExistingService;
  end;
  if CurStep = ssPostInstall then
  begin
    ResolveVolumesPlaceholder;

    if IsTaskSelected('addpath') then
      EnvAddPath(ExpandConstant('{app}'));

#ifexist NpcapInstaller
    if IsTaskSelected('npcap') then
      Exec(ExpandConstant('{tmp}\npcap-installer.exe'), '/S', '', SW_HIDE, ewWaitUntilTerminated, ResultCode);
#endif
#ifexist WinFspInstaller
    if IsTaskSelected('winfsp') then
      Exec('msiexec.exe', '/i "' + ExpandConstant('{tmp}\winfsp-installer.msi') + '" /quiet /norestart', '', SW_HIDE, ewWaitUntilTerminated, ResultCode);
#endif

    if IsTaskSelected('service') then
    begin
      ConfigPath := ExpandConstant('{commonappdata}\{#ConfigDirName}\server.toml');
      InstallAndStartService(ConfigPath);
      ConfigureFirewall(True);
    end;
  end;
end;

procedure CurUninstallStepChanged(CurUninstallStep: TUninstallStep);
begin
  if CurUninstallStep = usUninstall then
  begin
    StopAndRemoveExistingService;
    ConfigureFirewall(False);
    EnvRemovePath(ExpandConstant('{app}'));
  end;
end;

procedure CurPageChanged(CurPageID: Integer);
var
  Note: string;
begin
  if CurPageID = wpFinished then
  begin
    Note := '';
#ifnexist NpcapInstaller
    Note := Note + 'Npcap was not bundled with this installer. EtherTalk/IPX/NetBEUI need it - get it from https://npcap.com/#download.' + #13#10#13#10;
#endif
#ifnexist WinFspInstaller
    Note := Note + 'WinFsp was not bundled with this installer. csmount needs it to mount shares as drives - get it from https://winfsp.dev/rel/.' + #13#10#13#10;
#endif
    if Note <> '' then
      WizardForm.FinishedLabel.Caption := WizardForm.FinishedLabel.Caption + #13#10#13#10 + Note;
  end;
end;
