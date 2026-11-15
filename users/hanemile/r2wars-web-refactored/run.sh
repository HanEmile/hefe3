set +e

# CGO_ENABLED=0 is required: r2pipe-go's native/CGO path links libr_core which
# isn't present; with cgo disabled it uses the r2 subprocess pipe instead.
# SALT is used for argon2 password hashing, SESSION_KEY for the session cookies.
CGO_ENABLED=0 \
	SESSION_KEY=${SESSION_KEY:-aes1Itheich4aeQu9Ouz7ahcaiVoogh9} \
	SALT=${SALT:-changeme-salt} \
	go run ./cmd/r2wars-web
