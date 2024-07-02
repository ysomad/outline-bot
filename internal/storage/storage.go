package storage

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"time"

	sq "github.com/Masterminds/squirrel"

	"github.com/ysomad/outline-bot/internal/domain"
)

type Storage struct {
	db *sql.DB
	sq sq.StatementBuilderType
}

func New(db *sql.DB, b sq.StatementBuilderType) *Storage {
	return &Storage{
		db: db,
		sq: b,
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
	sql, args, err := s.sq.
		Select("id, uid, username, first_name, last_name, key_amount, price, status, closed_at, created_at").
		From("orders").
		Where(sq.Eq{"id": oid}).
		ToSql()
	if err != nil {
		return Order{}, err
	}

	row := s.db.QueryRow(sql, args...)
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
	sql, args, err := s.sq.
		Insert("orders").
		Columns("uid, username, first_name, last_name, key_amount, price, created_at, status").
		Values(p.UID, p.Username, p.FirstName, p.LastName, p.KeyAmount, p.Price, p.CreatedAt, p.Status).
		ToSql()
	if err != nil {
		return 0, err
	}

	res, err := s.db.Exec(sql, args...)
	if err != nil {
		return 0, fmt.Errorf("exec: %w", err)
	}

	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("last insert id: %w", err)
	}

	return domain.OrderID(id), nil
}

func (s *Storage) Close(oid domain.OrderID, status domain.OrderStatus, closedAt time.Time) error {
	if _, err := s.db.Exec("UPDATE orders SET closed_at = ?, status = ? WHERE id = ?",
		closedAt.UTC(), status, oid); err != nil {
		return err
	}
	return nil
}

type Key struct {
	ID        string
	Name      string
	URL       string
	ExpiresAt time.Time
}

func (s *Storage) ApproveOrder(oid domain.OrderID, keys []Key, approvedAt time.Time) error {
	sql1, args1, err := s.sq.
		Update("orders").
		Set("closed_at", approvedAt.UTC()).
		Set("status", domain.OrderStatusApproved).
		Where(sq.Eq{"id": oid}).
		ToSql()
	if err != nil {
		return err
	}

	b := s.sq.
		Insert("access_keys").
		Columns("id, name, url, order_id, expires_at")

	for _, k := range keys {
		b = b.Values(k.ID, k.Name, k.URL, oid, k.ExpiresAt)
	}

	sql2, args2, err := b.ToSql()
	if err != nil {
		return err
	}

	tx, err := s.db.BeginTx(context.TODO(), nil)
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

type KeyFromOrder struct {
	ID        string
	Name      string
	URL       string
	ExpiresAt time.Time
	OrderID   domain.OrderID
	Price     int
}

func (s *Storage) ListActiveUserKeys(uid int64) ([]KeyFromOrder, error) {
	sql, args, err := s.sq.
		Select("ak.id, ak.url, ak.expires_at, ak.name, o.id, o.price").
		From("access_keys ak").
		InnerJoin("orders o ON ak.order_id = o.id").
		Where(sq.Eq{"o.uid": uid}).
		Where(sq.Lt{"ak.expires_at": "current_timestamp"}).
		OrderBy("o.id").
		ToSql()
	if err != nil {
		return nil, err
	}

	rows, err := s.db.Query(sql, args...)
	if err != nil {
		return nil, fmt.Errorf("query: %w", err)
	}
	defer rows.Close()

	var keys []KeyFromOrder

	for rows.Next() {
		k := KeyFromOrder{}

		if err := rows.Scan(&k.ID, &k.URL, &k.ExpiresAt, &k.Name, &k.OrderID, &k.Price); err != nil {
			return nil, err
		}

		keys = append(keys, k)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows: %w", err)
	}

	return keys, nil
}

func (s *Storage) CountActiveKeys(uid int64) (uint8, error) {
	sql, args, err := s.sq.
		Select("count(*)").
		From("access_keys ak").
		InnerJoin("orders o on o.id = ak.order_id").
		Where(sq.Eq{"o.uid": uid}).
		Where("ak.expires_at > current_timestamp").
		ToSql()
	if err != nil {
		return 0, err
	}

	slog.Debug(sql)

	row := s.db.QueryRow(sql, args...)

	var count uint8

	if err := row.Scan(&count); err != nil {
		return 0, fmt.Errorf("scan: %w", err)
	}

	return count, nil
}
