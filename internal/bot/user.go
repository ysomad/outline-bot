package bot

import (
	"fmt"
	"strconv"
	"strings"

	"gopkg.in/telebot.v3"
)

type user struct {
	id        int64
	username  string
	firstName string
	lastName  string
}

// recipient for using in telebot.Bot.Send and telebot.Bot.Edit methods.
func recipient(uid int64) *user {
	return &user{id: uid}
}

func newUser(c *telebot.Chat) user {
	return user{
		id:        c.ID,
		username:  c.Username,
		firstName: c.FirstName,
		lastName:  c.LastName,
	}
}

func (u *user) ID() string {
	return strconv.FormatInt(u.id, 10)
}

// Recipient implements telebot.Recipient
func (u *user) Recipient() string {
	if u.username != "" {
		return u.username
	}
	return u.ID()
}

func (u *user) write(sb *strings.Builder) error {
	if _, err := fmt.Fprintf(sb, "ID: %d", u.id); err != nil {
		return err
	}

	if u.username != "" {
		_, err := fmt.Fprintf(sb, "\nЛогин: @%s", u.username)
		if err != nil {
			return err
		}
	}

	if u.firstName != "" {
		_, err := fmt.Fprintf(sb, "\nИмя: %s", u.firstName)
		if err != nil {
			return err
		}
	}
	if u.lastName != "" {
		_, err := fmt.Fprintf(sb, "\nФамилия: %s", u.lastName)
		if err != nil {
			return err
		}
	}

	return nil
}
