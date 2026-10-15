//go:build windows

package platform

import (
	"bufio"
	"context"
	"encoding/csv"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

var (
	user32                        = syscall.NewLazyDLL("user32.dll")
	kernel32                      = syscall.NewLazyDLL("kernel32.dll")
	procGetLastInputInfo          = user32.NewProc("GetLastInputInfo")
	procGetTickCount64            = kernel32.NewProc("GetTickCount64")
	procGetForegroundWindow       = user32.NewProc("GetForegroundWindow")
	procGetWindowThreadProcessID  = user32.NewProc("GetWindowThreadProcessId")
	procGetWindowRect             = user32.NewProc("GetWindowRect")
	procMonitorFromWindow         = user32.NewProc("MonitorFromWindow")
	procGetMonitorInfoW           = user32.NewProc("GetMonitorInfoW")
	procOpenProcess               = kernel32.NewProc("OpenProcess")
	procQueryFullProcessImageName = kernel32.NewProc("QueryFullProcessImageNameW")
	procCloseHandle               = kernel32.NewProc("CloseHandle")
)

const (
	processQueryLimitedInformation = 0x1000
	monitorDefaultToNearest        = 2
)

type lastInputInfo struct {
	Size uint32
	Time uint32
}

type rect struct{ Left, Top, Right, Bottom int32 }

type monitorInfo struct {
	Size    uint32
	Monitor rect
	Work    rect
	Flags   uint32
}

type windowsSensor struct{}

func newSensor() Sensor { return windowsSensor{} }

func (windowsSensor) Snapshot(ctx context.Context, excluded []string) (Snapshot, error) {
	idle, err := userIdle()
	if err != nil {
		return Snapshot{}, err
	}
	foreground, fullscreen := foregroundInfo()
	gpus, _ := queryGPUs(ctx)
	otherGPU, processErr := hasOtherGPUProcess(ctx, excluded)
	return Snapshot{
		CapturedAt: time.Now().UTC(), IdleSeconds: int64(idle.Seconds()), Foreground: foreground,
		Fullscreen: fullscreen, OtherGPUProcess: otherGPU, GPUProcessKnown: processErr == nil,
		GPUs: gpus, SensingAvailable: true,
	}, nil
}

func userIdle() (time.Duration, error) {
	info := lastInputInfo{Size: uint32(unsafe.Sizeof(lastInputInfo{}))}
	ok, _, err := procGetLastInputInfo.Call(uintptr(unsafe.Pointer(&info)))
	if ok == 0 {
		return 0, err
	}
	tick, _, _ := procGetTickCount64.Call()
	elapsed := uint32(tick) - info.Time
	return time.Duration(elapsed) * time.Millisecond, nil
}

func foregroundInfo() (string, bool) {
	hwnd, _, _ := procGetForegroundWindow.Call()
	if hwnd == 0 {
		return "", false
	}
	var pid uint32
	procGetWindowThreadProcessID.Call(hwnd, uintptr(unsafe.Pointer(&pid)))
	name := processName(pid)

	var window rect
	if ok, _, _ := procGetWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&window))); ok == 0 {
		return name, false
	}
	monitor, _, _ := procMonitorFromWindow.Call(hwnd, monitorDefaultToNearest)
	info := monitorInfo{Size: uint32(unsafe.Sizeof(monitorInfo{}))}
	if monitor == 0 {
		return name, false
	}
	if ok, _, _ := procGetMonitorInfoW.Call(monitor, uintptr(unsafe.Pointer(&info))); ok == 0 {
		return name, false
	}
	fullscreen := window.Left <= info.Monitor.Left && window.Top <= info.Monitor.Top && window.Right >= info.Monitor.Right && window.Bottom >= info.Monitor.Bottom
	return name, fullscreen
}

func processName(pid uint32) string {
	handle, _, _ := procOpenProcess.Call(processQueryLimitedInformation, 0, uintptr(pid))
	if handle == 0 {
		return ""
	}
	defer procCloseHandle.Call(handle)
	buffer := make([]uint16, 32768)
	size := uint32(len(buffer))
	ok, _, _ := procQueryFullProcessImageName.Call(handle, 0, uintptr(unsafe.Pointer(&buffer[0])), uintptr(unsafe.Pointer(&size)))
	if ok == 0 {
		return ""
	}
	return filepath.Base(syscall.UTF16ToString(buffer[:size]))
}

func queryGPUs(ctx context.Context) ([]GPU, error) {
	command := exec.CommandContext(ctx, "nvidia-smi", "--query-gpu=index,name,utilization.gpu,temperature.gpu,power.draw", "--format=csv,noheader,nounits")
	output, err := command.Output()
	if err != nil {
		return nil, err
	}
	reader := csv.NewReader(strings.NewReader(string(output)))
	records, err := reader.ReadAll()
	if err != nil {
		return nil, err
	}
	result := make([]GPU, 0, len(records))
	for _, record := range records {
		if len(record) != 5 {
			continue
		}
		index, _ := strconv.Atoi(strings.TrimSpace(record[0]))
		utilization, _ := strconv.Atoi(strings.TrimSpace(record[2]))
		temperature, _ := strconv.Atoi(strings.TrimSpace(record[3]))
		power, _ := strconv.ParseFloat(strings.TrimSpace(record[4]), 64)
		result = append(result, GPU{Index: index, Name: strings.TrimSpace(record[1]), Utilization: utilization, Temperature: temperature, PowerWatts: power})
	}
	return result, nil
}

func hasOtherGPUProcess(ctx context.Context, excluded []string) (bool, error) {
	command := exec.CommandContext(ctx, "nvidia-smi", "--query-compute-apps=process_name", "--format=csv,noheader")
	output, err := command.Output()
	if err != nil {
		return false, err
	}
	exclusions := make(map[string]struct{}, len(excluded))
	for _, item := range excluded {
		exclusions[strings.ToLower(filepath.Base(item))] = struct{}{}
	}
	scanner := bufio.NewScanner(strings.NewReader(string(output)))
	for scanner.Scan() {
		name := strings.ToLower(filepath.Base(strings.TrimSpace(scanner.Text())))
		if name == "" || name == "no running processes found" {
			continue
		}
		if _, ok := exclusions[name]; !ok {
			return true, nil
		}
	}
	return false, scanner.Err()
}
