//! The record the policy service keeps for an organization (ADR-0148):
//! every write of a policy, in the transaction that makes it, and
//! every denial, collapsed and capped, for the organization's owners
//! and administrators to read. No record holds a policy's text, a
//! request's context, a token or the text of a fault: the table has no
//! column for them.

mod denial;
mod error;
mod read;
mod write;

pub use denial::{
    Denial, FAULT, MAX_FIELD_BYTES, MAX_LIVE_DENIALS, RECORD_BOUND, Recorded, Recorder,
    organization_of,
};
pub use error::Error;
pub use read::{Kind, Page, RETENTION_DAYS, Record};
pub use write::{Origin, Outcome, Write, Written, record_write};
