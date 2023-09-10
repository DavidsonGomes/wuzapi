package helpers

import (
	"strings"

	"github.com/rs/zerolog/log"
	"go.mau.fi/whatsmeow/types"
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
