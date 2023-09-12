package webhook

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	controllerBase "wuzapi/controllers/controller_base"
	"wuzapi/internal/helpers"
	internalTypes "wuzapi/internal/types"

	"github.com/justinas/alice"
	"github.com/patrickmn/go-cache"
)

type WebhookController struct {
	*controllerBase.Controller
}

func (s *WebhookController) SignRoutes(c alice.Chain) {
	s.Router.Handle("/webhook", c.Then(s.SetWebhook())).Methods("POST")
	s.Router.Handle("/webhook", c.Then(s.GetWebhook())).Methods("GET")
}

// Gets WebHook
func (s *WebhookController) GetWebhook() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {

		txtid := r.Context().Value("userinfo").(internalTypes.Values).Get("Id")
		id, err := strconv.Atoi(txtid)
		if err != nil {
			s.Respond(w, r, http.StatusBadRequest, fmt.Errorf("Invalid Id"))
		}
		user, err := s.Repository.GetUserById(id)

		eventarray := strings.Split(user.Events, ",")

		response := map[string]interface{}{"webhook": user.Webhook, "subscribe": eventarray}
		responseJson, err := json.Marshal(response)
		if err != nil {
			s.Respond(w, r, http.StatusInternalServerError, err)
		} else {
			s.Respond(w, r, http.StatusOK, string(responseJson))
		}
		return
	}
}

// Sets WebHook
func (s *WebhookController) SetWebhook() http.HandlerFunc {
	type webhookStruct struct {
		WebhookURL string
	}
	return func(w http.ResponseWriter, r *http.Request) {

		txtid := r.Context().Value("userinfo").(internalTypes.Values).Get("Id")
		token := r.Context().Value("userinfo").(internalTypes.Values).Get("Token")
		userid, _ := strconv.Atoi(txtid)

		decoder := json.NewDecoder(r.Body)
		var t webhookStruct
		err := decoder.Decode(&t)
		if err != nil {
			s.Respond(w, r, http.StatusInternalServerError, fmt.Errorf("Could not set webhook: %v", err))
			return
		}
		var webhook = t.WebhookURL

		err = s.Repository.SetWebhook(webhook, userid)
		if err != nil {
			s.Respond(w, r, http.StatusInternalServerError, fmt.Errorf("%s", err))
			return
		}

		v := helpers.UpdateUserInfo(r.Context().Value("userinfo"), "Webhook", webhook)
		s.UserInfoCache.Set(token, v, cache.NoExpiration)

		response := map[string]interface{}{"webhook": webhook}
		responseJson, err := json.Marshal(response)
		if err != nil {
			s.Respond(w, r, http.StatusInternalServerError, err)
		} else {
			s.Respond(w, r, http.StatusOK, string(responseJson))
		}
		return
	}
}
