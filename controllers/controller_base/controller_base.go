package controllerBase

import (
	"context"
	"crypto/tls"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
	"wuzapi/internal/helpers"
	internalTypes "wuzapi/internal/types"

	"github.com/go-resty/resty/v2"
	"github.com/gorilla/mux"
	"github.com/mdp/qrterminal/v3"
	"github.com/patrickmn/go-cache"
	"github.com/rs/zerolog/log"
	"github.com/skip2/go-qrcode"
	"go.mau.fi/whatsmeow"
	waProto "go.mau.fi/whatsmeow/binary/proto"
	"go.mau.fi/whatsmeow/store"
	"go.mau.fi/whatsmeow/store/sqlstore"
	waLog "go.mau.fi/whatsmeow/util/log"
)

type Controller struct {
	Db            *sql.DB
	Router        *mux.Router
	ExPath        string
	ClientPointer map[int]*whatsmeow.Client
	KillChannel   map[int](chan bool)
	UserInfoCache *cache.Cache
	Container     *sqlstore.Container
	WaDebug       *string
	ClientHttp    map[int]*resty.Client
	LogType       *string
}

// Writes JSON response to API clients
func (s *Controller) Respond(w http.ResponseWriter, r *http.Request, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	dataenvelope := map[string]interface{}{"code": status}
	if err, ok := data.(error); ok {
		dataenvelope["error"] = err.Error()
		dataenvelope["success"] = false
	} else {
		mydata := make(map[string]interface{})
		err = json.Unmarshal([]byte(data.(string)), &mydata)
		if err != nil {
			log.Error().Str("error", fmt.Sprintf("%v", err)).Msg("Error unmarshalling JSON")
		}
		dataenvelope["data"] = mydata
		dataenvelope["success"] = true
	}
	data = dataenvelope

	if err := json.NewEncoder(w).Encode(data); err != nil {
		panic("respond: " + err.Error())
	}
}

// Connects to Whatsapp Websocket on server startup if last state was connected
func (s *Controller) ConnectOnStartup() {
	rows, err := s.Db.Query("SELECT id,token,jid,webhook,events FROM users WHERE connected=1")
	if err != nil {
		log.Error().Err(err).Msg("DB Problem")
		return
	}
	defer rows.Close()
	for rows.Next() {
		txtid := ""
		token := ""
		jid := ""
		webhook := ""
		events := ""
		err = rows.Scan(&txtid, &token, &jid, &webhook, &events)
		if err != nil {
			log.Error().Err(err).Msg("DB Problem")
			return
		} else {
			log.Info().Str("token", token).Msg("Connect to Whatsapp on startup")
			v := internalTypes.Values{M: map[string]string{
				"Id":      txtid,
				"Jid":     jid,
				"Webhook": webhook,
				"Token":   token,
				"Events":  events,
			}}
			s.UserInfoCache.Set(token, v, cache.NoExpiration)
			userid, _ := strconv.Atoi(txtid)
			// Gets and set subscription to webhook events
			eventarray := strings.Split(events, ",")

			var subscribedEvents []string
			if len(eventarray) < 1 {
				if !helpers.Find(subscribedEvents, "All") {
					subscribedEvents = append(subscribedEvents, "All")
				}
			} else {
				for _, arg := range eventarray {
					if !helpers.Find(internalTypes.MessageTypes, arg) {
						log.Warn().Str("Type", arg).Msg("Message type discarded")
						continue
					}
					if !helpers.Find(subscribedEvents, arg) {
						subscribedEvents = append(subscribedEvents, arg)
					}
				}
			}
			eventstring := strings.Join(subscribedEvents, ",")
			log.Info().Str("events", eventstring).Str("jid", jid).Msg("Attempt to connect")
			s.KillChannel[userid] = make(chan bool)
			go s.StartClient(userid, jid, token, subscribedEvents)
		}
	}
	err = rows.Err()
	if err != nil {
		log.Error().Err(err).Msg("DB Problem")
	}
}

