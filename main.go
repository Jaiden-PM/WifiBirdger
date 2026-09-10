//go:build windows

package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

const (
	appName    = "WiFiEthernetBridge"
	taskName   = "WiFi to Ethernet Bridge"
	githubRepo = "Jaiden-PM/WifiBirdger"

	WM_CREATE          = 0x0001
	WM_DESTROY         = 0x0002
	WM_PAINT           = 0x000F
	WM_ERASEBKGND      = 0x0014
	WM_DRAWITEM        = 0x002B
	WM_COMMAND         = 0x0111
	WM_TIMER           = 0x0113
	WM_CLOSE           = 0x0010
	WM_SETFONT         = 0x0030
	WM_CTLCOLORSTATIC  = 0x0138
	WM_CTLCOLOREDIT    = 0x0133
	WM_CTLCOLORLISTBOX = 0x0134
	WM_CTLCOLORBTN     = 0x0135
	WM_APP_UPDATE      = 0x8001

	WS_OVERLAPPED  = 0x00000000
	WS_CAPTION     = 0x00C00000
	WS_SYSMENU     = 0x00080000
	WS_MINIMIZEBOX = 0x00020000
	WS_VISIBLE     = 0x10000000
	WS_CHILD       = 0x40000000
	WS_TABSTOP     = 0x00010000
	WS_VSCROLL     = 0x00200000
	WS_BORDER      = 0x00800000

	BS_PUSHBUTTON    = 0x00000000
	BS_AUTOCHECKBOX  = 0x00000003
	BS_OWNERDRAW     = 0x0000000B
	BS_FLAT          = 0x00008000
	ES_LEFT          = 0x0000
	ES_MULTILINE     = 0x0004
	ES_AUTOVSCROLL   = 0x0040
	ES_READONLY      = 0x0800
	CBS_DROPDOWNLIST = 0x0003

	SW_SHOW          = 5
	SW_SHOWNORMAL    = 1
	CW_USEDEFAULT    = ^uintptr(0x7fffffff)
	COLOR_WINDOW     = 5
	DEFAULT_GUI_FONT = 17
	HOLLOW_BRUSH     = 5
	FW_NORMAL        = 400
	FW_SEMIBOLD      = 600
	FW_BOLD          = 700
	TRANSPARENT      = 1
	PS_SOLID         = 0
	IDI_APPLICATION  = 32512
	IDC_ARROW        = 32512

	CB_ADDSTRING    = 0x0143
	CB_RESETCONTENT = 0x014B
	CB_GETCURSEL    = 0x0147
	CB_GETLBTEXT    = 0x0148
	CB_GETLBTEXTLEN = 0x0149
	CB_SETCURSEL    = 0x014E
	CBN_SELCHANGE   = 1

	BM_GETCHECK   = 0x00F0
	BM_SETCHECK   = 0x00F1
	BST_CHECKED   = 1
	BST_UNCHECKED = 0

	EM_SETSEL     = 0x00B1
	EM_REPLACESEL = 0x00C2

	BN_CLICKED    = 0
	ODS_SELECTED  = 0x0001
	DT_CENTER     = 0x00000001
	DT_VCENTER    = 0x00000004
	DT_SINGLELINE = 0x00000020

	ID_WIFI         = 1001
	ID_ETHERNET     = 1002
	ID_AUTODETECT   = 1003
	ID_AUTOSTART    = 1004
	ID_REFRESH      = 1005
	ID_START        = 1006
	ID_REPAIR       = 1007
	ID_STOP         = 1008
	ID_STATUS       = 1009
	ID_LOG          = 1010
	ID_AUTOUPDATE   = 1011
	ID_CHECKUPDATE  = 1012
	ID_UPDATESTATUS = 1013
	ID_AUTOREPAIR   = 1014
	ID_TESTCONN     = 1015
	ID_COPYDIAG     = 1016
	ID_OPENSETTINGS = 1017
	ID_OPENLOG      = 1018
	ID_CLEARLOG     = 1019
	ID_TOOLSTATUS   = 1020

	PROCESS_QUERY_LIMITED_INFORMATION = 0x1000
	CREATE_NO_WINDOW                  = 0x08000000
)

var currentVersion = "1.4.0"

type POINT struct{ X, Y int32 }
type RECT struct{ Left, Top, Right, Bottom int32 }
type PAINTSTRUCT struct {
	Hdc         syscall.Handle
	FErase      int32
	RcPaint     RECT
	FRestore    int32
	FIncUpdate  int32
	RgbReserved [32]byte
}
type DRAWITEMSTRUCT struct {
	CtlType    uint32
	CtlID      uint32
	ItemID     uint32
	ItemAction uint32
	ItemState  uint32
	HwndItem   syscall.Handle
	HDC        syscall.Handle
	RcItem     RECT
	ItemData   uintptr
}
type MSG struct {
	Hwnd    syscall.Handle
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      POINT
}
type WNDCLASSEX struct {
	CbSize        uint32
	Style         uint32
	LpfnWndProc   uintptr
	CbClsExtra    int32
	CbWndExtra    int32
	HInstance     syscall.Handle
	HIcon         syscall.Handle
	HCursor       syscall.Handle
	HbrBackground syscall.Handle
	LpszMenuName  *uint16
	LpszClassName *uint16
	HIconSm       syscall.Handle
}

type Config struct {
	WiFi                  string `json:"wifi"`
	Ethernet              string `json:"ethernet"`
	AutoDetect            bool   `json:"autoDetect"`
	CheckIntervalSeconds  int    `json:"checkIntervalSeconds"`
	StartupWaitSeconds    int    `json:"startupWaitSeconds"`
	RepairCooldownSeconds int    `json:"repairCooldownSeconds"`
	AutoRepair            bool   `json:"autoRepair"`
	AutoUpdate            bool   `json:"autoUpdate"`
	UpdateIntervalHours   int    `json:"updateIntervalHours"`
}

type GitHubReleaseAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Digest             string `json:"digest"`
	Size               int64  `json:"size"`
}

type GitHubRelease struct {
	TagName string               `json:"tag_name"`
	HTMLURL string               `json:"html_url"`
	Assets  []GitHubReleaseAsset `json:"assets"`
}

type Adapter struct {
	Name        string
	Description string
	Status      string
}

type Health struct {
	WiFiName              string
	WiFiStatus            string
	WiFiIPv4              string
	WiFiHasDefaultRoute   bool
	EthernetName          string
	EthernetStatus        string
	EthernetIPv4          string
	PublicSharingEnabled  bool
	PublicSharingType     int
	PrivateSharingEnabled bool
	PrivateSharingType    int
	Healthy               bool
}

type Telemetry struct {
	SSID              string
	SignalPercent     int
	WiFiLinkSpeed     string
	EthernetLinkSpeed string
	Gateway           string
	DNS               string
	ConnectedDevices  []string
	PublicIP          string
}

type ConnectivityResult struct {
	RouteOK      bool
	DNSOK        bool
	HTTPSOK      bool
	Latency      time.Duration
	ResolvedHost string
	PublicIP     string
}

var (
	user32   = syscall.NewLazyDLL("user32.dll")
	kernel32 = syscall.NewLazyDLL("kernel32.dll")
	gdi32    = syscall.NewLazyDLL("gdi32.dll")
	shell32  = syscall.NewLazyDLL("shell32.dll")
	dwmapi   = syscall.NewLazyDLL("dwmapi.dll")
	uxtheme  = syscall.NewLazyDLL("uxtheme.dll")

	procCreateWindowExW     = user32.NewProc("CreateWindowExW")
	procDefWindowProcW      = user32.NewProc("DefWindowProcW")
	procDestroyWindow       = user32.NewProc("DestroyWindow")
	procDispatchMessage     = user32.NewProc("DispatchMessageW")
	procGetMessage          = user32.NewProc("GetMessageW")
	procLoadCursor          = user32.NewProc("LoadCursorW")
	procLoadIcon            = user32.NewProc("LoadIconW")
	procPostQuitMessage     = user32.NewProc("PostQuitMessage")
	procRegisterClassEx     = user32.NewProc("RegisterClassExW")
	procShowWindow          = user32.NewProc("ShowWindow")
	procTranslateMsg        = user32.NewProc("TranslateMessage")
	procUpdateWindow        = user32.NewProc("UpdateWindow")
	procSendMessage         = user32.NewProc("SendMessageW")
	procPostMessage         = user32.NewProc("PostMessageW")
	procSetWindowText       = user32.NewProc("SetWindowTextW")
	procGetWindowText       = user32.NewProc("GetWindowTextW")
	procGetWindowTextLength = user32.NewProc("GetWindowTextLengthW")
	procSetTimer            = user32.NewProc("SetTimer")
	procKillTimer           = user32.NewProc("KillTimer")
	procBeginPaint          = user32.NewProc("BeginPaint")
	procEndPaint            = user32.NewProc("EndPaint")
	procInvalidateRect      = user32.NewProc("InvalidateRect")

	procGetModuleHandle       = kernel32.NewProc("GetModuleHandleW")
	procGetStockObject        = gdi32.NewProc("GetStockObject")
	procCreateSolidBrush      = gdi32.NewProc("CreateSolidBrush")
	procCreatePen             = gdi32.NewProc("CreatePen")
	procCreateFont            = gdi32.NewProc("CreateFontW")
	procSelectObject          = gdi32.NewProc("SelectObject")
	procDeleteObject          = gdi32.NewProc("DeleteObject")
	procRoundRect             = gdi32.NewProc("RoundRect")
	procRectangle             = gdi32.NewProc("Rectangle")
	procSetBkColor            = gdi32.NewProc("SetBkColor")
	procSetBkMode             = gdi32.NewProc("SetBkMode")
	procSetTextColor          = gdi32.NewProc("SetTextColor")
	procDrawText              = user32.NewProc("DrawTextW")
	procShellExecute          = shell32.NewProc("ShellExecuteW")
	procIsUserAnAdmin         = shell32.NewProc("IsUserAnAdmin")
	procDwmSetWindowAttribute = dwmapi.NewProc("DwmSetWindowAttribute")
	procSetWindowTheme        = uxtheme.NewProc("SetWindowTheme")

	hwndMain        syscall.Handle
	hWiFi           syscall.Handle
	hEthernet       syscall.Handle
	hAutoDetect     syscall.Handle
	hAutoStart      syscall.Handle
	hRefresh        syscall.Handle
	hStart          syscall.Handle
	hRepair         syscall.Handle
	hStop           syscall.Handle
	hStatus         syscall.Handle
	hHealthTitle    syscall.Handle
	hWiFiMetric     syscall.Handle
	hEthernetMetric syscall.Handle
	hShareMetric    syscall.Handle
	hLog            syscall.Handle
	hAutoUpdate     syscall.Handle
	hCheckUpdate    syscall.Handle
	hUpdateStatus   syscall.Handle
	hSubtitle       syscall.Handle
	hVersion        syscall.Handle
	hSectionNetwork syscall.Handle
	hSectionDetails syscall.Handle
	hSectionTools   syscall.Handle
	hSectionUpdates syscall.Handle
	hSectionLog     syscall.Handle
	hAutoRepair     syscall.Handle
	hSSIDValue      syscall.Handle
	hSignalValue    syscall.Handle
	hWiFiSpeedValue syscall.Handle
	hEthSpeedValue  syscall.Handle
	hDevicesValue   syscall.Handle
	hPublicIPValue  syscall.Handle
	hGatewayValue   syscall.Handle
	hTestConn       syscall.Handle
	hCopyDiag       syscall.Handle
	hOpenSettings   syscall.Handle
	hOpenLog        syscall.Handle
	hClearLog       syscall.Handle
	hToolStatus     syscall.Handle

	uiQueue = make(chan func(), 64)
	opMu    sync.Mutex
	busy    bool

	darkBrush        syscall.Handle
	editBrush        syscall.Handle
	cardBrush        syscall.Handle
	softBrush        syscall.Handle
	borderPen        syscall.Handle
	hFontTitle       syscall.Handle
	hFontHero        syscall.Handle
	hFontSection     syscall.Handle
	hFontBody        syscall.Handle
	hFontSmall       syscall.Handle
	hFontMono        syscall.Handle
	lastHealth       Health
	lastHealthErr    error
	lastTelemetry    Telemetry
	lastTelemetryErr error
	publicIPMu       sync.Mutex
	publicIPCache    string
	publicIPAt       time.Time
)

