package repository

import (
	"fmt"
	internalTypes "wuzapi/internal/types"
)

type User struct {
	Id         int
	Name       string
	Token      string
	Webhook    string
	Jid        string
	QrCode     string
	Connected  int
	Expiration int
	Events     string
}

type UserDb struct {
	*User
	repository UserRepository
}

func (u *User) ToValues() internalTypes.Values {
	return internalTypes.Values{M: map[string]string{
		"Id":      fmt.Sprint(u.Id),
		"Jid":     u.Jid,
		"Webhook": u.Webhook,
		"Token":   u.Token,
		"Events":  u.Events,
	}}
}

func (u *UserDb) loadUser() (err error) {
	u.User, err = u.repository.GetUserById(u.Id)
	return
}

func (u *UserDb) SaveChanges() error {
	return u.repository.Updates(u.User)
}

func (u *UserDb) Connect() error {
	u.Connected = 1
	return u.SaveChanges()
}

func (u *UserDb) SetJid(jid string) error {
	u.Jid = jid
	return u.SaveChanges()
}

func (u *UserDb) Disconnect() error {
	u.Connected = 0
	return u.SaveChanges()
}

func (u *UserDb) SetQrCode(qrcode string) error {
	u.QrCode = qrcode
	return u.SaveChanges()
}

func (u *UserDb) SetEvents(events string) error {
	u.Events = events
	return u.SaveChanges()
}

func (u *UserDb) SetWebhook(webhook string) error {
	u.Webhook = webhook
	return u.SaveChanges()
}

func NewUser(repository UserRepository, userId int) (*UserDb, error) {
	user := &UserDb{
		User:       &User{Id: userId},
		repository: repository,
	}
	err := user.loadUser()
	if err != nil {
		return nil, err
	}
	return user, nil
}
