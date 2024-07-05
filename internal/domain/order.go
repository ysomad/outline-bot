package domain

import (
	"strconv"
	"time"
)

const (
	OrderTTL              = 24 * time.Hour * 30 // 30 days
	BeforeOrderExpiration = time.Hour * 24 * 3  // 3 days
)

type OrderID int32

func (id OrderID) String() string {
	return strconv.Itoa(int(id))
}

func OrderIDFromString(s string) (OrderID, error) {
	i, err := strconv.Atoi(s)
	if err != nil {
		return 0, err
	}
	return OrderID(i), nil
}

type OrderStatus string

const (
	OrderStatusAwaitingPayment OrderStatus = "awaiting payment"
	OrderStatusRejected        OrderStatus = "rejected"
	OrderStatusApproved        OrderStatus = "approved"
	OrderStatusAwaitingRenewal OrderStatus = "awaiting renewal"
	OrderStatusRenewed         OrderStatus = "renewed"
)
