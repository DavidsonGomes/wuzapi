package user

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	controllerBase "wuzapi/controllers/controller_base"
	"wuzapi/internal/helpers"
	internalTypes "wuzapi/internal/types"
	"wuzapi/repository"

	"github.com/justinas/alice"
	"github.com/rs/zerolog/log"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types"
)

type UserController struct {
	*controllerBase.Controller
}

func (s *UserController) SignRoutes(c alice.Chain) {
	s.Router.Handle("/user/create", c.Then(s.CreateUser())).Methods("POST")
	s.Router.Handle("/user/delete", c.Then(s.DeleteUser())).Methods("POST")
	s.Router.Handle("/user/fetch", c.Then(s.GetUserByToken())).Methods("POST")
	s.Router.Handle("/user/info", c.Then(s.GetUser())).Methods("POST")
	s.Router.Handle("/user/check", c.Then(s.CheckUser())).Methods("POST")
	s.Router.Handle("/user/avatar", c.Then(s.GetAvatar())).Methods("POST")
	s.Router.Handle("/user/contacts", c.Then(s.GetContacts())).Methods("GET")
}

func (s *UserController) CreateUser() http.HandlerFunc {
	type userStruct struct {
		Name  string `json:"name" binding:"required"`
		Token string `json:"token" binding:"required"`
	}

	return func(w http.ResponseWriter, r *http.Request) {
		decoder := json.NewDecoder(r.Body)

		var t userStruct
		err := decoder.Decode(&t)
		if err != nil {
			s.Respond(w, r, http.StatusBadRequest, errors.New("Could not decode Payload"))
			return
		}

		user, err := s.Repository.CreateUser(&repository.User{Name: t.Name, Token: t.Token})
		if err == nil {
			s.Respond(w, r, http.StatusBadRequest, err.Error())
			return
		} else if err != sql.ErrNoRows {
			s.Respond(w, r, http.StatusInternalServerError, err)
			return
		}

		userResponse := struct {
			ID    int    `json:"id"`
			Name  string `json:"name"`
			Token string `json:"token"`
		}{
			ID:    user.Id,
			Name:  user.Name,
			Token: user.Token,
		}

		responseJSON, err := json.Marshal(userResponse)
		if err != nil {
			s.Respond(w, r, http.StatusInternalServerError, err.Error())
			return
		}

		s.Respond(w, r, http.StatusCreated, string(responseJSON))
		return
	}
}

func (s *UserController) DeleteUser() http.HandlerFunc {
	type userStruct struct {
		Token string `json:"token" binding:"required"`
	}

	return func(w http.ResponseWriter, r *http.Request) {
		decoder := json.NewDecoder(r.Body)

		var t userStruct
		err := decoder.Decode(&t)
		if err != nil {
			s.Respond(w, r, http.StatusBadRequest, errors.New("Could not decode Payload"))
			return
		}

		err = s.Repository.DeleteUserByToken(t.Token)
		if err != nil {
			s.Respond(w, r, http.StatusInternalServerError, err)
			return
		}

		s.Respond(w, r, http.StatusOK, "User deleted")
		return
	}
}

func (s *UserController) GetUserByToken() http.HandlerFunc {
	type userResponse struct {
		ID      int    `json:"id"`
		Name    string `json:"name"`
		Token   string `json:"token"`
		Webhook string `json:"webhook"`
		Jid     string `json:"jid"`
	}

	return func(w http.ResponseWriter, r *http.Request) {
		decoder := json.NewDecoder(r.Body)

		var t struct {
			Token string `json:"token" binding:"required"`
		}
		if err := decoder.Decode(&t); err != nil {
			s.Respond(w, r, http.StatusBadRequest, err.Error())
			return
		}

		user, err := s.Repository.GetUserByToken(t.Token)

		userResponse := &userResponse{
			ID:      user.Id,
			Name:    user.Name,
			Token:   user.Token,
			Webhook: user.Webhook,
			Jid:     user.Jid,
		}

		responseJSON, err := json.Marshal(userResponse)
		if err != nil {
			s.Respond(w, r, http.StatusInternalServerError, err.Error())
			return
		}

		s.Respond(w, r, http.StatusOK, string(responseJSON))
	}
}

// checks if users/phones are on Whatsapp
func (s *UserController) CheckUser() http.HandlerFunc {

	type checkUserStruct struct {
		Phone []string
	}

	type User struct {
		Query        string
		IsInWhatsapp bool
		JID          string
		VerifiedName string
	}

	type UserCollection struct {
		Users []User
	}

	return func(w http.ResponseWriter, r *http.Request) {

		txtid := r.Context().Value("userinfo").(internalTypes.Values).Get("Id")
		userid, _ := strconv.Atoi(txtid)

		if s.ClientPointer[userid] == nil {
			s.Respond(w, r, http.StatusInternalServerError, errors.New("No session"))
			return
		}

		decoder := json.NewDecoder(r.Body)
		var t checkUserStruct
		err := decoder.Decode(&t)
		if err != nil {
			s.Respond(w, r, http.StatusBadRequest, errors.New("Could not decode Payload"))
			return
		}

		if len(t.Phone) < 1 {
			s.Respond(w, r, http.StatusBadRequest, errors.New("Missing Phone in Payload"))
			return
		}

		resp, err := s.ClientPointer[userid].IsOnWhatsApp(t.Phone)
		if err != nil {
			s.Respond(w, r, http.StatusInternalServerError, errors.New(fmt.Sprintf("Failed to check if users are on WhatsApp: %s", err)))
			return
		}

		uc := new(UserCollection)
		for _, item := range resp {
			if item.VerifiedName != nil {
				var msg = User{Query: item.Query, IsInWhatsapp: item.IsIn, JID: fmt.Sprintf("%s", item.JID), VerifiedName: item.VerifiedName.Details.GetVerifiedName()}
				uc.Users = append(uc.Users, msg)
			} else {
				var msg = User{Query: item.Query, IsInWhatsapp: item.IsIn, JID: fmt.Sprintf("%s", item.JID), VerifiedName: ""}
				uc.Users = append(uc.Users, msg)
			}
		}
		responseJson, err := json.Marshal(uc)
		if err != nil {
			s.Respond(w, r, http.StatusInternalServerError, err)
		} else {
			s.Respond(w, r, http.StatusOK, string(responseJson))
		}
		return
	}
}

