package control

import (
	"fmt"
	"io/ioutil"
	"net/url"
	"os"
	"runtime"
	"strings"

	"github.com/sveil/os/cmd/power"
	"github.com/sveil/os/config"
	"github.com/sveil/os/pkg/compose"
	"github.com/sveil/os/pkg/docker"
	"github.com/sveil/os/pkg/log"
	"github.com/sveil/os/pkg/util"
	"github.com/sveil/os/pkg/util/network"

	yaml "github.com/cloudfoundry-incubator/candiedyaml"
	"github.com/codegangsta/cli"
	dockerClient "github.com/docker/engine-api/client"
	composeConfig "github.com/docker/libcompose/config"
	"github.com/docker/libcompose/project/options"
	"golang.org/x/net/context"
)

type Images struct {
	Current   string   `yaml:"current,omitempty"`
	Available []string `yaml:"available,omitempty"`
}

func osSubcommands() []cli.Command {
	return []cli.Command{
		{
			Name:   "upgrade",
			Usage:  "upgrade to latest version",
			Action: osUpgrade,
			Flags: []cli.Flag{
				cli.BoolFlag{
					Name:  "stage, s",
					Usage: "Only stage the new upgrade, don't apply it",
				},
				cli.StringFlag{
					Name:  "image, i",
					Usage: "upgrade to a certain image",
				},
				cli.BoolFlag{
					Name:  "force, f",
					Usage: "do not prompt for input",
				},
				cli.BoolFlag{
					Name:  "no-reboot",
					Usage: "do not reboot after upgrade",
				},
				cli.BoolFlag{
					Name:  "kexec, k",
					Usage: "reboot using kexec",
				},
				cli.StringFlag{
					Name:  "append",
					Usage: "append additional kernel parameters",
				},
				cli.BoolFlag{
					Name:  "upgrade-console",
					Usage: "upgrade console even if persistent",
				},
				cli.BoolFlag{
					Name:  "debug",
					Usage: "Run installer with debug output",
				},
			},
		},
		{
			Name:  "list",
			Usage: "list the current available versions",
			Flags: []cli.Flag{
				cli.BoolFlag{
					Name:  "update, u",
					Usage: "update engine cache",
				},
			},
			Action: osMetaDataGet,
		},
		{
			Name:   "version",
			Usage:  "show the currently installed version",
			Action: osVersion,
		},
	}
}

func getImages(update bool) (*Images, error) {
	upgradeURL, err := getUpgradeURL()
	if err != nil {
		return nil, err
	}

	var body []byte

	if strings.HasPrefix(upgradeURL, "/") {
		body, err = ioutil.ReadFile(upgradeURL)
		if err != nil {
			return nil, err
		}
	} else {
		u, err := url.Parse(upgradeURL)
		if err != nil {
			return nil, err
		}

		q := u.Query()
		q.Set("current", config.Version)
		if hypervisor := util.GetHypervisor(); hypervisor == "" {
			q.Set("hypervisor", hypervisor)
		}
		u.RawQuery = q.Encode()
		upgradeURL = u.String()

		if update {
			_, err := network.UpdateCache(upgradeURL)
			if err != nil {
				log.Errorf("Failed to update os caches: %v", err)
			}
		}

		body, err = network.LoadFromNetwork(upgradeURL)
		if err != nil {
			return nil, err
		}
	}

	images, err := parseBody(body)
	if err != nil {
		return nil, err
	}

	cfg := config.LoadConfig()
	images.Current = formatImage(images.Current, cfg)
	for i := len(images.Available) - 1; i >= 0; i-- {
		images.Available[i] = formatImage(images.Available[i], cfg)
	}

	return images, nil
}

func osMetaDataGet(c *cli.Context) error {
	images, err := getImages(c.Bool("update"))
	if err != nil {
		log.Fatal(err)
	}

	client, err := docker.NewSystemClient()
	if err != nil {
		log.Fatal(err)
	}

	cfg := config.LoadConfig()
	runningName := cfg.Rancher.Upgrade.Image + ":" + config.Version
	runningName = formatImage(runningName, cfg)

	foundRunning := false
	for i := len(images.Available) - 1; i >= 0; i-- {
		image := images.Available[i]
		_, _, err := client.ImageInspectWithRaw(context.Background(), image, false)
		local := "local"
		if dockerClient.IsErrImageNotFound(err) {
			local = "remote"
		}
		available := "available"
		if image == images.Current {
			available = "latest"
		}
		var running string
		if image == runningName {
			foundRunning = true
			running = "running"
		}
		fmt.Println(image, local, available, running)
	}
	if !foundRunning {
		fmt.Println(config.Version, "running")
	}

	return nil
}