func utf16(s string) *uint16 {
	p, _ := syscall.UTF16PtrFromString(s)
	return p
}

func loWord(v uintptr) uint16 { return uint16(v & 0xffff) }
func hiWord(v uintptr) uint16 { return uint16((v >> 16) & 0xffff) }

func appDir() string {
	base := os.Getenv("ProgramData")
	if base == "" {
		base = `C:\ProgramData`
	}
	return filepath.Join(base, appName)
}
func configPath() string       { return filepath.Join(appDir(), "config.json") }
func logPath() string          { return filepath.Join(appDir(), "bridge.log") }
func pidPath() string          { return filepath.Join(appDir(), "watchdog.pid") }
func installedExePath() string { return filepath.Join(appDir(), "WiFiEthernetBridge.exe") }

func defaultConfig() Config {
	return Config{
		WiFi: "Wi-Fi", Ethernet: "Ethernet", AutoDetect: true,
		CheckIntervalSeconds: 15, StartupWaitSeconds: 120, RepairCooldownSeconds: 20,
		AutoRepair: true, AutoUpdate: true, UpdateIntervalHours: 6,
	}
}

func ensureDir() { _ = os.MkdirAll(appDir(), 0755) }

func loadConfig() Config {
	cfg := defaultConfig()
	b, err := os.ReadFile(configPath())
	if err == nil {
		_ = json.Unmarshal(b, &cfg)
	}
	if cfg.CheckIntervalSeconds < 5 {
		cfg.CheckIntervalSeconds = 15
	}
	if cfg.StartupWaitSeconds < 10 {
		cfg.StartupWaitSeconds = 120
	}
	if cfg.RepairCooldownSeconds < 5 {
		cfg.RepairCooldownSeconds = 20
	}
	if cfg.UpdateIntervalHours < 1 {
		cfg.UpdateIntervalHours = 6
	}
	return cfg
}

func saveConfig(cfg Config) error {
	ensureDir()
	b, _ := json.MarshalIndent(cfg, "", "  ")
	return os.WriteFile(configPath(), b, 0644)
}

func logLine(level, msg string) {
	ensureDir()
	line := fmt.Sprintf("[%s] [%s] %s", time.Now().Format("2006-01-02 15:04:05"), level, msg)
	if st, err := os.Stat(logPath()); err == nil && st.Size() > 1024*1024 {
		_ = os.Remove(logPath() + ".1")
		_ = os.Rename(logPath(), logPath()+".1")
	}
	if f, err := os.OpenFile(logPath(), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644); err == nil {
		_, _ = f.WriteString(line + "\r\n")
		_ = f.Close()
	}
	if hwndMain != 0 {
		postUI(func() { appendLog(line) })
	}
}

func psQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }

