package helpers

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"mime"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	internalTypes "wuzapi/internal/types"
	"wuzapi/repository"
	"wuzapi/webhook"

	"github.com/go-resty/resty/v2"
	"github.com/patrickmn/go-cache"
	"github.com/rs/zerolog/log"
	"github.com/skip2/go-qrcode"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/appstate"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
)

func ParseJID(arg string) (types.JID, bool) {
	if arg == "" {
		return types.NewJID("", types.DefaultUserServer), false
	}
	if arg[0] == '+' {
		arg = arg[1:]
	}

	// Basic only digit check for recipient phone number, we want to remove @server and .session
	phonenumber := ""
	phonenumber = strings.Split(arg, "@")[0]
	phonenumber = strings.Split(phonenumber, ".")[0]
	b := true
	for _, c := range phonenumber {
		if c < '0' || c > '9' {
			b = false
			break
		}
	}
	if b == false {
		log.Warn().Msg("Bad jid format, return empty")
		recipient, _ := types.ParseJID("")
		return recipient, false
	}

	if !strings.ContainsRune(arg, '@') {
		return types.NewJID(arg, types.DefaultUserServer), true
	} else {
		recipient, err := types.ParseJID(arg)
		if err != nil {
			log.Error().Err(err).Str("jid", arg).Msg("Invalid jid")
			return recipient, false
		} else if recipient.User == "" {
			log.Error().Err(err).Str("jid", arg).Msg("Invalid jid. No server specified")
			return recipient, false
		}
		return recipient, true
	}
}

var historySyncID int32

type MyClient struct {
	WAClient       *whatsmeow.Client
	EventHandlerID uint32
	user           *repository.UserDb
	Token          string
	Subscriptions  []string
	UserInfoCache  *cache.Cache
	KillChannel    map[int](chan bool)
	ClientHttp     map[int]*resty.Client
}

func NewClient(
	WAClient *whatsmeow.Client,
	EventHandlerID uint32,
	UserID int,
	Token string,
	Subscriptions []string,
	Repository repository.UserRepository,
	UserInfoCache *cache.Cache,
	KillChannel map[int](chan bool),
	ClientHttp map[int]*resty.Client,
) (*MyClient, error) {

	user, err := repository.NewUser(Repository, UserID)
	if err != nil {
		return nil, err
	}
	return &MyClient{
		WAClient:       WAClient,
		EventHandlerID: EventHandlerID,
		user:           user,
		Token:          Token,
		Subscriptions:  Subscriptions,
		UserInfoCache:  UserInfoCache,
		KillChannel:    KillChannel,
		ClientHttp:     ClientHttp,
	}, nil
}

