package logger

import (
	"fmt"
	"log"
	"os"
	"time"

	"flowhook/internal/config"
)

type LogLevel int

const (
	DEBUG LogLevel = iota
	INFO
	WARN
	ERROR
)

var currentLevel LogLevel

func init() {
	levelStr := "info"
	if config.AppConfig != nil {
		levelStr = config.AppConfig.LogLevel
	}

	switch levelStr {
	case "debug":
		currentLevel = DEBUG
	case "info":
		currentLevel = INFO
	case "warn":
		currentLevel = WARN
	case "error":
		currentLevel = ERROR
	default:
		currentLevel = INFO
	}
}

func Debug(format string, v ...interface{}) {
	if currentLevel <= DEBUG {
		log.Printf("[DEBUG] %s", fmt.Sprintf(format, v...))
	}
}

func Info(format string, v ...interface{}) {
	if currentLevel <= INFO {
		log.Printf("[INFO] %s", fmt.Sprintf(format, v...))
	}
}

func Warn(format string, v ...interface{}) {
	if currentLevel <= WARN {
		log.Printf("[WARN] %s", fmt.Sprintf(format, v...))
	}
}

func Error(format string, v ...interface{}) {
	if currentLevel <= ERROR {
		log.Printf("[ERROR] %s", fmt.Sprintf(format, v...))
	}
}

func Fatal(format string, v ...interface{}) {
	log.Fatalf("[FATAL] %s", fmt.Sprintf(format, v...))
}

// LogRequest logs HTTP request details
func LogRequest(method, path string, statusCode int, duration time.Duration) {
	Info("%s %s - %d - %v", method, path, statusCode, duration)
}

// LogError logs errors with context
func LogError(err error, context string) {
	Error("%s: %v", context, err)
}

// LogDatabase logs database operations
func LogDatabase(operation string, err error) {
	if err != nil {
		Error("Database %s failed: %v", operation, err)
	} else {
		Debug("Database %s succeeded", operation)
	}
}

// SetOutput sets the log output destination
func SetOutput(file *os.File) {
	log.SetOutput(file)
}

