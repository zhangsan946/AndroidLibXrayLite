package libv2ray

import (
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"syscall"

	coreapplog "github.com/xtls/xray-core/app/log"
	corecommlog "github.com/xtls/xray-core/common/log"
	corefilesystem "github.com/xtls/xray-core/common/platform/filesystem"
	core "github.com/xtls/xray-core/core"
	corestats "github.com/xtls/xray-core/features/stats"
	coreserial "github.com/xtls/xray-core/infra/conf/serial"
	_ "github.com/xtls/xray-core/main/distro/all"
	coreinternet "github.com/xtls/xray-core/transport/internet"
	mobasset "golang.org/x/mobile/asset"
)

type DialerController interface {
	ProtectFd(int) bool
}

var pingMap sync.Map

// Constants for environment variables
const (
	coreAsset   = "xray.location.asset"
	xudpBaseKey = "xray.xudp.basekey"
	tunFdKey    = "xray.tun.fd"
)

// CoreController represents a controller for managing Xray core instance lifecycle
type CoreController struct {
	ConfigureFile string
	IsRunning     bool
	coreInstance  *core.Instance
	coreMutex     sync.Mutex
	statsManager  corestats.Manager
}

// setEnvVariable safely sets an environment variable and logs any errors encountered.
func setEnvVariable(key, value string) {
	if err := os.Setenv(key, value); err != nil {
		log.Printf("Failed to set environment variable %s: %v. Please check your configuration.", key, err)
	}
}

// InitCoreEnv initializes environment variables and file system handlers for the core
// It sets up asset path, certificate path, XUDP base key and customizes the file reader
// to support Android asset system
func InitCoreEnv(envPath string, key string) {
	// Set asset/cert paths
	if len(envPath) > 0 {
		setEnvVariable(coreAsset, envPath)
	}

	// Set XUDP encryption key
	if len(key) > 0 {
		setEnvVariable(xudpBaseKey, key)
	}

	// Custom file reader with path validation
	corefilesystem.NewFileReader = func(path string) (io.ReadCloser, error) {
		if _, err := os.Stat(path); os.IsNotExist(err) {
			_, file := filepath.Split(path)
			return mobasset.Open(file)
		}
		return os.Open(path)
	}
}

func registerDialerController(controller func(fd uintptr)) {
	if err := coreinternet.RegisterDialerController(func(network, address string, conn syscall.RawConn) error {
		return conn.Control(controller)
	}); err != nil {
		log.Printf("Failed to register dialer controller: %v", err)
	}
}

// AddCtrlFunc allows to call android protect function after socket is created
func RegisterDialerController(controller DialerController) {
	registerDialerController(func(fd uintptr) {
		controller.ProtectFd(int(fd))
	})
}

// NewCoreController initializes and returns a new CoreController instance
// Sets up the console log handler and associates it with the provided callback handler
func NewCoreController(controller DialerController) *CoreController {
	// Register custom logger
	if err := coreapplog.RegisterHandlerCreator(
		coreapplog.LogType_Console,
		func(lt coreapplog.LogType, options coreapplog.HandlerCreatorOptions) (corecommlog.Handler, error) {
			return corecommlog.NewLogger(createStdoutLogWriter()), nil
		},
	); err != nil {
		log.Printf("Failed to register log handler: %v", err)
	}

	RegisterDialerController(controller)

	return &CoreController{
		IsRunning: false,
	}
}

// StartLoop initializes and starts the core processing loop
// Thread-safe method that configures and runs the Xray core with the provided configuration
// Returns immediately if the core is already running
func (x *CoreController) StartLoop(tunFd int32) (err error) {
	x.coreMutex.Lock()
	defer x.coreMutex.Unlock()

	setEnvVariable(tunFdKey, strconv.Itoa(int(tunFd)))

	if x.IsRunning {
		log.Println("Core is already running")
		return nil
	}

	return x.doStartLoop()
}

// StopLoop safely stops the core processing loop and releases resources
// Thread-safe method that shuts down the core instance and triggers necessary callbacks
func (x *CoreController) StopLoop() {
	x.coreMutex.Lock()
	defer x.coreMutex.Unlock()

	if x.IsRunning {
		x.doShutdown()
	}
}

// doStartLoop sets up and starts the Xray core
func (x *CoreController) doStartLoop() error {
	log.Println("initializing core...")
	file, err := os.Open(x.ConfigureFile)
	if err != nil {
		return fmt.Errorf("config error: %w", err)
	}

	config, err := coreserial.LoadJSONConfig(file)
	if err != nil {
		return fmt.Errorf("config error: %w", err)
	}

	x.coreInstance, err = core.New(config)
	if err != nil {
		return fmt.Errorf("core init failed: %w", err)
	}
	x.statsManager = x.coreInstance.GetFeature(corestats.ManagerType()).(corestats.Manager)

	log.Println("starting core...")
	x.IsRunning = true
	if err := x.coreInstance.Start(); err != nil {
		x.IsRunning = false
		return fmt.Errorf("startup failed: %w", err)
	}

	log.Println("Starting core successfully")
	return nil
}

func (x *CoreController) doShutdown() {
	if x.coreInstance != nil {
		if err := x.coreInstance.Close(); err != nil {
			log.Printf("core shutdown error: %v", err)
		}
		x.coreInstance = nil
	}
	x.IsRunning = false
	x.statsManager = nil
}

func StartSimple(configFile string, key int32) error {
	file, err := os.Open(configFile)
	if err != nil {
		return err
	}

	config, err := coreserial.LoadJSONConfig(file)
	if err != nil {
		return err
	}

	instance, err := core.New(config)
	if err != nil {
		return err
	}

	instance.Start()
	_, loaded := pingMap.LoadOrStore(key, instance)
	if loaded {
		return errors.New("point already exist")
	}
	return nil
}

func StopSimple(key int32) {
	val, loaded := pingMap.LoadAndDelete(key)
	if loaded {
		if instance, ok := val.(*core.Instance); ok {
			instance.Close()
		}
	}
}

/*
CheckVersionX string
This func will return libv2ray binding version and V2Ray version used.
*/
func CheckVersion() string {
	return fmt.Sprintf("Xray-core v%s", core.Version())
}

// consoleLogWriter implements a log writer without datetime stamps
// as Android system already adds timestamps to each log line
type consoleLogWriter struct {
	logger *log.Logger // Standard logger
}

// Log writer implementation
func (w *consoleLogWriter) Write(s string) error {
	w.logger.Print(s)
	return nil
}

func (w *consoleLogWriter) Close() error {
	return nil
}

// createStdoutLogWriter creates a logger that won't print date/time stamps
func createStdoutLogWriter() corecommlog.WriterCreator {
	return func() corecommlog.Writer {
		return &consoleLogWriter{
			logger: log.New(os.Stdout, "", 0),
		}
	}
}
