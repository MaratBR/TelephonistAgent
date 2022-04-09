package main

import (
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"os/user"
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

User=telephonist
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

func modifyDirectoryOwnershipAndPermissions(dir string) error {
	if err := exec.Command("/usr/bin/chmod", "-Rf", "664", dir).Run(); err != nil {
		return utils.ChainError(locales.M.CreateService.FailedToChmodDir, err)
	}
	if err := exec.Command("/usr/bin/chmod", "-Rf", "+X", dir).Run(); err != nil {
		return utils.ChainError(locales.M.CreateService.FailedToChmodDir, err)
	}
	if err := exec.Command("/usr/bin/chown", "-Rf", "telephonist:telephonist", dir).Run(); err != nil {
		return utils.ChainError(locales.M.CreateService.FailedToChownDir, err)
	}
	return nil
}

func createUserAndModifyConfDirPerms() error {
	println(locales.M.CreateService.CreatingGroup)
	if err := exec.Command("/usr/sbin/groupadd", "telephonist", "-f").Run(); err != nil {
		return utils.ChainError(locales.M.CreateService.FailedToCreateGroup, err)
	}
	println(locales.M.CreateService.CreatingUser)
	if _, err := user.Lookup("telephonist"); err != nil {
		if _, ok := err.(user.UnknownUserError); ok {
			if err := exec.Command("/usr/sbin/useradd", "-g", "telephonist", "-r", "telephonist").Run(); err != nil {
				return utils.ChainError(locales.M.CreateService.FailedToCreateUser, err)
			}
		} else {
			return utils.ChainError("Failed to get user with name telephonist", err)
		}
	}

	somethingFailed := false

	for _, folder := range allFolders {
		println(fmt.Sprintf(locales.M.CreateService.ModifyingDirectoryPermissions, folder))
		err := modifyDirectoryOwnershipAndPermissions(folder)
		if err != nil {
			fmt.Fprintf(os.Stderr, locales.M.CreateService.FailedToModifyDirectoryPermissions+"\n", folder, err.Error())
			somethingFailed = true
		}
	}

	if somethingFailed {
		return errors.New("something is wrong, check messages above")
	}
	return nil
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
	if err := exec.Command("/usr/bin/chown", "-f", "telephonist:telephonist", "/usr/local/bin/telephonist-agent").Run(); err != nil {
		return utils.ChainError(locales.M.CreateService.FailedToChownDir, err)
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