// Gets user information
func (s *UserController) GetUser() http.HandlerFunc {

	type checkUserStruct struct {
		Phone []string
	}

	type UserCollection struct {
		Users map[types.JID]types.UserInfo
	}

	return func(w http.ResponseWriter, r *http.Request) {

		txtid := r.Context().Value("userinfo").(internalTypes.Values).Get("Id")
		userid, _ := strconv.Atoi(txtid)

		if s.ClientPointer[userid] == nil {
			s.Respond(w, r, http.StatusInternalServerError, errors.New("No session"))
			return
		}

		decoder := json.NewDecoder(r.Body)
		var t checkUserStruct
		err := decoder.Decode(&t)
		if err != nil {
			s.Respond(w, r, http.StatusBadRequest, errors.New("Could not decode Payload"))
			return
		}

		if len(t.Phone) < 1 {
			s.Respond(w, r, http.StatusBadRequest, errors.New("Missing Phone in Payload"))
			return
		}

		var jids []types.JID
		for _, arg := range t.Phone {
			jid, ok := helpers.ParseJID(arg)
			if !ok {
				return
			}
			jids = append(jids, jid)
		}
		resp, err := s.ClientPointer[userid].GetUserInfo(jids)

		if err != nil {
			msg := fmt.Sprintf("Failed to get user info: %v", err)
			log.Error().Msg(msg)
			s.Respond(w, r, http.StatusInternalServerError, msg)
			return
		}

		uc := new(UserCollection)
		uc.Users = make(map[types.JID]types.UserInfo)

		for jid, info := range resp {
			uc.Users[jid] = info
		}

		responseJson, err := json.Marshal(uc)
		if err != nil {
			s.Respond(w, r, http.StatusInternalServerError, err)
		} else {
			s.Respond(w, r, http.StatusOK, string(responseJson))
		}
		return
	}
}

// Gets avatar info for user
func (s *UserController) GetAvatar() http.HandlerFunc {

	type getAvatarStruct struct {
		Phone   string
		Preview bool
	}

	return func(w http.ResponseWriter, r *http.Request) {

		txtid := r.Context().Value("userinfo").(internalTypes.Values).Get("Id")
		userid, _ := strconv.Atoi(txtid)

		if s.ClientPointer[userid] == nil {
			s.Respond(w, r, http.StatusInternalServerError, errors.New("No session"))
			return
		}

		decoder := json.NewDecoder(r.Body)
		var t getAvatarStruct
		err := decoder.Decode(&t)
		if err != nil {
			s.Respond(w, r, http.StatusBadRequest, errors.New("Could not decode Payload"))
			return
		}

		if len(t.Phone) < 1 {
			s.Respond(w, r, http.StatusBadRequest, errors.New("Missing Phone in Payload"))
			return
		}

		jid, ok := helpers.ParseJID(t.Phone)
		if !ok {
			s.Respond(w, r, http.StatusBadRequest, errors.New("Could not parse Phone"))
			return
		}

		var pic *types.ProfilePictureInfo

		existingID := ""
		pic, err = s.ClientPointer[userid].GetProfilePictureInfo(jid, &whatsmeow.GetProfilePictureParams{
			Preview:    t.Preview,
			ExistingID: existingID,
		})
		if err != nil {
			msg := fmt.Sprintf("Failed to get avatar: %v", err)
			log.Error().Msg(msg)
			s.Respond(w, r, http.StatusInternalServerError, errors.New(msg))
			return
		}

		if pic == nil {
			s.Respond(w, r, http.StatusInternalServerError, errors.New("No avatar found"))
			return
		}

		log.Info().Str("id", pic.ID).Str("url", pic.URL).Msg("Got avatar")

		responseJson, err := json.Marshal(pic)
		if err != nil {
			s.Respond(w, r, http.StatusInternalServerError, err)
		} else {
			s.Respond(w, r, http.StatusOK, string(responseJson))
		}
		return
	}
}

// Gets all contacts
func (s *UserController) GetContacts() http.HandlerFunc {

	return func(w http.ResponseWriter, r *http.Request) {

		txtid := r.Context().Value("userinfo").(internalTypes.Values).Get("Id")
		userid, _ := strconv.Atoi(txtid)

		if s.ClientPointer[userid] == nil {
			s.Respond(w, r, http.StatusInternalServerError, errors.New("No session"))
			return
		}

		result := map[types.JID]types.ContactInfo{}
		result, err := s.ClientPointer[userid].Store.Contacts.GetAllContacts()
		if err != nil {
			s.Respond(w, r, http.StatusInternalServerError, err)
			return
		}

		responseJson, err := json.Marshal(result)
		if err != nil {
			s.Respond(w, r, http.StatusInternalServerError, err)
		} else {
			s.Respond(w, r, http.StatusOK, string(responseJson))
		}

		return
	}
}
