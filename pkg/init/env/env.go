package env

import (
	"os"

	"github.com/sveil/os/config"
	"github.com/sveil/os/pkg/init/fsmount"
	"github.com/sveil/os/pkg/log"
	"github.com/sveil/os/pkg/util/network"
)

func Init(c *config.CloudConfig) (*config.CloudConfig, error) {
	os.Setenv("PATH", "/sbin:/usr/sbin:/usr/bin")
	if fsmount.IsInitrd() {
		log.Debug("Booting off an in-memory filesystem")
		// Magic setting to tell Docker to do switch_root and not pivot_root
		os.Setenv("DOCKER_RAMDISK", "true")
	} else {
		log.Debug("Booting off a persistent filesystem")
	}

	return c, nil
}

func Proxy(cfg *config.CloudConfig) (*config.CloudConfig, error) {
	network.SetProxyEnvironmentVariables()

	return cfg, nil
}
