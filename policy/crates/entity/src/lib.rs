//! Holds the containment and membership decisions read: what each
//! owner writes, and the slice one decision reads back (ADR-0130 §7,
//! ADR-0138).

mod change;
mod error;
mod postgres;

pub use change::EntityChange;
pub use error::Error;
pub use postgres::{Applied, EntityStore};
