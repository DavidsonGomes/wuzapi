package repository

import (
	"errors"
	"fmt"
	internalTypes "wuzapi/internal/types"

	"gorm.io/gorm"
)

type User struct {
	Id         int
	Name       string
	Token      string
	Webhook    string
	Jid        string
	QrCode     string
	Connected  *int
	Expiration *int
	Events     string
}

func (u User) ToValues() internalTypes.Values {
	return internalTypes.Values{M: map[string]string{
		"Id":      fmt.Sprint(u.Id),
		"Jid":     u.Jid,
		"Webhook": u.Webhook,
		"Token":   u.Token,
		"Events":  u.Events,
	}}
}

type UserRepository interface {
	CreateUser(user *User) (*User, error)
	CreateAdminUser(token string) (error)
	GetAdminUser() (*User, error)
	GetUserByToken(token string) (*User, error)
	GetUserById(userId int) (*User, error)
	GetConnectedUsers() ([]*User, error)
	DeleteUserByToken(token string) error
	GetQrCode(userId int) (string, error)
	SetQrCode(qrCode string, userId int) error
	SetJid(jid string, userId int) error
	SetEvents(events string, userId int) error
	SetWebhook(webhooks string, userId int) error
	ConnectUser(userId int) error
	DisconnectUser(userId int) error
}

type userRepository struct {
	db *gorm.DB
}

func NewUserRepository(db *gorm.DB) UserRepository {
	return &userRepository{db: db}
}

func (r *userRepository) CreateUser(user *User) (*User, error) {
	if foundUser, _ := r.GetUserByToken(user.Token); foundUser != nil {
		return nil, errors.New("User already exists")
	}

	err := r.db.Create(user).Error
	if err != nil {
		return nil, err
	}
	return r.GetUserByToken(user.Token)
}

func (r *userRepository) CreateAdminUser(token string) error {
	if user, _ := r.GetAdminUser(); user != nil {
		user.Token = token
		return r.db.Updates(user).Error
	}
	return r.db.Create(&User{Name: "admin", Token: token}).Error
}

func (r *userRepository) ConnectUser(userId int) error {
	return r.db.Update("connected", 1).Error
}

func (r *userRepository) DisconnectUser(userId int) error {
	return r.db.Update("connected", 0).Error
}

func (r *userRepository) GetConnectedUsers() ([]*User, error) {
	var users []*User

	err := r.db.Find(&users, "connected = 1").Error

	if err != nil {
		return nil, err
	}
	return users, nil
}

func (r *userRepository) DeleteUserByToken(token string) error {
	var user *User
	err := r.db.Find(&user, "token = ?", token).Error
	if err != nil {
		return err
	}
	if user == nil {
		return fmt.Errorf("user not found for token %s", token)
	}
	err = r.db.Delete(&user).Error
	if err != nil {
		return err
	}
	return nil
}

func (r *userRepository) SetJid(jid string, userId int) error {
	return r.db.Where(userId).Update("jid", jid).Error
}

func (r *userRepository) SetEvents(events string, userId int) error {
	return r.db.Where(userId).Update("events", events).Error
}

func (r *userRepository) SetWebhook(webhooks string, userId int) error {
	return r.db.Where(userId).Update("webhooks", webhooks).Error
}

func (r *userRepository) SetQrCode(qrCode string, userId int) error {
	return r.db.Where(userId).Update("qr_code", qrCode).Error
}

func (r *userRepository) GetQrCode(userId int) (string, error) {
	user, err := r.GetUserById(userId)
	if err != nil {
		return "", err
	}
	return user.QrCode, nil
}

func (r *userRepository) GetUserByToken(token string) (user *User, err error) {
	err = r.db.Find(&user, "token like ?", token).Error
	return
}

func (r *userRepository) GetAdminUser() (user *User, err error) {
	err = r.db.Find(&user, "name like 'admin'").Error
	return
}

func (r *userRepository) GetUserById(userId int) (user *User, err error) {
	err = r.db.Find(&user, userId).Error
	return
}