func (s *Controller) StartClient(userID int, textjid string, token string, subscriptions []string) {

	log.Info().Str("userid", strconv.Itoa(userID)).Str("jid", textjid).Msg("Starting websocket connection to Whatsapp")

	var deviceStore *store.Device
	var err error

	if s.ClientPointer[userID] != nil {
		isConnected := s.ClientPointer[userID].IsConnected()
		if isConnected == true {
			return
		}
	}

	/*  container is initialized on main to have just one connection and avoid sqlite locks

		dbDirectory := "dbdata"
	    _, err = os.Stat(dbDirectory)
	    if os.IsNotExist(err) {
	        errDir := os.MkdirAll(dbDirectory, 0751)
	        if errDir != nil {
	            panic("Could not create dbdata directory")
	        }
	    }

		var container *sqlstore.Container

		if(*waDebug!="") {
			dbLog := waLog.Stdout("Database", *waDebug, true)
			container, err = sqlstore.New("sqlite", "file:./dbdata/main.db?_foreign_keys=on", dbLog)
		} else {
			container, err = sqlstore.New("sqlite", "file:./dbdata/main.db?_foreign_keys=on", nil)
		}
		if err != nil {
			panic(err)
		}
	*/

	if textjid != "" {
		jid, _ := helpers.ParseJID(textjid)
		// If you want multiple sessions, remember their JIDs and use .GetDevice(jid) or .GetAllDevices() instead.
		//deviceStore, err := container.GetFirstDevice()
		deviceStore, err = s.Container.GetDevice(jid)
		if err != nil {
			panic(err)
		}
	} else {
		log.Warn().Msg("No jid found. Creating new device")
		deviceStore = s.Container.NewDevice()
	}

	if deviceStore == nil {
		log.Warn().Msg("No store found. Creating new one")
		deviceStore = s.Container.NewDevice()
	}

	//store.CompanionProps.PlatformType = waProto.CompanionProps_CHROME.Enum()
	//store.CompanionProps.Os = proto.String("Mac OS")

	osName := "Mac OS 10"
	store.DeviceProps.PlatformType = waProto.DeviceProps_UNKNOWN.Enum()
	store.DeviceProps.Os = &osName

	s.ClientHttp[userID] = resty.New()
	var client *whatsmeow.Client
	if *s.WaDebug == "DEBUG" {
		client = whatsmeow.NewClient(
			deviceStore,
			waLog.Stdout("Client", "DEBUG", true),
		)
		s.ClientHttp[userID].SetDebug(true)
	} else {
		s.ClientHttp[userID].SetDebug(false)
		client = whatsmeow.NewClient(deviceStore, nil)
	}

	s.ClientPointer[userID] = client

	mycli := helpers.MyClient{
		WAClient:       client,
		EventHandlerID: 1,
		UserID:         userID,
		Token:          token,
		Subscriptions:  subscriptions,
		UserInfoCache:  s.UserInfoCache,
		KillChannel:    s.KillChannel,
		Db:             s.Db,
	}
	mycli.EventHandlerID = mycli.WAClient.AddEventHandler(mycli.MyEventHandler)
	s.ClientHttp[userID].SetRedirectPolicy(resty.FlexibleRedirectPolicy(15))

	s.ClientHttp[userID].SetTimeout(5 * time.Second)
	s.ClientHttp[userID].SetTLSClientConfig(&tls.Config{InsecureSkipVerify: true})

	if client.Store.ID == nil {
		// No ID stored, new login

		qrChan, err := client.GetQRChannel(context.Background())
		if err != nil {
			// This error means that we're already logged in, so ignore it.
			if !errors.Is(err, whatsmeow.ErrQRStoreContainsID) {
				log.Error().Err(err).Msg("Failed to get QR channel")
			}
		} else {
			err = client.Connect() // Si no conectamos no se puede generar QR
			if err != nil {
				panic(err)
			}
			for evt := range qrChan {
				if evt.Event == "code" {
					// Display QR code in terminal (useful for testing/developing)
					if *s.LogType != "json" {
						qrterminal.GenerateHalfBlock(evt.Code, qrterminal.L, os.Stdout)
						fmt.Println("QR code:\n", evt.Code)
					}
					// Store encoded/embeded base64 QR on database for retrieval with the /qr endpoint
					image, _ := qrcode.Encode(evt.Code, qrcode.Medium, 256)
					base64qrcode := "data:image/png;base64," + base64.StdEncoding.EncodeToString(image)
					sqlStmt := `UPDATE users SET qrcode=? WHERE id=?`
					_, err := s.Db.Exec(sqlStmt, base64qrcode, userID)
					if err != nil {
						log.Error().Err(err).Msg(sqlStmt)
					}
				} else if evt.Event == "timeout" {
					// Clear QR code from DB on timeout
					sqlStmt := `UPDATE users SET qrcode=? WHERE id=?`
					_, err := s.Db.Exec(sqlStmt, "", userID)
					if err != nil {
						log.Error().Err(err).Msg(sqlStmt)
					}
					log.Warn().Msg("QR timeout killing channel")
					delete(s.ClientPointer, userID)
					s.KillChannel[userID] <- true
				} else if evt.Event == "success" {
					log.Info().Msg("QR pairing ok!")
					// Clear QR code after pairing
					sqlStmt := `UPDATE users SET qrcode=? WHERE id=?`
					_, err := s.Db.Exec(sqlStmt, "", userID)
					if err != nil {
						log.Error().Err(err).Msg(sqlStmt)
					}
				} else {
					log.Info().Str("event", evt.Event).Msg("Login event")
				}
			}
		}

	} else {
		// Already logged in, just connect
		log.Info().Msg("Already logged in, just connect")
		err = client.Connect()
		if err != nil {
			panic(err)
		}
	}

	// Keep connected client live until disconnected/killed
	for {
		select {
		case <-s.KillChannel[userID]:
			log.Info().Str("userid", strconv.Itoa(userID)).Msg("Received kill signal")
			client.Disconnect()
			delete(s.ClientPointer, userID)
			sqlStmt := `UPDATE users SET connected=0 WHERE id=?`
			_, err := s.Db.Exec(sqlStmt, userID)
			if err != nil {
				log.Error().Err(err).Msg(sqlStmt)
			}
			return
		default:
			time.Sleep(1000 * time.Millisecond)
			//log.Info().Str("jid",textjid).Msg("Loop the loop")
		}
	}
}

