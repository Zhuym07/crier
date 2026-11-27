[Setup]
AppId=Crier
AppName=Crier
AppVersion=0.0.0
SetupIconFile=..\brand.ico
AppPublisher=Crier
DefaultDirName={localappdata}\Programs\Crier
DefaultGroupName=Crier
DisableProgramGroupPage=yes
PrivilegesRequired=lowest
ArchitecturesAllowed=x64compatible
ArchitecturesInstallIn64BitMode=x64compatible
OutputDir=..\dist
OutputBaseFilename=Crier-Setup-x64
Compression=lzma2
SolidCompression=yes
CloseApplications=yes
RestartApplications=no

[Files]
Source: "..\build\Crier.exe"; DestDir: "{app}"; Flags: ignoreversion
Source: "..\build\crier-email.exe"; DestDir: "{app}"; Flags: ignoreversion
Source: "..\build\crier-telegram.exe"; DestDir: "{app}"; Flags: ignoreversion

[Icons]
Name: "{autoprograms}\Crier"; Filename: "{app}\Crier.exe"

[Run]
Filename: "{app}\Crier.exe"; Description: "Launch Crier"; Flags: nowait postinstall skipifsilent

[Registry]
Root: HKCU; Subkey: "Software\Microsoft\Windows\CurrentVersion\Run"; ValueType: none; ValueName: "Crier"; Flags: dontcreatekey uninsdeletevalue

[Code]
function WebView2Installed: Boolean;
var Version: String;
begin
  Result := (RegQueryStringValue(HKLM32, 'SOFTWARE\Microsoft\EdgeUpdate\Clients\{F3017226-FE2A-4295-8BDF-00C3A9A7E4C5}', 'pv', Version) and (Version <> '') and (Version <> '0.0.0.0')) or
            (RegQueryStringValue(HKCU, 'SOFTWARE\Microsoft\EdgeUpdate\Clients\{F3017226-FE2A-4295-8BDF-00C3A9A7E4C5}', 'pv', Version) and (Version <> '') and (Version <> '0.0.0.0'));
end;

function PrepareToInstall(var NeedsRestart: Boolean): String;
var ResultCode: Integer;
    Page: TDownloadWizardPage;
begin
  Result := '';
  if WebView2Installed then exit;
  Page := CreateDownloadPage('Installing Microsoft WebView2', 'Crier needs the Microsoft WebView2 Runtime.', nil);
  Page.Add('https://go.microsoft.com/fwlink/p/?LinkId=2124703', 'WebView2Setup.exe', '');
  Page.Show;
  try
    try
      Page.Download;
      if not Exec(ExpandConstant('{tmp}\WebView2Setup.exe'), '/silent /install', '', SW_HIDE, ewWaitUntilTerminated, ResultCode) or (ResultCode <> 0) or not WebView2Installed then
        Result := 'Microsoft WebView2 could not be installed.';
    except
      Result := GetExceptionMessage;
    end;
  finally
    Page.Hide;
  end;
end;
