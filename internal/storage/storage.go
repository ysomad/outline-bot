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
	CreatedAt sql.NullTime
	ExpiresAt sql.NullTime
}

func (s *Storage) GetOrder(oid domain.OrderID) (Order, error) {
	sql, args, err := s.sq.
		Select("id, uid, username, first_name, last_name, key_amount, price, status, created_at, expires_at").
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
		&o.CreatedAt,
		&o.ExpiresAt,
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

func (s *Storage) CloseOrder(oid domain.OrderID, status domain.OrderStatus, closedAt time.Time) error {
	if _, err := s.db.Exec("UPDATE orders SET closed_at = ?, status = ? WHERE id = ?",
		closedAt.UTC(), status, oid); err != nil {
		return err
	}
	return nil
}

type Key struct {
	ID   string
	Name string
	URL  string
}

// ApprovedOrder approves order and creates key for the order.
func (s *Storage) ApproveOrder(oid domain.OrderID, keys []Key, expiresAt time.Time) error {
	sql1, args1, err := s.sq.
		Update("orders").
		Set("expires_at", expiresAt.UTC()).
		Set("status", domain.OrderStatusApproved).
		Where(sq.Eq{"id": oid}).
		ToSql()
	if err != nil {
		return err
	}

	b := s.sq.
		Insert("access_keys").
		Columns("id, name, url, order_id")

	for _, k := range keys {
		b = b.Values(k.ID, k.Name, k.URL, oid)
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

	if _, err = tx.Exec(sql1, args1...); err != nil {
		return fmt.Errorf("order not closed: %w", err)
	}

	if _, err = tx.Exec(sql2, args2...); err != nil {
		return fmt.Errorf("access keys not created: %w", err)
	}

	if err = tx.Commit(); err != nil {
		return fmt.Errorf("tx commit: %w", err)
	}

	return nil
}

type ActiveKey struct {
	ID        string
	Name      string
	URL       string
	ExpiresAt time.Time
	OrderID   domain.OrderID
	Price     int
	UID       int64
}

func (s *Storage) ListActiveUserKeys(uid int64) ([]ActiveKey, error) {
	sql, args, err := s.sq.
		Select("ak.id, ak.url, o.expires_at, ak.name, o.id, o.price, o.uid").
		From("access_keys ak").
		InnerJoin("orders o ON ak.order_id = o.id").
		Where(sq.Eq{"o.uid": uid}).
		Where(sq.Lt{"o.expires_at": "current_timestamp"}).
		Where(sq.Eq{"o.closed_at": nil}).
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

	var keys []ActiveKey

	for rows.Next() {
		k := ActiveKey{}

		if err := rows.Scan(&k.ID, &k.URL, &k.ExpiresAt, &k.Name, &k.OrderID, &k.Price, &k.UID); err != nil {
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
		Where("o.expires_at > current_timestamp").
		ToSql()
	if err != nil {
		return 0, err
	}

	row := s.db.QueryRow(sql, args...)

	var count uint8

	if err := row.Scan(&count); err != nil {
		return 0, fmt.Errorf("scan: %w", err)
	}

	return count, nil
}

type ExpiringKey struct {
	ID        string
	Name      string
	URL       string
	ExpiresAt time.Time
	ExpiresIn time.Duration
	OrderID   domain.OrderID
	KeyAmount int
	Price     int
	UID       int64
	Username  sql.NullString
	FirstName sql.NullString
	LastName  sql.NullString
}

// ListExpiringKeys returns keys that expire in or less than exp.
func (s *Storage) ListExpiringKeys(exp time.Duration) ([]ExpiringKey, error) {
	sql, args, err := s.sq.
		Select("ak.id, ak.name, ak.url, o.expires_at, o.id, o.key_amount, o.price",
			"o.uid, o.username, o.first_name, o.last_name",
			"(JULIANDAY(o.expires_at) - JULIANDAY(current_timestamp)) * 24 * 60 * 60 expires_in").
		From("access_keys ak").
		InnerJoin("orders o ON o.id = ak.order_id").
		Where(sq.LtOrEq{"expires_in": exp.Seconds()}).
		Where(sq.Eq{"closed_at": nil}).
		OrderBy("expires_in").
		ToSql()
	if err != nil {
		return nil, err
	}

	slog.Debug(sql, "args", args)

	rows, err := s.db.Query(sql, args...)
	if err != nil {
		return nil, fmt.Errorf("query: %w", err)
	}
	defer rows.Close()

	var (
		keys []ExpiringKey
		diff float64
	)

	for rows.Next() {
		k := ExpiringKey{}

		err := rows.Scan(
			&k.ID, &k.Name, &k.URL, &k.ExpiresAt, &k.OrderID,
			&k.KeyAmount, &k.Price, &k.UID, &k.Username, &k.FirstName, &k.LastName,
			&diff)
		if err != nil {
			return nil, fmt.Errorf("scan: %w", err)
		}

		k.ExpiresIn = time.Duration(diff) * time.Second
		keys = append(keys, k)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows: %w", err)
	}

	return keys, nil
}

func (s *Storage) RenewOrder(oid domain.OrderID, exp time.Duration) error {
	sql := fmt.Sprintf(`
UPDATE orders
SET expires_at = datetime(expires_at, '+%.f seconds')
WHERE id = ?
`, exp.Seconds())

	res, err := s.db.Exec(sql, oid)
	if err != nil {
		return fmt.Errorf("exec: %w", err)
	}

	if _, err := res.RowsAffected(); err != nil {
		return fmt.Errorf("rows affected: %w", err)
	}

	return nil
}

func (s *Storage) AllActiveKeys() ([]ActiveKey, error) {
	sql, args, err := s.sq.
		Select("ak.id, ak.url, o.expires_at, ak.name, o.id, o.price").
		From("access_keys ak").
		InnerJoin("orders o ON ak.order_id = o.id").
		Where(sq.Lt{"o.expires_at": "current_timestamp"}).
		Where(sq.Eq{"o.closed_at": nil}).
		OrderBy("ak.id").
		ToSql()
	if err != nil {
		return nil, err
	}

	rows, err := s.db.Query(sql, args...)
	if err != nil {
		return nil, fmt.Errorf("query: %w", err)
	}
	defer rows.Close()

	var keys []ActiveKey

	for rows.Next() {
		k := ActiveKey{}

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

type ActiveOrder struct {
	ID        domain.OrderID
	UID       int64
	KeyAmount int
	ExpiresAt sql.NullTime
}

func (s *Storage) ListActiveOrders() ([]ActiveOrder, error) {
	sql, args, err := s.sq.
		Select("id,key_amount,uid,expires_at").
		From("orders").
		Where(sq.Eq{"status": domain.OrderStatusApproved}).
		ToSql()
	if err != nil {
		return nil, fmt.Errorf("builder: %w", err)
	}

	rows, err := s.db.Query(sql, args...)
	if err != nil {
		return nil, fmt.Errorf("quiery: %w", err)
	}
	defer rows.Close()

	var orders []ActiveOrder

	for rows.Next() {
		o := ActiveOrder{}

		if err := rows.Scan(&o.ID, &o.KeyAmount, &o.UID, &o.ExpiresAt); err != nil {
			return nil, fmt.Errorf("scan: %w", err)
		}

		orders = append(orders, o)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows: %w", err)
	}

	return orders, nil
}

func (s *Storage) DeteleAllKeys() error {
	sql, args, err := s.sq.Delete("access_keys").ToSql()
	if err != nil {
		return fmt.Errorf("builder: %w", err)
	}

	if _, err := s.db.Exec(sql, args...); err != nil {
		return fmt.Errorf("exec: %w", err)
	}

	return nil
}
