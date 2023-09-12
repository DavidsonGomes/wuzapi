package session

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
	controllerBase "wuzapi/controllers/controller_base"
	"wuzapi/internal/helpers"
	internalTypes "wuzapi/internal/types"

	"github.com/justinas/alice"
	"github.com/patrickmn/go-cache"
	"github.com/rs/zerolog/log"
)

type SessionController struct {
	*controllerBase.Controller
}

func (s *SessionController) SignRoutes(c alice.Chain) {
	s.Router.Handle("/session/connect", c.Then(s.Connect())).Methods("POST")
	s.Router.Handle("/session/disconnect", c.Then(s.Disconnect())).Methods("POST")
	s.Router.Handle("/session/logout", c.Then(s.Logout())).Methods("POST")
	s.Router.Handle("/session/status", c.Then(s.GetStatus())).Methods("GET")
	s.Router.Handle("/session/qr", c.Then(s.GetQR())).Methods("GET")
}

// Connects to Whatsapp Servers
func (s *SessionController) Connect() http.HandlerFunc {

	type connectStruct struct {
		Subscribe []string
		Immediate bool
	}

	return func(w http.ResponseWriter, r *http.Request) {

		webhook := r.Context().Value("userinfo").(internalTypes.Values).Get("Webhook")
		jid := r.Context().Value("userinfo").(internalTypes.Values).Get("Jid")
		txtid := r.Context().Value("userinfo").(internalTypes.Values).Get("Id")
		token := r.Context().Value("userinfo").(internalTypes.Values).Get("Token")
		userid, _ := strconv.Atoi(txtid)
		eventstring := ""

		// Decodes request BODY looking for events to subscribe
		decoder := json.NewDecoder(r.Body)
		var t connectStruct
		err := decoder.Decode(&t)
		if err != nil {
			s.Respond(w, r, http.StatusBadRequest, errors.New("Could not decode Payload"))
			return
		}

		if s.ClientPointer[userid] != nil {
			s.Respond(w, r, http.StatusInternalServerError, errors.New("Already Connected"))
			return
		} else {

			var subscribedEvents []string
			if len(t.Subscribe) < 1 {
				if !helpers.Find(subscribedEvents, "All") {
					subscribedEvents = append(subscribedEvents, "All")
				}
			} else {
				for _, arg := range t.Subscribe {
					if !helpers.Find(internalTypes.MessageTypes, arg) {
						log.Warn().Str("Type", arg).Msg("Message type discarded")
						continue
					}
					if !helpers.Find(subscribedEvents, arg) {
						subscribedEvents = append(subscribedEvents, arg)
					}
				}
			}
			eventstring = strings.Join(subscribedEvents, ",")
			err = s.Repository.SetEvents(eventstring, userid)
			if err != nil {
				log.Warn().Msg("Could not set events in users table")
			}
			log.Info().Str("events", eventstring).Msg("Setting subscribed events")
			v := helpers.UpdateUserInfo(r.Context().Value("userinfo"), "Events", eventstring)
			s.UserInfoCache.Set(token, v, cache.NoExpiration)

			log.Info().Str("jid", jid).Msg("Attempt to connect")
			s.KillChannel[userid] = make(chan bool)
			go s.StartClient(userid, jid, token, subscribedEvents)

			if t.Immediate == false {
				log.Warn().Msg("Waiting 10 seconds")
				time.Sleep(10000 * time.Millisecond)

				if s.ClientPointer[userid] != nil {
					if !s.ClientPointer[userid].IsConnected() {
						s.Respond(w, r, http.StatusInternalServerError, errors.New("Failed to Connect"))
						return
					}
				} else {
					s.Respond(w, r, http.StatusInternalServerError, errors.New("Failed to Connect"))
					return
				}
			}
		}

		response := map[string]interface{}{"webhook": webhook, "jid": jid, "events": eventstring, "details": "Connected!"}
		responseJson, err := json.Marshal(response)
		if err != nil {
			s.Respond(w, r, http.StatusInternalServerError, err)
			return
		} else {
			s.Respond(w, r, http.StatusOK, string(responseJson))
			return
		}
	}
}

