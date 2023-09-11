package repository

import (
	"database/sql"
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
	GetUserByToken(token string) (*User, error)
	GetConnectedUsers() ([]*User, error)
	SetQrCode(qrCode string, userId int) error
	SetJid(jid string, userId int) error
	DisconnectUser(userId int) error
	ConnectUser(userId int) error
}

type userRepository struct {
	Db *sql.DB
}

func (r *userRepository) ConnectUser(userId int) error {
	sqlStmt := `UPDATE users SET connected=1 WHERE id=?`
	_, err := r.Db.Exec(sqlStmt, userId)
	return err
}

func (r *userRepository) DisconnectUser(userId int) error {
	sqlStmt := `UPDATE users SET connected=0 WHERE id=?`
	_, err := r.Db.Exec(sqlStmt, userId)
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

func (r *userRepository) SetJid(jid string, userId int) error {
	sqlStmt := `UPDATE users SET jid=? WHERE id=?`
	_, err := r.Db.Exec(sqlStmt, jid, userId)
	return err
}

func (r *userRepository) SetQrCode(qrCode string, userId int) error {
	sqlStmt := `UPDATE users SET qrcode=? WHERE id=?`
	_, err := r.Db.Exec(sqlStmt, qrCode, userId)
	return err
}

func NewUserRepository(db *sql.DB) UserRepository {
	return &userRepository{Db: db}
}

func (r *userRepository) GetUserByToken(token string) (*User, error) {
	rows, err := r.Db.Query("SELECT id,webhook,jid,events FROM users WHERE token=? LIMIT 1", token)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		txtid := ""
		jid := ""
		webhook := ""
		events := ""
		err = rows.Scan(&txtid, &webhook, &jid, &events)
		if err != nil {
			return nil, err
		}
		userid, _ := strconv.Atoi(txtid)
		return &User{Id: userid, Webhook: webhook, Events: events}, nil
	}
	return nil, nil
}
