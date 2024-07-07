#!/bin/bash

if [ -z "$OUTLINE_HOST" ]; then
    echo "Error: OUTLINE_HOST environment variable is not set."
    exit 1
fi

SERVICE_NAME=outline-bot
USER=root

CGO_ENABLED=1 GOOS=linux GOARCH=amd64 CC=x86_64-linux-musl-gcc go build -o $SERVICE_NAME

ssh $USER@$OUTLINE_HOST "sudo systemctl stop $SERVICE_NAME"

scp -r $SERVICE_NAME .env assets $USER@$OUTLINE_HOST:~/
scp build/outline-bot.service $USER@$OUTLINE_HOST:/etc/systemd/system

LOG_FILE=/var/log/outline-bot.log
LOG_FILE_ERROR=/var/log/outline-bot-error.log

COMMANDS="
test -e $LOG_FILE || touch $LOG_FILE
test -e $LOG_FILE_ERROR || touch $LOG_FILE_ERROR
sudo file $SERVICE_NAME
sudo chmod +x $SERVICE_NAME
sudo systemctl daemon-reload
sudo systemctl start $SERVICE_NAME
sudo systemctl enable $SERVICE_NAME
sudo systemctl status $SERVICE_NAME
"

ssh $USER@$OUTLINE_HOST "$COMMANDS"