// Disconnects from Whatsapp websocket, does not log out device
func (s *SessionController) Disconnect() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {

		txtid := r.Context().Value("userinfo").(internalTypes.Values).Get("Id")
		jid := r.Context().Value("userinfo").(internalTypes.Values).Get("Jid")
		token := r.Context().Value("userinfo").(internalTypes.Values).Get("Token")
		userid, _ := strconv.Atoi(txtid)

		if s.ClientPointer[userid] == nil {
			s.Respond(w, r, http.StatusInternalServerError, errors.New("No session"))
			return
		}
		if s.ClientPointer[userid].IsConnected() == true {
			if s.ClientPointer[userid].IsLoggedIn() == true {
				log.Info().Str("jid", jid).Msg("Disconnection successfull")
				s.KillChannel[userid] <- true
				err := s.Repository.SetEvents("", userid)
				if err != nil {
					log.Warn().Str("userid", txtid).Msg("Could not set events in users table")
				}
				v := helpers.UpdateUserInfo(r.Context().Value("userinfo"), "Events", "")
				s.UserInfoCache.Set(token, v, cache.NoExpiration)

				response := map[string]interface{}{"Details": "Disconnected"}
				responseJson, err := json.Marshal(response)
				if err != nil {
					s.Respond(w, r, http.StatusInternalServerError, err)
				} else {
					s.Respond(w, r, http.StatusOK, string(responseJson))
				}
				return
			} else {
				log.Warn().Str("jid", jid).Msg("Ignoring disconnect as it was not connected")
				s.Respond(w, r, http.StatusInternalServerError, errors.New("Cannot disconnect because it is not logged in"))
				return
			}
		} else {
			log.Warn().Str("jid", jid).Msg("Ignoring disconnect as it was not connected")
			s.Respond(w, r, http.StatusInternalServerError, errors.New("Cannot disconnect because it is not logged in"))
			return
		}
	}
}

// Logs out device from Whatsapp (requires to scan QR next time)
func (s *SessionController) Logout() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {

		txtid := r.Context().Value("userinfo").(internalTypes.Values).Get("Id")
		jid := r.Context().Value("userinfo").(internalTypes.Values).Get("Jid")
		userid, _ := strconv.Atoi(txtid)

		if s.ClientPointer[userid] == nil {
			s.Respond(w, r, http.StatusInternalServerError, errors.New("No session"))
			return
		} else {
			if s.ClientPointer[userid].IsLoggedIn() == true && s.ClientPointer[userid].IsConnected() == true {
				err := s.ClientPointer[userid].Logout()
				if err != nil {
					log.Error().Str("jid", jid).Msg("Could not perform logout")
					s.Respond(w, r, http.StatusInternalServerError, errors.New("Could not perform logout"))
					return
				} else {
					log.Info().Str("jid", jid).Msg("Logged out")
					s.KillChannel[userid] <- true
				}
			} else {
				if s.ClientPointer[userid].IsConnected() == true {
					log.Warn().Str("jid", jid).Msg("Ignoring logout as it was not logged in")
					s.Respond(w, r, http.StatusInternalServerError, errors.New("Could not disconnect as it was not logged in"))
					return
				} else {
					log.Warn().Str("jid", jid).Msg("Ignoring logout as it was not connected")
					s.Respond(w, r, http.StatusInternalServerError, errors.New("Could not disconnect as it was not connected"))
					return
				}
			}
		}

		response := map[string]interface{}{"Details": "Logged out"}
		responseJson, err := json.Marshal(response)
		if err != nil {
			s.Respond(w, r, http.StatusInternalServerError, err)
		} else {
			s.Respond(w, r, http.StatusOK, string(responseJson))
		}
		return
	}
}

// Gets Connected and LoggedIn Status
func (s *SessionController) GetStatus() http.HandlerFunc {

	return func(w http.ResponseWriter, r *http.Request) {

		txtid := r.Context().Value("userinfo").(internalTypes.Values).Get("Id")
		userid, _ := strconv.Atoi(txtid)

		if s.ClientPointer[userid] == nil {
			s.Respond(w, r, http.StatusInternalServerError, errors.New("No session"))
			return
		}

		isConnected := s.ClientPointer[userid].IsConnected()
		isLoggedIn := s.ClientPointer[userid].IsLoggedIn()

		response := map[string]interface{}{"Connected": isConnected, "LoggedIn": isLoggedIn}
		responseJson, err := json.Marshal(response)
		if err != nil {
			s.Respond(w, r, http.StatusInternalServerError, err)
		} else {
			s.Respond(w, r, http.StatusOK, string(responseJson))
		}
		return
	}
}

// Gets QR code encoded in Base64
func (s *SessionController) GetQR() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {

		txtid := r.Context().Value("userinfo").(internalTypes.Values).Get("Id")
		userid, _ := strconv.Atoi(txtid)
		code := ""

		if s.ClientPointer[userid] == nil {
			s.Respond(w, r, http.StatusInternalServerError, errors.New("No session"))
			return
		}

		if s.ClientPointer[userid].IsConnected() == false {
			s.Respond(w, r, http.StatusInternalServerError, errors.New("Not connected"))
			return
		}

		if s.ClientPointer[userid].IsLoggedIn() == true {
			s.Respond(w, r, http.StatusInternalServerError, errors.New("Already Loggedin"))
			return
		}

		code, err := s.Repository.GetQrCode(userid)
		if err != nil {
			s.Respond(w, r, http.StatusInternalServerError, err)
			return
		}
		
		log.Info().Str("userid", txtid).Str("qrcode", code).Msg("Get QR successful")
		response := map[string]interface{}{"QRCode": fmt.Sprintf("%s", code)}
		responseJson, err := json.Marshal(response)
		if err != nil {
			s.Respond(w, r, http.StatusInternalServerError, err)
		} else {
			s.Respond(w, r, http.StatusOK, string(responseJson))
		}
		return
	}
}
