/// SQLite database layer — central persistence for amalthea.
///
/// Manages an SQLite database with tables for actions, workspaces,
/// observation plans, stacking sessions, and key-value settings.

use rusqlite::{Connection, params};
use std::sync::Mutex;
use std::path::Path;

/// Thread-safe database handle.
pub struct Database {
    conn: Mutex<Connection>,
}

impl Database {
    /// Open or create the database at the given path and run migrations.
    pub fn open(path: &Path) -> Result<Self, rusqlite::Error> {
        let conn = Connection::open(path)?;
        conn.execute_batch("PRAGMA journal_mode=WAL; PRAGMA foreign_keys=ON;")?;
        let db = Self { conn: Mutex::new(conn) };
        db.migrate()?;
        Ok(db)
    }

    /// Open an in-memory database (for testing).
    pub fn open_memory() -> Result<Self, rusqlite::Error> {
        let conn = Connection::open_in_memory()?;
        let db = Self { conn: Mutex::new(conn) };
        db.migrate()?;
        Ok(db)
    }

    fn migrate(&self) -> Result<(), rusqlite::Error> {
        let conn = self.conn.lock().unwrap();
        conn.execute_batch(
            "CREATE TABLE IF NOT EXISTS actions (
                id INTEGER PRIMARY KEY AUTOINCREMENT,
                timestamp TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
                action_type TEXT NOT NULL,
                entity_type TEXT,
                entity_id TEXT,
                payload TEXT,
                user_id TEXT
            );

            CREATE TABLE IF NOT EXISTS workspaces (
                id INTEGER PRIMARY KEY AUTOINCREMENT,
                name TEXT NOT NULL,
                layout_json TEXT NOT NULL,
                created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
                updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
            );

            CREATE TABLE IF NOT EXISTS observation_plans (
                id INTEGER PRIMARY KEY AUTOINCREMENT,
                name TEXT NOT NULL,
                location_lat REAL,
                location_lon REAL,
                location_name TEXT,
                date_from TEXT,
                date_to TEXT,
                target_name TEXT,
                target_ra REAL,
                target_dec REAL,
                equipment_json TEXT,
                notes TEXT,
                created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
                updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
            );

            CREATE TABLE IF NOT EXISTS stacking_sessions (
                id INTEGER PRIMARY KEY AUTOINCREMENT,
                name TEXT NOT NULL,
                target_asset_id TEXT,
                frame_ids TEXT,
                calibration_json TEXT,
                result_path TEXT,
                status TEXT NOT NULL DEFAULT 'pending',
                created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
                updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
            );

            CREATE TABLE IF NOT EXISTS settings (
                key TEXT PRIMARY KEY,
                value TEXT NOT NULL
            );

            CREATE INDEX IF NOT EXISTS idx_actions_timestamp ON actions(timestamp);
            CREATE INDEX IF NOT EXISTS idx_actions_type ON actions(action_type);",
        )?;
        Ok(())
    }

    // -- Actions ---------------------------------------------------------------

    /// Log an action.
    pub fn log_action(
        &self,
        action_type: &str,
        entity_type: Option<&str>,
        entity_id: Option<&str>,
        payload: Option<&str>,
        user_id: Option<&str>,
    ) -> Result<i64, rusqlite::Error> {
        let conn = self.conn.lock().unwrap();
        conn.execute(
            "INSERT INTO actions (action_type, entity_type, entity_id, payload, user_id)
             VALUES (?1, ?2, ?3, ?4, ?5)",
            params![action_type, entity_type, entity_id, payload, user_id],
        )?;
        Ok(conn.last_insert_rowid())
    }

    /// Query actions with optional filters.
    pub fn query_actions(
        &self,
        action_type: Option<&str>,
        from: Option<&str>,
        to: Option<&str>,
        limit: u32,
    ) -> Result<Vec<serde_json::Value>, rusqlite::Error> {
        let conn = self.conn.lock().unwrap();
        let mut sql = "SELECT id, timestamp, action_type, entity_type, entity_id, payload, user_id FROM actions WHERE 1=1".to_string();
        let mut param_values: Vec<Box<dyn rusqlite::types::ToSql>> = Vec::new();

        if let Some(t) = action_type {
            sql.push_str(" AND action_type = ?");
            param_values.push(Box::new(t.to_string()));
        }
        if let Some(f) = from {
            sql.push_str(" AND timestamp >= ?");
            param_values.push(Box::new(f.to_string()));
        }
        if let Some(t) = to {
            sql.push_str(" AND timestamp <= ?");
            param_values.push(Box::new(t.to_string()));
        }
        sql.push_str(" ORDER BY timestamp DESC LIMIT ?");
        param_values.push(Box::new(limit));

        let params_ref: Vec<&dyn rusqlite::types::ToSql> = param_values.iter().map(|p| p.as_ref()).collect();
        let mut stmt = conn.prepare(&sql)?;
        let rows = stmt.query_map(params_ref.as_slice(), |row| {
            Ok(serde_json::json!({
                "id": row.get::<_, i64>(0)?,
                "timestamp": row.get::<_, String>(1)?,
                "action_type": row.get::<_, String>(2)?,
                "entity_type": row.get::<_, Option<String>>(3)?,
                "entity_id": row.get::<_, Option<String>>(4)?,
                "payload": row.get::<_, Option<String>>(5)?,
                "user_id": row.get::<_, Option<String>>(6)?,
            }))
        })?;
        rows.collect()
    }

    // -- Workspaces ------------------------------------------------------------

    pub fn list_workspaces(&self) -> Result<Vec<serde_json::Value>, rusqlite::Error> {
        let conn = self.conn.lock().unwrap();
        let mut stmt = conn.prepare(
            "SELECT id, name, layout_json, created_at, updated_at FROM workspaces ORDER BY id"
        )?;
        let rows = stmt.query_map([], |row| {
            Ok(serde_json::json!({
                "id": row.get::<_, i64>(0)?,
                "name": row.get::<_, String>(1)?,
                "layout_json": row.get::<_, String>(2)?,
                "created_at": row.get::<_, String>(3)?,
                "updated_at": row.get::<_, String>(4)?,
            }))
        })?;
        rows.collect()
    }

    pub fn upsert_workspace(&self, id: Option<i64>, name: &str, layout_json: &str) -> Result<i64, rusqlite::Error> {
        let conn = self.conn.lock().unwrap();
        if let Some(id) = id {
            conn.execute(
                "UPDATE workspaces SET name = ?1, layout_json = ?2, updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now') WHERE id = ?3",
                params![name, layout_json, id],
            )?;
            Ok(id)
        } else {
            conn.execute(
                "INSERT INTO workspaces (name, layout_json) VALUES (?1, ?2)",
                params![name, layout_json],
            )?;
            Ok(conn.last_insert_rowid())
        }
    }

    pub fn delete_workspace(&self, id: i64) -> Result<(), rusqlite::Error> {
        let conn = self.conn.lock().unwrap();
        conn.execute("DELETE FROM workspaces WHERE id = ?1", params![id])?;
        Ok(())
    }

    // -- Observation Plans -----------------------------------------------------

    pub fn list_plans(&self) -> Result<Vec<serde_json::Value>, rusqlite::Error> {
        let conn = self.conn.lock().unwrap();
        let mut stmt = conn.prepare(
            "SELECT id, name, location_lat, location_lon, location_name, date_from, date_to,
                    target_name, target_ra, target_dec, equipment_json, notes, created_at, updated_at
             FROM observation_plans ORDER BY created_at DESC"
        )?;
        let rows = stmt.query_map([], |row| {
            Ok(serde_json::json!({
                "id": row.get::<_, i64>(0)?,
                "name": row.get::<_, String>(1)?,
                "location_lat": row.get::<_, Option<f64>>(2)?,
                "location_lon": row.get::<_, Option<f64>>(3)?,
                "location_name": row.get::<_, Option<String>>(4)?,
                "date_from": row.get::<_, Option<String>>(5)?,
                "date_to": row.get::<_, Option<String>>(6)?,
                "target_name": row.get::<_, Option<String>>(7)?,
                "target_ra": row.get::<_, Option<f64>>(8)?,
                "target_dec": row.get::<_, Option<f64>>(9)?,
                "equipment_json": row.get::<_, Option<String>>(10)?,
                "notes": row.get::<_, Option<String>>(11)?,
                "created_at": row.get::<_, String>(12)?,
                "updated_at": row.get::<_, String>(13)?,
            }))
        })?;
        rows.collect()
    }

    pub fn upsert_plan(&self, id: Option<i64>, data: &serde_json::Value) -> Result<i64, rusqlite::Error> {
        let conn = self.conn.lock().unwrap();
        if let Some(id) = id {
            conn.execute(
                "UPDATE observation_plans SET name=?1, location_lat=?2, location_lon=?3, location_name=?4,
                 date_from=?5, date_to=?6, target_name=?7, target_ra=?8, target_dec=?9,
                 equipment_json=?10, notes=?11, updated_at=strftime('%Y-%m-%dT%H:%M:%fZ','now') WHERE id=?12",
                params![
                    data["name"].as_str().unwrap_or(""),
                    data["location_lat"].as_f64(),
                    data["location_lon"].as_f64(),
                    data["location_name"].as_str(),
                    data["date_from"].as_str(),
                    data["date_to"].as_str(),
                    data["target_name"].as_str(),
                    data["target_ra"].as_f64(),
                    data["target_dec"].as_f64(),
                    data["equipment_json"].as_str(),
                    data["notes"].as_str(),
                    id,
                ],
            )?;
            Ok(id)
        } else {
            conn.execute(
                "INSERT INTO observation_plans (name, location_lat, location_lon, location_name,
                 date_from, date_to, target_name, target_ra, target_dec, equipment_json, notes)
                 VALUES (?1,?2,?3,?4,?5,?6,?7,?8,?9,?10,?11)",
                params![
                    data["name"].as_str().unwrap_or("Untitled"),
                    data["location_lat"].as_f64(),
                    data["location_lon"].as_f64(),
                    data["location_name"].as_str(),
                    data["date_from"].as_str(),
                    data["date_to"].as_str(),
                    data["target_name"].as_str(),
                    data["target_ra"].as_f64(),
                    data["target_dec"].as_f64(),
                    data["equipment_json"].as_str(),
                    data["notes"].as_str(),
                ],
            )?;
            Ok(conn.last_insert_rowid())
        }
    }

    pub fn delete_plan(&self, id: i64) -> Result<(), rusqlite::Error> {
        let conn = self.conn.lock().unwrap();
        conn.execute("DELETE FROM observation_plans WHERE id = ?1", params![id])?;
        Ok(())
    }

    // -- Settings --------------------------------------------------------------

    pub fn get_setting(&self, key: &str) -> Result<Option<String>, rusqlite::Error> {
        let conn = self.conn.lock().unwrap();
        let mut stmt = conn.prepare("SELECT value FROM settings WHERE key = ?1")?;
        let mut rows = stmt.query_map(params![key], |row| row.get(0))?;
        match rows.next() {
            Some(Ok(val)) => Ok(Some(val)),
            _ => Ok(None),
        }
    }

    pub fn set_setting(&self, key: &str, value: &str) -> Result<(), rusqlite::Error> {
        let conn = self.conn.lock().unwrap();
        conn.execute(
            "INSERT INTO settings (key, value) VALUES (?1, ?2)
             ON CONFLICT(key) DO UPDATE SET value = excluded.value",
            params![key, value],
        )?;
        Ok(())
    }

    // -- Export -----------------------------------------------------------------

    /// Export a table as a Vec of JSON objects.
    pub fn export_table(&self, table: &str) -> Result<Vec<serde_json::Value>, String> {
        // Whitelist allowed table names to prevent SQL injection.
        let allowed = ["actions", "workspaces", "observation_plans", "stacking_sessions", "settings"];
        if !allowed.contains(&table) {
            return Err(format!("unknown table: {table}"));
        }
        let conn = self.conn.lock().unwrap();
        let mut stmt = conn.prepare(&format!("SELECT * FROM {table}"))
            .map_err(|e| e.to_string())?;
        let col_count = stmt.column_count();
        let col_names: Vec<String> = (0..col_count).map(|i| stmt.column_name(i).unwrap().to_string()).collect();

        let rows = stmt.query_map([], |row| {
            let mut obj = serde_json::Map::new();
            for (i, name) in col_names.iter().enumerate() {
                let val: rusqlite::types::Value = row.get(i)?;
                let json_val = match val {
                    rusqlite::types::Value::Null => serde_json::Value::Null,
                    rusqlite::types::Value::Integer(n) => serde_json::json!(n),
                    rusqlite::types::Value::Real(f) => serde_json::json!(f),
                    rusqlite::types::Value::Text(s) => serde_json::json!(s),
                    rusqlite::types::Value::Blob(b) => serde_json::json!(format!("<blob {} bytes>", b.len())),
                };
                obj.insert(name.clone(), json_val);
            }
            Ok(serde_json::Value::Object(obj))
        }).map_err(|e| e.to_string())?;

        rows.collect::<Result<Vec<_>, _>>().map_err(|e| e.to_string())
    }
}
