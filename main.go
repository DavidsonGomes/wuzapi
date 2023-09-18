package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"
	controllerBase "wuzapi/controllers/controller_base"
	"wuzapi/repository"

	"github.com/glebarez/sqlite"
	"github.com/go-resty/resty/v2"
	"github.com/gorilla/mux"
	"github.com/patrickmn/go-cache"
	"github.com/rs/zerolog"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/store/sqlstore"
	waLog "go.mau.fi/whatsmeow/util/log"
	"gorm.io/gorm"
)

var (
	address    = flag.String("address", "0.0.0.0", "Bind IP Address")
	port       = flag.String("port", "8080", "Listen Port")
	waDebug    = flag.String("wadebug", "", "Enable whatsmeow debug (INFO or DEBUG)")
	logType    = flag.String("logtype", "console", "Type of log output (console or json)")
	sslcert    = flag.String("sslcertificate", "", "SSL Certificate File")
	sslprivkey = flag.String("sslprivatekey", "", "SSL Certificate Private Key File")
	token      = flag.String("token", "", "Token for authentication an Admin user")
	container  *sqlstore.Container

	killchannel   = make(map[int](chan bool))
	userinfocache = cache.New(5*time.Minute, 10*time.Minute)
	logger        zerolog.Logger
)

func init() {

	flag.Parse()

	if *logType == "json" {
		logger = zerolog.New(os.Stdout).With().Timestamp().Str("role", filepath.Base(os.Args[0])).Logger()
	} else {
		output := zerolog.ConsoleWriter{Out: os.Stdout, TimeFormat: time.RFC3339}
		logger = zerolog.New(output).With().Timestamp().Str("role", filepath.Base(os.Args[0])).Logger()
	}
}

func main() {

	ex, err := os.Executable()
	if err != nil {
		panic(err)
	}
	exPath := filepath.Dir(ex)

	dbDirectory := exPath + "/dbdata"
	_, err = os.Stat(dbDirectory)
	if os.IsNotExist(err) {
		errDir := os.MkdirAll(dbDirectory, 0751)
		if errDir != nil {
			panic("Could not create dbdata directory")
		}
	}

	if token == nil || *token == "" {
		*token = "1234ABC"
	}

	db, err := gorm.Open(sqlite.Open(exPath+"/dbdata/users.db"), &gorm.Config{})
	if err != nil {
		log.Fatal(err)
	}

	err = db.AutoMigrate(&repository.User{})
	if err != nil {
		log.Fatal(err)
	}

	userRepository := repository.NewUserRepository(db)
	CreateAdminUser(*token, userRepository)

	if *waDebug != "" {
		dbLog := waLog.Stdout("Database", *waDebug, true)
		container, err = sqlstore.New("sqlite", "file:"+exPath+"/dbdata/main.db?_foreign_keys=on&_busy_timeout=3000", dbLog)
	} else {
		container, err = sqlstore.New("sqlite", "file:"+exPath+"/dbdata/main.db?_foreign_keys=on&_busy_timeout=3000", nil)
	}
	if err != nil {
		panic(err)
	}

	s := &controllerBase.Controller{
		Router:        mux.NewRouter(),
		Repository:    userRepository,
		ExPath:        exPath,
		UserInfoCache: userinfocache,
		KillChannel:   killchannel,
		ClientPointer: make(map[int]*whatsmeow.Client),
		Container:     container,
		ClientHttp:    make(map[int]*resty.Client),
		WaDebug:       waDebug,
		LogType:       logType,
	}

	routes(s)

	s.ConnectOnStartup()

	srv := &http.Server{
		Addr:    *address + ":" + *port,
		Handler: s.Router,
	}

	done := make(chan os.Signal, 1)
	signal.Notify(done, os.Interrupt, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		if *sslcert != "" {
			if err := srv.ListenAndServeTLS(*sslcert, *sslprivkey); err != nil && err != http.ErrServerClosed {
				//log.Fatalf("listen: %s\n", err)
				logger.Fatal().Err(err).Msg("Startup failed")
			}
		} else {
			if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				//log.Fatalf("listen: %s\n", err)
				logger.Fatal().Err(err).Msg("Startup failed")
			}
		}
	}()
	//wlog.Infof("Server Started. Listening on %s:%s", *address, *port)
	logger.Info().Str("address", *address).Str("port", *port).Msg("Server Started")

	<-done
	logger.Info().Msg("Server Stoped")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer func() {
		// extra handling here
		cancel()
	}()

	if err := srv.Shutdown(ctx); err != nil {
		logger.Error().Str("error", fmt.Sprintf("%+v", err)).Msg("Server Shutdown Failed")
		os.Exit(1)
	}
	logger.Info().Msg("Server Exited Properly")
}

func CreateAdminUser(token string, userRepository repository.UserRepository) {
	err := userRepository.CreateAdminUser(token)
	if err != nil {
		log.Fatal(err)
	}
}
