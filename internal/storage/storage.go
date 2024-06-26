package storage

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	sq "github.com/Masterminds/squirrel"

	"github.com/ysomad/outline-bot/internal/domain"
)

type Storage struct {
	*sql.DB
	builder sq.StatementBuilderType
}

func New(db *sql.DB, b sq.StatementBuilderType) *Storage {
	return &Storage{
		DB:      db,
		builder: b,
	}
}

type Order struct {
	ID        domain.OrderID
	UID       int64
	Username  sql.NullString
	FirstName sql.NullString
	LastName  sql.NullString
	KeyAmount int
	Price     int
	Status    sql.NullString
	ClosedAt  sql.NullTime
	CreatedAt sql.NullTime
}

func (s *Storage) GetOrder(oid domain.OrderID) (Order, error) {
	sql, args, err := s.builder.
		Select("id, uid, username, first_name, last_name, key_amount, price, status, closed_at, created_at").
		From("orders").
		Where(sq.Eq{"id": oid}).
		ToSql()
	if err != nil {
		return Order{}, err
	}

	row := s.QueryRow(sql, args...)
	o := Order{}
	err = row.Scan(
		&o.ID,
		&o.UID,
		&o.Username,
		&o.FirstName,
		&o.LastName,
		&o.KeyAmount,
		&o.Price,
		&o.Status,
		&o.ClosedAt,
		&o.CreatedAt,
	)
	if err != nil {
		return Order{}, err
	}

	return o, nil
}

type CreateOrderParams struct {
	UID       int64
	Username  string
	FirstName string
	LastName  string
	KeyAmount int
	Price     int
	CreatedAt time.Time
	Status    domain.OrderStatus
}

func (s *Storage) CreateOrder(p CreateOrderParams) (domain.OrderID, error) {
	sql, args, err := s.builder.
		Insert("orders").
		Columns("uid, username, first_name, last_name, key_amount, price, created_at, status").
		Values(p.UID, p.Username, p.FirstName, p.LastName, p.KeyAmount, p.Price, p.CreatedAt, p.Status).
		ToSql()
	if err != nil {
		return 0, err
	}

	res, err := s.Exec(sql, args...)
	if err != nil {
		return 0, fmt.Errorf("exec: %w", err)
	}

	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("last insert id: %w", err)
	}

	return domain.OrderID(id), nil
}

func (db *Storage) Close(oid domain.OrderID, s domain.OrderStatus, closedAt time.Time) error {
	if _, err := db.Exec("UPDATE orders SET closed_at = ?, status = ? WHERE id = ?", closedAt, s, oid); err != nil {
		return err
	}
	return nil
}

type AccessKey struct {
	ID        string
	Name      string
	URL       string
	ExpiresAt time.Time
}

func (s *Storage) ApproveOrder(oid domain.OrderID, keys []AccessKey, approvedAt time.Time) error {
	sql1, args1, err := s.builder.
		Update("orders").
		Set("closed_at", approvedAt).
		Set("status", domain.OrderStatusApproved).
		Where(sq.Eq{"id": oid}).
		ToSql()
	if err != nil {
		return err
	}

	b := s.builder.
		Insert("access_keys").
		Columns("id, name, url, order_id, expires_at")

	for _, k := range keys {
		b = b.Values(k.ID, k.Name, k.URL, oid, k.ExpiresAt)
	}

	sql2, args2, err := b.ToSql()
	if err != nil {
		return err
	}

	tx, err := s.BeginTx(context.TODO(), nil)
	if err != nil {
		return fmt.Errorf("tx not started: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.Exec(sql1, args1...); err != nil {
		return fmt.Errorf("order not closed: %w", err)
	}

	if _, err := tx.Exec(sql2, args2...); err != nil {
		return fmt.Errorf("access keys not created: %w", err)
	}

	if err = tx.Commit(); err != nil {
		return fmt.Errorf("tx commit: %w", err)
	}

	return nil
}
