// Copyright 2015 CoreOS, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package proccmdline

import (
	"errors"
	"fmt"
	"io/ioutil"
	"strings"

	"github.com/sveil/os/config/cloudinit/datasource"
	"github.com/sveil/os/config/cloudinit/pkg"
	"github.com/sveil/os/pkg/log"
)

const (
	ProcCmdlineLocation        = "/proc/cmdline"
	ProcCmdlineCloudConfigFlag = "cloud-config-url"
)

type ProcCmdline struct {
	Location  string
	lastError error
}

func NewDatasource() *ProcCmdline {
	return &ProcCmdline{Location: ProcCmdlineLocation}
}

func (c *ProcCmdline) IsAvailable() bool {
	var contents []byte
	contents, c.lastError = ioutil.ReadFile(c.Location)
	if c.lastError != nil {
		return false
	}

	cmdline := strings.TrimSpace(string(contents))
	_, c.lastError = findCloudConfigURL(cmdline)
	return (c.lastError == nil)
}

func (c *ProcCmdline) Finish() error {
	return nil
}

func (c *ProcCmdline) String() string {
	return fmt.Sprintf("%s: %s (lastError: %v)", c.Type(), c.Location, c.lastError)
}

func (c *ProcCmdline) AvailabilityChanges() bool {
	return false
}

func (c *ProcCmdline) ConfigRoot() string {
	return ""
}

func (c *ProcCmdline) FetchMetadata() (datasource.Metadata, error) {
	return datasource.Metadata{}, nil
}

func (c *ProcCmdline) FetchUserdata() ([]byte, error) {
	contents, err := ioutil.ReadFile(c.Location)
	if err != nil {
		return nil, err
	}

	cmdline := strings.TrimSpace(string(contents))
	url, err := findCloudConfigURL(cmdline)
	if err != nil {
		return nil, err
	}

	client := pkg.NewHTTPClient()
	cfg, err := client.GetRetry(url)
	if err != nil {
		return nil, err
	}

	return cfg, nil
}

func (c *ProcCmdline) Type() string {
	return "proc-cmdline"
}

func findCloudConfigURL(input string) (url string, err error) {
	err = errors.New("cloud-config-url not found")
	for _, token := range strings.Split(input, " ") {
		parts := strings.SplitN(token, "=", 2)

		key := parts[0]
		key = strings.Replace(key, "_", "-", -1)

		if key != "cloud-config-url" {
			continue
		}

		if len(parts) != 2 {
			log.Printf("Found cloud-config-url in /proc/cmdline with no value, ignoring.")
			continue
		}

		url = parts[1]
		err = nil
	}

	return
}
