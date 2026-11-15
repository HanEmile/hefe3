use anyhow::Result;
use async_trait::async_trait;
use russh::server::*;
use russh::{Channel, ChannelId};
use russh_keys::key::{KeyPair, PublicKey};
use russh_keys::PublicKeyBase64;
use std::net::SocketAddr;
use std::path::PathBuf;
use std::sync::{Arc, Mutex};
use tokio::sync::mpsc;

use crate::broadcast;
use crate::db::Db;

pub struct SshServer {
    db: Arc<Mutex<Db>>,
    broadcast_tx: broadcast::Sender,
    bind: SocketAddr,
    allowed_keys: Option<Arc<std::collections::HashSet<String>>>,
    presence: broadcast::Presence,
}

impl SshServer {
    pub fn new(db: Arc<Mutex<Db>>, broadcast_tx: broadcast::Sender) -> Self {
        Self {
            db,
            broadcast_tx,
            bind: "0.0.0.0:22".parse().unwrap(),
            allowed_keys: load_authorized_keys().map(Arc::new),
            presence: broadcast::new_presence(),
        }
    }

    pub async fn run(mut self, bind: &str) -> Result<()> {
        self.bind = bind.parse().unwrap_or_else(|_| "0.0.0.0:22".parse().unwrap());
        let config = russh::server::Config {
            keys: vec![load_or_generate_host_key()?],
            auth_rejection_time: std::time::Duration::from_secs(1),
            ..Default::default()
        };
        self.run_on_address(Arc::new(config), bind).await?;
        Ok(())
    }
}

/// Load the allowlist from $SSHUB_AUTHORIZED_KEYS, if set.
/// Returns None for "no restriction" (everyone may auth).
fn load_authorized_keys() -> Option<std::collections::HashSet<String>> {
    let path = std::env::var("SSHUB_AUTHORIZED_KEYS").ok()?;
    let content = match std::fs::read_to_string(&path) {
        Ok(c) => c,
        Err(e) => {
            eprintln!("sshub: failed to read SSHUB_AUTHORIZED_KEYS at {path}: {e}");
            // Fail closed: an unreadable file shouldn't silently mean "allow everyone".
            return Some(std::collections::HashSet::new());
        }
    };
    let mut set = std::collections::HashSet::new();
    for line in content.lines() {
        let line = line.trim();
        if line.is_empty() || line.starts_with('#') { continue; }
        // openssh authorized_keys lines may have leading options; the key is the
        // first token that starts with a known SSH key type, then a base64 blob.
        let mut parts = line.split_whitespace();
        let mut keytype: Option<&str> = None;
        let mut keyblob: Option<&str> = None;
        while let Some(tok) = parts.next() {
            if is_ssh_key_type(tok) {
                keytype = Some(tok);
                keyblob = parts.next();
                break;
            }
        }
        if let (Some(t), Some(k)) = (keytype, keyblob) {
            set.insert(format!("{} {}", t, k));
        }
    }
    Some(set)
}

fn is_ssh_key_type(s: &str) -> bool {
    matches!(s,
        "ssh-rsa" | "ssh-ed25519" | "ssh-dss"
        | "ecdsa-sha2-nistp256" | "ecdsa-sha2-nistp384" | "ecdsa-sha2-nistp521"
        | "sk-ssh-ed25519@openssh.com" | "sk-ecdsa-sha2-nistp256@openssh.com"
    )
}

impl Server for SshServer {
    type Handler = SessionHandler;

    fn new_client(&mut self, peer: Option<SocketAddr>) -> SessionHandler {
        SessionHandler {
            db: Arc::clone(&self.db),
            broadcast_tx: self.broadcast_tx.clone(),
            user_id: None,
            username: None,
            pubkey: None,
            pty_size: (80, 24),
            input_tx: None,
            peer,
            bind: self.bind,
            allowed_keys: self.allowed_keys.clone(),
            presence: Arc::clone(&self.presence),
        }
    }
}

