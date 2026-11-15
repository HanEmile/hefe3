//! Wok - a collection of binary analysis tools
//!
//! * hex edit
//! * disassemble
//! * decompile
//! * analyze
//! * libvirt VM management
//! * symbolic execution

#![warn(clippy::all, rust_2018_idioms)]

mod app;
// mod backend_panel;
// mod frame_history;

pub use app::WokApp;

