package process

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

type Credentials struct {
	PID int
	UID uint32
	GID uint32
}

type Info struct {
	PID     int
	UID     uint32
	GID     uint32
	Exe     string
	Cmdline []string
	Parents []Summary
}

type Summary struct {
	PID     int
	Exe     string
	Cmdline []string
}

func PeerCredentials(conn *net.UnixConn) (Credentials, error) {
	raw, err := conn.SyscallConn()
	if err != nil {
		return Credentials{}, err
	}

	var cred *syscall.Ucred
	var controlErr error
	if err := raw.Control(func(fd uintptr) {
		cred, controlErr = syscall.GetsockoptUcred(int(fd), syscall.SOL_SOCKET, syscall.SO_PEERCRED)
	}); err != nil {
		return Credentials{}, err
	}
	if controlErr != nil {
		return Credentials{}, controlErr
	}
	if cred == nil {
		return Credentials{}, errors.New("missing peer credentials")
	}

	return Credentials{
		PID: int(cred.Pid),
		UID: cred.Uid,
		GID: cred.Gid,
	}, nil
}

func Inspect(pid int, parentDepth int) (Info, error) {
	summary, ppid, uid, gid, err := readProcess(pid)
	info := Info{
		PID:     pid,
		UID:     uid,
		GID:     gid,
		Exe:     summary.Exe,
		Cmdline: summary.Cmdline,
	}
	if err != nil {
		return info, err
	}

	for range parentDepth {
		if ppid <= 0 {
			break
		}
		parent, nextPPID, _, _, err := readProcess(ppid)
		if err != nil {
			return info, fmt.Errorf("inspect parent pid %d: %w", ppid, err)
		}
		info.Parents = append(info.Parents, parent)
		ppid = nextPPID
	}

	return info, nil
}

func CommandLine(args []string) string {
	return strings.Join(args, " ")
}

func ProcessName(summary Summary) string {
	if summary.Exe != "" {
		return filepath.Base(summary.Exe)
	}
	if len(summary.Cmdline) > 0 && summary.Cmdline[0] != "" {
		return filepath.Base(summary.Cmdline[0])
	}
	if summary.PID > 0 {
		return fmt.Sprintf("pid %d", summary.PID)
	}
	return "unknown process"
}

func ParentChain(parents []Summary) string {
	names := make([]string, 0, len(parents))
	for _, parent := range parents {
		names = append(names, ProcessName(parent))
	}
	return strings.Join(names, " <- ")
}

func readProcess(pid int) (Summary, int, uint32, uint32, error) {
	summary := Summary{PID: pid}
	procDir := filepath.Join("/proc", strconv.Itoa(pid))

	exe, exeErr := os.Readlink(filepath.Join(procDir, "exe"))
	if exeErr == nil {
		summary.Exe = exe
	}

	cmdline, cmdlineErr := readCmdline(filepath.Join(procDir, "cmdline"))
	if cmdlineErr == nil {
		summary.Cmdline = cmdline
	}

	ppid, uid, gid, statusErr := readStatus(filepath.Join(procDir, "status"))
	if statusErr != nil {
		return summary, 0, 0, 0, statusErr
	}
	if exeErr != nil && cmdlineErr != nil {
		return summary, ppid, uid, gid, fmt.Errorf("read exe: %v; read cmdline: %w", exeErr, cmdlineErr)
	}

	return summary, ppid, uid, gid, nil
}

func readCmdline(path string) ([]string, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	content = []byte(strings.TrimRight(string(content), "\x00"))
	if len(content) == 0 {
		return nil, nil
	}
	return strings.Split(string(content), "\x00"), nil
}

func readStatus(path string) (int, uint32, uint32, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return 0, 0, 0, err
	}

	var ppid int
	var uid uint32
	var gid uint32
	for line := range strings.Lines(string(content)) {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		switch fields[0] {
		case "PPid:":
			value, err := strconv.Atoi(fields[1])
			if err != nil {
				return 0, 0, 0, err
			}
			ppid = value
		case "Uid:":
			value, err := strconv.ParseUint(fields[1], 10, 32)
			if err != nil {
				return 0, 0, 0, err
			}
			uid = uint32(value)
		case "Gid:":
			value, err := strconv.ParseUint(fields[1], 10, 32)
			if err != nil {
				return 0, 0, 0, err
			}
			gid = uint32(value)
		}
	}

	return ppid, uid, gid, nil
}
