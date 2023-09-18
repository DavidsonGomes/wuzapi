package message

import (
	"errors"
	"wuzapi/internal/helpers"

	waProto "go.mau.fi/whatsmeow/binary/proto"
	"go.mau.fi/whatsmeow/types"
)

type Message struct {
	Phone       string
	ContextInfo waProto.ContextInfo
}

func (m *Message) ValidateMessageFields() (types.JID, error) {

	recipient, ok := helpers.ParseJID(m.Phone)
	if !ok {
		return types.NewJID("", types.DefaultUserServer), errors.New("Could not parse Phone")
	}

	if m.ContextInfo.StanzaId != nil {
		if m.ContextInfo.Participant == nil {
			return types.NewJID("", types.DefaultUserServer), errors.New("Missing Participant in ContextInfo")
		}
	}

	if m.ContextInfo.Participant != nil {
		if m.ContextInfo.StanzaId == nil {
			return types.NewJID("", types.DefaultUserServer), errors.New("Missing StanzaId in ContextInfo")
		}
	}

	return recipient, nil
}
