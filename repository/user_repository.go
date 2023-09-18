package repository

import (
	"errors"
	"fmt"
	"gorm.io/gorm"
)

type UserRepository interface {
	CreateUser(user *User) (*User, error)
	CreateAdminUser(token string) error
	GetAdminUser() (*User, error)
	GetUserByToken(token string) (*User, error)
	GetUserById(userId int) (*User, error)
	GetConnectedUsers() ([]*User, error)
	DeleteUserByToken(token string) error
	GetQrCode(userId int) (string, error)
	Updates(user *User) error
}

type userRepository struct {
	db *gorm.DB
}

func NewUserRepository(db *gorm.DB) UserRepository {
	return &userRepository{db: db}
}

func (r *userRepository) CreateUser(user *User) (*User, error) {
	if foundUser, _ := r.GetUserByToken(user.Token); foundUser != nil {
		return nil, errors.New("UserDb already exists")
	}

	err := r.db.Create(user).Error
	if err != nil {
		return nil, err
	}
	return r.GetUserByToken(user.Token)
}

func (r *userRepository) CreateAdminUser(token string) error {
	if user, _ := r.GetAdminUser(); user.Id != 0 {
		user.Token = token
		return r.db.Model(&User{}).Where("name like 'admin'").Updates(user).Error
	}
	return r.db.Create(&User{Name: "admin", Token: token}).Error
}

func (r *userRepository) Updates(user *User) error {
	return r.db.Updates(user).Error
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
	var user *UserDb
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
