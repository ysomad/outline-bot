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

func newUser(c *telebot.Chat) *user {
	return &user{
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
	return u.ID()
}

func (u *user) write(sb *strings.Builder) {
	fmt.Fprintf(sb, "ID: %d", u.id)
	if u.username != "" {
		fmt.Fprintf(sb, "\nЛогин: @%s", u.username)
	}
	if u.firstName != "" {
		fmt.Fprintf(sb, "\nИмя: %s", u.firstName)
	}
	if u.lastName != "" {
		fmt.Fprintf(sb, "\nФамилия: %s", u.lastName)
	}
}
