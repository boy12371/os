package install

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/sveil/os/config"
	"github.com/sveil/os/pkg/log"
	"github.com/sveil/os/pkg/util"
)

type MenuEntry struct {
	Name, BootDir, Version, KernelArgs, Append string
}
type BootVars struct {
	BaseName, BootDir string
	Timeout           uint
	Fallback          int
	Entries           []MenuEntry
}

func MountDevice(baseName, device, partition string, raw bool) (string, string, error) {
	log.Debugf("mountdevice %s, raw %v", partition, raw)

	if partition == "" {
		if raw {
			log.Debugf("util.Mount (raw) %s, %s", partition, baseName)

			cmd := exec.Command("lsblk", "-no", "pkname", partition)
			log.Debugf("Run(%v)", cmd)
			cmd.Stderr = os.Stderr
			device := ""
			// TODO: out can == "" - this is used to "detect software RAID" which is terrible
			if out, err := cmd.Output(); err == nil {
				device = "/dev/" + strings.TrimSpace(string(out))
			}

			log.Debugf("mountdevice return -> d: %s, p: %s", device, partition)
			return device, partition, util.Mount(partition, baseName, "", "")
		}

		//rootfs := partition
		// Don't use ResolveDevice - it can fail, whereas `blkid -L LABEL` works more often

		d, _, err := util.Blkid("RANCHER_BOOT")
		if err != nil {
			log.Errorf("Failed to run blkid: %s", err)
		}
		if d != "" {
			partition = d
			baseName = filepath.Join(baseName, config.BootDir)
		} else {
			partition = GetStatePartition()
		}
		cmd := exec.Command("lsblk", "-no", "pkname", partition)
		log.Debugf("Run(%v)", cmd)
		cmd.Stderr = os.Stderr
		// TODO: out can == "" - this is used to "detect software RAID" which is terrible
		if out, err := cmd.Output(); err == nil {
			device = "/dev/" + strings.TrimSpace(string(out))
		}
	}
	os.MkdirAll(baseName, 0755)
	cmd := exec.Command("mount", partition, baseName)
	//cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	log.Debugf("mountdevice return2 -> d: %s, p: %s", device, partition)
	return device, partition, cmd.Run()
}

func GetStatePartition() string {
	cfg := config.LoadConfig()

	if dev := util.ResolveDevice(cfg.Rancher.State.Dev); dev != "" {
		// try the rancher.state.dev setting
		return dev
	}
	d, _, err := util.Blkid("RANCHER_STATE")
	if err != nil {
		log.Errorf("Failed to run blkid: %s", err)
	}
	return d
}

func GetDefaultPartition(device string) string {
	if strings.Contains(device, "nvme") {
		return device + "p1"
	}
	return device + "1"
}
