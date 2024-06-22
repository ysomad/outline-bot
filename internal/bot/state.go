package bot

import (
	"fmt"
	"io"
	"strconv"

	"gopkg.in/telebot.v3"
)

type step string

const (
	stepCancel          step = "cancel"
	stepSelectKeyAmount step = "select_key_amount"

	stepApproveOrder step = "approve_order"
	stepRejectOrder  step = "reject_order"
)

func (s step) String() string { return string(s) }

type State struct {
	step string
	data any
}

type order struct {
	keyAmount int8
	sum       int16
}

type user struct {
	id        int64
	username  string
	firstName string
	lastName  string
}

func recipient(uid int64) user {
	return user{id: uid}
}

func newUser(c *telebot.Chat) user {
	return user{
		id:        c.ID,
		username:  c.Username,
		firstName: c.FirstName,
		lastName:  c.LastName,
	}
}

func (u user) ID() string {
	return strconv.FormatInt(u.id, 10)
}

// Recipient implements telebot.Recipient
func (u user) Recipient() string {
	if u.username != "" {
		return u.username
	}
	return u.ID()
}

func (u user) Write(w io.Writer) {
	fmt.Fprintf(w, "ID: %d", u.id)
	if u.username != "" {
		fmt.Fprintf(w, "\nЛогин: @%s", u.username)
	}
	if u.firstName != "" {
		fmt.Fprintf(w, "\nИмя: %s", u.firstName)
	}
	if u.lastName != "" {
		fmt.Fprintf(w, "\nФамилия: %s", u.lastName)
	}
}
