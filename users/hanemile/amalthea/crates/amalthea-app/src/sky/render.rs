//! wgpu-based renderer for the sky viewer (placeholder module).
//!
//! Planned contents — none of this is implemented yet, the sky viewer
//! currently uses a 2-D equirectangular fallback in
//! `panels/sky_viewer.rs`:
//!
//! - Earth sphere mesh with day/night texture.
//! - Star points from a catalogue blob.
//! - RA/Dec + Alt/Az grid lines.
//! - Telescope FOV rectangle overlay.
//! - Camera controls (orbit, zoom).
//! - Time slider for simulating sky rotation.
//!
//! Integration: `egui::PaintCallback` + `eframe::egui_wgpu::Renderer`.
//!
//! Last reviewed: 2026-05-01 | Review frequency: monthly
