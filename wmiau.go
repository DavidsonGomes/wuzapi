package main

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
	"wuzapi/internal/helpers"
	internalTypes "wuzapi/internal/types"

	"github.com/go-resty/resty/v2"
	"github.com/mdp/qrterminal/v3"
	"github.com/patrickmn/go-cache"
	"github.com/skip2/go-qrcode"
	"go.mau.fi/whatsmeow"
	waProto "go.mau.fi/whatsmeow/binary/proto"
	"go.mau.fi/whatsmeow/store"
	_ "modernc.org/sqlite"

	//	"go.mau.fi/whatsmeow/store/sqlstore"

	waLog "go.mau.fi/whatsmeow/util/log"
	//"google.golang.org/protobuf/proto"
)

// var wlog waLog.Logger
var clientPointer = make(map[int]*whatsmeow.Client)
var clientHttp = make(map[int]*resty.Client)
var historySyncID int32

// Connects to Whatsapp Websocket on server startup if last state was connected
func (s *server) connectOnStartup() {
	rows, err := s.db.Query("SELECT id,token,jid,webhook,events FROM users WHERE connected=1")
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
			userinfocache.Set(token, v, cache.NoExpiration)
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
					if !helpers.Find(messageTypes, arg) {
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
			killchannel[userid] = make(chan bool)
			go s.startClient(userid, jid, token, subscribedEvents)
		}
	}
	err = rows.Err()
	if err != nil {
		log.Error().Err(err).Msg("DB Problem")
	}
}

func (s *server) startClient(userID int, textjid string, token string, subscriptions []string) {

	log.Info().Str("userid", strconv.Itoa(userID)).Str("jid", textjid).Msg("Starting websocket connection to Whatsapp")

	var deviceStore *store.Device
	var err error

	if clientPointer[userID] != nil {
		isConnected := clientPointer[userID].IsConnected()
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
		deviceStore, err = container.GetDevice(jid)
		if err != nil {
			panic(err)
		}
	} else {
		log.Warn().Msg("No jid found. Creating new device")
		deviceStore = container.NewDevice()
	}

	if deviceStore == nil {
		log.Warn().Msg("No store found. Creating new one")
		deviceStore = container.NewDevice()
	}

	//store.CompanionProps.PlatformType = waProto.CompanionProps_CHROME.Enum()
	//store.CompanionProps.Os = proto.String("Mac OS")

	osName := "Mac OS 10"
	store.DeviceProps.PlatformType = waProto.DeviceProps_UNKNOWN.Enum()
	store.DeviceProps.Os = &osName

	clientLog := waLog.Stdout("Client", *waDebug, true)
	var client *whatsmeow.Client
	if *waDebug != "" {
		client = whatsmeow.NewClient(deviceStore, clientLog)
	} else {
		client = whatsmeow.NewClient(deviceStore, nil)
	}
	clientPointer[userID] = client

	mycli := helpers.MyClient{
		WAClient:       client,
		EventHandlerID: 1,
		UserID:         userID,
		Token:          token,
		Subscriptions:  subscriptions,
		Db:             s.db,
		UserInfoCache:  userinfocache,
		KillChannel:    killchannel,
		ClientHttp:     clientHttp,
	}

	mycli.EventHandlerID = mycli.WAClient.AddEventHandler(mycli.MyEventHandler)
	clientHttp[userID] = resty.New()
	clientHttp[userID].SetRedirectPolicy(resty.FlexibleRedirectPolicy(15))
	if *waDebug == "DEBUG" {
		clientHttp[userID].SetDebug(true)
	}
	clientHttp[userID].SetTimeout(5 * time.Second)
	clientHttp[userID].SetTLSClientConfig(&tls.Config{InsecureSkipVerify: true})

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
					if *logType != "json" {
						qrterminal.GenerateHalfBlock(evt.Code, qrterminal.L, os.Stdout)
						fmt.Println("QR code:\n", evt.Code)
					}
					// Store encoded/embeded base64 QR on database for retrieval with the /qr endpoint
					image, _ := qrcode.Encode(evt.Code, qrcode.Medium, 256)
					base64qrcode := "data:image/png;base64," + base64.StdEncoding.EncodeToString(image)
					sqlStmt := `UPDATE users SET qrcode=? WHERE id=?`
					_, err := s.db.Exec(sqlStmt, base64qrcode, userID)
					if err != nil {
						log.Error().Err(err).Msg(sqlStmt)
					}
				} else if evt.Event == "timeout" {
					// Clear QR code from DB on timeout
					sqlStmt := `UPDATE users SET qrcode=? WHERE id=?`
					_, err := s.db.Exec(sqlStmt, "", userID)
					if err != nil {
						log.Error().Err(err).Msg(sqlStmt)
					}
					log.Warn().Msg("QR timeout killing channel")
					delete(clientPointer, userID)
					killchannel[userID] <- true
				} else if evt.Event == "success" {
					log.Info().Msg("QR pairing ok!")
					// Clear QR code after pairing
					sqlStmt := `UPDATE users SET qrcode=? WHERE id=?`
					_, err := s.db.Exec(sqlStmt, "", userID)
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
		case <-killchannel[userID]:
			log.Info().Str("userid", strconv.Itoa(userID)).Msg("Received kill signal")
			client.Disconnect()
			delete(clientPointer, userID)
			sqlStmt := `UPDATE users SET connected=0 WHERE id=?`
			_, err := s.db.Exec(sqlStmt, userID)
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
