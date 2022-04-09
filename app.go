package main

import (
	"bufio"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"time"

	"github.com/MaratBR/TelephonistAgent/locales"
	"github.com/MaratBR/TelephonistAgent/logging"
	"github.com/MaratBR/TelephonistAgent/telephonist"
	"github.com/juju/fslock"
	"github.com/parnurzeal/gorequest"
	"github.com/urfave/cli/v2"
	"go.uber.org/zap"
)

const (
	APPLICATION_NAME     = "TelephonistAgent"
	VERBOSE_F            = "verbose"
	CONFIG_FOLDER_PATH_F = "rootPath"
	CONFIG_PATH_F        = "config"
	AGENT_CONFIG_PATH_F  = "agent-config"
	SOCKET_F             = "socket"
)

var (
	ErrApplicationKeyIsMissing = fmt.Errorf(
		locales.M.KeyMissing+". "+locales.M.TryRunInit+"(%s init)", os.Args[0])
	logger = logging.ChildLogger("app")
)

type AppConfig struct {
	ApplicationKey     string
	TelephonistAddress string
}

type App struct {
	cli.App
	fsLock *fslock.Lock

	appConfigFile     *ConfigFile
	appConfig         *AppConfig
	telephonistClient *telephonist.Client
	scheduler         *ApplicationScheduler
	configFolderPath  string
}

func fatalOnErr(err error) {
	if err != nil {
		logger.Fatal(err.Error())
	}
}

func CreateNewApp() *App {
	app := &App{
		appConfig: &AppConfig{
			TelephonistAddress: "https://localhost:5789",
		},
	}
	app.Before = app.before
	app.Name = APPLICATION_NAME
	app.Usage = "Supervise multiple tasks and report all information to the Telephonist server"
	app.After = app.after
	app.Flags = []cli.Flag{
		&cli.BoolFlag{
			Name:  "restart",
			Usage: locales.M.Cli.Flag.Restart,
		},
		&cli.BoolFlag{
			Name:  "secure",
			Value: false,
			Usage: locales.M.Cli.Flag.Secure,
		},
		&cli.StringFlag{
			Name:  CONFIG_FOLDER_PATH_F,
			Value: "/etc/telephonist-agent",
			Usage: "path to the main configuration folder with client.json and client.lock",
		},
		&cli.StringFlag{
			Name:  CONFIG_PATH_F,
			Value: "",
			Usage: "sets the path to the main config file",
		},
		&cli.StringFlag{
			Name:  AGENT_CONFIG_PATH_F,
			Value: "",
			Usage: "sets the path to the agent config file",
		},
		&cli.BoolFlag{
			Name:    VERBOSE_F,
			Aliases: []string{"v"},
			Value:   false,
			Usage:   "if set, outputs debug messages",
		},
	}
	app.Commands = []*cli.Command{
		{
			Name:   "test",
			Usage:  "tests if configuration is valid or not",
			Action: app.testAction,
		},
		{
			Name:   "refetch-config",
			Usage:  "tries to connect to the running instance of TelephonistAgent and request a config reload",
			Action: app.refetchConfigAction,
		},
		{
			Name:   "init",
			Usage:  locales.M.Cli.Actions.Init,
			Action: app.initAction,
		},
		{
			Name: "service",
			Subcommands: []*cli.Command{
				{
					Name:   "install",
					Action: app.installServiceAction,
				},
			},
		},
	}
	app.Action = app.action
	return app
}

func (a *App) before(c *cli.Context) error {
	if c.Bool(VERBOSE_F) {

	}

	fatalOnErr(os.MkdirAll(c.String(CONFIG_FOLDER_PATH_F), os.ModePerm))
	a.configFolderPath = c.String(CONFIG_FOLDER_PATH_F)
	configPath := c.String(CONFIG_PATH_F)
	if configPath == "" {
		configPath = a.filenameHelper("client-config.json")
	}
	a.appConfigFile = NewConfigFile(configPath)
	//TODO: check if we can open or write config file

	return nil
}

func (a *App) after(c *cli.Context) error {
	if a.fsLock != nil {
		if err := a.fsLock.Unlock(); err != nil {
			return err
		}
	}
	logging.Root.Sync()
	return nil
}

// #region Actions

func (a *App) refetchConfigAction(c *cli.Context) error {
	return nil
}

