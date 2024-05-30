package libv2ray

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	mobasset "golang.org/x/mobile/asset"

	v2net "github.com/xtls/xray-core/common/net"
	v2filesystem "github.com/xtls/xray-core/common/platform/filesystem"
	v2core "github.com/xtls/xray-core/core"
	v2stats "github.com/xtls/xray-core/features/stats"
	v2serial "github.com/xtls/xray-core/infra/conf/serial"
	_ "github.com/xtls/xray-core/main/distro/all"
	v2internet "github.com/xtls/xray-core/transport/internet"

	v2applog "github.com/xtls/xray-core/app/log"
	v2commlog "github.com/xtls/xray-core/common/log"
)

var pingMap sync.Map

const (
	v2Asset     = "xray.location.asset"
	xudpBaseKey = "xray.xudp.basekey"
)

/*
V2RayPoint V2Ray Point Server
This is territory of Go, so no getter and setters!
*/
type V2RayPoint struct {
	SupportSet   V2RayVPNServiceSupportsSet
	statsManager v2stats.Manager

	dialer  *ProtectedDialer
	v2rayOP sync.Mutex

	Vpoint    *v2core.Instance
	IsRunning bool

	DomainName    string
	ConfigureFile string
}

/*V2RayVPNServiceSupportsSet To support Android VPN mode*/
type V2RayVPNServiceSupportsSet interface {
	Setup(Conf string) int
	Prepare() int
	Shutdown() int
	Protect(int) bool
	OnEmitStatus(int, string) int
}

/*RunLoop Run V2Ray main loop
 */
func (v *V2RayPoint) RunLoop() (err error) {
	v.v2rayOP.Lock()
	defer v.v2rayOP.Unlock()
	//Construct Context

	if !v.IsRunning {
		err = v.pointloop()
	}
	return
}

/*StopLoop Stop V2Ray main loop
 */
func (v *V2RayPoint) StopLoop() {
	v.v2rayOP.Lock()
	defer v.v2rayOP.Unlock()
	if v.IsRunning {
		v.shutdownInit()
		v.SupportSet.OnEmitStatus(0, "Closed")
	}
	return
}

func (v *V2RayPoint) shutdownInit() {
	v.IsRunning = false
	v.Vpoint.Close()
	v.Vpoint = nil
	v.statsManager = nil
}

func (v *V2RayPoint) pointloop() error {
	log.Println("loading core config")

	file, err := os.Open(v.ConfigureFile)
	if err != nil {
		log.Println(err)
		return err
	}

	config, err := v2serial.LoadJSONConfig(file)
	if err != nil {
		log.Println(err)
		return err
	}

	log.Println("new core")
	v.Vpoint, err = v2core.New(config)
	if err != nil {
		v.Vpoint = nil
		log.Println(err)
		return err
	}
	v.statsManager = v.Vpoint.GetFeature(v2stats.ManagerType()).(v2stats.Manager)

	log.Println("start core")
	v.IsRunning = true
	if err := v.Vpoint.Start(); err != nil {
		v.IsRunning = false
		log.Println(err)
		return err
	}

	v.SupportSet.Prepare()
	v.SupportSet.Setup("")
	v.SupportSet.OnEmitStatus(0, "Running")
	return nil
}

// InitV2Env set v2 asset path
func InitV2Env(envPath string, key string) {
	//Initialize asset API, Since Raymond Will not let notify the asset location inside Process,
	//We need to set location outside V2Ray
	if len(envPath) > 0 {
		os.Setenv(v2Asset, envPath)
	}
	if len(key) > 0 {
		os.Setenv(xudpBaseKey, key)
	}

	//Now we handle read, fallback to gomobile asset (apk assets)
	v2filesystem.NewFileReader = func(path string) (io.ReadCloser, error) {
		if _, err := os.Stat(path); os.IsNotExist(err) {
			_, file := filepath.Split(path)
			return mobasset.Open(file)
		}
		return os.Open(path)
	}
}

func StartSimpleV2RayPoint(configFile string, key int32) error {
	file, err := os.Open(configFile)
	if err != nil {
		return err
	}

	config, err := v2serial.LoadJSONConfig(file)
	if err != nil {
		return err
	}

	instance, err := v2core.New(config)
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

func StopSimpleV2RayPoint(key int32) {
	val, loaded := pingMap.LoadAndDelete(key)
	if loaded {
		if instance, ok := val.(*v2core.Instance); ok {
			instance.Close()
		}
	}
}

/*NewV2RayPoint new V2RayPoint*/
func NewV2RayPoint(s V2RayVPNServiceSupportsSet) *V2RayPoint {
	// inject our own log writer
	v2applog.RegisterHandlerCreator(v2applog.LogType_Console,
		func(lt v2applog.LogType,
			options v2applog.HandlerCreatorOptions) (v2commlog.Handler, error) {
			return v2commlog.NewLogger(createStdoutLogWriter()), nil
		})

	dialer := NewPreotectedDialer(s)
	v2internet.UseAlternativeSystemDialer(dialer)
	return &V2RayPoint{
		SupportSet: s,
		dialer:     dialer,
	}
}

/*
CheckVersionX string
This func will return libv2ray binding version and V2Ray version used.
*/
func CheckVersionX() string {
	var version = 27
	return fmt.Sprintf("Lib v%d, Xray-core v%s", version, v2core.Version())
}

func measureInstDelay(ctx context.Context, inst *v2core.Instance, url string) (int64, error) {
	if inst == nil {
		return -1, errors.New("core instance nil")
	}

	tr := &http.Transport{
		TLSHandshakeTimeout: 6 * time.Second,
		DisableKeepAlives:   true,
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			dest, err := v2net.ParseDestination(fmt.Sprintf("%s:%s", network, addr))
			if err != nil {
				return nil, err
			}
			return v2core.Dial(ctx, inst, dest)
		},
	}

	c := &http.Client{
		Transport: tr,
		Timeout:   12 * time.Second,
	}

	if len(url) <= 0 {
		url = "https://www.google.com/generate_204"
	}
	req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
	start := time.Now()
	resp, err := c.Do(req)
	if err != nil {
		return -1, err
	}

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		return -1, fmt.Errorf("status != 20x: %s", resp.Status)
	}
	resp.Body.Close()
	return time.Since(start).Milliseconds(), nil
}

// This struct creates our own log writer without datatime stamp
// As Android adds time stamps on each line
type consoleLogWriter struct {
	logger *log.Logger
}

func (w *consoleLogWriter) Write(s string) error {
	w.logger.Print(s)
	return nil
}

func (w *consoleLogWriter) Close() error {
	return nil
}

// This logger won't print data/time stamps
func createStdoutLogWriter() v2commlog.WriterCreator {
	return func() v2commlog.Writer {
		return &consoleLogWriter{
			logger: log.New(os.Stdout, "", 0)}
	}
}