func (s *Controller) AuthAlice(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

		var ctx context.Context
		userid := 0
		txtid := ""
		webhook := ""
		jid := ""
		events := ""

		// Get token from headers or uri parameters
		token := r.Header.Get("token")
		if token == "" {
			token = strings.Join(r.URL.Query()["token"], "")
		}

		myuserinfo, found := s.UserInfoCache.Get(token)
		if !found {
			log.Info().Msg("Looking for user information in DB")
			// Checks DB from matching user and store user values in context
			rows, err := s.Db.Query("SELECT id,webhook,jid,events FROM users WHERE token=? LIMIT 1", token)
			if err != nil {
				s.Respond(w, r, http.StatusInternalServerError, err)
				return
			}
			defer rows.Close()
			for rows.Next() {
				err = rows.Scan(&txtid, &webhook, &jid, &events)
				if err != nil {
					s.Respond(w, r, http.StatusInternalServerError, err)
					return
				}
				userid, _ = strconv.Atoi(txtid)
				v := internalTypes.Values{M: map[string]string{
					"Id":      txtid,
					"Jid":     jid,
					"Webhook": webhook,
					"Token":   token,
					"Events":  events,
				}}

				s.UserInfoCache.Set(token, v, cache.NoExpiration)
				ctx = context.WithValue(r.Context(), "userinfo", v)
			}
		} else {
			ctx = context.WithValue(r.Context(), "userinfo", myuserinfo)
			userid, _ = strconv.Atoi(myuserinfo.(internalTypes.Values).Get("Id"))
		}

		if userid == 0 {
			s.Respond(w, r, http.StatusUnauthorized, errors.New("Unauthorized"))
			return
		}
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// Middleware: Authenticate connections based on Token header/uri parameter
func (s *Controller) Auth(handler http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {

		var ctx context.Context
		userid := 0
		txtid := ""
		webhook := ""
		jid := ""
		events := ""

		// Get token from headers or uri parameters
		token := r.Header.Get("token")
		if token == "" {
			token = strings.Join(r.URL.Query()["token"], "")
		}

		myuserinfo, found := s.UserInfoCache.Get(token)
		if !found {
			log.Info().Msg("Looking for user information in DB")
			// Checks DB from matching user and store user values in context
			rows, err := s.Db.Query("SELECT id,webhook,jid,events FROM users WHERE token=? LIMIT 1", token)
			if err != nil {
				s.Respond(w, r, http.StatusInternalServerError, err)
				return
			}
			defer rows.Close()
			for rows.Next() {
				err = rows.Scan(&txtid, &webhook, &jid, &events)
				if err != nil {
					s.Respond(w, r, http.StatusInternalServerError, err)
					return
				}
				userid, _ = strconv.Atoi(txtid)
				v := internalTypes.Values{M: map[string]string{
					"Id":      txtid,
					"Jid":     jid,
					"Webhook": webhook,
					"Token":   token,
					"Events":  events,
				}}

				s.UserInfoCache.Set(token, v, cache.NoExpiration)
				ctx = context.WithValue(r.Context(), "userinfo", v)
			}
		} else {
			ctx = context.WithValue(r.Context(), "userinfo", myuserinfo)
			userid, _ = strconv.Atoi(myuserinfo.(internalTypes.Values).Get("Id"))
		}

		if userid == 0 {
			s.Respond(w, r, http.StatusUnauthorized, errors.New("Unauthorized"))
			return
		}
		handler(w, r.WithContext(ctx))
	}
}