func getLatestImage() (string, error) {
	images, err := getImages(false)
	if err != nil {
		return "", err
	}

	return images.Current, nil
}

func osUpgrade(c *cli.Context) error {
	if runtime.GOARCH != "amd64" {
		log.Fatalf("ros install / upgrade only supported on 'amd64', not '%s'", runtime.GOARCH)
	}

	if isExist := checkGlobalCfg(); !isExist {
		log.Fatalf("ros upgrade cannot be supported")
	}

	image := c.String("image")

	if image == "" {
		var err error
		image, err = getLatestImage()
		if err != nil {
			log.Fatal(err)
		}
		if image == "" {
			log.Fatal("Failed to find latest image")
		}
	}
	if c.Args().Present() {
		log.Fatalf("invalid arguments %v", c.Args())
	}
	if err := startUpgradeContainer(
		image,
		c.Bool("stage"),
		c.Bool("force"),
		!c.Bool("no-reboot"),
		c.Bool("kexec"),
		c.Bool("upgrade-console"),
		c.Bool("debug"),
		c.String("append"),
	); err != nil {
		log.Fatal(err)
	}

	return nil
}

func osVersion(c *cli.Context) error {
	fmt.Println(config.Version)
	return nil
}

func startUpgradeContainer(image string, stage, force, reboot, kexec, upgradeConsole, debug bool, kernelArgs string) error {
	command := []string{
		"-t", "rancher-upgrade",
		"-r", config.Version,
	}

	if kexec {
		command = append(command, "--kexec")
	}
	if debug {
		command = append(command, "--debug")
	}

	kernelArgs = strings.TrimSpace(kernelArgs)
	if kernelArgs != "" {
		command = append(command, "-a", kernelArgs)
	}

	if upgradeConsole {
		if err := config.Set("rancher.force_console_rebuild", true); err != nil {
			log.Fatal(err)
		}
	}

	fmt.Printf("Upgrading to %s\n", image)
	confirmation := "Continue"
	imageSplit := strings.Split(image, ":")
	if len(imageSplit) > 1 && imageSplit[1] == config.Version+config.Suffix {
		confirmation = fmt.Sprintf("Already at version %s. Continue anyway", imageSplit[1])
	}
	if !force && !yes(confirmation) {
		os.Exit(1)
	}

	container, err := compose.CreateService(nil, "os-upgrade", &composeConfig.ServiceConfigV1{
		LogDriver:  "json-file",
		Privileged: true,
		Net:        "host",
		Pid:        "host",
		Image:      image,
		Labels: map[string]string{
			config.ScopeLabel: config.System,
		},
		Command: command,
	})
	if err != nil {
		return err
	}

	client, err := docker.NewSystemClient()
	if err != nil {
		return err
	}

	// Only pull image if not found locally
	if _, _, err := client.ImageInspectWithRaw(context.Background(), image, false); err != nil {
		if err := container.Pull(context.Background()); err != nil {
			return err
		}
	}

	if !stage {
		// If there is already an upgrade container, delete it
		// Up() should to this, but currently does not due to a bug
		if err := container.Delete(context.Background(), options.Delete{}); err != nil {
			return err
		}

		if err := container.Up(context.Background(), options.Up{}); err != nil {
			return err
		}

		if err := container.Log(context.Background(), true); err != nil {
			return err
		}

		if err := container.Delete(context.Background(), options.Delete{}); err != nil {
			return err
		}

		if reboot && (force || yes("Continue with reboot")) {
			log.Info("Rebooting")
			power.Reboot()
		}
	}

	return nil
}

func parseBody(body []byte) (*Images, error) {
	update := &Images{}
	err := yaml.Unmarshal(body, update)
	if err != nil {
		return nil, err
	}

	return update, nil
}

func getUpgradeURL() (string, error) {
	cfg := config.LoadConfig()
	return cfg.Rancher.Upgrade.URL, nil
}
