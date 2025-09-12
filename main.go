package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
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

var (
	countOnlyFlag bool
	updateFlag    bool
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, os.Kill, syscall.SIGTERM, syscall.SIGQUIT)
	defer cancel()

	flag.BoolVar(&countOnlyFlag, "count-only", false, "Run count once and exit")
	flag.BoolVar(&updateFlag, "update", false, "Update days API")
	flag.Parse()

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
		apiUser         = os.Getenv("DAYS_API_USER")
		apiPass         = os.Getenv("DAYS_API_PASS")
		apiURL          = os.Getenv("DAYS_API")
		countries       = []string{"Andorra", "Spain"}
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

	dayApi := NewDaysApi(apiURL, apiUser, apiPass)

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
		res, err := countDays(imapServer, imapUsername, imapPassword, imapMailboxName, location, countries...)
		if err != nil {
			slog.Warnf("countDays failed: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, res)
	})

	if countOnlyFlag {
		consoleCount(imapServer, imapUsername, imapPassword, imapMailboxName, location, countries...)
		return
	}

	if updateFlag {
		go updateAPI(ctx, dayApi, imapServer, imapUsername, imapPassword, imapMailboxName, location, countries...)
	}

	srv := &http.Server{
		Addr:    ":" + port,
		Handler: r,
	}

	go func() {
		<-ctx.Done()
		slog.Infof("shutting down server...")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			slog.Errorf("server forced to shutdown: %v", err)
		}
	}()

	slog.Infof("server listening on %s", srv.Addr)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		slog.Fatalf("listen: %s\n", err)
	}
}

func updateAPI(
	ctx context.Context,
	api *DaysApi,
	server, user, pass, mailbox string,
	location *time.Location,
	countries ...string,
) {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	slog.Infof("starting cron to update days API")

	// trigger once immediately
	once := make(chan struct{}, 1)
	once <- struct{}{}

	for {
		select {
		case <-ctx.Done():
			return
		case <-once:
		case <-ticker.C:
		}

		res, err := countDays(server, user, pass, mailbox, location, countries...)
		if err != nil {
			slog.Warnf("[CRON] countDays failed: %v", err)
			continue
		}

		for date, day := range res.DaysMap {
			current, err := api.GetDay(ctx, date)
			if err != nil {
				slog.Warnf("[CRON] GetDay %s failed: %v", date, err)
				continue
			}
			var update bool
			if current == nil {
				update = true
				current = &Day{
					Day:     date,
					Andorra: day.Andorra,
					Spain:   day.Spain,
					Note:    "[auto-updated from email]",
				}
			} else if current.Andorra != day.Andorra || current.Spain != day.Spain {
				// join note with existing note if any
				current.Note = "[auto-updated from email] " + current.Note
				current.Andorra = day.Andorra
				current.Spain = day.Spain
				update = true
			} else {
				slog.Debugf("[CRON] No updates needed for day %s", date)
			}
			if update {
				err := api.UpdateDay(ctx, *current)
				if err != nil {
					slog.Warnf("[CRON] UpdateDay %s failed: %v", date, err)
				} else {
					slog.Infof("[CRON] Updated day %s: %+v", date, day)
				}
			}
		}
	}
}