func runPowerShell(script string) (string, error) {
	cmd := exec.Command("powershell.exe", "-NoLogo", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-Command", script)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: CREATE_NO_WINDOW}
	var out, er bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &er
	err := cmd.Run()
	if err != nil {
		msg := strings.TrimSpace(er.String())
		if msg == "" {
			msg = err.Error()
		}
		return strings.TrimSpace(out.String()), fmt.Errorf("%s", firstLine(msg))
	}
	return strings.TrimSpace(out.String()), nil
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexAny(s, "\r\n"); i >= 0 {
		return strings.TrimSpace(s[:i])
	}
	return s
}

func listAdapters() ([]Adapter, error) {
	script := `Get-NetAdapter -ErrorAction Stop | Sort-Object ifIndex | Select-Object Name,InterfaceDescription,Status | ConvertTo-Csv -NoTypeInformation`
	out, err := runPowerShell(script)
	if err != nil {
		return nil, err
	}
	r := csv.NewReader(strings.NewReader(out))
	rows, err := r.ReadAll()
	if err != nil || len(rows) < 2 {
		return nil, fmt.Errorf("could not parse network adapters")
	}
	var result []Adapter
	for _, row := range rows[1:] {
		if len(row) >= 3 {
			result = append(result, Adapter{Name: row[0], Description: row[1], Status: row[2]})
		}
	}
	return result, nil
}

func discover(cfg Config) (string, string, error) {
	script := fmt.Sprintf(`
$ErrorActionPreference = "Stop"
$requestedWifi = %s
$requestedEthernet = %s
$autoDetect = $%t
$all = @(Get-NetAdapter -ErrorAction Stop)
$wifi = $all | Where-Object { $_.Name -eq $requestedWifi } | Select-Object -First 1
$eth = $all | Where-Object { $_.Name -eq $requestedEthernet } | Select-Object -First 1
if ($autoDetect -and -not $wifi) {
  $routes = @(Get-NetRoute -DestinationPrefix "0.0.0.0/0" -AddressFamily IPv4 -ErrorAction SilentlyContinue | Sort-Object RouteMetric)
  foreach ($route in $routes) {
    $c = $all | Where-Object { $_.ifIndex -eq $route.ifIndex -and ($_.Name -match "Wi-Fi|Wireless|WLAN" -or $_.InterfaceDescription -match "Wireless|Wi-Fi|802\\.11") } | Select-Object -First 1
    if ($c) { $wifi = $c; break }
  }
  if (-not $wifi) { $wifi = $all | Where-Object { $_.Name -match "Wi-Fi|Wireless|WLAN" -or $_.InterfaceDescription -match "Wireless|Wi-Fi|802\\.11" } | Select-Object -First 1 }
}
if ($autoDetect -and -not $eth) {
  $eth = $all | Where-Object { (!$wifi -or $_.ifIndex -ne $wifi.ifIndex) -and ($_.Name -match "^Ethernet" -or $_.InterfaceDescription -match "Ethernet|GbE|2\\.5GbE|LAN") } | Sort-Object @{Expression={if ($_.Status -eq "Up") {0} else {1}}}, ifIndex | Select-Object -First 1
}
if (-not $wifi) { throw "Wi-Fi adapter not found: $requestedWifi" }
if (-not $eth) { throw "Ethernet adapter not found: $requestedEthernet" }
Write-Output $wifi.Name
Write-Output $eth.Name
`, psQuote(cfg.WiFi), psQuote(cfg.Ethernet), cfg.AutoDetect)
	out, err := runPowerShell(script)
	if err != nil {
		return "", "", err
	}
	lines := strings.FieldsFunc(out, func(r rune) bool { return r == '\r' || r == '\n' })
	if len(lines) < 2 {
		return "", "", fmt.Errorf("adapter discovery returned incomplete data")
	}
	return strings.TrimSpace(lines[0]), strings.TrimSpace(lines[1]), nil
}

func boolVal(m map[string]string, k string) bool {
	return strings.EqualFold(m[k], "True") || m[k] == "1"
}
func intVal(m map[string]string, k string) int { n, _ := strconv.Atoi(m[k]); return n }

func health(cfg Config) (Health, error) {
	wifi, eth, err := discover(cfg)
	if err != nil {
		return Health{}, err
	}
	script := fmt.Sprintf(`
$ErrorActionPreference = "Stop"
$wifiName = %s
$ethName = %s
$wifi = Get-NetAdapter -Name $wifiName -ErrorAction Stop
$eth = Get-NetAdapter -Name $ethName -ErrorAction Stop
$wifiIp = Get-NetIPAddress -InterfaceIndex $wifi.ifIndex -AddressFamily IPv4 -ErrorAction SilentlyContinue | Where-Object {$_.IPAddress -notlike "169.254.*"} | Select-Object -First 1
$ethIp = Get-NetIPAddress -InterfaceIndex $eth.ifIndex -AddressFamily IPv4 -ErrorAction SilentlyContinue | Where-Object {$_.IPAddress -notlike "169.254.*"} | Select-Object -First 1
$route = Get-NetRoute -InterfaceIndex $wifi.ifIndex -DestinationPrefix "0.0.0.0/0" -AddressFamily IPv4 -ErrorAction SilentlyContinue | Sort-Object RouteMetric | Select-Object -First 1
$pubEnabled=$false; $privEnabled=$false; $pubType=-1; $privType=-1
try {
 $mgr=New-Object -ComObject HNetCfg.HNetShare
 foreach($c in @($mgr.EnumEveryConnection())) {
   $p=$mgr.NetConnectionProps($c); $s=$mgr.INetSharingConfigurationForINetConnection($c)
   if($p.Name -eq $wifiName){$pubEnabled=[bool]$s.SharingEnabled;if($s.SharingEnabled){$pubType=[int]$s.SharingConnectionType}}
   if($p.Name -eq $ethName){$privEnabled=[bool]$s.SharingEnabled;if($s.SharingEnabled){$privType=[int]$s.SharingConnectionType}}
 }
} catch {}
$healthy=($wifi.Status -eq "Up" -and [bool]$wifiIp -and [bool]$route -and $pubEnabled -and $pubType -eq 0 -and $privEnabled -and $privType -eq 1)
"WiFiName=$wifiName"
"WiFiStatus=$($wifi.Status)"
"WiFiIPv4=$(if($wifiIp){$wifiIp.IPAddress}else{''})"
"WiFiHasDefaultRoute=$([bool]$route)"
"EthernetName=$ethName"
"EthernetStatus=$($eth.Status)"
"EthernetIPv4=$(if($ethIp){$ethIp.IPAddress}else{''})"
"PublicSharingEnabled=$pubEnabled"
"PublicSharingType=$pubType"
"PrivateSharingEnabled=$privEnabled"
"PrivateSharingType=$privType"
"Healthy=$healthy"
`, psQuote(wifi), psQuote(eth))
	out, err := runPowerShell(script)
	if err != nil {
		return Health{}, err
	}
	m := map[string]string{}
	for _, line := range strings.Split(strings.ReplaceAll(out, "\r\n", "\n"), "\n") {
		if i := strings.Index(line, "="); i > 0 {
			m[line[:i]] = strings.TrimSpace(line[i+1:])
		}
	}
	return Health{
		WiFiName: m["WiFiName"], WiFiStatus: m["WiFiStatus"], WiFiIPv4: m["WiFiIPv4"], WiFiHasDefaultRoute: boolVal(m, "WiFiHasDefaultRoute"),
		EthernetName: m["EthernetName"], EthernetStatus: m["EthernetStatus"], EthernetIPv4: m["EthernetIPv4"],
		PublicSharingEnabled: boolVal(m, "PublicSharingEnabled"), PublicSharingType: intVal(m, "PublicSharingType"),
		PrivateSharingEnabled: boolVal(m, "PrivateSharingEnabled"), PrivateSharingType: intVal(m, "PrivateSharingType"), Healthy: boolVal(m, "Healthy"),
	}, nil
}

func telemetry(cfg Config) (Telemetry, error) {
	wifi, eth, err := discover(cfg)
	if err != nil {
		return Telemetry{}, err
	}

	script := fmt.Sprintf(`
$ErrorActionPreference = "Stop"
$wifiName = %s
$ethName = %s
$wifi = Get-NetAdapter -Name $wifiName -ErrorAction Stop
$eth = Get-NetAdapter -Name $ethName -ErrorAction Stop

$profile = Get-NetConnectionProfile -InterfaceIndex $wifi.ifIndex -ErrorAction SilentlyContinue | Select-Object -First 1
$ssid = if ($profile) { [string]$profile.Name } else { "" }
$signal = 0
try {
  foreach ($line in @(netsh wlan show interfaces 2>$null)) {
    if ($line -match '^\s*Signal\s*:\s*(\d+)%%') {
      $signal = [int]$matches[1]
      break
    }
  }
} catch {}

$ipcfg = Get-NetIPConfiguration -InterfaceIndex $wifi.ifIndex -ErrorAction SilentlyContinue
$gateway = if ($ipcfg -and $ipcfg.IPv4DefaultGateway) { [string]$ipcfg.IPv4DefaultGateway.NextHop } else { "" }
$dns = @(Get-DnsClientServerAddress -InterfaceIndex $wifi.ifIndex -AddressFamily IPv4 -ErrorAction SilentlyContinue).ServerAddresses -join ", "

$ethIp = Get-NetIPAddress -InterfaceIndex $eth.ifIndex -AddressFamily IPv4 -ErrorAction SilentlyContinue |
  Where-Object { $_.IPAddress -notlike "169.254.*" } |
  Select-Object -First 1
$localEth = if ($ethIp) { [string]$ethIp.IPAddress } else { "" }

$devices = @(
  Get-NetNeighbor -InterfaceIndex $eth.ifIndex -AddressFamily IPv4 -ErrorAction SilentlyContinue |
  Where-Object {
    $_.State -in @("Reachable","Stale","Delay","Probe","Permanent") -and
    $_.IPAddress -ne $localEth -and
    $_.IPAddress -ne "0.0.0.0" -and
    $_.IPAddress -notlike "224.*" -and
    $_.IPAddress -notlike "239.*" -and
    $_.IPAddress -notlike "255.*" -and
    $_.LinkLayerAddress -and
    $_.LinkLayerAddress -ne "00-00-00-00-00-00" -and
    $_.LinkLayerAddress -ne "FF-FF-FF-FF-FF-FF"
  } |
  Select-Object -ExpandProperty IPAddress -Unique
)

"SSID=$ssid"
"SignalPercent=$signal"
"WiFiLinkSpeed=$($wifi.LinkSpeed)"
"EthernetLinkSpeed=$($eth.LinkSpeed)"
"Gateway=$gateway"
"DNS=$dns"
"ConnectedDevices=$($devices -join '|')"
`, psQuote(wifi), psQuote(eth))

	out, err := runPowerShell(script)
	if err != nil {
		return Telemetry{}, err
	}

	values := map[string]string{}
	for _, line := range strings.Split(strings.ReplaceAll(out, "\r\n", "\n"), "\n") {
		if i := strings.Index(line, "="); i > 0 {
			values[line[:i]] = strings.TrimSpace(line[i+1:])
		}
	}

	signal, _ := strconv.Atoi(values["SignalPercent"])
	var devices []string
	if raw := strings.TrimSpace(values["ConnectedDevices"]); raw != "" {
		for _, item := range strings.Split(raw, "|") {
			item = strings.TrimSpace(item)
			if item != "" {
				devices = append(devices, item)
			}
		}
	}

	publicIP, _ := getPublicIP(false)
	return Telemetry{
		SSID:              values["SSID"],
		SignalPercent:     signal,
		WiFiLinkSpeed:     values["WiFiLinkSpeed"],
		EthernetLinkSpeed: values["EthernetLinkSpeed"],
		Gateway:           values["Gateway"],
		DNS:               values["DNS"],
		ConnectedDevices:  devices,
		PublicIP:          publicIP,
	}, nil
}

func getPublicIP(force bool) (string, error) {
	publicIPMu.Lock()
	if !force && publicIPCache != "" && time.Since(publicIPAt) < 5*time.Minute {
		value := publicIPCache
		publicIPMu.Unlock()
		return value, nil
	}
	publicIPMu.Unlock()

	client := &http.Client{Timeout: 4 * time.Second}
	req, err := http.NewRequest(http.MethodGet, "https://api.ipify.org", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", appName+"/"+currentVersion)
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("public IP service returned %s", resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 128))
	if err != nil {
		return "", err
	}
	value := strings.TrimSpace(string(body))
	if net.ParseIP(value) == nil {
		return "", fmt.Errorf("public IP service returned invalid data")
	}

	publicIPMu.Lock()
	publicIPCache = value
	publicIPAt = time.Now()
	publicIPMu.Unlock()
	return value, nil
}

func testConnectivity(cfg Config) ConnectivityResult {
	result := ConnectivityResult{}
	h, err := health(cfg)
	if err == nil {
		result.RouteOK = h.WiFiHasDefaultRoute && strings.EqualFold(h.WiFiStatus, "Up")
	}

	if addrs, err := net.LookupHost("github.com"); err == nil && len(addrs) > 0 {
		result.DNSOK = true
		result.ResolvedHost = addrs[0]
	}

	client := &http.Client{Timeout: 6 * time.Second}
	start := time.Now()
	req, _ := http.NewRequest(http.MethodGet, "https://api.github.com/", nil)
	req.Header.Set("User-Agent", appName+"/"+currentVersion)
	if resp, err := client.Do(req); err == nil {
		result.Latency = time.Since(start)
		result.HTTPSOK = resp.StatusCode >= 200 && resp.StatusCode < 500
		_ = resp.Body.Close()
	}

	if ip, err := getPublicIP(true); err == nil {
		result.PublicIP = ip
	}
	return result
}

func connectivitySummary(r ConnectivityResult) string {
	if r.RouteOK && r.DNSOK && r.HTTPSOK {
		if r.Latency > 0 {
			return fmt.Sprintf("All tests passed • HTTPS %d ms", r.Latency.Milliseconds())
		}
		return "All tests passed"
	}

	var failed []string
	if !r.RouteOK {
		failed = append(failed, "route")
	}
	if !r.DNSOK {
		failed = append(failed, "DNS")
	}
	if !r.HTTPSOK {
		failed = append(failed, "HTTPS")
	}
	return "Failed: " + strings.Join(failed, ", ")
}

func diagnosticReport(cfg Config, h Health, hErr error, t Telemetry, tErr error) string {
	var b strings.Builder
	fmt.Fprintf(&b, "WifiBirdger diagnostics\r\n")
	fmt.Fprintf(&b, "Version: %s\r\n", currentVersion)
	fmt.Fprintf(&b, "Generated: %s\r\n", time.Now().Format(time.RFC3339))
	fmt.Fprintf(&b, "Watchdog: %t\r\n", watcherRunning())
	fmt.Fprintf(&b, "Start with Windows: %t\r\n", autoStartEnabled())
	fmt.Fprintf(&b, "Auto detect: %t\r\n", cfg.AutoDetect)
	fmt.Fprintf(&b, "Auto repair: %t\r\n", cfg.AutoRepair)
	fmt.Fprintf(&b, "Auto update: %t\r\n", cfg.AutoUpdate)
	fmt.Fprintf(&b, "\r\n")

	if hErr != nil {
		fmt.Fprintf(&b, "Health error: %s\r\n", hErr.Error())
	} else {
		fmt.Fprintf(&b, "Healthy: %t\r\n", h.Healthy)
		fmt.Fprintf(&b, "Wi-Fi: %s | %s | %s\r\n", h.WiFiName, h.WiFiStatus, blankAs(h.WiFiIPv4, "no IPv4"))
		fmt.Fprintf(&b, "Wi-Fi default route: %t\r\n", h.WiFiHasDefaultRoute)
		fmt.Fprintf(&b, "Ethernet: %s | %s | %s\r\n", h.EthernetName, h.EthernetStatus, blankAs(h.EthernetIPv4, "no IPv4"))
		fmt.Fprintf(&b, "ICS public: %t type=%d\r\n", h.PublicSharingEnabled, h.PublicSharingType)
		fmt.Fprintf(&b, "ICS private: %t type=%d\r\n", h.PrivateSharingEnabled, h.PrivateSharingType)
	}

	if tErr != nil {
		fmt.Fprintf(&b, "Telemetry error: %s\r\n", tErr.Error())
	} else {
		fmt.Fprintf(&b, "\r\nSSID: %s\r\n", blankAs(t.SSID, "unknown"))
		fmt.Fprintf(&b, "Wi-Fi signal: %d%%\r\n", t.SignalPercent)
		fmt.Fprintf(&b, "Wi-Fi link speed: %s\r\n", blankAs(t.WiFiLinkSpeed, "unknown"))
		fmt.Fprintf(&b, "Ethernet link speed: %s\r\n", blankAs(t.EthernetLinkSpeed, "unknown"))
		fmt.Fprintf(&b, "Gateway: %s\r\n", blankAs(t.Gateway, "unknown"))
		fmt.Fprintf(&b, "DNS: %s\r\n", blankAs(t.DNS, "unknown"))
		fmt.Fprintf(&b, "Public IP: %s\r\n", blankAs(t.PublicIP, "unknown"))
		fmt.Fprintf(&b, "Connected Ethernet devices: %d\r\n", len(t.ConnectedDevices))
		if len(t.ConnectedDevices) > 0 {
			fmt.Fprintf(&b, "Device IPs: %s\r\n", strings.Join(t.ConnectedDevices, ", "))
		}
	}

	fmt.Fprintf(&b, "\r\nConfig: %s\r\n", configPath())
	fmt.Fprintf(&b, "Log: %s\r\n", logPath())
	return b.String()
}

func copyTextToClipboard(text string) error {
	cmd := exec.Command("clip.exe")
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: CREATE_NO_WINDOW}
	cmd.Stdin = strings.NewReader(text)
	return cmd.Run()
}

func openWithShell(target string) error {
	r, _, _ := procShellExecute.Call(
		0,
		uintptr(unsafe.Pointer(utf16("open"))),
		uintptr(unsafe.Pointer(utf16(target))),
		0,
		0,
		SW_SHOWNORMAL,
	)
	if r <= 32 {
		return fmt.Errorf("Windows could not open %s", target)
	}
	return nil
}

func openLogFile() error {
	ensureDir()
	if _, err := os.Stat(logPath()); os.IsNotExist(err) {
		_ = os.WriteFile(logPath(), nil, 0644)
	}
	return openWithShell(logPath())
}

func clearLogFile() error {
	ensureDir()
	return os.WriteFile(logPath(), nil, 0644)
}

func applyICS(cfg Config, force bool) error {
	wifi, eth, err := discover(cfg)
	if err != nil {
		return err
	}
	script := fmt.Sprintf(`
$ErrorActionPreference = "Stop"
$wifiName=%s; $ethName=%s; $force=$%t
$wifi=Get-NetAdapter -Name $wifiName -ErrorAction Stop
if($wifi.Status -ne "Up"){throw "Wi-Fi '$wifiName' is not connected"}
$ip=Get-NetIPAddress -InterfaceIndex $wifi.ifIndex -AddressFamily IPv4 -ErrorAction SilentlyContinue | Where-Object {$_.IPAddress -notlike "169.254.*"} | Select-Object -First 1
$route=Get-NetRoute -InterfaceIndex $wifi.ifIndex -DestinationPrefix "0.0.0.0/0" -AddressFamily IPv4 -ErrorAction SilentlyContinue | Sort-Object RouteMetric | Select-Object -First 1
if(-not $ip -or -not $route){throw "Wi-Fi is connected but has no usable IPv4 internet route"}
Set-Service SharedAccess -StartupType Manual -ErrorAction SilentlyContinue
Start-Service SharedAccess -ErrorAction SilentlyContinue
$mgr=New-Object -ComObject HNetCfg.HNetShare
$conns=@($mgr.EnumEveryConnection()); $wc=$null; $ec=$null
foreach($c in $conns){$p=$mgr.NetConnectionProps($c);if($p.Name -eq $wifiName){$wc=$c};if($p.Name -eq $ethName){$ec=$c}}
if(-not $wc){throw "ICS cannot see Wi-Fi '$wifiName'"}; if(-not $ec){throw "ICS cannot see Ethernet '$ethName'"}
$ws=$mgr.INetSharingConfigurationForINetConnection($wc); $es=$mgr.INetSharingConfigurationForINetConnection($ec)
$correct=($ws.SharingEnabled -and [int]$ws.SharingConnectionType -eq 0 -and $es.SharingEnabled -and [int]$es.SharingConnectionType -eq 1)
if($correct -and -not $force){exit 0}
foreach($c in $conns){try{$s=$mgr.INetSharingConfigurationForINetConnection($c);if($s.SharingEnabled){$s.DisableSharing()}}catch{}}
Start-Sleep -Milliseconds 700
$ws=$mgr.INetSharingConfigurationForINetConnection($wc);$es=$mgr.INetSharingConfigurationForINetConnection($ec)
$ws.EnableSharing(0);Start-Sleep -Milliseconds 500;$es.EnableSharing(1);Start-Sleep -Seconds 2
$ws=$mgr.INetSharingConfigurationForINetConnection($wc);$es=$mgr.INetSharingConfigurationForINetConnection($ec)
if(-not $ws.SharingEnabled -or [int]$ws.SharingConnectionType -ne 0){throw "Failed to enable public sharing on '$wifiName'"}
if(-not $es.SharingEnabled -or [int]$es.SharingConnectionType -ne 1){throw "Failed to enable private sharing on '$ethName'"}
`, psQuote(wifi), psQuote(eth), force)
	_, err = runPowerShell(script)
	return err
}

