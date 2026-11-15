CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o miki-linux-amd64
ssh root@git "rm /root/miki-linux-amd64"
scp miki-linux-amd64 root@git:/root/miki-linux-amd64
ssh root@git "/root/miki-linux-amd64"

