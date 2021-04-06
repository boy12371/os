package wait

import (
	"os"

	"github.com/sveil/os/config"
	"github.com/sveil/os/pkg/docker"
	"github.com/sveil/os/pkg/log"
)

func Main() {
	log.InitLogger()
	_, err := docker.NewClient(config.DockerHost)
	if err != nil {
		log.Errorf("Failed to connect to Docker")
		os.Exit(1)
	}

	log.Infof("Docker is ready")
}