func disableICS(cfg Config) error {
	wifi, eth, _ := discover(cfg)
	script := fmt.Sprintf(`
$ErrorActionPreference="SilentlyContinue"
$wifiName=%s;$ethName=%s
$mgr=New-Object -ComObject HNetCfg.HNetShare
foreach($c in @($mgr.EnumEveryConnection())){$p=$mgr.NetConnectionProps($c);if($p.Name -eq $wifiName -or $p.Name -eq $ethName){$s=$mgr.INetSharingConfigurationForINetConnection($c);if($s.SharingEnabled){$s.DisableSharing()}}}
`, psQuote(wifi), psQuote(eth))
	_, err := runPowerShell(script)
	return err
}

func parseVersion(value string) []int {
	v := strings.TrimSpace(strings.TrimPrefix(strings.ToLower(value), "v"))
	if i := strings.IndexAny(v, "-+"); i >= 0 {
		v = v[:i]
	}
	parts := strings.Split(v, ".")
	result := make([]int, 3)
	for i := 0; i < len(result) && i < len(parts); i++ {
		result[i], _ = strconv.Atoi(parts[i])
	}
	return result
}

func compareVersion(a, b string) int {
	av := parseVersion(a)
	bv := parseVersion(b)
	for i := 0; i < 3; i++ {
		if av[i] < bv[i] {
			return -1
		}
		if av[i] > bv[i] {
			return 1
		}
	}
	return 0
}

func githubHTTPClient() *http.Client {
	return &http.Client{Timeout: 30 * time.Second}
}

func fetchLatestRelease() (GitHubRelease, error) {
	url := "https://api.github.com/repos/" + githubRepo + "/releases/latest"
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return GitHubRelease{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", appName+"/"+currentVersion)

	resp, err := githubHTTPClient().Do(req)
	if err != nil {
		return GitHubRelease{}, fmt.Errorf("GitHub update check failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return GitHubRelease{}, fmt.Errorf("no published GitHub release exists yet")
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		msg := firstLine(string(body))
		if msg == "" {
			msg = resp.Status
		}
		return GitHubRelease{}, fmt.Errorf("GitHub returned %s: %s", resp.Status, msg)
	}

	var release GitHubRelease
	if err := json.NewDecoder(io.LimitReader(resp.Body, 2*1024*1024)).Decode(&release); err != nil {
		return GitHubRelease{}, fmt.Errorf("invalid GitHub release response: %w", err)
	}
	if strings.TrimSpace(release.TagName) == "" {
		return GitHubRelease{}, fmt.Errorf("latest GitHub release has no tag")
	}
	return release, nil
}

func findReleaseAsset(release GitHubRelease, name string) (GitHubReleaseAsset, bool) {
	for _, asset := range release.Assets {
		if strings.EqualFold(asset.Name, name) {
			return asset, true
		}
	}
	return GitHubReleaseAsset{}, false
}

func selectExecutableAsset(release GitHubRelease) (GitHubReleaseAsset, error) {
	if asset, ok := findReleaseAsset(release, "WiFiEthernetBridge.exe"); ok {
		return asset, nil
	}
	for _, asset := range release.Assets {
		n := strings.ToLower(asset.Name)
		if strings.HasSuffix(n, ".exe") &&
			strings.Contains(n, "wifi") &&
			strings.Contains(n, "bridge") {
			return asset, nil
		}
	}
	return GitHubReleaseAsset{}, fmt.Errorf("release %s does not contain WiFiEthernetBridge.exe", release.TagName)
}

func downloadReleaseAsset(asset GitHubReleaseAsset, dst string) error {
	req, err := http.NewRequest(http.MethodGet, asset.BrowserDownloadURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", appName+"/"+currentVersion)

	resp, err := githubHTTPClient().Do(req)
	if err != nil {
		return fmt.Errorf("update download failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("update download returned %s", resp.Status)
	}

	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, io.LimitReader(resp.Body, 250*1024*1024))
	closeErr := out.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}

func sha256File(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()

	h := sha256.New()
	if _, err := io.Copy(h, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func downloadTextAsset(asset GitHubReleaseAsset) (string, error) {
	req, err := http.NewRequest(http.MethodGet, asset.BrowserDownloadURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", appName+"/"+currentVersion)

	resp, err := githubHTTPClient().Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("checksum download returned %s", resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(body)), nil
}

func verifyReleaseAsset(path string, release GitHubRelease, asset GitHubReleaseAsset) error {
	actual, err := sha256File(path)
	if err != nil {
		return err
	}

	digest := strings.TrimSpace(asset.Digest)
	if strings.HasPrefix(strings.ToLower(digest), "sha256:") {
		expected := strings.TrimSpace(strings.SplitN(digest, ":", 2)[1])
		if !strings.EqualFold(actual, expected) {
			return fmt.Errorf("GitHub SHA-256 verification failed")
		}
		logLine("INFO", "Update SHA-256 verified using GitHub asset digest")
		return nil
	}

	checksumNames := []string{
		asset.Name + ".sha256",
		"WiFiEthernetBridge.exe.sha256",
		"SHA256SUMS",
		"sha256sums.txt",
	}
	for _, name := range checksumNames {
		checksumAsset, ok := findReleaseAsset(release, name)
		if !ok {
			continue
		}
		text, err := downloadTextAsset(checksumAsset)
		if err != nil {
			return fmt.Errorf("checksum download failed: %w", err)
		}

		expected := ""
		for _, line := range strings.Split(text, "\n") {
			fields := strings.Fields(strings.TrimSpace(line))
			if len(fields) == 1 && len(fields[0]) == 64 {
				expected = fields[0]
				break
			}
			if len(fields) >= 2 && len(fields[0]) == 64 {
				filename := strings.TrimLeft(fields[len(fields)-1], "*")
				if strings.EqualFold(filepath.Base(filename), asset.Name) {
					expected = fields[0]
					break
				}
			}
		}
		if expected == "" {
			return fmt.Errorf("checksum file did not contain a valid SHA-256 for %s", asset.Name)
		}
		if !strings.EqualFold(actual, expected) {
			return fmt.Errorf("release SHA-256 verification failed")
		}
		logLine("INFO", "Update SHA-256 verified using release checksum")
		return nil
	}

	return fmt.Errorf("release has no SHA-256 checksum for %s", asset.Name)
}

func validateWindowsExecutable(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	magic := make([]byte, 2)
	if _, err := io.ReadFull(f, magic); err != nil {
		return err
	}
	if string(magic) != "MZ" {
		return fmt.Errorf("downloaded update is not a Windows executable")
	}
	return nil
}

func checkForUpdate() (GitHubRelease, bool, error) {
	release, err := fetchLatestRelease()
	if err != nil {
		return GitHubRelease{}, false, err
	}
	return release, compareVersion(currentVersion, release.TagName) < 0, nil
}

func stageUpdate(release GitHubRelease) (string, error) {
	asset, err := selectExecutableAsset(release)
	if err != nil {
		return "", err
	}

	updateDir := filepath.Join(appDir(), "update")
	if err := os.RemoveAll(updateDir); err != nil {
		return "", err
	}
	if err := os.MkdirAll(updateDir, 0755); err != nil {
		return "", err
	}

	staged := filepath.Join(updateDir, "WiFiEthernetBridge.new.exe")
	logLine("INFO", fmt.Sprintf("Downloading update %s (%s)", release.TagName, asset.Name))
	if err := downloadReleaseAsset(asset, staged); err != nil {
		return "", err
	}
	if err := verifyReleaseAsset(staged, release, asset); err != nil {
		_ = os.Remove(staged)
		return "", err
	}
	if err := validateWindowsExecutable(staged); err != nil {
		_ = os.Remove(staged)
		return "", err
	}
	return staged, nil
}

func scheduleSelfUpdate(staged string, restartWatch bool) error {
	currentExe, err := os.Executable()
	if err != nil {
		return err
	}
	currentExe, _ = filepath.Abs(currentExe)

	installed := ""
	if autoStartEnabled() {
		installed = installedExePath()
	}

	helper := filepath.Join(appDir(), "apply-update.ps1")
	restartArgs := ""
	if restartWatch {
		restartArgs = "--watch"
	}

	script := fmt.Sprintf(`
$ErrorActionPreference = "Stop"
$pidToWait = %d
$target = %s
$newExe = %s
$installed = %s
$restartArgs = %s

try { Wait-Process -Id $pidToWait -ErrorAction SilentlyContinue } catch {}
Start-Sleep -Milliseconds 800

Copy-Item -LiteralPath $newExe -Destination $target -Force

if ($installed -and -not [string]::Equals($installed, $target, [System.StringComparison]::OrdinalIgnoreCase)) {
    Copy-Item -LiteralPath $newExe -Destination $installed -Force
}

if ($restartArgs) {
    Start-Process -FilePath $target -ArgumentList $restartArgs
} else {
    Start-Process -FilePath $target
}

Start-Sleep -Milliseconds 500
Remove-Item -LiteralPath $newExe -Force -ErrorAction SilentlyContinue
Remove-Item -LiteralPath $MyInvocation.MyCommand.Path -Force -ErrorAction SilentlyContinue
`, os.Getpid(), psQuote(currentExe), psQuote(staged), psQuote(installed), psQuote(restartArgs))

	if err := os.WriteFile(helper, []byte(script), 0644); err != nil {
		return err
	}

	cmd := exec.Command("powershell.exe",
		"-NoLogo", "-NoProfile", "-NonInteractive",
		"-ExecutionPolicy", "Bypass",
		"-File", helper,
	)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: CREATE_NO_WINDOW}
	return cmd.Start()
}

func installUpdate(release GitHubRelease, restartWatch bool) error {
	staged, err := stageUpdate(release)
	if err != nil {
		return err
	}
	if err := scheduleSelfUpdate(staged, restartWatch); err != nil {
		return err
	}
	logLine("INFO", "Update staged successfully; application will restart")
	return nil
}

func autoUpdateHeadless() bool {
	cfg := loadConfig()
	if !cfg.AutoUpdate {
		return false
	}

	release, available, err := checkForUpdate()
	if err != nil {
		logLine("WARN", "Automatic update check: "+err.Error())
		return false
	}
	if !available {
		return false
	}

	logLine("INFO", fmt.Sprintf("Automatic update found: %s -> %s", currentVersion, release.TagName))
	if err := installUpdate(release, true); err != nil {
		logLine("ERROR", "Automatic update failed: "+err.Error())
		return false
	}
	return true
}

func isAdmin() bool {
	r, _, _ := procIsUserAnAdmin.Call()
	return r != 0
}

func quoteArg(s string) string {
	if !strings.ContainsAny(s, " \t\"") {
		return s
	}
	return `"` + strings.ReplaceAll(s, `"`, `\"`) + `"`
}

func elevateAndExit() {
	exe, _ := os.Executable()
	args := make([]string, 0, len(os.Args)-1)
	for _, a := range os.Args[1:] {
		args = append(args, quoteArg(a))
	}
	verb := utf16("runas")
	file := utf16(exe)
	params := utf16(strings.Join(args, " "))
	cwd, _ := os.Getwd()
	r, _, _ := procShellExecute.Call(0, uintptr(unsafe.Pointer(verb)), uintptr(unsafe.Pointer(file)), uintptr(unsafe.Pointer(params)), uintptr(unsafe.Pointer(utf16(cwd))), SW_SHOWNORMAL)
	if r <= 32 {
		os.Exit(1)
	}
	os.Exit(0)
}

func isWatchProcess(pid int) bool {
	if pid <= 0 {
		return false
	}
	script := fmt.Sprintf(`$p=Get-CimInstance Win32_Process -Filter "ProcessId=%d" -ErrorAction SilentlyContinue;if($p -and $p.CommandLine -match "--watch"){"yes"}`, pid)
	out, _ := runPowerShell(script)
	return strings.TrimSpace(out) == "yes"
}

func watcherRunning() bool {
	b, err := os.ReadFile(pidPath())
	if err != nil {
		return false
	}
	pid, _ := strconv.Atoi(strings.TrimSpace(string(b)))
	return isWatchProcess(pid)
}

func startWatcher() error {
	if watcherRunning() {
		return nil
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	cmd := exec.Command(exe, "--watch")
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: CREATE_NO_WINDOW}
	return cmd.Start()
}

func stopWatcher() {
	b, err := os.ReadFile(pidPath())
	if err != nil {
		return
	}
	pid, _ := strconv.Atoi(strings.TrimSpace(string(b)))
	if isWatchProcess(pid) {
		cmd := exec.Command("taskkill.exe", "/PID", strconv.Itoa(pid), "/F")
		cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: CREATE_NO_WINDOW}
		_ = cmd.Run()
	}
	_ = os.Remove(pidPath())
}

func watchdog() {
	ensureDir()
	if watcherRunning() {
		return
	}
	_ = os.WriteFile(pidPath(), []byte(strconv.Itoa(os.Getpid())), 0644)
	defer os.Remove(pidPath())
	logLine("INFO", "Watchdog started")
	cfg := loadConfig()
	deadline := time.Now().Add(time.Duration(cfg.StartupWaitSeconds) * time.Second)
	for time.Now().Before(deadline) {
		if _, _, err := discover(cfg); err == nil {
			break
		}
		time.Sleep(3 * time.Second)
	}
	lastRepair := time.Time{}
	lastUpdateCheck := time.Now()
	for {
		cfg = loadConfig()
		if cfg.AutoUpdate && time.Since(lastUpdateCheck) >= time.Duration(cfg.UpdateIntervalHours)*time.Hour {
			lastUpdateCheck = time.Now()
			if autoUpdateHeadless() {
				return
			}
		}
		h, err := health(cfg)
		if cfg.AutoRepair && (err != nil || !h.Healthy) {
			if time.Since(lastRepair) >= time.Duration(cfg.RepairCooldownSeconds)*time.Second {
				if err != nil {
					logLine("WARN", "Health check: "+err.Error())
				} else {
					logLine("WARN", "Sharing unhealthy; attempting repair")
				}
				if err2 := applyICS(cfg, false); err2 != nil {
					logLine("ERROR", "Repair failed: "+err2.Error())
				} else {
					logLine("INFO", "Sharing repaired")
				}
				lastRepair = time.Now()
			}
		}
		time.Sleep(time.Duration(cfg.CheckIntervalSeconds) * time.Second)
	}
}

func autoStartEnabled() bool {
	cmd := exec.Command("schtasks.exe", "/Query", "/TN", taskName)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: CREATE_NO_WINDOW}
	return cmd.Run() == nil
}

func setAutoStart(enable bool, cfg Config) error {
	ensureDir()
	if err := saveConfig(cfg); err != nil {
		return err
	}
	if enable {
		src, err := os.Executable()
		if err != nil {
			return err
		}
		dst := installedExePath()
		if !strings.EqualFold(src, dst) {
			in, err := os.Open(src)
			if err != nil {
				return err
			}
			defer in.Close()
			out, err := os.Create(dst)
			if err != nil {
				return err
			}
			if _, err = io.Copy(out, in); err != nil {
				out.Close()
				return err
			}
			if err = out.Close(); err != nil {
				return err
			}
		}
		tr := fmt.Sprintf(`"%s" --watch`, dst)
		cmd := exec.Command("schtasks.exe", "/Create", "/TN", taskName, "/SC", "ONSTART", "/RU", "SYSTEM", "/RL", "HIGHEST", "/TR", tr, "/F")
		cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: CREATE_NO_WINDOW}
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("%s", firstLine(string(out)))
		}
		return nil
	}
	cmd := exec.Command("schtasks.exe", "/Delete", "/TN", taskName, "/F")
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: CREATE_NO_WINDOW}
	if out, err := cmd.CombinedOutput(); err != nil && !strings.Contains(strings.ToLower(string(out)), "cannot find") {
		return fmt.Errorf("%s", firstLine(string(out)))
	}
	return nil
}

