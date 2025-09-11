package main

import (
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

var slog *zap.SugaredLogger

func setupLog(logLevel string) {
	level := zap.InfoLevel
	if strings.ToLower(logLevel) == "debug" {
		level = zap.DebugLevel
	}
	encoderCfg := zapcore.EncoderConfig{
		TimeKey:        "T",
		LevelKey:       "L",
		NameKey:        "N",
		CallerKey:      "C",
		MessageKey:     "M",
		StacktraceKey:  "S",
		LineEnding:     zapcore.DefaultLineEnding,
		EncodeLevel:    zapcore.CapitalColorLevelEncoder,
		EncodeDuration: zapcore.StringDurationEncoder,
	}
	consoleEncoder := zapcore.NewConsoleEncoder(encoderCfg)
	core := zapcore.NewCore(consoleEncoder, zapcore.AddSync(os.Stdout), level)
	slog = zap.New(core, zap.AddCaller(), zap.AddStacktrace(zapcore.ErrorLevel)).Sugar()
}

func main() {
	// --- Load environment ---
	err := godotenv.Load()
	if err != nil {
		log.Fatalf("loading .env file: %v", err)
	}
	setupLog(os.Getenv("LOG_LEVEL"))
	defer slog.Sync()

	var (
		imapServer      = os.Getenv("GMAIL_IMAP")
		imapUsername    = os.Getenv("GMAIL_ACCOUNT")
		imapPassword    = os.Getenv("GMAIL_APP_PASS")
		imapMailboxName = os.Getenv("GMAIL_MAILBOX")
		timezone        = os.Getenv("TIMEZONE")
		port            = os.Getenv("PORT")
	)
	location, err := time.LoadLocation(timezone)
	if err != nil {
		slog.Fatalf("loading TIMEZONE %q: %v", timezone, err)
	}
	if imapServer == "" {
		imapServer = "imap.gmail.com:993"
	}
	if imapMailboxName == "" {
		imapMailboxName = "MyLocation"
	}
	if port == "" {
		port = "8080"
	}
	if imapUsername == "" || imapPassword == "" {
		slog.Fatalf("Please set GMAIL_ACCOUNT and GMAIL_APP_PASS environment variables.")
	}

	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery())

	r.GET("/api/health", func(c *gin.Context) {
		_, closeImap, err := connectIMAP(imapServer, imapUsername, imapPassword, imapMailboxName)
		if err != nil {
			slog.Warnf("health check failed: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"ok": false, "error": err.Error()})
			return
		}
		closeImap()
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})
	r.GET("/api/count", func(c *gin.Context) {
		res, err := countDays(imapServer, imapUsername, imapPassword, imapMailboxName, location, "Andorra", "Spain")
		if err != nil {
			slog.Warnf("countDays failed: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, res)
	})

	done := make(chan struct{})
	go func() {
		defer close(done)
		consoleCount(imapServer, imapUsername, imapPassword, imapMailboxName, location, "Andorra", "Spain")
	}()

	slog.Infof("listening on :%s", port)
	if err := r.Run(":" + port); err != nil {
		slog.Fatalf("listening on port %s: %v", port, err)
	}
}
