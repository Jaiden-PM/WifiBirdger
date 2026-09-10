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
	appName        = "WiFiEthernetBridge"
	taskName       = "WiFi to Ethernet Bridge"
	currentVersion = "1.3.3"
	githubRepo     = "Jaiden-PM/WifiBirdger"

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

	PROCESS_QUERY_LIMITED_INFORMATION = 0x1000
	CREATE_NO_WINDOW                  = 0x08000000
)

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
	hSectionUpdates syscall.Handle
	hSectionLog     syscall.Handle

	uiQueue = make(chan func(), 64)
	opMu    sync.Mutex
	busy    bool

	darkBrush     syscall.Handle
	editBrush     syscall.Handle
	cardBrush     syscall.Handle
	softBrush     syscall.Handle
	borderPen     syscall.Handle
	hFontTitle    syscall.Handle
	hFontHero     syscall.Handle
	hFontSection  syscall.Handle
	hFontBody     syscall.Handle
	hFontSmall    syscall.Handle
	hFontMono     syscall.Handle
	lastHealth    Health
	lastHealthErr error
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
		AutoUpdate: true, UpdateIntervalHours: 6,
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
		if err != nil || !h.Healthy {
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

	bg := rgb(12, 17, 24)
	card := rgb(20, 27, 37)
	card2 := rgb(17, 24, 33)
	border := rgb(42, 52, 65)
	accent := rgb(73, 139, 255)

	bgBrush, _, _ := procCreateSolidBrush.Call(bg)
	old, _, _ := procSelectObject.Call(hdc, bgBrush)
	procRectangle.Call(hdc, 0, 0, 920, 760)
	procSelectObject.Call(hdc, old)
	procDeleteObject.Call(bgBrush)

	// Brand mark.
	drawRoundedPanel(syscall.Handle(hdc), RECT{Left: 28, Top: 24, Right: 70, Bottom: 66}, accent, accent, 12)
	procSetBkMode.Call(hdc, TRANSPARENT)
	procSetTextColor.Call(hdc, rgb(255, 255, 255))
	oldFont, _, _ := procSelectObject.Call(hdc, uintptr(hFontSection))
	logo := utf16("WB")
	rcLogo := RECT{Left: 28, Top: 24, Right: 70, Bottom: 66}
	procDrawText.Call(hdc, uintptr(unsafe.Pointer(logo)), ^uintptr(0), uintptr(unsafe.Pointer(&rcLogo)), DT_CENTER|DT_VCENTER|DT_SINGLELINE)
	procSelectObject.Call(hdc, oldFont)

	// Main dashboard cards.
	drawRoundedPanel(syscall.Handle(hdc), RECT{Left: 28, Top: 92, Right: 892, Bottom: 210}, card, border, 18)
	drawRoundedPanel(syscall.Handle(hdc), RECT{Left: 28, Top: 240, Right: 892, Bottom: 404}, card2, border, 18)
	drawRoundedPanel(syscall.Handle(hdc), RECT{Left: 28, Top: 434, Right: 892, Bottom: 544}, card2, border, 18)
	drawRoundedPanel(syscall.Handle(hdc), RECT{Left: 28, Top: 574, Right: 892, Bottom: 716}, card2, border, 18)

	// Metric dividers inside the health card.
	pen, _, _ := procCreatePen.Call(PS_SOLID, 1, rgb(38, 48, 61))
	oldPen, _, _ := procSelectObject.Call(hdc, pen)
	procRectangle.Call(hdc, 502, 118, 503, 184)
	procRectangle.Call(hdc, 627, 118, 628, 184)
	procRectangle.Call(hdc, 752, 118, 753, 184)
	procSelectObject.Call(hdc, oldPen)
	procDeleteObject.Call(pen)
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
		border = rgb(80, 145, 255)
	case ID_STOP:
		fill = rgb(92, 38, 45)
		border = rgb(132, 55, 66)
	case ID_CHECKUPDATE:
		fill = rgb(37, 83, 155)
		border = rgb(53, 105, 190)
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
	user32.NewProc("EnableWindow").Call(uintptr(hAutoUpdate), v)
	user32.NewProc("EnableWindow").Call(uintptr(hCheckUpdate), v)
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
		auto := autoStartEnabled()
		postUI(func() {
			if err == nil {
				comboSetItems(hWiFi, wifiItems, cfg.WiFi)
				comboSetItems(hEthernet, ethItems, cfg.Ethernet)
			}
			setCheck(hAutoDetect, cfg.AutoDetect)
			setCheck(hAutoStart, auto)
			setCheck(hAutoUpdate, cfg.AutoUpdate)
			updateHealthUI(h, herr)
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
		watch = "Watchdog active"
	}

	if h.Healthy {
		setWindowText(hHealthTitle, "Connected & sharing")
		setWindowText(hStatus, fmt.Sprintf("Internet is being shared from %s to %s • %s", h.WiFiName, h.EthernetName, watch))
	} else if strings.EqualFold(h.WiFiStatus, "Up") && !strings.EqualFold(h.EthernetStatus, "Up") {
		setWindowText(hHealthTitle, "Waiting for Ethernet")
		setWindowText(hStatus, fmt.Sprintf("Wi-Fi is ready. Connect the Ethernet cable to continue • %s", watch))
	} else {
		setWindowText(hHealthTitle, "Needs attention")
		setWindowText(hStatus, fmt.Sprintf("Sharing is not fully healthy yet • %s", watch))
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

		// Typography is created once and reused by all controls.
		hFontTitle = makeFont(-26, FW_BOLD, "Segoe UI")
		hFontHero = makeFont(-23, FW_SEMIBOLD, "Segoe UI")
		hFontSection = makeFont(-16, FW_SEMIBOLD, "Segoe UI")
		hFontBody = makeFont(-15, FW_NORMAL, "Segoe UI")
		hFontSmall = makeFont(-13, FW_NORMAL, "Segoe UI")
		hFontMono = makeFont(-13, FW_NORMAL, "Cascadia Mono")

		// Header.
		title := createControl("STATIC", "WifiBirdger", WS_CHILD|WS_VISIBLE, 84, 20, 330, 32, 0)
		applyFont(title, hFontTitle)
		hSubtitle = createControl("STATIC", "Fast, self-healing Wi-Fi → Ethernet sharing", WS_CHILD|WS_VISIBLE, 85, 52, 520, 20, 0)
		applyFont(hSubtitle, hFontSmall)
		hVersion = createControl("STATIC", fmt.Sprintf("v%s", currentVersion), WS_CHILD|WS_VISIBLE, 810, 31, 60, 22, 0)
		applyFont(hVersion, hFontSmall)

		// Health hero card.
		hHealthTitle = createControl("STATIC", "Checking connection…", WS_CHILD|WS_VISIBLE, 52, 116, 420, 30, 0)
		applyFont(hHealthTitle, hFontHero)
		hStatus = createControl("STATIC", "Reading network state…", WS_CHILD|WS_VISIBLE, 52, 151, 420, 42, ID_STATUS)
		applyFont(hStatus, hFontSmall)

		metricLabel1 := createControl("STATIC", "WI-FI", WS_CHILD|WS_VISIBLE, 522, 118, 88, 18, 0)
		metricLabel2 := createControl("STATIC", "ETHERNET", WS_CHILD|WS_VISIBLE, 647, 118, 92, 18, 0)
		metricLabel3 := createControl("STATIC", "SHARING", WS_CHILD|WS_VISIBLE, 772, 118, 90, 18, 0)
		applyFont(metricLabel1, hFontSmall)
		applyFont(metricLabel2, hFontSmall)
		applyFont(metricLabel3, hFontSmall)
		hWiFiMetric = createControl("STATIC", "—", WS_CHILD|WS_VISIBLE, 522, 143, 94, 45, 0)
		hEthernetMetric = createControl("STATIC", "—", WS_CHILD|WS_VISIBLE, 647, 143, 94, 45, 0)
		hShareMetric = createControl("STATIC", "—", WS_CHILD|WS_VISIBLE, 772, 143, 94, 45, 0)
		applyFont(hWiFiMetric, hFontSection)
		applyFont(hEthernetMetric, hFontSection)
		applyFont(hShareMetric, hFontSection)

		// Network card.
		hSectionNetwork = createControl("STATIC", "Network", WS_CHILD|WS_VISIBLE, 52, 258, 220, 24, 0)
		applyFont(hSectionNetwork, hFontSection)
		wifiLabel := createControl("STATIC", "Internet source", WS_CHILD|WS_VISIBLE, 52, 292, 180, 20, 0)
		ethLabel := createControl("STATIC", "Share to", WS_CHILD|WS_VISIBLE, 466, 292, 180, 20, 0)
		applyFont(wifiLabel, hFontSmall)
		applyFont(ethLabel, hFontSmall)
		hWiFi = createControl("COMBOBOX", "", WS_CHILD|WS_VISIBLE|WS_TABSTOP|CBS_DROPDOWNLIST, 52, 315, 360, 180, ID_WIFI)
		hEthernet = createControl("COMBOBOX", "", WS_CHILD|WS_VISIBLE|WS_TABSTOP|CBS_DROPDOWNLIST, 466, 315, 360, 180, ID_ETHERNET)
		applyFont(hWiFi, hFontBody)
		applyFont(hEthernet, hFontBody)
		darkTheme(hWiFi)
		darkTheme(hEthernet)

		hAutoDetect = createControl("BUTTON", "Auto-detect adapters", WS_CHILD|WS_VISIBLE|WS_TABSTOP|BS_AUTOCHECKBOX|BS_FLAT, 52, 358, 190, 24, ID_AUTODETECT)
		hAutoStart = createControl("BUTTON", "Start with Windows", WS_CHILD|WS_VISIBLE|WS_TABSTOP|BS_AUTOCHECKBOX|BS_FLAT, 255, 358, 180, 24, ID_AUTOSTART)
		applyFont(hAutoDetect, hFontSmall)
		applyFont(hAutoStart, hFontSmall)

		hRefresh = createControl("BUTTON", "Refresh", WS_CHILD|WS_VISIBLE|WS_TABSTOP|BS_OWNERDRAW, 466, 352, 100, 34, ID_REFRESH)
		hRepair = createControl("BUTTON", "Repair", WS_CHILD|WS_VISIBLE|WS_TABSTOP|BS_OWNERDRAW, 576, 352, 100, 34, ID_REPAIR)
		hStop = createControl("BUTTON", "Stop", WS_CHILD|WS_VISIBLE|WS_TABSTOP|BS_OWNERDRAW, 686, 352, 76, 34, ID_STOP)
		hStart = createControl("BUTTON", "Start sharing", WS_CHILD|WS_VISIBLE|WS_TABSTOP|BS_OWNERDRAW, 772, 352, 96, 34, ID_START)
		applyFont(hRefresh, hFontBody)
		applyFont(hRepair, hFontBody)
		applyFont(hStop, hFontBody)
		applyFont(hStart, hFontBody)

		// Updates card.
		hSectionUpdates = createControl("STATIC", "Updates", WS_CHILD|WS_VISIBLE, 52, 452, 150, 22, 0)
		applyFont(hSectionUpdates, hFontSection)
		updateSource := createControl("STATIC", fmt.Sprintf("GitHub • %s", githubRepo), WS_CHILD|WS_VISIBLE, 52, 480, 300, 20, 0)
		applyFont(updateSource, hFontSmall)
		hUpdateStatus = createControl("STATIC", fmt.Sprintf("v%s • checking…", currentVersion), WS_CHILD|WS_VISIBLE, 355, 480, 285, 20, ID_UPDATESTATUS)
		applyFont(hUpdateStatus, hFontSmall)
		hAutoUpdate = createControl("BUTTON", "Install updates automatically", WS_CHILD|WS_VISIBLE|WS_TABSTOP|BS_AUTOCHECKBOX|BS_FLAT, 52, 510, 235, 24, ID_AUTOUPDATE)
		applyFont(hAutoUpdate, hFontSmall)
		hCheckUpdate = createControl("BUTTON", "Check for update", WS_CHILD|WS_VISIBLE|WS_TABSTOP|BS_OWNERDRAW, 704, 484, 164, 38, ID_CHECKUPDATE)
		applyFont(hCheckUpdate, hFontBody)

		// Log card.
		hSectionLog = createControl("STATIC", "Activity", WS_CHILD|WS_VISIBLE, 52, 593, 150, 22, 0)
		applyFont(hSectionLog, hFontSection)
		hLog = createControl("EDIT", "", WS_CHILD|WS_VISIBLE|WS_VSCROLL|ES_LEFT|ES_MULTILINE|ES_AUTOVSCROLL|ES_READONLY, 52, 624, 816, 70, ID_LOG)
		applyFont(hLog, hFontMono)
		darkTheme(hLog)

		darkBrush = syscall.Handle(mustCall(procCreateSolidBrush.Call(rgb(12, 17, 24))))
		editBrush = syscall.Handle(mustCall(procCreateSolidBrush.Call(rgb(15, 21, 29))))
		cardBrush = syscall.Handle(mustCall(procCreateSolidBrush.Call(rgb(20, 27, 37))))
		softBrush = syscall.Handle(mustCall(procCreateSolidBrush.Call(rgb(17, 24, 33))))
		borderPen = syscall.Handle(mustCall(procCreatePen.Call(PS_SOLID, 1, rgb(42, 52, 65))))

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
		if h == hSubtitle || h == hVersion || h == hStatus || h == hUpdateStatus {
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
	hwnd, _, _ := procCreateWindowExW.Call(0, uintptr(unsafe.Pointer(className)), uintptr(unsafe.Pointer(utf16("WifiBirdger"))), uintptr(style), CW_USEDEFAULT, CW_USEDEFAULT, 920, 760, 0, 0, hInst, 0)
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