func rgb(r, g, b byte) uintptr { return uintptr(r) | uintptr(g)<<8 | uintptr(b)<<16 }

func makeFont(height int32, weight int32, face string) syscall.Handle {
	h, _, _ := procCreateFont.Call(
		uintptr(height), 0, 0, 0, uintptr(weight), 0, 0, 0,
		1, 0, 0, 5, 0, uintptr(unsafe.Pointer(utf16(face))),
	)
	return syscall.Handle(h)
}

func applyFont(hwnd syscall.Handle, font syscall.Handle) {
	if hwnd != 0 && font != 0 {
		procSendMessage.Call(uintptr(hwnd), WM_SETFONT, uintptr(font), 1)
	}
}

func enableDarkTitleBar(hwnd syscall.Handle) {
	var enabled int32 = 1
	procDwmSetWindowAttribute.Call(uintptr(hwnd), 20, uintptr(unsafe.Pointer(&enabled)), unsafe.Sizeof(enabled))
}

func darkTheme(hwnd syscall.Handle) {
	if hwnd != 0 {
		procSetWindowTheme.Call(uintptr(hwnd), uintptr(unsafe.Pointer(utf16("DarkMode_Explorer"))), 0)
	}
}

func drawRoundedPanel(hdc syscall.Handle, r RECT, fill uintptr, border uintptr, radius int32) {
	brush, _, _ := procCreateSolidBrush.Call(fill)
	pen, _, _ := procCreatePen.Call(PS_SOLID, 1, border)
	oldBrush, _, _ := procSelectObject.Call(uintptr(hdc), brush)
	oldPen, _, _ := procSelectObject.Call(uintptr(hdc), pen)
	procRoundRect.Call(uintptr(hdc), uintptr(r.Left), uintptr(r.Top), uintptr(r.Right), uintptr(r.Bottom), uintptr(radius), uintptr(radius))
	procSelectObject.Call(uintptr(hdc), oldBrush)
	procSelectObject.Call(uintptr(hdc), oldPen)
	procDeleteObject.Call(brush)
	procDeleteObject.Call(pen)
}

func paintDashboard(hwnd syscall.Handle) {
	var ps PAINTSTRUCT
	hdc, _, _ := procBeginPaint.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&ps)))
	if hdc == 0 {
		return
	}
	defer procEndPaint.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&ps)))

	bg := rgb(10, 14, 20)
	card := rgb(19, 26, 36)
	card2 := rgb(15, 22, 31)
	border := rgb(38, 48, 61)
	accent := rgb(76, 139, 245)

	bgBrush, _, _ := procCreateSolidBrush.Call(bg)
	old, _, _ := procSelectObject.Call(hdc, bgBrush)
	procRectangle.Call(hdc, 0, 0, 1040, 870)
	procSelectObject.Call(hdc, old)
	procDeleteObject.Call(bgBrush)

	// Brand mark.
	drawRoundedPanel(syscall.Handle(hdc), RECT{Left: 30, Top: 24, Right: 72, Bottom: 66}, accent, accent, 13)
	procSetBkMode.Call(hdc, TRANSPARENT)
	procSetTextColor.Call(hdc, rgb(255, 255, 255))
	oldFont, _, _ := procSelectObject.Call(hdc, uintptr(hFontSection))
	logo := utf16("WB")
	rcLogo := RECT{Left: 30, Top: 24, Right: 72, Bottom: 66}
	procDrawText.Call(hdc, uintptr(unsafe.Pointer(logo)), ^uintptr(0), uintptr(unsafe.Pointer(&rcLogo)), DT_CENTER|DT_VCENTER|DT_SINGLELINE)
	procSelectObject.Call(hdc, oldFont)

	// Main dashboard cards.
	drawRoundedPanel(syscall.Handle(hdc), RECT{Left: 28, Top: 92, Right: 1012, Bottom: 220}, card, border, 18)
	drawRoundedPanel(syscall.Handle(hdc), RECT{Left: 28, Top: 240, Right: 680, Bottom: 468}, card2, border, 18)
	drawRoundedPanel(syscall.Handle(hdc), RECT{Left: 700, Top: 240, Right: 1012, Bottom: 468}, card2, border, 18)
	drawRoundedPanel(syscall.Handle(hdc), RECT{Left: 28, Top: 488, Right: 1012, Bottom: 590}, card2, border, 18)
	drawRoundedPanel(syscall.Handle(hdc), RECT{Left: 28, Top: 610, Right: 1012, Bottom: 700}, card2, border, 18)
	drawRoundedPanel(syscall.Handle(hdc), RECT{Left: 28, Top: 720, Right: 1012, Bottom: 828}, card2, border, 18)

	// Health indicator.
	healthColor := rgb(255, 186, 84)
	if lastHealthErr != nil {
		healthColor = rgb(244, 93, 106)
	} else if lastHealth.Healthy {
		healthColor = rgb(74, 202, 142)
	}
	drawRoundedPanel(syscall.Handle(hdc), RECT{Left: 52, Top: 119, Right: 64, Bottom: 131}, healthColor, healthColor, 6)

	// Hero metric dividers.
	pen, _, _ := procCreatePen.Call(PS_SOLID, 1, rgb(36, 46, 58))
	oldPen, _, _ := procSelectObject.Call(hdc, pen)
	procRectangle.Call(hdc, 585, 118, 586, 192)
	procRectangle.Call(hdc, 727, 118, 728, 192)
	procRectangle.Call(hdc, 869, 118, 870, 192)
	procSelectObject.Call(hdc, oldPen)
	procDeleteObject.Call(pen)

	// Wi-Fi signal bars.
	activeBars := 0
	if lastTelemetry.SignalPercent > 0 {
		activeBars = (lastTelemetry.SignalPercent + 19) / 20
		if activeBars > 5 {
			activeBars = 5
		}
	}
	for i := 0; i < 5; i++ {
		barColor := rgb(45, 55, 68)
		if i < activeBars {
			barColor = rgb(74, 202, 142)
		}
		height := int32(5 + i*4)
		left := int32(943 + i*10)
		drawRoundedPanel(syscall.Handle(hdc), RECT{
			Left: left, Top: 326 + (22 - height),
			Right: left + 6, Bottom: 348,
		}, barColor, barColor, 3)
	}
}