func (a *App) startCRProcedure() {
	u, err := url.Parse(a.appConfig.TelephonistAddress)
	if err != nil {
		fmt.Fprintf(os.Stderr, locales.M.InvalidURL+"\n", a.appConfig.TelephonistAddress)
		os.Exit(1)
	}
	uiURL, _ := url.Parse(a.appConfig.TelephonistAddress)
	uiURL.Path = "/admin/applications/cr"
	{
		query := uiURL.Query()
		query.Add("gc", "1")
		uiURL.RawQuery = query.Encode()
	}
	fmt.Printf(locales.M.PleaseGoToCR+"\n", uiURL.String())
	println(locales.M.DomainNameNote)
	valid := false
	reader := bufio.NewReader(os.Stdin)
	name := readStringWithCondition(locales.M.Name, isNotEmptyString)
	displayName := readString(locales.M.DisplayNameOrEmpty)
	tags := readTags(locales.M.Tags)

	client, err := telephonist.NewClient(telephonist.ClientOptions{URL: u})
	if err != nil {
		panic(err)
	}

	var response telephonist.CodeRegistrationCompleted
	for !valid {
		code, _ := reader.ReadString('\n')
		code = strings.Trim(code[:len(code)-1], " \t")
		response, err = client.SubmitCodeRegistration(code, &telephonist.CreateApplicationRequest{
			Name:        name,
			DisplayName: &displayName,
			Tags:        tags,
		})
		if err != nil {
			fmt.Fprintln(os.Stderr, err.Error())
		} else {
			valid = true
			a.appConfig.ApplicationKey = response.Key
			err = a.appConfigFile.Write(a.appConfig)
			if err != nil {
				fmt.Fprintf(os.Stderr, locales.M.FailedToWriteConfig+"\n", err.Error())
				fmt.Printf(locales.M.KeyIs+"\n", response.Key)
			}
		}
	}
}

func (a *App) initAction(c *cli.Context) error {
	println(fmt.Sprintf("config: %s", a.appConfigFile.filepath))
	err := a.appConfigFile.LoadOrCreate(a.appConfig)
	if err != nil {
		println(fmt.Sprintf("failed to load config file: %s", err.Error()))
		os.Exit(1)
	}
	reader := bufio.NewReader(os.Stdin)
	if strings.Trim(a.appConfig.TelephonistAddress, " \t\n") == "" {
		println(locales.M.APIURLMissing)
		valid := false

		var (
			err  error
			text string
			u    *url.URL
		)

		for !valid {
			println(locales.M.EnterAPIURL)
			print(">")
			text, _ = reader.ReadString('\n')
			text = text[:len(text)-1]
			u, err = url.Parse(text)
			if err != nil {
				fmt.Fprintf(os.Stderr, locales.M.InvalidURL+"\n", text)
			}
		}

		client, err := telephonist.NewClient(telephonist.ClientOptions{
			URL: u,
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to create telephonist client: %s\n", err.Error())
		}
		err = client.Probe()
		if err != nil {
			fmt.Fprintf(os.Stderr, locales.M.FailedToConnectToTheServer+"\n", err.Error())
			os.Exit(1)
		}

		a.appConfig.TelephonistAddress = text
	} else {
		fmt.Printf(locales.M.APIURLIs+"\n", a.appConfig.TelephonistAddress)
		valid := false

		for !valid {
			println(locales.M.InputNewURLOrEmpty)
			print(">")
			text, _ := reader.ReadString('\n')
			text = text[:len(text)-1]
			if strings.Trim(text, " \n\t") == "" {
				valid = true
			} else {
				_, err := url.Parse(text)
				if err != nil {
					fmt.Fprintf(os.Stderr, locales.M.InvalidURL+"\n", text)
				} else {
					valid = true
					a.appConfig.TelephonistAddress = text
				}
			}
		}
	}

	if strings.Trim(a.appConfig.ApplicationKey, " \n\t") == "" {
		fmt.Fprintln(os.Stderr, locales.M.KeyMissing)

		println(locales.M.WantCR)
		if readYn() {
			a.startCRProcedure()
		} else {
			var key string
			for len(key) == 0 {
				println(locales.M.InputKey)
				print(">")
				key, _ = reader.ReadString('\n')
				key = strings.Trim(key, " \n\t")
			}
		}
	} else {
		fmt.Printf(locales.M.KeyIs+"\n", a.appConfig.ApplicationKey)
		println(locales.M.InputNewKeyOrEmpty)
		print(">")
		text, _ := reader.ReadString('\n')
		text = strings.Trim(text[:len(text)-1], " \t")
		if text != "" {
			a.appConfig.ApplicationKey = text
		}
	}

	println(locales.M.FinalConfigIs)
	println("ApplicationKey = " + a.appConfig.ApplicationKey)
	println("TelephonistAddress = " + a.appConfig.TelephonistAddress)
	println()
	println(locales.M.IsThatOkay)
	if readYn() {
		err = a.appConfigFile.Write(a.appConfig)
		if err != nil {
			fmt.Fprintf(os.Stderr, locales.M.FailedToWriteConfig+"\n", err.Error())
		}
	}
	return nil
}

func (a *App) testAction(c *cli.Context) error {
	err := a.appConfigFile.Load(&AppConfig{})
	if err != nil {
		return err
	}
	println("config is OK")
	return nil
}

// action runs the main action of this app (agent itself)
func (a *App) action(c *cli.Context) error {
	fatalOnErr(a.lockFile(c))
	var (
		err error
	)
	err = a.appConfigFile.LoadOrCreate(a.appConfig)
	if err != nil {
		logger.Fatal("failed to load config file",
			zap.String("error", err.Error()),
			zap.String("file", a.appConfigFile.filepath),
		)
	}
	logger.Debug("config file located: "+a.appConfigFile.filepath,
		zap.String("cfgPath", a.appConfigFile.filepath),
	)
	logger.Debug("api url = " + a.appConfig.TelephonistAddress)

	err = a.prepareConfig()
	if err != nil {
		return fmt.Errorf("prepareConfig: %s", err.Error())
	}

	err = a.createTelephonistClient()
	if err != nil {
		return err
	}

	err = a.createScheduler()
	if err != nil {
		return err
	}

	err = a.scheduler.Start()
	if err != nil {
		return fmt.Errorf("failed to start executor: %s", err.Error())
	}

	server := NewServer(&ServerOptions{
		Scheduler: a.scheduler,
	})
	server.Start()

	// wait for interrupt
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt)
	<-sigChan

	println("\rgraceful shutdown...")

	server.Stop()
	a.scheduler.Stop()

	return nil
}