pub struct SessionHandler {
    db: Arc<Mutex<Db>>,
    broadcast_tx: broadcast::Sender,
    user_id: Option<String>,
    username: Option<String>,
    pubkey: Option<String>,
    pty_size: (u16, u16),
    input_tx: Option<mpsc::UnboundedSender<SessionEvent>>,
    peer: Option<SocketAddr>,
    bind: SocketAddr,
    allowed_keys: Option<Arc<std::collections::HashSet<String>>>,
    presence: broadcast::Presence,
}

#[derive(Debug)]
pub enum SessionEvent {
    Input(Vec<u8>),
    Resize(u16, u16),
}

#[async_trait]
impl Handler for SessionHandler {
    type Error = anyhow::Error;

    async fn channel_open_session(
        &mut self,
        _channel: Channel<Msg>,
        _session: &mut Session,
    ) -> Result<bool, Self::Error> {
        Ok(true)
    }

    async fn auth_publickey(
        &mut self,
        _user: &str,
        public_key: &PublicKey,
    ) -> Result<Auth, Self::Error> {
        let key_line = format!("{} {}", public_key.name(), public_key.public_key_base64());

        // Enforce the authorized-keys allowlist, if one is configured.
        if let Some(allowed) = &self.allowed_keys {
            if !allowed.contains(&key_line) {
                return Ok(Auth::Reject {
                    proceed_with_methods: None,
                });
            }
        }

        self.pubkey = Some(key_line.clone());
        let db = self.db.lock().expect("db mutex poisoned");
        let user = match db.find_user_by_pubkey(&key_line)? {
            Some(u) => u,
            None => {
                let name = db.unique_guest_name()?;
                db.create_user(&name, &key_line)?
            }
        };
        self.user_id = Some(user.id);
        self.username = Some(user.name);
        Ok(Auth::Accept)
    }

    async fn pty_request(
        &mut self,
        _channel: ChannelId,
        _term: &str,
        col_width: u32,
        row_height: u32,
        _pix_width: u32,
        _pix_height: u32,
        _modes: &[(russh::Pty, u32)],
        session: &mut Session,
    ) -> Result<(), Self::Error> {
        self.pty_size = (col_width as u16, row_height as u16);
        session.request_success();
        Ok(())
    }

    async fn shell_request(
        &mut self,
        channel: ChannelId,
        session: &mut Session,
    ) -> Result<(), Self::Error> {
        let db = Arc::clone(&self.db);
        let user_id = self.user_id.clone();
        let username = self.username.clone().unwrap_or_else(|| "guest".into());
        let pubkey = self.pubkey.clone().unwrap_or_default();
        let handle = session.handle();
        let broadcast_tx = self.broadcast_tx.clone();
        let pty_size = self.pty_size;
        let presence = Arc::clone(&self.presence);

        let (tx, rx) = mpsc::unbounded_channel::<SessionEvent>();
        self.input_tx = Some(tx);

        tokio::spawn(async move {
            if let Err(e) = crate::tui_session::run_tui(
                handle, channel, db, user_id, username, pubkey, rx, broadcast_tx,
                pty_size, presence,
            ).await {
                eprintln!("TUI session error: {e}");
            }
        });

        session.request_success();
        Ok(())
    }

    async fn data(
        &mut self,
        _channel: ChannelId,
        data: &[u8],
        _session: &mut Session,
    ) -> Result<(), Self::Error> {
        if let Some(tx) = &self.input_tx {
            let _ = tx.send(SessionEvent::Input(data.to_vec()));
        }
        Ok(())
    }

    async fn window_change_request(
        &mut self,
        _channel: ChannelId,
        col_width: u32,
        row_height: u32,
        _pix_width: u32,
        _pix_height: u32,
        _session: &mut Session,
    ) -> Result<(), Self::Error> {
        self.pty_size = (col_width as u16, row_height as u16);
        if let Some(tx) = &self.input_tx {
            let _ = tx.send(SessionEvent::Resize(col_width as u16, row_height as u16));
        }
        Ok(())
    }

