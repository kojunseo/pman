package process

import (
	"fmt"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/shirou/gopsutil/v3/net"
	"github.com/shirou/gopsutil/v3/process"
)

// PortInfo represents information about a listening port and its process
type PortInfo struct {
	Port     int
	Protocol string
	PID      int32
	User     string
	Command  string
	Language string
}

// GetListeningPorts fetches all listening ports and their process details
func GetListeningPorts() ([]PortInfo, error) {
	connections, err := net.Connections("all")
	if err != nil {
		return nil, err
	}

	var results []PortInfo
	seen := make(map[string]bool)

	for _, conn := range connections {
		// Filter for LISTEN status
		if conn.Status != "LISTEN" {
			continue
		}

		// Create a unique key to avoid duplicates (sometimes same socket appears multiple times)
		key := fmt.Sprintf("%d-%d", conn.Laddr.Port, conn.Pid)
		if seen[key] {
			continue
		}
		seen[key] = true

		info := PortInfo{
			Port:     int(conn.Laddr.Port),
			Protocol: getProtocol(conn.Type),
			PID:      conn.Pid,
		}

		// Fetch process details
		if conn.Pid > 0 {
			proc, err := process.NewProcess(conn.Pid)
			if err == nil {
				// Get User
				username, err := proc.Username()
				if err != nil {
					// Fallback if Username fails (sometimes happens with different UIDs)
					// Try getting UID and looking it up
					uids, err := proc.Uids()
					if err == nil && len(uids) > 0 {
						u, err := user.LookupId(strconv.Itoa(int(uids[0])))
						if err == nil {
							username = u.Username
						}
					}
				}
				if username == "" {
					username = "unknown"
				}
				info.User = username

				// Get Command/Name
				name, err := proc.Name()
				if err == nil {
					info.Command = name
				}

				// Get full command line for better heuristic
				cmdline, err := proc.Cmdline()
				if err == nil {
					info.Language = guessLanguage(name, cmdline)
				} else {
					info.Language = guessLanguage(name, "")
				}
			}
		}

		results = append(results, info)
	}

	return results, nil
}

// KillProcess terminates the process with the given PID
func KillProcess(pid int32) error {
	proc, err := process.NewProcess(pid)
	if err != nil {
		return err
	}
	return proc.Kill()
}

func getProtocol(t uint32) string {
	switch t {
	case 1: // TCP
		return "TCP"
	case 2: // UDP
		return "UDP"
	default:
		return fmt.Sprintf("%d", t)
	}
}

func guessLanguage(name, cmdline string) string {
	name = strings.ToLower(name)
	cmdline = strings.ToLower(cmdline)

	if strings.Contains(name, "python") || strings.Contains(cmdline, "python") {
		return "Python"
	}
	if strings.Contains(name, "java") || strings.Contains(cmdline, "java") {
		return "Java"
	}
	if strings.Contains(name, "node") || strings.Contains(cmdline, "node") {
		return "Node.js"
	}
	if strings.Contains(name, "go") || strings.HasSuffix(name, "main") { // Common Go binary names
		return "Go"
	}
	if strings.Contains(name, "ruby") {
		return "Ruby"
	}
	if strings.Contains(name, "php") {
		return "PHP"
	}
	if strings.Contains(name, "docker") || strings.Contains(name, "com.docker") {
		return "Docker"
	}

	// System processes
	if name == "launchd" || name == "kernel_task" || name == "systemd" {
		return "System"
	}

	// If it's a binary path, try to guess from extension or just return Binary
	if filepath.Ext(name) != "" {
		return "Binary"
	}

	return "System/Binary"
}
