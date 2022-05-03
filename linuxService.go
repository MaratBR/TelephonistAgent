package main

import (
	"errors"
	"io/ioutil"
	"os"
	"os/exec"
	"strings"

	"github.com/MaratBR/TelephonistAgent/locales"
	"github.com/MaratBR/TelephonistAgent/utils"
)

const (
	serviceFile        = "/etc/systemd/system/telephonist-agent.service"
	serviceFileContent = `
[Unit]
Description=Telephonist agent
After=network-online.target
After=syslog.target

[Service]
ExecStart=/usr/local/bin/telephonist-agent
RestartSec=30

# User=telephonist
Group=telephonist

[Install]
WantedBy=multi-user.target
`
)

var allFolders = []string{
	"/etc/telephonist-agent",
	"/var/log/telephonist-agent",
	"/var/telephonist-agent",
}

func isServiceFileExists() (bool, error) {
	_, err := os.Stat(serviceFile)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

func moveExecutable() error {
	path, err := os.Executable()
	if err != nil {
		return utils.ChainError("Failed to get current executable path", err)
	}
	if path == "/usr/local/bin/telephonist-agent" {
		return errors.New(locales.M.CreateService.FileIsAlreadyInPlace)
	}
	input, err := ioutil.ReadFile(path)
	if err != nil {
		return errors.New(locales.M.CreateService.FailedToCopyExecutable)
	}
	if err := ioutil.WriteFile("/usr/local/bin/telephonist-agent", input, 0764); err != nil {
		return errors.New(locales.M.CreateService.FailedToCopyExecutable)
	}
	return nil
}

func createServiceFile() error {
	err := os.WriteFile(serviceFile, []byte(strings.Trim(serviceFileContent, " \n\t")), 0644)
	if err != nil {
		return utils.ChainError("failed to write to "+serviceFile, err)
	}

	return nil
}

func reloadDaemon() error {
	println("/usr/bin/systemctl daemon-reload")
	if err := exec.Command("/usr/bin/systemctl", "daemon-reload").Run(); err != nil {
		return utils.ChainError("Failed to reload systemd", err)
	}
	println("/usr/bin/systemctl start telephonist-agent.service")
	if err := exec.Command("/usr/bin/systemctl", "start", "telephonist-agent.service").Run(); err != nil {
		return utils.ChainError("Failed to start telephonist service through systemd", err)
	}
	return nil
}

func stopService() error {
	println("/usr/bin/systemctl stop telephonist-agent.service")
	if err := exec.Command("/usr/bin/systemctl", "stop", "telephonist-agent.service").Run(); err != nil {
		return utils.ChainError("Failed to stop service", err)
	}
	return nil
}
