package bootstrap

import (
	"github.com/sveil/os/config"
	"github.com/sveil/os/pkg/compose"
	"github.com/sveil/os/pkg/init/docker"
	"github.com/sveil/os/pkg/log"
	"github.com/sveil/os/pkg/sysinit"
	"github.com/sveil/os/pkg/util"
)

func bootstrapServices(cfg *config.CloudConfig) (*config.CloudConfig, error) {
	if util.ResolveDevice(cfg.Rancher.State.Dev) != "" && len(cfg.Bootcmd) == 0 {
		log.Info("NOT Running Bootstrap")

		return cfg, nil
	}
	log.Info("Running Bootstrap")
	_, err := compose.RunServiceSet("bootstrap", cfg, cfg.Rancher.BootstrapContainers)
	return cfg, err
}

func Bootstrap(cfg *config.CloudConfig) error {
	log.Info("Launching Bootstrap Docker")

	c, err := docker.Start(cfg)
	if err != nil {
		return err
	}

	defer docker.Stop(c)

	_, err = config.ChainCfgFuncs(cfg,
		[]config.CfgFuncData{
			{"bootstrap loadImages", sysinit.LoadBootstrapImages},
			{"bootstrap Services", bootstrapServices},
		})
	return err
}
