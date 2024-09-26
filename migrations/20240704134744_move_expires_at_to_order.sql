-- +goose Up
-- +goose StatementBegin
ALTER TABLE orders
    ADD COLUMN expires_at timestamp;

UPDATE
    orders
SET
    expires_at = (
        SELECT
            ak.expires_at
        FROM
            access_keys ak
        WHERE
            ak.order_id = orders.id
        GROUP BY
            ak.order_id);

CREATE TABLE access_keys_new (
    id varchar(64) PRIMARY KEY NOT NULL,
    name varchar(32) NOT NULL,
    order_id int NOT NULL,
    url text NOT NULL,
    FOREIGN KEY (order_id) REFERENCES orders (id) ON DELETE RESTRICT
);

INSERT INTO access_keys_new (id, name, order_id, url)
SELECT
    id,
    name,
    order_id,
    url
FROM
    access_keys;

DROP TABLE access_keys;

ALTER TABLE access_keys_new RENAME TO access_keys;

-- +goose StatementEnd
