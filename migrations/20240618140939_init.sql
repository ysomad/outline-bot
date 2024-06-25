-- +goose Up
-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS orders (
    id integer PRIMARY KEY AUTOINCREMENT NOT NULL,

    uid bigint NOT NULL,
    username varchar(32),
    first_name varchar(64),
    last_name varchar(64),

    key_amount smallint NOT NULL,
    price int NOT NULL,

    status varchar(32) NOT NULL,

    closed_at timestamp,
    created_at timestamp NOT NULL
);

CREATE TABLE IF NOT EXISTS access_keys (
    id varchar(64) PRIMARY KEY NOT NULL,
    name varchar(32) NOT NULL,
    order_id int REFERENCES orders (id) NOT NULL,
    url text NOT NULL,
    expires_at timestamp NOT NULL
);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS access_keys;
DROP TABLE IF EXISTS orders;
-- +goose StatementEnd
