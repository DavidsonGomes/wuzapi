package webhook

import (
	"github.com/go-resty/resty/v2"
	"github.com/rs/zerolog/log"
)

type Webhook struct {
	ClientHttp map[int]*resty.Client
}

// webhook for regular messages
func (w *Webhook) CallHook(myurl string, payload map[string]string, id int) {
	log.Info().Str("url", myurl).Msg("Sending POST")
	_, err := w.ClientHttp[id].R().SetFormData(payload).Post(myurl)

	if err != nil {
		log.Debug().Str("error", err.Error())
	}
}

// webhook for messages with file attachments
func (w *Webhook) CallHookFile(myurl string, payload map[string]string, id int, file string) {
	log.Info().Str("file", file).Str("url", myurl).Msg("Sending POST")
	w.ClientHttp[id].R().SetFiles(map[string]string{
		"file": file,
	}).SetFormData(payload).Post(myurl)
}