func (mycli *MyClient) MyEventHandler(rawEvt interface{}) {
	txtid := strconv.Itoa(mycli.user.Id)
	postmap := make(map[string]interface{})
	postmap["event"] = rawEvt
	dowebhook := 0
	path := ""

	ex, err := os.Executable()
	if err != nil {
		panic(err)
	}
	exPath := filepath.Dir(ex)

	switch evt := rawEvt.(type) {
	case *events.AppStateSyncComplete:
		if len(mycli.WAClient.Store.PushName) > 0 && evt.Name == appstate.WAPatchCriticalBlock {
			err := mycli.WAClient.SendPresence(types.PresenceAvailable)
			if err != nil {
				log.Warn().Err(err).Msg("Failed to send available presence")
			} else {
				log.Info().Msg("Marked self as available")
			}
		}
	case *events.Connected, *events.PushNameSetting:
		postmap["type"] = "SessionStatus"
		postmap["state"] = "Connected"
		dowebhook = 1

		if len(mycli.WAClient.Store.PushName) == 0 {
			return
		}
		// Send presence available when connecting and when the pushname is changed.
		// This makes sure that outgoing messages always have the right pushname.
		err := mycli.WAClient.SendPresence(types.PresenceAvailable)
		if err != nil {
			log.Warn().Err(err).Msg("Failed to send available presence")
		} else {
			log.Info().Msg("Marked self as available")
		}
		err = mycli.user.Connect()
		if err != nil {
			log.Error().Err(err).Msg(err.Error())
			return
		}
	case *events.PairSuccess:
		postmap["type"] = "PairSuccess"
		dowebhook = 1
		log.Info().Str("userid", strconv.Itoa(mycli.user.Id)).Str("token", mycli.Token).Str("ID", evt.ID.String()).Str("BusinessName", evt.BusinessName).Str("Platform", evt.Platform).Msg("QR Pair Success")

		jid := evt.ID
		mycli.user.Jid = jid.String()
		err := mycli.user.SetJid(jid.String())
		if err != nil {
			log.Error().Err(err).Msg(err.Error())
			return
		}

		myuserinfo, found := mycli.UserInfoCache.Get(mycli.Token)
		if !found {
			log.Warn().Msg("No user info cached on pairing?")
		} else {
			txtid := myuserinfo.(internalTypes.Values).Get("Id")
			token := myuserinfo.(internalTypes.Values).Get("Token")
			v := UpdateUserInfo(myuserinfo, "Jid", fmt.Sprintf("%s", jid))
			mycli.UserInfoCache.Set(token, v, cache.NoExpiration)
			log.Info().Str("jid", jid.String()).Str("userid", txtid).Str("token", token).Msg("UserDb information set")
		}
	case *events.StreamReplaced:
		log.Info().Msg("Received StreamReplaced event")
		return
	case *events.Message:
		postmap["type"] = "Message"
		dowebhook = 1
		metaParts := []string{fmt.Sprintf("pushname: %s", evt.Info.PushName), fmt.Sprintf("timestamp: %s", evt.Info.Timestamp)}
		if evt.Info.Type != "" {
			metaParts = append(metaParts, fmt.Sprintf("type: %s", evt.Info.Type))
		}
		if evt.Info.Category != "" {
			metaParts = append(metaParts, fmt.Sprintf("category: %s", evt.Info.Category))
		}
		if evt.IsViewOnce {
			metaParts = append(metaParts, "view once")
		}
		if evt.IsViewOnce {
			metaParts = append(metaParts, "ephemeral")
		}

		log.Info().Str("id", evt.Info.ID).Str("source", evt.Info.SourceString()).Str("parts", strings.Join(metaParts, ", ")).Msg("Message Received")

		// try to get Image if any
		img := evt.Message.GetImageMessage()
		if img != nil {

			// check/creates user directory for files
			userDirectory := fmt.Sprintf("%s/files/user_%s", exPath, txtid)
			_, err := os.Stat(userDirectory)
			if os.IsNotExist(err) {
				errDir := os.MkdirAll(userDirectory, 0751)
				if errDir != nil {
					log.Error().Err(errDir).Msg("Could not create user directory")
					return
				}
			}

			data, err := mycli.WAClient.Download(img)
			if err != nil {
				log.Error().Err(err).Msg("Failed to download image")
				return
			}
			exts, _ := mime.ExtensionsByType(img.GetMimetype())
			path = fmt.Sprintf("%s/%s%s", userDirectory, evt.Info.ID, exts[0])
			err = os.WriteFile(path, data, 0600)
			if err != nil {
				log.Error().Err(err).Msg("Failed to save image")
				return
			}
			log.Info().Str("path", path).Msg("Image saved")
		}

		// try to get Audio if any
		audio := evt.Message.GetAudioMessage()
		if audio != nil {

			// check/creates user directory for files
			userDirectory := fmt.Sprintf("%s/files/user_%s", exPath, txtid)
			_, err := os.Stat(userDirectory)
			if os.IsNotExist(err) {
				errDir := os.MkdirAll(userDirectory, 0751)
				if errDir != nil {
					log.Error().Err(errDir).Msg("Could not create user directory")
					return
				}
			}

			data, err := mycli.WAClient.Download(audio)
			if err != nil {
				log.Error().Err(err).Msg("Failed to download audio")
				return
			}
			exts, _ := mime.ExtensionsByType(audio.GetMimetype())
			path = fmt.Sprintf("%s/%s%s", userDirectory, evt.Info.ID, exts[0])
			err = os.WriteFile(path, data, 0600)
			if err != nil {
				log.Error().Err(err).Msg("Failed to save audio")
				return
			}
			log.Info().Str("path", path).Msg("Audio saved")
		}

		// try to get Document if any
		document := evt.Message.GetDocumentMessage()
		if document != nil {

			// check/creates user directory for files
			userDirectory := fmt.Sprintf("%s/files/user_%s", exPath, txtid)
			_, err := os.Stat(userDirectory)
			if os.IsNotExist(err) {
				errDir := os.MkdirAll(userDirectory, 0751)
				if errDir != nil {
					log.Error().Err(errDir).Msg("Could not create user directory")
					return
				}
			}

			data, err := mycli.WAClient.Download(document)
			if err != nil {
				log.Error().Err(err).Msg("Failed to download document")
				return
			}
			extension := ""
			exts, err := mime.ExtensionsByType(document.GetMimetype())
			if err != nil {
				extension = exts[0]
			} else {
				filename := document.FileName
				extension = filepath.Ext(*filename)
			}
			path = fmt.Sprintf("%s/%s%s", userDirectory, evt.Info.ID, extension)
			err = os.WriteFile(path, data, 0600)
			if err != nil {
				log.Error().Err(err).Msg("Failed to save document")
				return
			}
			log.Info().Str("path", path).Msg("Document saved")
		}
	case *events.Receipt:
		postmap["type"] = "ReadReceipt"
		dowebhook = 1
		if evt.Type == events.ReceiptTypeRead || evt.Type == events.ReceiptTypeReadSelf {
			log.Info().Strs("id", evt.MessageIDs).Str("source", evt.SourceString()).Str("timestamp", fmt.Sprintf("%d", evt.Timestamp)).Msg("Message was read")
			if evt.Type == events.ReceiptTypeRead {
				postmap["state"] = "Read"
			} else {
				postmap["state"] = "ReadSelf"
			}
		} else if evt.Type == events.ReceiptTypeDelivered {
			postmap["state"] = "Delivered"
			log.Info().Str("id", evt.MessageIDs[0]).Str("source", evt.SourceString()).Str("timestamp", fmt.Sprintf("%d", evt.Timestamp)).Msg("Message delivered")
		} else {
			// Discard webhooks for inactive or other delivery types
			return
		}
	case *events.Presence:
		postmap["type"] = "Presence"
		dowebhook = 1
		if evt.Unavailable {
			postmap["state"] = "offline"
			if evt.LastSeen.IsZero() {
				log.Info().Str("from", evt.From.String()).Msg("UserDb is now offline")
			} else {
				log.Info().Str("from", evt.From.String()).Str("lastSeen", fmt.Sprintf("%d", evt.LastSeen)).Msg("UserDb is now offline")
			}
		} else {
			postmap["state"] = "online"
			log.Info().Str("from", evt.From.String()).Msg("UserDb is now online")
		}
	case *events.HistorySync:
		postmap["type"] = "HistorySync"
		dowebhook = 1

		// check/creates user directory for files
		userDirectory := fmt.Sprintf("%s/files/user_%s", exPath, txtid)
		_, err := os.Stat(userDirectory)
		if os.IsNotExist(err) {
			errDir := os.MkdirAll(userDirectory, 0751)
			if errDir != nil {
				log.Error().Err(errDir).Msg("Could not create user directory")
				return
			}
		}

		id := atomic.AddInt32(&historySyncID, 1)
		fileName := fmt.Sprintf("%s/history-%d.json", userDirectory, id)
		file, err := os.OpenFile(fileName, os.O_WRONLY|os.O_CREATE, 0600)
		if err != nil {
			log.Error().Err(err).Msg("Failed to open file to write history sync")
			return
		}
		enc := json.NewEncoder(file)
		enc.SetIndent("", "  ")
		err = enc.Encode(evt.Data)
		if err != nil {
			log.Error().Err(err).Msg("Failed to write history sync")
			return
		}
		log.Info().Str("filename", fileName).Msg("Wrote history sync")
		_ = file.Close()
	case *events.AppState:
		log.Info().Str("index", fmt.Sprintf("%+v", evt.Index)).Str("actionValue", fmt.Sprintf("%+v", evt.SyncActionValue)).Msg("App state event received")
	case *events.LoggedOut:
		postmap["type"] = "SessionStatus"
		postmap["state"] = "LoggedOut"
		dowebhook = 1

		log.Info().Str("reason", evt.Reason.String()).Msg("Logged out")
		mycli.KillChannel[mycli.user.Id] <- true
		err := mycli.user.Disconnect()
		if err != nil {
			log.Error().Err(err).Msg(err.Error())
			return
		}
	case *events.ChatPresence:
		postmap["type"] = "ChatPresence"
		dowebhook = 1
		log.Info().Str("state", fmt.Sprintf("%s", evt.State)).Str("media", fmt.Sprintf("%s", evt.Media)).Str("chat", evt.MessageSource.Chat.String()).Str("sender", evt.MessageSource.Sender.String()).Msg("Chat Presence received")
	case *events.CallOffer:
		log.Info().Str("event", fmt.Sprintf("%+v", evt)).Msg("Got call offer")
	case *events.CallAccept:
		log.Info().Str("event", fmt.Sprintf("%+v", evt)).Msg("Got call accept")
	case *events.CallTerminate:
		log.Info().Str("event", fmt.Sprintf("%+v", evt)).Msg("Got call terminate")
	case *events.CallOfferNotice:
		log.Info().Str("event", fmt.Sprintf("%+v", evt)).Msg("Got call offer notice")
	case *events.CallRelayLatency:
		log.Info().Str("event", fmt.Sprintf("%+v", evt)).Msg("Got call relay latency")
	case *events.QR:
		postmap["type"] = "QR"
		image, _ := qrcode.Encode(evt.Codes[0], qrcode.Medium, 256)
		base64qrcode := "data:image/png;base64," + base64.StdEncoding.EncodeToString(image)
		postmap["code"] = base64qrcode
		dowebhook = 1
		log.Info().Str("event", fmt.Sprintf("%+v", evt)).Msg("Got QR")
	case *events.PairError:
		postmap["type"] = "PairError"
		dowebhook = 1
		log.Info().Str("event", fmt.Sprintf("%+v", evt)).Msg("Got QR")
	default:
		log.Warn().Str("event", fmt.Sprintf("%+v", evt)).Msg("Unhandled event")
	}

	if dowebhook == 1 {
		// call webhook
		webhookurl := ""
		myuserinfo, found := mycli.UserInfoCache.Get(mycli.Token)
		if !found {
			log.Warn().Str("token", mycli.Token).Msg("Could not call webhook as there is no user for this token")
		} else {
			webhookurl = myuserinfo.(internalTypes.Values).Get("Webhook")
		}

		if !Find(mycli.Subscriptions, postmap["type"].(string)) && !Find(mycli.Subscriptions, "All") {
			log.Warn().Str("type", postmap["type"].(string)).Msg("Skipping webhook. Not subscribed for this type")
			return
		}

		if webhookurl != "" {
			log.Info().Str("url", webhookurl).Msg("Calling webhook")
			values, _ := json.Marshal(postmap)
			webhook := webhook.Webhook{ClientHttp: mycli.ClientHttp}
			if path == "" {
				data := make(map[string]string)
				data["data"] = string(values)
				data["token"] = mycli.Token
				go webhook.CallHook(webhookurl, data, mycli.user.Id)
			} else {
				data := make(map[string]string)
				data["data"] = string(values)
				data["token"] = mycli.Token
				go webhook.CallHookFile(webhookurl, data, mycli.user.Id, path)
			}
		} else {
			log.Warn().Str("userid", strconv.Itoa(mycli.user.Id)).Msg("No webhook set for user")
		}
	}
}
