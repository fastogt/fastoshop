// Package app holds what the binary needs but no feature slice owns.
package app

import (
	"fmt"
	"strings"

	log "github.com/sirupsen/logrus"
	"gitlab.com/fastogt/gofastogt/gofastogt"

	"github.com/fastogt/fastoshop/app/version"
)

// kMaxLogFileSize: at startup a file past this is dropped and started again.
const kMaxLogFileSize = 10 * 1024 * 1024

// ProjectFormatter matches our other services: time, project, level, message.
type ProjectFormatter struct {
	log.TextFormatter
}

func (f *ProjectFormatter) Format(entry *log.Entry) ([]byte, error) {
	var levelColor int
	switch entry.Level {
	case log.DebugLevel, log.TraceLevel:
		levelColor = 30 // gray
	case log.WarnLevel:
		levelColor = 33 // yellow
	case log.ErrorLevel, log.FatalLevel, log.PanicLevel:
		levelColor = 31 // red
	default:
		levelColor = 36 // blue
	}
	return fmt.Appendf(nil, "%s %s \x1b[%dm[%s]\x1b[0m %s\n",
		entry.Time.Format(f.TimestampFormat), version.ProjectName, levelColor,
		strings.ToUpper(entry.Level.String()), entry.Message), nil
}

// SetupLogging returns the path actually used, or "" when the file cannot be opened.
func SetupLogging(logLevel, logPath string) string {
	level, err := log.ParseLevel(logLevel)
	if err != nil {
		level = log.InfoLevel
	}
	log.SetLevel(level)

	formatter := &ProjectFormatter{}
	formatter.TimestampFormat = "02/01/2006 15:04:05.000"
	log.SetFormatter(formatter)

	stable, err := gofastogt.StableFilePath(logPath)
	if err != nil {
		log.Warnf("log path %q: %v", logPath, err)
		return ""
	}
	file, err := gofastogt.InitLogFile(*stable, kMaxLogFileSize)
	if err != nil {
		log.Warnf("log file %q: %v", *stable, err)
		return ""
	}
	log.SetOutput(file)
	return *stable
}
