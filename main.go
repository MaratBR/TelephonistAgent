package main

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"syscall"
	"time"

	"github.com/MaratBR/telephonist-supervisor/config"
	"github.com/MaratBR/telephonist-supervisor/telephonist"
	"github.com/gorilla/mux"
	"github.com/juju/fslock"
	log "github.com/sirupsen/logrus"
	"github.com/urfave/cli/v2"
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
	app := &cli.App{
		Before: func(c *cli.Context) error {
			if c.Bool("verbose") {
				log.SetLevel(log.DebugLevel)
			}

			os.MkdirAll(c.String("rootPath"), os.ModePerm)

			return nil
		},
		Name:  "telephonist-supervisor",
		Usage: "Supervise multiple tasks and report all information to the Telephonist server",
		Commands: []*cli.Command{
			{
				Name:  "test",
				Usage: "tests if configuration is valid or not",
				Action: func(c *cli.Context) error {
					err := config.Test()
					if err != nil {
						return err
					}
					log.Info("config is OK")
					return nil
				},
			},
			{
				Name:  "refetch-config",
				Usage: "tries to connect to the running instance of telephonist-supervisor and request a config reload",
				Action: func(c *cli.Context) error {
					return nil
				},
			},
		},
		Flags: []cli.Flag{
			&cli.BoolFlag{
				Name:  "secure",
				Value: false,
				Usage: "if set connects to API using https and wss",
			},
			&cli.StringFlag{
				Name:  "rootPath",
				Value: "/etc/telephonist",
				Usage: "path to the main configuration folder with client.yaml and client.lock",
			},

			&cli.BoolFlag{
				Name:    "verbose",
				Aliases: []string{"v"},
				Value:   false,
				Usage:   "if set, outputs debug messages",
			},
		},
		Action: mainAction,
	}
	err := app.Run(os.Args)
	if err != nil {
		log.Fatalln(err)
	}
}

func mainAction(c *cli.Context) error {
	// lock file
	lockFilePath := filepath.Join(c.String("rootPath"), "client.lock")
	lock, err := lockFile(lockFilePath)
	if err != nil {
		return fmt.Errorf("failed to acquire lock on %s: %s", lockFilePath, err)
	}
	defer lock.Unlock()

	// initialize config
	err = config.Init()
	if err != nil {
		return fmt.Errorf("failed to initialize config: %s", err)
	}

	if !config.Ptr.HasApplicationKey() {
		log.Fatalf("application key is not set, please set application key in the settings file: %s", config.GetConfigPath())
	}
	if matched, _ := regexp.MatchString("application\\..+", config.Ptr.ApplicationKey); matched {
		log.Warnln("application key doesn't seem to match the pattern: application.*")
	}

	err = runClient()
	if err != nil {
		log.Fatalln(err)
	}
	go runUnixServerWrapped(c)

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGINT)

	for range sigChan {
		os.Exit(1)
	}
	return nil
}

func runClient() error {
	u, err := url.Parse(config.Ptr.TelephonistAddress)
	if err != nil {
		return errors.New("failed to parse server address: " + err.Error())
	}
	var client *telephonist.Client
	client, err = telephonist.NewClient(telephonist.ClientOptions{
		URL:    u,
		APIKey: config.Ptr.ApplicationKey,
	})

	if err != nil {
		return err
	}

	return client.Start()
}

func runUnixServerWrapped(c *cli.Context) {
	err := runUnixServer(filepath.Join(c.String("rootPath"), "client.socket"))
	if err != nil {
		log.Panicf("Failed to start unix server: %s", err.Error())
	}
}

func runUnixServer(socketPath string) error {
	os.Remove(socketPath)
	conn, err := net.Listen("unix", socketPath)
	if err != nil {
		return errors.New("in net.Listen: " + err.Error())
	}
	defer conn.Close()
	router := mux.NewRouter()

	router.HandleFunc("/telephonist/config-reload", func(rw http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			rw.WriteHeader(405)
		} else {
			reloadConfig()
		}
	})

	log.Info("starting unix socket server on " + socketPath)
	err = http.Serve(conn, router)
	if err != nil {
		return errors.New("in http.Serve: " + err.Error())
	}
	return nil
}

func onConfigUpdate() {

}

func reloadConfig() error {
	return nil
}
