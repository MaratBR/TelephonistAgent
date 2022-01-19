package main

import (
	"os"
	"time"

	log "github.com/inconshreveable/log15"
	"github.com/juju/fslock"
)

func lockFile(lockPath string) (*fslock.Lock, error) {
	lock := fslock.New(lockPath)
	err := lock.LockWithTimeout(time.Second)
	if err != nil {
		return nil, err
	}
	return lock, nil
}

func main() {
	app := CreateNewApp()
	err := app.Run(os.Args)
	if err != nil {
		log.Crit(err.Error())
	}
}