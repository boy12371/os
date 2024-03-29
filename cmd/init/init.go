// +build linux

package init

import (
	"fmt"

	"github.com/sveil/os/config"
	"github.com/sveil/os/pkg/dfs"
	"github.com/sveil/os/pkg/init/b2d"
	"github.com/sveil/os/pkg/init/cloudinit"
	"github.com/sveil/os/pkg/init/configfiles"
	"github.com/sveil/os/pkg/init/debug"
	"github.com/sveil/os/pkg/init/docker"
	"github.com/sveil/os/pkg/init/env"
	"github.com/sveil/os/pkg/init/fsmount"
	"github.com/sveil/os/pkg/init/hypervisor"
	"github.com/sveil/os/pkg/init/modules"
	"github.com/sveil/os/pkg/init/one"
	"github.com/sveil/os/pkg/init/prepare"
	"github.com/sveil/os/pkg/init/recovery"
	"github.com/sveil/os/pkg/init/selinux"
	"github.com/sveil/os/pkg/init/sharedroot"
	"github.com/sveil/os/pkg/init/switchroot"
	"github.com/sveil/os/pkg/log"
	"github.com/sveil/os/pkg/sysinit"
)

func MainInit() {
	log.InitLogger()
	// TODO: this breaks and does nothing if the cfg is invalid (or is it due to threading?)
	defer func() {
		if r := recover(); r != nil {
			fmt.Printf("Starting Recovery console: %v\n", r)
			recovery.Recovery(nil)
		}
	}()

	if err := RunInit(); err != nil {
		log.Fatal(err)
	}
}

func RunInit() error {
	initFuncs := config.CfgFuncs{
		{"set env", env.Init},
		{"preparefs", prepare.FS},
		{"save init cmdline", prepare.SaveCmdline},
		{"mount OEM", fsmount.MountOem},
		{"debug save cfg", debug.PrintAndLoadConfig},
		{"load modules", modules.LoadModules},
		{"recovery console", recovery.LoadRecoveryConsole},
		{"b2d env", b2d.B2D},
		{"mount STATE and bootstrap", fsmount.MountStateAndBootstrap},
		{"cloud-init", cloudinit.CloudInit},
		{"read cfg and log files", configfiles.ReadConfigFiles},
		{"switchroot", switchroot.SwitchRoot},
		{"mount OEM2", fsmount.MountOem},
		{"mount BOOT", fsmount.MountBoot},
		{"write cfg and log files", configfiles.WriteConfigFiles},
		{"b2d Env", b2d.Env},
		{"hypervisor tools", hypervisor.Tools},
		{"preparefs2", prepare.FS},
		{"load modules2", modules.LoadModules},
		{"set proxy env", env.Proxy},
		{"init SELinux", selinux.Initialize},
		{"setupSharedRoot", sharedroot.Setup},
		{"sysinit", sysinit.RunSysInit},
	}

	cfg, err := config.ChainCfgFuncs(nil, initFuncs)
	if err != nil {
		recovery.Recovery(err)
	}

	launchConfig, args := docker.GetLaunchConfig(cfg, &cfg.Rancher.SystemDocker)
	launchConfig.Fork = !cfg.Rancher.SystemDocker.Exec
	//launchConfig.NoLog = true

	log.Info("Launching System Docker")
	_, err = dfs.LaunchDocker(launchConfig, config.SystemDockerBin, args...)
	if err != nil {
		log.Errorf("Error Launching System Docker: %s", err)
		recovery.Recovery(err)
		return err
	}
	// Code never gets here - rancher.system_docker.exec=true

	return one.PidOne()
}
