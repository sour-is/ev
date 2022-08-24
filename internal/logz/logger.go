package logz

import (
	"bytes"
	"encoding/json"
	"io"
	"log"
	"os"
	"runtime/debug"
	"strings"
	"time"

	"github.com/go-logr/stdr"
	"github.com/logzio/logzio-go"
	"go.opentelemetry.io/otel"
)

type logzwriter struct {
	name      string
	pkg       string
	goversion string
	hostname  string

	w io.Writer
}

func (l *logzwriter) Write(b []byte) (int, error) {
	i := 0
	for _, sp := range bytes.Split(b, []byte("\n")) {
		msg := struct {
			Message   string `json:"message"`
			Host      string `json:"host"`
			GoVersion string `json:"go_version"`
			Package   string `json:"pkg"`
			App       string `json:"app"`
		}{
			Message:   strings.TrimSpace(string(sp)),
			Host:      l.hostname,
			GoVersion: l.goversion,
			Package:   l.pkg,
			App:       l.name,
		}

		if msg.Message == "" || strings.HasPrefix(msg.Message, "#") {
			continue
		}

		b, err := json.Marshal(msg)
		if err != nil {
			return 0, err
		}

		j, err := l.w.Write(b)

		i += j
		if err != nil {
			return i, err
		}
	}
	return i, nil
}

func initLogger(name string) func() error {
	log.SetPrefix("[" + name + "] ")
	log.SetFlags(log.LstdFlags&^(log.Ldate|log.Ltime) | log.Lshortfile)

	token := envSecret("LOGZIO_LOG_TOKEN", "")
	if token == "" {
		return nil
	}

	l, err := logzio.New(
		token.Secret(),
		// logzio.SetDebug(os.Stderr),
		logzio.SetUrl(env("LOGZIO_LOG_URL", "https://listener.logz.io:8071")),
		logzio.SetDrainDuration(time.Second*5),
		logzio.SetTempDirectory(env("LOGZIO_DIR", os.TempDir())),
		logzio.SetCheckDiskSpace(true),
		logzio.SetDrainDiskThreshold(70),
	)
	if err != nil {
		return nil
	}

	w := io.MultiWriter(os.Stderr, lzw(l, name))
	log.SetOutput(w)
	otel.SetLogger(stdr.New(log.Default()))

	return func() error {
		defer log.Println("logger stopped")
		log.SetOutput(os.Stderr)
		l.Stop()
		return nil
	}
}
func lzw(l io.Writer, name string) io.Writer {
	lz := &logzwriter{
		name: name,
		w:    l,
	}

	if info, ok := debug.ReadBuildInfo(); ok {
		lz.goversion = info.GoVersion
		lz.pkg = info.Path
	}
	if hostname, err := os.Hostname(); err == nil {
		lz.hostname = hostname
	}

	return lz
}