func drawModernButton(dis *DRAWITEMSTRUCT) {
	if dis == nil {
		return
	}
	id := int(dis.CtlID)
	fill := rgb(31, 41, 54)
	border := rgb(52, 64, 80)
	textColor := rgb(236, 241, 248)

	switch id {
	case ID_START:
		fill = rgb(61, 126, 240)
		border = rgb(84, 151, 255)
	case ID_STOP:
		fill = rgb(91, 39, 47)
		border = rgb(137, 57, 70)
	case ID_TESTCONN:
		fill = rgb(38, 111, 89)
		border = rgb(55, 151, 120)
	case ID_CHECKUPDATE:
		fill = rgb(66, 70, 155)
		border = rgb(91, 96, 195)
	case ID_REPAIR:
		fill = rgb(55, 66, 82)
		border = rgb(77, 91, 111)
	}
	if dis.ItemState&ODS_SELECTED != 0 {
		fill = rgb(byte(fill&0xff)*4/5, byte((fill>>8)&0xff)*4/5, byte((fill>>16)&0xff)*4/5)
	}

	drawRoundedPanel(dis.HDC, dis.RcItem, fill, border, 10)
	procSetBkMode.Call(uintptr(dis.HDC), TRANSPARENT)
	procSetTextColor.Call(uintptr(dis.HDC), textColor)
	oldFont, _, _ := procSelectObject.Call(uintptr(dis.HDC), uintptr(hFontBody))
	buf := make([]uint16, 128)
	procGetWindowText.Call(uintptr(dis.HwndItem), uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	text := syscall.UTF16ToString(buf)
	p := utf16(text)
	r := dis.RcItem
	procDrawText.Call(uintptr(dis.HDC), uintptr(unsafe.Pointer(p)), ^uintptr(0), uintptr(unsafe.Pointer(&r)), DT_CENTER|DT_VCENTER|DT_SINGLELINE)
	procSelectObject.Call(uintptr(dis.HDC), oldFont)
}

func invalidateDashboard() {
	if hwndMain != 0 {
		procInvalidateRect.Call(uintptr(hwndMain), 0, 0)
	}
}

func postUI(fn func()) {
	select {
	case uiQueue <- fn:
	default:
	}
	if hwndMain != 0 {
		procPostMessage.Call(uintptr(hwndMain), WM_APP_UPDATE, 0, 0)
	}
}

func asyncOperation(name string, fn func() error, after func()) {
	opMu.Lock()
	if busy {
		opMu.Unlock()
		return
	}
	busy = true
	opMu.Unlock()
	postUI(func() { setControlsEnabled(false); setWindowText(hStatus, name+"…") })
	go func() {
		err := fn()
		postUI(func() {
			if err != nil {
				logLine("ERROR", name+": "+err.Error())
				setWindowText(hStatus, "Error: "+err.Error())
			} else {
				logLine("INFO", name+" completed")
				if after != nil {
					after()
				}
			}
			setControlsEnabled(true)
			opMu.Lock()
			busy = false
			opMu.Unlock()
		})
	}()
}

func setControlsEnabled(enabled bool) {
	v := uintptr(0)
	if enabled {
		v = 1
	}
	user32.NewProc("EnableWindow").Call(uintptr(hRefresh), v)
	user32.NewProc("EnableWindow").Call(uintptr(hStart), v)
	user32.NewProc("EnableWindow").Call(uintptr(hRepair), v)
	user32.NewProc("EnableWindow").Call(uintptr(hStop), v)
	user32.NewProc("EnableWindow").Call(uintptr(hWiFi), v)
	user32.NewProc("EnableWindow").Call(uintptr(hEthernet), v)
	user32.NewProc("EnableWindow").Call(uintptr(hAutoRepair), v)
	user32.NewProc("EnableWindow").Call(uintptr(hAutoUpdate), v)
	user32.NewProc("EnableWindow").Call(uintptr(hCheckUpdate), v)
	user32.NewProc("EnableWindow").Call(uintptr(hTestConn), v)
	user32.NewProc("EnableWindow").Call(uintptr(hCopyDiag), v)
	user32.NewProc("EnableWindow").Call(uintptr(hOpenSettings), v)
	user32.NewProc("EnableWindow").Call(uintptr(hOpenLog), v)
	user32.NewProc("EnableWindow").Call(uintptr(hClearLog), v)
}

func setWindowText(hwnd syscall.Handle, s string) {
	procSetWindowText.Call(uintptr(hwnd), uintptr(unsafe.Pointer(utf16(s))))
}

func appendLog(s string) {
	if hLog == 0 {
		return
	}
	procSendMessage.Call(uintptr(hLog), EM_SETSEL, ^uintptr(0), ^uintptr(0))
	t := utf16(s + "\r\n")
	procSendMessage.Call(uintptr(hLog), EM_REPLACESEL, 0, uintptr(unsafe.Pointer(t)))
}

func comboText(hwnd syscall.Handle) string {
	idx, _, _ := procSendMessage.Call(uintptr(hwnd), CB_GETCURSEL, 0, 0)
	if int32(idx) < 0 {
		return ""
	}
	n, _, _ := procSendMessage.Call(uintptr(hwnd), CB_GETLBTEXTLEN, idx, 0)
	buf := make([]uint16, n+1)
	procSendMessage.Call(uintptr(hwnd), CB_GETLBTEXT, idx, uintptr(unsafe.Pointer(&buf[0])))
	return syscall.UTF16ToString(buf)
}

func comboSetItems(hwnd syscall.Handle, items []string, selected string) {
	procSendMessage.Call(uintptr(hwnd), CB_RESETCONTENT, 0, 0)
	sel := 0
	for i, s := range items {
		p := utf16(s)
		procSendMessage.Call(uintptr(hwnd), CB_ADDSTRING, 0, uintptr(unsafe.Pointer(p)))
		if strings.EqualFold(s, selected) {
			sel = i
		}
	}
	if len(items) > 0 {
		procSendMessage.Call(uintptr(hwnd), CB_SETCURSEL, uintptr(sel), 0)
	}
}

func checkState(hwnd syscall.Handle) bool {
	r, _, _ := procSendMessage.Call(uintptr(hwnd), BM_GETCHECK, 0, 0)
	return r == BST_CHECKED
}
func setCheck(hwnd syscall.Handle, v bool) {
	x := uintptr(BST_UNCHECKED)
	if v {
		x = BST_CHECKED
	}
	procSendMessage.Call(uintptr(hwnd), BM_SETCHECK, x, 0)
}

func currentConfigFromUI() Config {
	cfg := loadConfig()
	if s := comboText(hWiFi); s != "" {
		cfg.WiFi = s
	}
	if s := comboText(hEthernet); s != "" {
		cfg.Ethernet = s
	}
	cfg.AutoDetect = checkState(hAutoDetect)
	cfg.AutoRepair = checkState(hAutoRepair)
	cfg.AutoUpdate = checkState(hAutoUpdate)
	return cfg
}

func refreshAdaptersAndStatus() {
	go func() {
		cfg := loadConfig()
		adapters, err := listAdapters()
		var wifiItems, ethItems []string
		if err == nil {
			for _, a := range adapters {
				d := strings.ToLower(a.Name + " " + a.Description)
				if strings.Contains(d, "wi-fi") || strings.Contains(d, "wireless") || strings.Contains(d, "wlan") || strings.Contains(d, "802.11") {
					wifiItems = append(wifiItems, a.Name)
				}
				if strings.Contains(d, "ethernet") || strings.Contains(d, "gbe") || strings.Contains(d, "lan") {
					ethItems = append(ethItems, a.Name)
				}
			}
		}
		h, herr := health(cfg)
		t, terr := telemetry(cfg)
		auto := autoStartEnabled()
		postUI(func() {
			if err == nil {
				comboSetItems(hWiFi, wifiItems, cfg.WiFi)
				comboSetItems(hEthernet, ethItems, cfg.Ethernet)
			}
			setCheck(hAutoDetect, cfg.AutoDetect)
			setCheck(hAutoStart, auto)
			setCheck(hAutoRepair, cfg.AutoRepair)
			setCheck(hAutoUpdate, cfg.AutoUpdate)
			updateHealthUI(h, herr)
			updateTelemetryUI(t, terr)
		})
	}()
}

func checkUpdateFromGUI() {
	cfg := currentConfigFromUI()
	_ = saveConfig(cfg)

	asyncOperation("Checking for updates", func() error {
		postUI(func() {
			setWindowText(hUpdateStatus, "Checking GitHub Releases…")
		})

		release, available, err := checkForUpdate()
		if err != nil {
			return err
		}
		if !available {
			postUI(func() {
				setWindowText(hUpdateStatus, fmt.Sprintf("v%s • Up to date", currentVersion))
			})
			return nil
		}

		postUI(func() {
			setWindowText(hUpdateStatus, fmt.Sprintf("%s available • downloading and verifying…", release.TagName))
		})

		if err := installUpdate(release, false); err != nil {
			return err
		}

		postUI(func() {
			setWindowText(hUpdateStatus, fmt.Sprintf("%s installed • restarting…", release.TagName))
		})
		return nil
	}, func() {
		procDestroyWindow.Call(uintptr(hwndMain))
	})
}

func autoCheckUpdateFromGUI() {
	cfg := loadConfig()
	go func() {
		release, available, err := checkForUpdate()
		if err != nil {
			logLine("WARN", "Update check: "+err.Error())
			postUI(func() {
				setWindowText(hUpdateStatus, fmt.Sprintf("v%s • No release/update available", currentVersion))
			})
			return
		}

		if !available {
			postUI(func() {
				setWindowText(hUpdateStatus, fmt.Sprintf("v%s • Up to date", currentVersion))
			})
			return
		}

		if !cfg.AutoUpdate {
			postUI(func() {
				setWindowText(hUpdateStatus, fmt.Sprintf("%s available • click Check Update", release.TagName))
			})
			return
		}

		postUI(func() {
			setWindowText(hUpdateStatus, fmt.Sprintf("%s available • auto-updating…", release.TagName))
		})

		if err := installUpdate(release, false); err != nil {
			logLine("ERROR", "Automatic update failed: "+err.Error())
			postUI(func() {
				setWindowText(hUpdateStatus, "Automatic update failed • see log")
			})
			return
		}

		postUI(func() {
			setWindowText(hUpdateStatus, fmt.Sprintf("%s installed • restarting…", release.TagName))
			procDestroyWindow.Call(uintptr(hwndMain))
		})
	}()
}

func updateTelemetryUI(t Telemetry, err error) {
	lastTelemetry = t
	lastTelemetryErr = err
	if err != nil {
		setWindowText(hSSIDValue, "Unavailable")
		setWindowText(hSignalValue, "—")
		setWindowText(hWiFiSpeedValue, "—")
		setWindowText(hEthSpeedValue, "—")
		setWindowText(hDevicesValue, "—")
		setWindowText(hPublicIPValue, "—")
		setWindowText(hGatewayValue, "—")
		invalidateDashboard()
		return
	}

	signal := "Unknown"
	if t.SignalPercent > 0 {
		signal = fmt.Sprintf("%d%%", t.SignalPercent)
	}
	devices := fmt.Sprintf("%d connected", len(t.ConnectedDevices))
	if len(t.ConnectedDevices) == 1 {
		devices = "1 connected"
	}
	if len(t.ConnectedDevices) > 0 {
		devices += " • " + t.ConnectedDevices[0]
	}

	setWindowText(hSSIDValue, blankAs(t.SSID, "Unknown network"))
	setWindowText(hSignalValue, signal)
	setWindowText(hWiFiSpeedValue, blankAs(t.WiFiLinkSpeed, "Unknown"))
	setWindowText(hEthSpeedValue, blankAs(t.EthernetLinkSpeed, "Unknown"))
	setWindowText(hDevicesValue, devices)
	setWindowText(hPublicIPValue, blankAs(t.PublicIP, "Unavailable"))
	setWindowText(hGatewayValue, blankAs(t.Gateway, "Unavailable"))
	invalidateDashboard()
}

func testConnectionFromGUI() {
	cfg := currentConfigFromUI()
	_ = saveConfig(cfg)
	setWindowText(hToolStatus, "Running route, DNS and HTTPS checks…")
	setControlsEnabled(false)

	go func() {
		result := testConnectivity(cfg)
		postUI(func() {
			summary := connectivitySummary(result)
			setWindowText(hToolStatus, summary)
			if result.RouteOK && result.DNSOK && result.HTTPSOK {
				logLine("INFO", "Connectivity test passed: "+summary)
			} else {
				logLine("WARN", "Connectivity test: "+summary)
			}
			if result.PublicIP != "" {
				lastTelemetry.PublicIP = result.PublicIP
				setWindowText(hPublicIPValue, result.PublicIP)
			}
			setControlsEnabled(true)
		})
	}()
}

func copyDiagnosticsFromGUI() {
	cfg := currentConfigFromUI()
	report := diagnosticReport(cfg, lastHealth, lastHealthErr, lastTelemetry, lastTelemetryErr)
	if err := copyTextToClipboard(report); err != nil {
		setWindowText(hToolStatus, "Could not copy diagnostics")
		logLine("ERROR", "Copy diagnostics: "+err.Error())
		return
	}
	setWindowText(hToolStatus, "Diagnostics copied to clipboard")
	logLine("INFO", "Diagnostics copied to clipboard")
}

func updateHealthUI(h Health, err error) {
	lastHealth = h
	lastHealthErr = err
	if err != nil {
		setWindowText(hHealthTitle, "Needs attention")
		setWindowText(hStatus, err.Error())
		setWindowText(hWiFiMetric, "Unavailable")
		setWindowText(hEthernetMetric, "Unavailable")
		setWindowText(hShareMetric, "Offline")
		invalidateDashboard()
		return
	}

	watch := "Watchdog off"
	if watcherRunning() {
		if loadConfig().AutoRepair {
			watch = "Auto-repair active"
		} else {
			watch = "Monitoring only"
		}
	}

	if h.Healthy {
		setWindowText(hHealthTitle, "Connected & sharing")
		setWindowText(hStatus, fmt.Sprintf("%s → %s is healthy • %s", h.WiFiName, h.EthernetName, watch))
	} else if !strings.EqualFold(h.WiFiStatus, "Up") {
		setWindowText(hHealthTitle, "Wi-Fi is offline")
		setWindowText(hStatus, fmt.Sprintf("Connect %s to the internet, then WifiBirdger will continue automatically • %s", h.WiFiName, watch))
	} else if strings.EqualFold(h.WiFiStatus, "Up") && !strings.EqualFold(h.EthernetStatus, "Up") {
		setWindowText(hHealthTitle, "Waiting for Ethernet")
		setWindowText(hStatus, fmt.Sprintf("Wi-Fi is ready. Connect the Ethernet cable to %s • %s", h.EthernetName, watch))
	} else if !h.PublicSharingEnabled || !h.PrivateSharingEnabled {
		setWindowText(hHealthTitle, "Sharing is not active")
		setWindowText(hStatus, fmt.Sprintf("Adapters are ready. Click Start sharing or Repair • %s", watch))
	} else {
		setWindowText(hHealthTitle, "Needs attention")
		setWindowText(hStatus, fmt.Sprintf("ICS is configured but the route is not fully healthy • %s", watch))
	}

	wifiText := h.WiFiStatus
	if h.WiFiIPv4 != "" {
		wifiText += "\r\n" + h.WiFiIPv4
	}
	ethText := h.EthernetStatus
	if h.EthernetIPv4 != "" {
		ethText += "\r\n" + h.EthernetIPv4
	}
	shareText := "Not active"
	if h.PublicSharingEnabled && h.PrivateSharingEnabled {
		shareText = "ICS active"
	}

	setWindowText(hWiFiMetric, wifiText)
	setWindowText(hEthernetMetric, ethText)
	setWindowText(hShareMetric, shareText)
	invalidateDashboard()
}
func blankAs(s, d string) string {
	if strings.TrimSpace(s) == "" {
		return d
	}
	return s
}

func createControl(class, text string, style uint32, x, y, w, h int32, id int) syscall.Handle {
	hwnd, _, _ := procCreateWindowExW.Call(0, uintptr(unsafe.Pointer(utf16(class))), uintptr(unsafe.Pointer(utf16(text))), uintptr(style), uintptr(x), uintptr(y), uintptr(w), uintptr(h), uintptr(hwndMain), uintptr(id), 0, 0)
	font, _, _ := procGetStockObject.Call(DEFAULT_GUI_FONT)
	procSendMessage.Call(hwnd, WM_SETFONT, font, 1)
	return syscall.Handle(hwnd)
}

func wndProc(hwnd syscall.Handle, msg uint32, wParam, lParam uintptr) uintptr {
	switch msg {
	case WM_CREATE:
		hwndMain = hwnd
		enableDarkTitleBar(hwnd)

		hFontTitle = makeFont(-27, FW_BOLD, "Segoe UI")
		hFontHero = makeFont(-24, FW_SEMIBOLD, "Segoe UI")
		hFontSection = makeFont(-16, FW_SEMIBOLD, "Segoe UI")
		hFontBody = makeFont(-15, FW_NORMAL, "Segoe UI")
		hFontSmall = makeFont(-13, FW_NORMAL, "Segoe UI")
		hFontMono = makeFont(-12, FW_NORMAL, "Cascadia Mono")

		// Header.
		title := createControl("STATIC", "WifiBirdger", WS_CHILD|WS_VISIBLE, 86, 20, 350, 32, 0)
		applyFont(title, hFontTitle)
		hSubtitle = createControl("STATIC", "Internet sharing that monitors and repairs itself", WS_CHILD|WS_VISIBLE, 87, 52, 560, 20, 0)
		applyFont(hSubtitle, hFontSmall)
		hVersion = createControl("STATIC", fmt.Sprintf("v%s", currentVersion), WS_CHILD|WS_VISIBLE, 930, 31, 62, 22, 0)
		applyFont(hVersion, hFontSmall)

		// Hero health card.
		hHealthTitle = createControl("STATIC", "Checking connection…", WS_CHILD|WS_VISIBLE, 76, 112, 470, 32, 0)
		applyFont(hHealthTitle, hFontHero)
		hStatus = createControl("STATIC", "Reading Windows network state…", WS_CHILD|WS_VISIBLE, 76, 151, 475, 44, ID_STATUS)
		applyFont(hStatus, hFontSmall)

		metricLabel1 := createControl("STATIC", "WI-FI", WS_CHILD|WS_VISIBLE, 608, 117, 92, 18, 0)
		metricLabel2 := createControl("STATIC", "ETHERNET", WS_CHILD|WS_VISIBLE, 750, 117, 96, 18, 0)
		metricLabel3 := createControl("STATIC", "SHARING", WS_CHILD|WS_VISIBLE, 892, 117, 92, 18, 0)
		applyFont(metricLabel1, hFontSmall)
		applyFont(metricLabel2, hFontSmall)
		applyFont(metricLabel3, hFontSmall)
		hWiFiMetric = createControl("STATIC", "—", WS_CHILD|WS_VISIBLE, 608, 143, 106, 48, 0)
		hEthernetMetric = createControl("STATIC", "—", WS_CHILD|WS_VISIBLE, 750, 143, 106, 48, 0)
		hShareMetric = createControl("STATIC", "—", WS_CHILD|WS_VISIBLE, 892, 143, 100, 48, 0)
		applyFont(hWiFiMetric, hFontSection)
		applyFont(hEthernetMetric, hFontSection)
		applyFont(hShareMetric, hFontSection)

		// Connection card.
		hSectionNetwork = createControl("STATIC", "Connection", WS_CHILD|WS_VISIBLE, 52, 258, 220, 24, 0)
		applyFont(hSectionNetwork, hFontSection)
		wifiLabel := createControl("STATIC", "Internet source", WS_CHILD|WS_VISIBLE, 52, 292, 160, 18, 0)
		ethLabel := createControl("STATIC", "Share internet to", WS_CHILD|WS_VISIBLE, 366, 292, 180, 18, 0)
		applyFont(wifiLabel, hFontSmall)
		applyFont(ethLabel, hFontSmall)

		hWiFi = createControl("COMBOBOX", "", WS_CHILD|WS_VISIBLE|WS_TABSTOP|CBS_DROPDOWNLIST, 52, 314, 280, 180, ID_WIFI)
		hEthernet = createControl("COMBOBOX", "", WS_CHILD|WS_VISIBLE|WS_TABSTOP|CBS_DROPDOWNLIST, 366, 314, 280, 180, ID_ETHERNET)
		applyFont(hWiFi, hFontBody)
		applyFont(hEthernet, hFontBody)
		darkTheme(hWiFi)
		darkTheme(hEthernet)

		hAutoDetect = createControl("BUTTON", "Auto-detect adapters", WS_CHILD|WS_VISIBLE|WS_TABSTOP|BS_AUTOCHECKBOX|BS_FLAT, 52, 357, 175, 24, ID_AUTODETECT)
		hAutoStart = createControl("BUTTON", "Start with Windows", WS_CHILD|WS_VISIBLE|WS_TABSTOP|BS_AUTOCHECKBOX|BS_FLAT, 237, 357, 158, 24, ID_AUTOSTART)
		hAutoRepair = createControl("BUTTON", "Auto-repair", WS_CHILD|WS_VISIBLE|WS_TABSTOP|BS_AUTOCHECKBOX|BS_FLAT, 405, 357, 125, 24, ID_AUTOREPAIR)
		applyFont(hAutoDetect, hFontSmall)
		applyFont(hAutoStart, hFontSmall)
		applyFont(hAutoRepair, hFontSmall)

		hRefresh = createControl("BUTTON", "Refresh", WS_CHILD|WS_VISIBLE|WS_TABSTOP|BS_OWNERDRAW, 52, 401, 102, 38, ID_REFRESH)
		hRepair = createControl("BUTTON", "Repair", WS_CHILD|WS_VISIBLE|WS_TABSTOP|BS_OWNERDRAW, 164, 401, 102, 38, ID_REPAIR)
		hStop = createControl("BUTTON", "Stop", WS_CHILD|WS_VISIBLE|WS_TABSTOP|BS_OWNERDRAW, 276, 401, 102, 38, ID_STOP)
		hStart = createControl("BUTTON", "Start sharing", WS_CHILD|WS_VISIBLE|WS_TABSTOP|BS_OWNERDRAW, 388, 401, 258, 38, ID_START)
		for _, h := range []syscall.Handle{hRefresh, hRepair, hStop, hStart} {
			applyFont(h, hFontBody)
		}

		// Live details card.
		hSectionDetails = createControl("STATIC", "Live details", WS_CHILD|WS_VISIBLE, 724, 258, 220, 24, 0)
		applyFont(hSectionDetails, hFontSection)

		detailLabels := []struct {
			text string
			y    int32
		}{
			{"Wi-Fi network", 292},
			{"Signal", 320},
			{"Wi-Fi link", 348},
			{"Ethernet link", 376},
			{"PC / devices", 404},
			{"Public IP", 432},
		}
		for _, item := range detailLabels {
			h := createControl("STATIC", item.text, WS_CHILD|WS_VISIBLE, 724, item.y, 102, 18, 0)
			applyFont(h, hFontSmall)
		}

		hSSIDValue = createControl("STATIC", "—", WS_CHILD|WS_VISIBLE, 830, 292, 154, 18, 0)
		hSignalValue = createControl("STATIC", "—", WS_CHILD|WS_VISIBLE, 830, 320, 88, 18, 0)
		hWiFiSpeedValue = createControl("STATIC", "—", WS_CHILD|WS_VISIBLE, 830, 348, 154, 18, 0)
		hEthSpeedValue = createControl("STATIC", "—", WS_CHILD|WS_VISIBLE, 830, 376, 154, 18, 0)
		hDevicesValue = createControl("STATIC", "—", WS_CHILD|WS_VISIBLE, 830, 404, 154, 18, 0)
		hPublicIPValue = createControl("STATIC", "—", WS_CHILD|WS_VISIBLE, 830, 432, 154, 18, 0)
		for _, h := range []syscall.Handle{hSSIDValue, hSignalValue, hWiFiSpeedValue, hEthSpeedValue, hDevicesValue, hPublicIPValue} {
			applyFont(h, hFontSmall)
		}
		hGatewayValue = createControl("STATIC", "", WS_CHILD, 0, 0, 0, 0, 0)

		// Tools card.
		hSectionTools = createControl("STATIC", "Tools", WS_CHILD|WS_VISIBLE, 52, 505, 110, 22, 0)
		applyFont(hSectionTools, hFontSection)
		hToolStatus = createControl("STATIC", "Ready", WS_CHILD|WS_VISIBLE, 150, 507, 820, 20, ID_TOOLSTATUS)
		applyFont(hToolStatus, hFontSmall)

		hTestConn = createControl("BUTTON", "Test connection", WS_CHILD|WS_VISIBLE|WS_TABSTOP|BS_OWNERDRAW, 52, 540, 164, 36, ID_TESTCONN)
		hCopyDiag = createControl("BUTTON", "Copy diagnostics", WS_CHILD|WS_VISIBLE|WS_TABSTOP|BS_OWNERDRAW, 226, 540, 164, 36, ID_COPYDIAG)
		hOpenSettings = createControl("BUTTON", "Network settings", WS_CHILD|WS_VISIBLE|WS_TABSTOP|BS_OWNERDRAW, 400, 540, 174, 36, ID_OPENSETTINGS)
		hOpenLog = createControl("BUTTON", "Open log", WS_CHILD|WS_VISIBLE|WS_TABSTOP|BS_OWNERDRAW, 584, 540, 132, 36, ID_OPENLOG)
		hClearLog = createControl("BUTTON", "Clear log", WS_CHILD|WS_VISIBLE|WS_TABSTOP|BS_OWNERDRAW, 726, 540, 132, 36, ID_CLEARLOG)
		for _, h := range []syscall.Handle{hTestConn, hCopyDiag, hOpenSettings, hOpenLog, hClearLog} {
			applyFont(h, hFontBody)
		}

		// Updates card.
		hSectionUpdates = createControl("STATIC", "Updates", WS_CHILD|WS_VISIBLE, 52, 627, 120, 22, 0)
		applyFont(hSectionUpdates, hFontSection)
		updateSource := createControl("STATIC", fmt.Sprintf("GitHub • %s", githubRepo), WS_CHILD|WS_VISIBLE, 150, 629, 300, 20, 0)
		applyFont(updateSource, hFontSmall)
		hUpdateStatus = createControl("STATIC", fmt.Sprintf("v%s • checking…", currentVersion), WS_CHILD|WS_VISIBLE, 460, 629, 335, 20, ID_UPDATESTATUS)
		applyFont(hUpdateStatus, hFontSmall)
		hAutoUpdate = createControl("BUTTON", "Install updates automatically", WS_CHILD|WS_VISIBLE|WS_TABSTOP|BS_AUTOCHECKBOX|BS_FLAT, 52, 663, 235, 24, ID_AUTOUPDATE)
		applyFont(hAutoUpdate, hFontSmall)
		hCheckUpdate = createControl("BUTTON", "Check for update", WS_CHILD|WS_VISIBLE|WS_TABSTOP|BS_OWNERDRAW, 826, 646, 164, 38, ID_CHECKUPDATE)
		applyFont(hCheckUpdate, hFontBody)

		// Activity card.
		hSectionLog = createControl("STATIC", "Activity", WS_CHILD|WS_VISIBLE, 52, 738, 120, 22, 0)
		applyFont(hSectionLog, hFontSection)
		hLog = createControl("EDIT", "", WS_CHILD|WS_VISIBLE|WS_VSCROLL|ES_LEFT|ES_MULTILINE|ES_AUTOVSCROLL|ES_READONLY, 52, 766, 938, 60, ID_LOG)
		applyFont(hLog, hFontMono)
		darkTheme(hLog)

		darkBrush = syscall.Handle(mustCall(procCreateSolidBrush.Call(rgb(10, 14, 20))))
		editBrush = syscall.Handle(mustCall(procCreateSolidBrush.Call(rgb(13, 19, 27))))
		cardBrush = syscall.Handle(mustCall(procCreateSolidBrush.Call(rgb(19, 26, 36))))
		softBrush = syscall.Handle(mustCall(procCreateSolidBrush.Call(rgb(15, 22, 31))))
		borderPen = syscall.Handle(mustCall(procCreatePen.Call(PS_SOLID, 1, rgb(38, 48, 61))))

		procSetTimer.Call(uintptr(hwnd), 1, 10000, 0)
		refreshAdaptersAndStatus()
		autoCheckUpdateFromGUI()
		return 0

	case WM_PAINT:
		paintDashboard(hwnd)
		return 0
	case WM_ERASEBKGND:
		return 1
	case WM_DRAWITEM:
		dis := (*DRAWITEMSTRUCT)(unsafe.Pointer(lParam))
		drawModernButton(dis)
		return 1
	case WM_COMMAND:
		id := int(loWord(wParam))
		note := hiWord(wParam)
		if note == CBN_SELCHANGE && (id == ID_WIFI || id == ID_ETHERNET) {
			cfg := currentConfigFromUI()
			_ = saveConfig(cfg)
			return 0
		}
		if note == BN_CLICKED {
			switch id {
			case ID_REFRESH:
				refreshAdaptersAndStatus()
			case ID_AUTODETECT:
				cfg := currentConfigFromUI()
				_ = saveConfig(cfg)
				refreshAdaptersAndStatus()
			case ID_AUTOREPAIR:
				cfg := currentConfigFromUI()
				_ = saveConfig(cfg)
				if cfg.AutoRepair {
					setWindowText(hToolStatus, "Auto-repair enabled")
				} else {
					setWindowText(hToolStatus, "Auto-repair disabled • watchdog will monitor only")
				}
			case ID_AUTOSTART:
				want := checkState(hAutoStart)
				cfg := currentConfigFromUI()
				_ = saveConfig(cfg)
				asyncOperation("Updating auto-start", func() error { return setAutoStart(want, cfg) }, func() { setCheck(hAutoStart, autoStartEnabled()); refreshAdaptersAndStatus() })
			case ID_AUTOUPDATE:
				cfg := currentConfigFromUI()
				_ = saveConfig(cfg)
				if cfg.AutoUpdate {
					autoCheckUpdateFromGUI()
				}
			case ID_CHECKUPDATE:
				checkUpdateFromGUI()
			case ID_TESTCONN:
				testConnectionFromGUI()
			case ID_COPYDIAG:
				copyDiagnosticsFromGUI()
			case ID_OPENSETTINGS:
				if err := openWithShell("ms-settings:network-status"); err != nil {
					setWindowText(hToolStatus, "Could not open Windows network settings")
					logLine("ERROR", "Open network settings: "+err.Error())
				} else {
					setWindowText(hToolStatus, "Opened Windows network settings")
				}
			case ID_OPENLOG:
				if err := openLogFile(); err != nil {
					setWindowText(hToolStatus, "Could not open log")
					logLine("ERROR", "Open log: "+err.Error())
				} else {
					setWindowText(hToolStatus, "Opened activity log")
				}
			case ID_CLEARLOG:
				if err := clearLogFile(); err != nil {
					setWindowText(hToolStatus, "Could not clear log")
					logLine("ERROR", "Clear log: "+err.Error())
				} else {
					setWindowText(hLog, "")
					setWindowText(hToolStatus, "Activity log cleared")
				}
			case ID_START:
				cfg := currentConfigFromUI()
				_ = saveConfig(cfg)
				asyncOperation("Starting sharing", func() error {
					if err := applyICS(cfg, false); err != nil {
						return err
					}
					return startWatcher()
				}, refreshAdaptersAndStatus)
			case ID_REPAIR:
				cfg := currentConfigFromUI()
				_ = saveConfig(cfg)
				asyncOperation("Repairing sharing", func() error { return applyICS(cfg, true) }, refreshAdaptersAndStatus)
			case ID_STOP:
				cfg := currentConfigFromUI()
				_ = saveConfig(cfg)
				asyncOperation("Stopping sharing", func() error { stopWatcher(); return disableICS(cfg) }, refreshAdaptersAndStatus)
			}
		}
		return 0
	case WM_TIMER:
		if wParam == 1 {
			refreshAdaptersAndStatus()
		}
		return 0
	case WM_APP_UPDATE:
		for {
			select {
			case fn := <-uiQueue:
				fn()
			default:
				return 0
			}
		}
	case WM_CTLCOLORSTATIC:
		procSetBkMode.Call(wParam, TRANSPARENT)
		color := rgb(222, 229, 238)
		h := syscall.Handle(lParam)
		if h == hSubtitle || h == hVersion || h == hStatus || h == hUpdateStatus || h == hToolStatus {
			color = rgb(137, 151, 169)
		}
		if h == hHealthTitle {
			if lastHealthErr != nil {
				color = rgb(255, 111, 120)
			} else if lastHealth.Healthy {
				color = rgb(93, 211, 158)
			} else {
				color = rgb(255, 190, 92)
			}
		}
		procSetTextColor.Call(wParam, color)
		hollow, _, _ := procGetStockObject.Call(HOLLOW_BRUSH)
		return hollow
	case WM_CTLCOLOREDIT, WM_CTLCOLORLISTBOX:
		procSetTextColor.Call(wParam, rgb(232, 238, 246))
		procSetBkColor.Call(wParam, rgb(15, 21, 29))
		return uintptr(editBrush)
	case WM_CTLCOLORBTN:
		procSetTextColor.Call(wParam, rgb(205, 215, 227))
		procSetBkColor.Call(wParam, rgb(17, 24, 33))
		return uintptr(softBrush)
	case WM_CLOSE:
		cfg := currentConfigFromUI()
		_ = saveConfig(cfg)
		procDestroyWindow.Call(uintptr(hwnd))
		return 0
	case WM_DESTROY:
		procKillTimer.Call(uintptr(hwnd), 1)
		for _, obj := range []syscall.Handle{darkBrush, editBrush, cardBrush, softBrush, borderPen, hFontTitle, hFontHero, hFontSection, hFontBody, hFontSmall, hFontMono} {
			if obj != 0 {
				procDeleteObject.Call(uintptr(obj))
			}
		}
		procPostQuitMessage.Call(0)
		return 0
	}
	r, _, _ := procDefWindowProcW.Call(uintptr(hwnd), uintptr(msg), wParam, lParam)
	return r
}

func mustCall(v uintptr, _ uintptr, _ error) uintptr { return v }

func runGUI() {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	hInst, _, _ := procGetModuleHandle.Call(0)
	icon, _, _ := procLoadIcon.Call(0, IDI_APPLICATION)
	cursor, _, _ := procLoadCursor.Call(0, IDC_ARROW)
	bg, _, _ := procCreateSolidBrush.Call(0x202020)
	className := utf16("WiFiEthernetBridgeWindow")
	wc := WNDCLASSEX{CbSize: uint32(unsafe.Sizeof(WNDCLASSEX{})), LpfnWndProc: syscall.NewCallback(wndProc), HInstance: syscall.Handle(hInst), HIcon: syscall.Handle(icon), HCursor: syscall.Handle(cursor), HbrBackground: syscall.Handle(bg), LpszClassName: className, HIconSm: syscall.Handle(icon)}
	if r, _, _ := procRegisterClassEx.Call(uintptr(unsafe.Pointer(&wc))); r == 0 {
		return
	}
	style := uint32(WS_OVERLAPPED | WS_CAPTION | WS_SYSMENU | WS_MINIMIZEBOX)
	hwnd, _, _ := procCreateWindowExW.Call(0, uintptr(unsafe.Pointer(className)), uintptr(unsafe.Pointer(utf16("WifiBirdger"))), uintptr(style), CW_USEDEFAULT, CW_USEDEFAULT, 1040, 890, 0, 0, hInst, 0)
	if hwnd == 0 {
		return
	}
	hwndMain = syscall.Handle(hwnd)
	procShowWindow.Call(hwnd, SW_SHOW)
	procUpdateWindow.Call(hwnd)
	var msg MSG
	for {
		r, _, _ := procGetMessage.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0)
		if int32(r) <= 0 {
			break
		}
		procTranslateMsg.Call(uintptr(unsafe.Pointer(&msg)))
		procDispatchMessage.Call(uintptr(unsafe.Pointer(&msg)))
	}
}

func main() {
	if !isAdmin() {
		elevateAndExit()
	}
	if len(os.Args) > 1 && os.Args[1] == "--watch" {
		if autoUpdateHeadless() {
			return
		}
		watchdog()
		return
	}
	runGUI()
}
