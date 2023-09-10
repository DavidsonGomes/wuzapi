package main

import (
	"net/http"
	"os"
	"path/filepath"
	"time"
	"wuzapi/controllers/chat"
	"wuzapi/controllers/group"
	internalTypes "wuzapi/internal/types"

	"github.com/justinas/alice"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/hlog"
)

type Middleware = alice.Constructor

func (s *server) routes() {

	ex, err := os.Executable()
	if err != nil {
		panic(err)
	}
	exPath := filepath.Dir(ex)

	if *logType == "json" {
		log = zerolog.New(os.Stdout).With().Timestamp().Str("role", filepath.Base(os.Args[0])).Str("host", *address).Logger()
	} else {
		output := zerolog.ConsoleWriter{Out: os.Stdout, TimeFormat: time.RFC3339}
		log = zerolog.New(output).With().Timestamp().Str("role", filepath.Base(os.Args[0])).Str("host", *address).Logger()
	}

	c := alice.New()
	c = c.Append(s.authalice)
	c = c.Append(hlog.NewHandler(log))

	c = c.Append(hlog.AccessHandler(func(r *http.Request, status, size int, duration time.Duration) {
		hlog.FromRequest(r).Info().
			Str("method", r.Method).
			Stringer("url", r.URL).
			Int("status", status).
			Int("size", size).
			Dur("duration", duration).
			Str("userid", r.Context().Value("userinfo").(internalTypes.Values).Get("Id")).
			Msg("Got API Request")
	}))
	c = c.Append(hlog.RemoteAddrHandler("ip"))
	c = c.Append(hlog.UserAgentHandler("user_agent"))
	c = c.Append(hlog.RefererHandler("referer"))
	c = c.Append(hlog.RequestIDHandler("req_id", "Request-Id"))

	s.Router.Handle("/session/connect", c.Then(s.Connect())).Methods("POST")
	s.Router.Handle("/session/disconnect", c.Then(s.Disconnect())).Methods("POST")
	s.Router.Handle("/session/logout", c.Then(s.Logout())).Methods("POST")
	s.Router.Handle("/session/status", c.Then(s.GetStatus())).Methods("GET")
	s.Router.Handle("/session/qr", c.Then(s.GetQR())).Methods("GET")

	s.Router.Handle("/webhook", c.Then(s.SetWebhook())).Methods("POST")
	s.Router.Handle("/webhook", c.Then(s.GetWebhook())).Methods("GET")

	chatController := &chat.ChatController{Controller: s.Controller}
	chatController.SignRoutes(c)

	chatMessageController := &chat.ChatMessageController{Controller: s.Controller}
	chatMessageController.SignRoutes(c)

	s.Router.Handle("/user/create", c.Then(s.CreateUser())).Methods("POST")
	s.Router.Handle("/user/delete", c.Then(s.DeleteUser())).Methods("POST")
	s.Router.Handle("/user/fetch", c.Then(s.GetUserByToken())).Methods("POST")
	s.Router.Handle("/user/info", c.Then(s.GetUser())).Methods("GET")
	s.Router.Handle("/user/check", c.Then(s.CheckUser())).Methods("POST")
	s.Router.Handle("/user/avatar", c.Then(s.GetAvatar())).Methods("POST")
	s.Router.Handle("/user/contacts", c.Then(s.GetContacts())).Methods("GET")

	groupController := &group.GroupController{Controller: s.Controller}
	groupController.SignRoutes(c)

	s.Router.PathPrefix("/").Handler(http.FileServer(http.Dir(exPath + "/static/")))
}
