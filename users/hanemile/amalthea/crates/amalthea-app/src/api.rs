/// Frontend API client for the amalthea backend.
///
/// Wraps HTTP calls to the backend's database REST API. Used by panels to
/// persist workspaces, observation plans, stacking sessions, and log actions.
/// On WASM, requests go to the same origin. On native, they go to the
/// configured backend URL.

use crate::config::AppConfig;

/// API client that talks to the amalthea backend.
#[derive(Clone)]
pub struct ApiClient {
    base_url: String,
    http: reqwest::Client,
}

impl ApiClient {
    /// Create a new API client from the app config.
    pub fn from_config(config: &AppConfig) -> Self {
        Self {
            base_url: config.backend_url.trim_end_matches('/').to_string(),
            http: reqwest::Client::new(),
        }
    }

    fn url(&self, path: &str) -> String {
        format!("{}/{}", self.base_url, path.trim_start_matches('/'))
    }

    // -- Actions ---------------------------------------------------------------

    /// Log an action to the backend.
    pub async fn log_action(
        &self,
        action_type: &str,
        entity_type: Option<&str>,
        entity_id: Option<&str>,
    ) -> Result<(), reqwest::Error> {
        let body = serde_json::json!({
            "action_type": action_type,
            "entity_type": entity_type,
            "entity_id": entity_id,
        });
        self.http.post(self.url("/db/actions"))
            .json(&body)
            .send()
            .await?;
        Ok(())
    }

    // -- Workspaces ------------------------------------------------------------

    /// List all workspaces from the backend.
    pub async fn list_workspaces(&self) -> Result<serde_json::Value, reqwest::Error> {
        let resp = self.http.get(self.url("/db/workspaces")).send().await?;
        resp.json().await
    }

    /// Save a workspace to the backend.
    pub async fn save_workspace(
        &self,
        id: Option<i64>,
        name: &str,
        layout_json: &str,
    ) -> Result<serde_json::Value, reqwest::Error> {
        let body = serde_json::json!({
            "name": name,
            "layout_json": layout_json,
        });
        let resp = if let Some(id) = id {
            self.http.put(self.url(&format!("/db/workspaces/{id}")))
                .json(&body)
                .send()
                .await?
        } else {
            self.http.post(self.url("/db/workspaces"))
                .json(&body)
                .send()
                .await?
        };
        resp.json().await
    }

    // -- Observation Plans -----------------------------------------------------

    /// Save an observation plan.
    pub async fn save_plan(&self, plan: &serde_json::Value) -> Result<serde_json::Value, reqwest::Error> {
        let resp = self.http.post(self.url("/db/plans"))
            .json(plan)
            .send()
            .await?;
        resp.json().await
    }

    /// List all plans.
    pub async fn list_plans(&self) -> Result<serde_json::Value, reqwest::Error> {
        let resp = self.http.get(self.url("/db/plans")).send().await?;
        resp.json().await
    }

    // -- Settings --------------------------------------------------------------

    /// Get a setting value.
    pub async fn get_setting(&self, key: &str) -> Result<Option<String>, reqwest::Error> {
        let resp = self.http.get(self.url(&format!("/db/settings/{key}"))).send().await?;
        if resp.status().is_success() {
            let val: serde_json::Value = resp.json().await?;
            Ok(val["value"].as_str().map(|s| s.to_string()))
        } else {
            Ok(None)
        }
    }

    /// Set a setting value.
    pub async fn set_setting(&self, key: &str, value: &str) -> Result<(), reqwest::Error> {
        self.http.put(self.url(&format!("/db/settings/{key}")))
            .json(&serde_json::json!({ "value": value }))
            .send()
            .await?;
        Ok(())
    }

    // -- Health ----------------------------------------------------------------

    /// Check if the backend is reachable.
    pub async fn health_check(&self) -> bool {
        matches!(
            self.http.get(self.url("/health")).send().await,
            Ok(resp) if resp.status().is_success()
        )
    }
}
