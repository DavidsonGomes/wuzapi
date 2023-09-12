package repository

import (
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	internalTypes "wuzapi/internal/types"
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
	Db *sql.DB
}

func NewUserRepository(db *sql.DB) UserRepository {
	return &userRepository{Db: db}
}

func (r *userRepository) CreateUser(user *User) (*User, error) {
	if user, _ := r.GetUserByToken(user.Token); user != nil {
		return nil, errors.New("User already exists")
	}

	_, err := r.Db.Exec("INSERT INTO users (name, token) VALUES (?, ?)", user.Name, user.Token)
	if err != nil {
		return nil, err
	}
	return r.GetUserByToken(user.Token)
}

func (r *userRepository) ConnectUser(userId int) error {
	_, err := r.Db.Exec("UPDATE users SET connected=1 WHERE id=?", userId)
	return err
}

func (r *userRepository) DisconnectUser(userId int) error {
	_, err := r.Db.Exec("UPDATE users SET connected=0 WHERE id=?", userId)
	return err
}

func (r *userRepository) GetConnectedUsers() ([]*User, error) {
	rows, err := r.Db.Query("SELECT id,token,jid,webhook,events FROM users WHERE connected=1")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	users := []*User{}
	for rows.Next() {
		txtid := ""
		token := ""
		jid := ""
		webhook := ""
		events := ""
		err = rows.Scan(&txtid, &token, &jid, &webhook, &events)
		if err != nil {
			return nil, err
		}
		id, err := strconv.Atoi(txtid)
		if err != nil {
			return nil, err
		}
		users = append(users, &User{Id: id, Token: token, Jid: jid, Webhook: webhook, Events: events})
	}
	return users, nil
}

func (r *userRepository) DeleteUserByToken(token string) error {
	rows, err := r.Db.Query("SELECT name FROM users WHERE token=? LIMIT 1", token)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var name string
		err = rows.Scan(&name)
		if err != nil {
			return err
		}

		if name == "admin" {
			return errors.New("Cannot delete admin user")
		}
	}

	_, err = r.Db.Query(fmt.Sprintf("delete from users where token='%s'", token))
	return err
}

func (r *userRepository) SetJid(jid string, userId int) error {
	_, err := r.Db.Exec("UPDATE users SET jid=? WHERE id=?", jid, userId)
	return err
}

func (r *userRepository) SetEvents(events string, userId int) error {
	_, err := r.Db.Exec("UPDATE users SET events=? WHERE id=?", events, userId)
	return err
}

func (r *userRepository)SetWebhook(webhooks string, userId int) error {
	_, err := r.Db.Exec("UPDATE users SET webhook=? WHERE id=?", webhooks, userId)
	return err
}

func (r *userRepository) GetQrCode(userId int) (string, error) {
	var code string
	rows, err := r.Db.Query("SELECT qrcode AS code FROM users WHERE id=? LIMIT 1", userId)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	for rows.Next() {
		err = rows.Scan(&code)
		if err != nil {
			return "", err
		}
	}
	return code, nil
}

func (r *userRepository) SetQrCode(qrCode string, userId int) error {
	_, err := r.Db.Exec("UPDATE users SET qrcode=? WHERE id=?", qrCode, userId)
	return err
}

func (r *userRepository) GetUserByToken(token string) (*User, error) {
	rows, err := r.Db.Query("SELECT id,webhook,jid,events,name,token FROM users WHERE token=? LIMIT 1", token)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		txtid := ""
		jid := ""
		webhook := ""
		events := ""
		name := ""
		token := ""
		err = rows.Scan(&txtid, &webhook, &jid, &events, &name, &token)
		if err != nil {
			return nil, err
		}
		userid, _ := strconv.Atoi(txtid)
		return &User{Id: userid, Name: name, Token: token, Webhook: webhook, Events: events}, nil
	}
	return nil, nil
}

func (r *userRepository) GetUserById(userId int) (*User, error) {

	name := ""
	token := ""
	webhook := ""
	jid := ""
	qrcode := ""
	events := ""

	rows, err := r.Db.Query("SELECT name,token,webhook,jid,qrcode,events FROM users WHERE id=? LIMIT 1", userId)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		err = rows.Scan(&name, &token, &webhook, &jid, &qrcode, &events)
		if err != nil {
			return nil, err
		}
	}
	err = rows.Err()
	if err != nil {
		return nil, err
	}

	return &User{Id: userId, Webhook: webhook, Events: events}, nil
}
