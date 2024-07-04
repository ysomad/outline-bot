package bot

import (
	tele "gopkg.in/telebot.v3"
)

func paymentKeyboard() *tele.ReplyMarkup {
	kb := &tele.ReplyMarkup{}
	kb.Inline(kb.Row(kb.URL("Оплатить", paymentURL)))
	return kb
}
