cargo build --release

# ── Whole-project scan, all tainted sinks ────────────────────────────────────
./target/release/taint \
  --source ~/src/github.com/openssl/openssl \
  --all-sinks \
  --exclude "test,fuzz,demos,doc,util" \
  --min-taint partial

# ── Trace to a specific known-dangerous function ─────────────────────────────
./target/release/taint \
  --source ~/src/github.com/openssl/openssl \
  --target SSL_read \
  --exclude "test,fuzz" \
  --min-taint full

# ── Trace to a specific variable inside a function ───────────────────────────
./target/release/taint \
  --source ~/src/github.com/openssl/openssl/ssl/ssl_lib.c \
  --target "SSL_read:buf"

# ── Full project dump to JSON for further processing ─────────────────────────
./target/release/taint \
  --source ~/src/github.com/openssl/openssl \
  --all-sinks \
  --exclude "test,fuzz,demos" \
  --min-taint full \
  --json \
  > openssl_full_taint.json 2>progress.log

# ── Use more threads (default = all CPUs) ────────────────────────────────────
./target/release/taint \
  --source ~/src/github.com/openssl/openssl \
  --all-sinks \
  --jobs 8 \
  --exclude "test,fuzz"
