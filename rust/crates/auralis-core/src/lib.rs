//! Auralis core domain types, constants, and errors.

pub mod close_gate;
pub mod constants;
pub mod error;
pub mod models;

pub use close_gate::{CloseAction, CloseGate, CloseState};
pub use constants::*;
pub use error::{AuralisError, Result};
pub use models::*;