// #endregion

func (a *App) createTelephonistClient() error {
	u, err := url.Parse(a.appConfig.TelephonistAddress)
	if err != nil {
		return fmt.Errorf("invalid URL format: %s", err.Error())
	}
	if u.Scheme != "" && u.Scheme != "https" && u.Scheme != "http" {
		return errors.New("invalid URL schema, schema must be either http or https or empty (\"\")")
	}

	{
		u.Scheme = "https"
		_, _, errs := gorequest.New().Get(u.String()).End()
		if errs != nil {
			logger.Warn("failed to connect to the API through https, will assume http protocol is used")
			u.Scheme = "http"
		}
	}

	a.telephonistClient, err = telephonist.NewClient(telephonist.ClientOptions{
		APIKey: a.appConfig.ApplicationKey,
		URL:    u,
	})
	if err != nil {
		return fmt.Errorf("failed to create a client: %s", err.Error())
	}

	err = a.telephonistClient.Probe()
	if err != (*telephonist.CombinedError)(nil) {
		if unexpected, ok := err.(*telephonist.UnexpectedStatusCode); ok {
			if unexpected.Status == 401 {
				return errors.New("authentication failed, make sure that API key is correct")
			}
		}
		return err

	}

	return nil
}

func (a *App) createScheduler() error {
	if a.telephonistClient == nil {
		panic("telephonistClient is not set")
	}
	var err error
	a.scheduler, err = NewApplicationScheduler(ApplicationSchedulerOptions{
		ConfigPath: a.filenameHelper("executor-config.json"),
		Client:     a.telephonistClient,
	})
	return err
}

//#region server

//#endregion

// #region Utils

func (a *App) prepareConfig() error {
	a.appConfig.ApplicationKey = strings.Trim(a.appConfig.ApplicationKey, " \t\n")
	if a.appConfig.ApplicationKey == "" {
		return ErrApplicationKeyIsMissing
	}
	return nil
}

func (a *App) filenameHelper(filename string) string {
	return filepath.Join(a.configFolderPath, filename)
}

func (self *App) lockFile(c *cli.Context) error {
	if self.fsLock != nil {
		return nil
	}
	lockFilepath := self.filenameHelper("client.lock")
	self.fsLock = fslock.New(lockFilepath)
	err := self.fsLock.LockWithTimeout(time.Second * 1)
	if err != nil {
		return fmt.Errorf("failed to acquire the lock %s: %s, make sure you only run one instance of the agent at a time and then try again", lockFilepath, err.Error())
	}
	return nil
}

// #endregion

//#region Service installation, checks etc.

func (self *App) installServiceAction(c *cli.Context) error {
	err := createUserAndModifyConfDirPerms()
	if err != nil {
		return err
	}
	err = moveExecutable()
	if err != nil {
		return err
	}
	err = createServiceFile()
	if err != nil {
		return err
	}
	return nil
}
