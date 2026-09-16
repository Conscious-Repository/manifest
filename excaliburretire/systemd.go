package excaliburretire

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

const engineUnit = "excalibur-engine.service"
const unitPath = "/etc/systemd/system/excalibur-engine.service"
const savedUnit = unitPath + ".pre-decommission"

// Systemd uses system services, not the per-user bus. Mutations require an
// explicit apply invocation and existing sudo authorization (never a prompt).
type Systemd struct{ Config Config }

func command(name string, args ...string) ([]byte, error) {
	b, e := exec.Command(name, args...).Output()
	if e != nil {
		return nil, fmt.Errorf("%s operation failed", name)
	}
	return b, nil
}
func properties(unit string) (map[string]string, error) {
	b, e := command("systemctl", "show", unit, "--property=LoadState,ActiveState,UnitFileState,FragmentPath,RequiredBy,WantedBy,TriggeredBy,ExecStart,DropInPaths,MainPID")
	p := map[string]string{}
	for _, line := range strings.Split(string(b), "\n") {
		k, v, ok := strings.Cut(line, "=")
		if ok {
			p[k] = v
		}
	}
	return p, e
}
func (m Systemd) Snapshot() ([]Service, []string, error) {
	rows := []Service{}
	blockers := []string{}
	enginePID := ""
	for _, name := range []string{engineUnit, "manifest-personal-email.service", "manifest-transcripts.service"} {
		p, e := properties(name)
		if e != nil {
			return rows, blockers, e
		}
		var def []byte
		if name == engineUnit && p["UnitFileState"] == "masked" {
			target, err := os.Readlink(unitPath)
			if err != nil || target != "/dev/null" {
				return rows, blockers, fmt.Errorf("persistent mask unavailable")
			}
			def, e = os.ReadFile(savedUnit)
		} else if name == engineUnit {
			def, e = os.ReadFile(unitPath)
		} else {
			def, e = command("systemctl", "cat", name)
		}
		if e != nil {
			return rows, blockers, e
		}
		consumers := strings.Fields(p["RequiredBy"] + " " + p["WantedBy"] + " " + p["TriggeredBy"])
		sort.Strings(consumers)
		binary := ""
		if match := regexp.MustCompile(`(?m)^ExecStart=(\S+)`).FindSubmatch(def); len(match) == 2 {
			binary = string(match[1])
		}
		bin, e := os.ReadFile(binary)
		if e != nil {
			return rows, blockers, fmt.Errorf("service binary unreadable")
		}
		if p["ActiveState"] == "active" {
			running, e := os.ReadFile(filepath.Join("/proc", p["MainPID"], "exe"))
			if e != nil || Hash(running) != Hash(bin) {
				blockers = append(blockers, name+": running executable unavailable or changed")
			}
		}
		rows = append(rows, Service{Name: name, Active: p["ActiveState"], Enabled: p["UnitFileState"], DefinitionHash: Hash(def), BinaryHash: Hash(bin), ConsumersHash: Hash([]byte(strings.Join(consumers, "\n"))), Consumers: len(consumers)})
		if name != engineUnit {
			configName := "personal-email-worker.json"
			binaryName := "manifest-personal-email-worker"
			if name == "manifest-transcripts.service" {
				configName = "transcript-worker.json"
				binaryName = "manifest-transcript-worker"
			}
			if !strings.Contains(p["ExecStart"], "/"+binaryName+" ") || !strings.Contains(p["ExecStart"], "-config "+m.Config.DataDir+"/"+configName+" ;") {
				blockers = append(blockers, name+": service configuration binding unsupported")
			}
			if p["LoadState"] != "loaded" || p["ActiveState"] != "active" || p["UnitFileState"] != "enabled" {
				blockers = append(blockers, name+": successor service not active and enabled")
			}
		} else {
			enginePID = p["MainPID"]
			if p["UnitFileState"] != "masked" && (!strings.Contains(p["ExecStart"], "/excalibur-engine serve -root "+m.Config.Root+" ") || p["DropInPaths"] != "") {
				blockers = append(blockers, "engine executable/root binding unsupported")
			}
			if p["ActiveState"] != "active" && p["ActiveState"] != "inactive" {
				blockers = append(blockers, "engine service state uncertain")
			}
			if p["UnitFileState"] != "masked" && (p["LoadState"] != "loaded" || p["FragmentPath"] != unitPath || p["DropInPaths"] != "") {
				blockers = append(blockers, "engine unit layout unsupported")
			}
			for _, c := range consumers {
				if c != "multi-user.target" && c != "engine-room.target" {
					blockers = append(blockers, "unaccounted service consumer: "+Hash([]byte(c)))
				}
			}
			if p["RequiredBy"] != "" || p["TriggeredBy"] != "" {
				blockers = append(blockers, "engine has required/trigger consumers")
			}
		}
	}
	processes, e := os.ReadDir("/proc")
	if e != nil {
		return rows, blockers, fmt.Errorf("process inventory unavailable")
	}
	for _, proc := range processes {
		exe, e := os.Readlink(filepath.Join("/proc", proc.Name(), "exe"))
		if e != nil {
			continue
		}
		if filepath.Base(strings.TrimSuffix(exe, " (deleted)")) == "excalibur-engine" && proc.Name() != enginePID {
			blockers = append(blockers, "unaccounted standalone engine process")
		}
	}
	b, e := command("systemctl", "list-units", "--all", "--plain", "--no-legend", "excalibur-engine@*.service")
	if e != nil {
		return rows, blockers, e
	}
	if strings.TrimSpace(string(b)) != "" {
		blockers = append(blockers, "engine template instances remain")
	}
	return rows, blockers, nil
}
func (Systemd) Retire() error {
	// A local /etc unit cannot be masked in place by systemctl. Preserve its
	// exact bytes under a fixed backup name before creating the /dev/null mask.
	p, e := properties(engineUnit)
	if e != nil {
		return e
	}
	if p["UnitFileState"] == "masked" && p["ActiveState"] == "inactive" {
		return nil
	}
	if p["FragmentPath"] != unitPath || p["DropInPaths"] != "" {
		return fmt.Errorf("unsupported engine unit layout")
	}
	if _, e = os.Lstat(savedUnit); !os.IsNotExist(e) {
		return fmt.Errorf("unit backup already exists or unreadable; manual recovery required")
	}
	if _, e = command("sudo", "-n", "systemctl", "disable", "--now", engineUnit); e != nil {
		return e
	}
	if _, e = command("sudo", "-n", "mv", "--", unitPath, savedUnit); e != nil {
		return e
	}
	if _, e = command("sudo", "-n", "ln", "-s", "/dev/null", unitPath); e != nil {
		return e
	}
	if _, e = command("sudo", "-n", "systemctl", "daemon-reload"); e != nil {
		return e
	}
	p, e = properties(engineUnit)
	if e != nil {
		return e
	}
	if p["UnitFileState"] != "masked" || p["ActiveState"] != "inactive" {
		return fmt.Errorf("engine retirement not confirmed")
	}
	return nil
}