    async fn exec_request(
        &mut self,
        channel: ChannelId,
        data: &[u8],
        session: &mut Session,
    ) -> Result<(), Self::Error> {
        let cmd = String::from_utf8_lossy(data);
        // We only support invocation of the bundled mosh-server bootstrap.
        // Mosh's client sends something like:
        //   mosh-server new -s -c 256 -l LANG=en_US.UTF-8
        if !cmd.split_whitespace().next().map_or(false, |w| w == "mosh-server") {
            session.channel_failure(channel);
            return Ok(());
        }

        let pubkey = self.pubkey.clone().unwrap_or_default();
        let ssh_connection = self
            .peer
            .map(|p| format!("{} {} {} {}", p.ip(), p.port(), self.bind.ip(), self.bind.port()))
            .unwrap_or_default();

        // mosh-client expects to read `MOSH CONNECT <port> <key>` from stdout,
        // then mosh-server detaches into the background, holding the UDP socket.
        // Re-run sshub itself as the user's "shell" through mosh-server.
        let self_exe = std::env::current_exe()
            .unwrap_or_else(|_| PathBuf::from("sshub"));

        let mut command = tokio::process::Command::new("mosh-server");
        command
            .arg("new")
            .arg("-s")
            .arg("-c").arg("256")
            .arg("--")
            .arg(&self_exe)
            .env("SSHUB_PUBKEY", &pubkey)
            .env("SSH_CONNECTION", &ssh_connection)
            .env_remove("SSHUB_LISTEN")
            .stdin(std::process::Stdio::null())
            .stdout(std::process::Stdio::piped())
            .stderr(std::process::Stdio::piped());

        let mut child = match command.spawn() {
            Ok(c) => c,
            Err(e) => {
                let msg = format!("sshub: failed to spawn mosh-server: {e}\r\n");
                session.data(channel, russh::CryptoVec::from_slice(msg.as_bytes()));
                session.channel_failure(channel);
                return Ok(());
            }
        };

        session.channel_success(channel);

        // Forward mosh-server's stdout/stderr to the SSH channel; this is
        // small (just the MOSH CONNECT line) since mosh-server then detaches.
        let stdout = child.stdout.take();
        let stderr = child.stderr.take();
        let handle = session.handle();
        tokio::spawn(async move {
            use tokio::io::AsyncReadExt;
            let mut buf = [0u8; 1024];
            if let Some(mut out) = stdout {
                while let Ok(n) = out.read(&mut buf).await {
                    if n == 0 { break; }
                    let _ = handle.data(channel, russh::CryptoVec::from_slice(&buf[..n])).await;
                }
            }
            if let Some(mut err) = stderr {
                while let Ok(n) = err.read(&mut buf).await {
                    if n == 0 { break; }
                    let _ = handle.extended_data(channel, 1, russh::CryptoVec::from_slice(&buf[..n])).await;
                }
            }
            let status = child.wait().await.ok().and_then(|s| s.code()).unwrap_or(0);
            let _ = handle.exit_status_request(channel, status as u32).await;
            let _ = handle.eof(channel).await;
            let _ = handle.close(channel).await;
        });

        Ok(())
    }
}

fn host_key_path() -> PathBuf {
    dirs::data_local_dir()
        .unwrap_or_else(|| PathBuf::from("."))
        .join("sshub")
        .join("host_ed25519")
}

fn load_or_generate_host_key() -> Result<KeyPair> {
    let path = host_key_path();
    if path.exists() {
        return Ok(russh_keys::load_secret_key(&path, None)?);
    }
    if let Some(parent) = path.parent() {
        std::fs::create_dir_all(parent)?;
    }
    let key = KeyPair::generate_ed25519().ok_or_else(|| anyhow::anyhow!("ed25519 keygen failed"))?;
    let file = std::fs::File::create(&path)?;
    russh_keys::encode_pkcs8_pem(&key, file)?;
    Ok(key)
}
